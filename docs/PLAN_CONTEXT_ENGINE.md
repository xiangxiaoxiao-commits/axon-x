# axon 上下文引擎方案：从「干瘪实体」到「有血有肉的上下文」

> 承接 `docs/RESEARCH_OBSIDIAN_GAP.md` 的诊断。目标：把 axon 从「只喂 LLM 二手蒸馏实体」升级为「原文 chunk（内容）+ 实体图谱（结构）双通道注入」的真正 HybridRAG，让 AI 第一次能读到项目的细节、代码、当时的权衡与理由。
>
> 只出方案，不写代码。所有改动点精确到现有文件与结构。

---

## 0. 一句话方案

在现有「实体图谱」通道旁，**并行加一条「原文 chunk 通道」**：
- 索引时：把对话/代码/任务的**原文切块 + embed**，和实体一起存进 `SessionCache`；
- 召回时：`MatchKnowledge` 用 **RRF 融合**（向量召回 chunk + 实体种子 + 图关系扩展 + 关键词兜底），产出「结构（实体/关系）+ 内容（原文片段，带来源）」两段式注入块；
- embedding：批量化、修掉静默降级（老实告警 + 可选内置本地模型），保证语义召回真生效；
- 迭代：chunk 与实体图谱**并行随对话/代码/任务回写持续长大**。

改动集中在 6 个文件：`internal/graph/cache.go`、`internal/graph/graph.go`（可选）、`graphbuild.go`、`codegraphbuild.go`、`writeback.go`、`internal/embed/`（批量接口 + 可选本地实现），外加 `memory.go` 降级信号。

---

## 1. 给 AI 到底喂什么：双通道构成

诊断已经定性：**只喂实体等于把 GraphRAG 砍掉了它必须搭配的原文 chunk 那半**。方案确定注入内容由两条正交通道组成，各司其职、互补而非替代（[GraphRAG vs RAG：各擅长场景，非替代](https://www.cognee.ai/blog/deep-dives/graphrag-vs-rag)）。

### 通道一：原文 chunk（内容 / "有血有肉"）

- **是什么**：对话轮次、代码片段、任务产出的**原始文字**，切成块、embed、按语义召回后**原样注入**。
- **解决什么**：AI 能看到「当时到底怎么讨论的、代码长什么样、为什么这么定」——推理链、具体实现、权衡理由。这正是 Obsidian（Smart Connections / Copilot Vault QA）有效的核心机制：检索并注入的是**真实文字块**，不是摘要。
- **为什么不能省**：「若检索破坏了原始文档结构、只留摘要，模型只能基于不完整上下文去合成」——这是 axon 现状的病根。

### 通道二：实体 / 关系图谱（结构 / "全局骨架"）

- **是什么**：现有的 `Entity`（模块/服务/概念/决策）+ `Relation`（依赖/调用/属于）+ observations。**保留，不动主体**。
- **解决什么**：chunk 缺的全局视角——「哪个模块依赖哪个」「有过什么决策」「这个概念关联到哪些东西」。多跳、跨会话、溯源，是单纯向量 chunk 给不出的。
- **定位调整**：从「唯一注入源」降级为「结构骨架 + chunk 的召回种子/导航」。它不再需要把「内容」都塞进 observations（那是 chunk 的活），可以更聚焦在「关系与决策标题」。

### 两者怎么组合注入（关键）

一次注入 = **先结构、后内容**，让模型先建立骨架认知再读细节：

```
以下是该项目的相关背景知识（来自你以往的对话/代码/任务，供参考）：

## 结构（实体与关系）
【支付服务 (service)】
- 关键决策：创建接口用 bizOrderId 做幂等键
关系：
- 支付服务 —依赖→ 订单服务
- 支付服务 —调用→ 风控网关

## 相关原文片段（细节与理由，最权威，优先据此判断）
〔来源：2024-11 支付重构讨论〕
user: 幂等键到底用订单号还是单独生成？
assistant: 用 bizOrderId。原因是回调可能重复投递，而 bizOrderId 在下单时已唯一…（原文）

〔来源：代码 payment/handler.go〕
func CreatePayment(...) { // 校验 bizOrderId 幂等 … }（原文片段）
```

排版原则（对模型最有效）：
1. **结构在前**（token 少、密度高，先给 AI 一张地图）；
2. **原文在后并明确标注「最权威，优先据此判断」**——告诉模型细节冲突时以原文为准，避免它只信摘要；
3. **每块 chunk 带来源标签**（会话标题 / `代码 <path>` / `任务 <id>`），既是溯源也让模型判断可信度；
4. chunk 之间用分隔线，避免模型把两段无关原文误当连续上下文。

---

## 2. 数据结构改动

### 2.1 新增 `Chunk`（放 `internal/graph/graph.go` 或 cache.go）

```go
// Chunk 是一段召回单元的原文 + 向量。原文通道的核心存储。
type Chunk struct {
    ID        string    `json:"id"`              // "<sessionID>#<seq>" / "code:<path>#<seq>" / "task:<id>#<seq>"
    Text      string    `json:"text"`            // 原文片段（对话轮次/代码/任务产出，原样）
    Source    string    `json:"source"`          // 溯源标记，复用现有约定：sessionID / "code:<path>" / "task:<id>"
    Kind      string    `json:"kind,omitempty"`  // "chat" | "code" | "task"，便于分通道调权/展示
    Embedding []float32 `json:"embedding,omitempty"`
}
```

`Source` 直接复用现有 `resolveSessionTitles` 的三种前缀约定（裸 sessionID / `code:` / `task:`），注入时的来源渲染零成本复用。

### 2.2 `SessionCache` 增字段 + 版本号（`internal/graph/cache.go`）

```go
type SessionCache struct {
    SessionID string     `json:"sessionId"`
    Mtime     int64      `json:"mtime"`
    Schema    int        `json:"schema"`            // 新增：缓存结构版本，用于强制回填 chunk
    Entities  []Entity   `json:"entities"`
    Relations []Relation `json:"relations"`
    Chunks    []Chunk    `json:"chunks,omitempty"`  // 新增：本会话的原文块
}
```

**为什么要 `Schema`**：现有 `LoadCache` 用 `Mtime` 判新鲜度，老缓存 mtime 没变会被判定「fresh」而跳过——加了 `Chunks` 也不会回填。加一个 `const cacheSchema = 2`，`LoadCache` 里 `c.Schema != cacheSchema` 也视作 stale，触发重建。这样已有用户升级后会自动补 chunk（见 §7 回填成本，只花 embedding 调用、不花 LLM）。

### 2.3 chunk 索引结构（召回期，`graphbuild.go` 内存态）

`MatchKnowledge` 现在只 `assembleGraph` 出 `*graph.Graph`。需要并行拿到全项目 chunk。两种做法，推荐 A：

- **A（推荐，改动小）**：`assembleGraph` / 一个新的 `loadChunks(projectSlug)` 从 `LoadAllCache` 里把每个 cache 的 `Chunks` 汇总成 `[]graph.Chunk` 返回。chunk 不需要跨会话去重合并（不像实体有别名归一），直接拼接即可。
- B（大项目再上）：单独的 chunk 向量文件 / sqlite-vec。PRD F4.3 原本就点名 sqlite-vec，但 MVP 阶段 A 足够，先不引依赖。

---

## 3. 切块策略（具体数值）

业界共识：**问答式检索的最佳块大小普遍在 256–512 token，重叠 10–20%**（[firecrawl 2026](https://www.firecrawl.dev/blog/best-chunking-strategies-rag)、[premai 2026 benchmark](https://www.premai.io/blog/rag-chunking-strategies-the-2026-benchmark-guide/)：Azure 建议 512 token + 25% overlap 起步；[NVIDIA](https://developer.nvidia.com/blog/finding-the-best-chunking-strategy-for-accurate-ai-responses/)：factoid 类小块 256–512 更优）。中文 1 token ≈ 1～1.5 字，故落到字符上：

| 数据源 | 切块单位 | 目标大小 | 重叠 | 说明 |
|---|---|---|---|---|
| **对话** | 按「轮次」聚合到目标大小 | ~450 token ≈ **600 中文字符** | **1 轮 或 ~100 字** | 绝不从句子中间切；user+紧邻 assistant 尽量同块（问答成对，语义完整） |
| **代码** | 按**函数/方法**边界 | 一函数一块，超 `maxCodeChars/…` 再按 ~500 token 二次切 | 0（函数天然边界） | 复用 `codegraph.BuildSkeleton` 已解析的符号边界，别用字符盲切 |
| **任务** | 按 spec 字段 + 结果段落 | ~450 token/块 | ~100 字 | `buildTaskTranscript` 已有结构，顺其自然切 |

补充原则：
- **保留发言人前缀**（`user:` / `assistant:`），模型靠它判断这是谁说的；`buildTranscript` 已经在写前缀，切块时保留。
- **块内不跨会话**：一个 chunk 只来自一个 session/文件/任务，`Source` 才干净。
- **过短块丢弃**：< ~80 字符的残块（寒暄、单行）直接扔，别污染向量空间。
- **overlap 的态度**：给 1 轮/~100 字的小重叠即可。有 2026 年 1 月的系统性分析指出 overlap 收益不明显（[digitalapplied 2026](https://www.digitalapplied.com/blog/rag-chunking-strategies-2026-retrieval-quality-playbook)），所以不必执着大重叠，按轮次切本身就保住了语义边界。

切块函数放哪：
- 对话：`graphbuild.go` 新增 `chunkTranscript(msgs []claudedata.SessionMessage) []graph.Chunk`，在 `IndexProject` 里与 `extractFromText` 并列调用（注意：切块用**完整 msgs**，不受 `maxTranscriptChars` 的 16000 字上限约束——那个上限是给 LLM 蒸馏省 token 的，chunk 通道应覆盖整场会话）。
- 代码：`codegraphbuild.go` 新增 `chunkCodeFile(rel, content) []graph.Chunk`，在 `BuildGraphFromCode` 遍历文件时产出。
- 任务：`writeback.go` 新增 `chunkTaskTranscript(...)`，在 `writeBackTaskKnowledge` 里产出。

---

## 4. 召回怎么做准：HybridRAG 升级

现状 `MatchKnowledge` 的三步（语义种子实体 → 关系扩展 → 关键词兜底）**保留**，在其上叠加 chunk 召回，用 **RRF（Reciprocal Rank Fusion）** 融合排序。

### 4.1 为什么用 RRF 而不是加权分数

不同通道的分数不可比（cosine 相似度 vs substring 命中 vs 图跳数），直接加权平均在生产里会失灵。RRF 只看**排名**不看分数，天然解决量纲不一致问题，且业界一致用 `k=60` 作平滑常数、被证明近最优且不敏感（[Azure AI Search](https://docs.microsoft.com/en-us/azure/search/hybrid-search-ranking)、[LanceDB](https://docs.lancedb.com/reranking/rrf)、[digitalapplied hybrid 2026](https://www.digitalapplied.com/blog/hybrid-search-bm25-vector-reranking-reference-2026)）。

公式：对每个候选 `d`，`RRF(d) = Σ_list 1/(60 + rank_list(d))`，`rank` 从 1 起。

### 4.2 召回流程（新）

```
query（见 4.3）
  │
  ├─(A) chunk 向量召回：query 向量 vs 所有 chunk 向量，cosine 降序 → chunkRankList（取前 ~30）
  ├─(B) 实体语义种子：现有 semanticSeeds（cosine ≥ 阈值，top-K）→ entitySeedRankList
  │        └─ 命中实体沿 relation 扩展 1~2 跳（现有 expandAlongRelations）
  ├─(C) 关键词兜底：现有 substring（实体名/别名出现在 query）→ keywordRankList
  │        └─（可选增强）chunk 里的关键词命中也进这条 list
  │
  └─ RRF 融合：
       · 实体侧：A? 不参与实体榜；B、C 融合出「注入哪些实体」
       · chunk 侧：A 为主榜；C 的 chunk 命中融合进来
       · 两侧各自取头部，进入 token 预算裁剪（§4.4）
```

去重：
- **chunk 去重**：按 `Chunk.ID` 去重；文本近重复（overlap 造成的相邻块）可按 `Source` + 文本前缀简单压制，避免注入两段几乎一样的话。
- **实体去重**：现有 `hit` map 已按 normalized name 去重，沿用。
- **chunk↔实体不互相挤占预算**：两条通道各有独立 token 预算（见下），因为它们注入到不同段落。

### 4.3 query 用什么

按调用场景分（现有已有雏形，统一强化）：
- **commit**：`buildKnowledgeQuery(diff)` 已经很好——改动文件路径 + 目录 + 基名 + hunk 符号名。**保留**，chunk 召回也用它。
- **enrich（任务）**：`enrichQuery(t)` = 任务输入 + scope 文件/模块。**保留**。
- **通用**：query 应偏「信息密集的符号/路径/模块名」而非整段自然语言，因为 chunk 向量建在原文上，符号名命中率更高。

### 4.4 top-K 与 token 预算（具体数值）

目标：**注入总预算 ≈ 2500～3500 token**，分配：

| 段落 | 预算 | 条数 | 说明 |
|---|---|---|---|
| 结构（实体+关系） | ~800 token | 实体 top **6~8**，关系全部（在命中集内） | 现有 `knowledgeSeedTopK=5` 可升到 6~8 |
| 原文 chunk | ~2000 token | chunk top **4~6** | 召回候选 ~30，RRF 后取头部，逐块累加到预算上限即停 |

参数落地（`graphbuild.go` 顶部常量区）：
```go
const (
    chunkRecallCandidates = 30   // 向量召回的 chunk 候选数（进 RRF 前）
    chunkInjectTopN       = 5    // 最终注入的 chunk 数
    chunkMinScore         = 0.30 // chunk cosine 下限（略低于实体，chunk 原文更长、相似度天然低一点）
    injectTokenBudget     = 3200 // 注入总 token 预算（约数，用字符/3 估算）
    knowledgeSeedTopK     = 8    // 实体种子上调 5 -> 8
    knowledgeSeedMinScore = 0.30 // 从 0.35 下调（中文 + 二手摘要相似度普遍偏低，诊断已指出偏高）
    knowledgeExpandHops   = 1    // 保持 1；人工确认关系可放宽（见诊断改进 4，本方案不含）
)
```
token 估算用简单 `len([]rune(text))/2`（中文近似）或字符数/3，不必精确——预算是软上限，逐块累加超了就停。

### 4.5 Method 信号扩展

现有 `RecallSemantic/Keyword/Hybrid/None` 只描述实体侧。chunk 上线后，`KnowledgeMatch` 建议加 `ChunkHits int` 字段，`Method` 语义微调：只要 chunk 向量召回生效就算 `semantic`（真语义），全程无向量才是 `keyword`（降级）。这直接喂给 §6 的降级告警。

---

## 5. embedding 落地：让语义召回真生效

诊断把「静默降级」列为**放大器**（不是根因，但让一切雪上加霜）。方案三管齐下。

### 5.1 批量 embed（必须，性价比最高）

现有 `embed.Embedder.Embed` 一次一条。切块后向量数 ×10 以上，逐条 HTTP 会慢到不可用。OpenAI embeddings API 的 `input` 支持**数组批量**。

- `internal/embed/embed.go`：接口加 `EmbedBatch(ctx, texts []string) ([][]float32, error)`。
- `internal/embed/openai.go`:`openaiEmbedRequest.Input` 改成 `[]string`（或用 `any` 兼容），一次发一批（建议每批 **64~128 条**，含退避重试）。
- `graphbuild.go`:新增 `embedChunks(emb, chunks)` 用批量接口；`embedEntities` 也可顺带改批量。

### 5.2 修掉静默降级：老实告警

现状 `newEmbedder` 失败 → `MatchKnowledge` 静默走 substring，中文场景基本召回不到，用户却不知在用阉割版。

- `KnowledgeMatch.Method == "keyword"` 字段**已经算好了**（`graphbuild.go:574`），前端在 enrich/commit 结果处**明确告警**：「当前为降级关键词召回，建议在设置里配置 embedding provider」。零后端改动，纯 UI。
- `IndexProject`/`BuildGraphFromCode` 在 `emb == nil` 时，通过 `EventGraphProgress` emit 一条 warning，让用户建索引时就知道「这次没生成向量」。

### 5.3 是否内置本地 embedding（权衡）

- **收益**：对标 Smart Connections 的「零配置、无 API key」开箱即用体验（[why local embeddings](https://smartconnections.app/smart-connections/why-local-embeddings/)），中文用户尤其受益（很多人没有 OpenAI 协议 embedding provider）。
- **成本**：打包一个 ONNX runtime + 小模型（`bge-small-zh` / `gte-small`，量化后约 30~100MB），或起本地进程。增大安装包、增加跨平台构建复杂度。
- **建议**：**分两步**。先做 5.1+5.2（低成本、当下就能让配了 provider 的人真正受益，没配的人至少知道自己在降级）；本地 embedding 作为独立里程碑，等验证 chunk 通道确实提效后再投入。接口层 `embed.Embedder` 已经抽象好，加一个 `LocalEmbedder` 实现即可，不影响其它代码。

### 5.4 模型一致性

chunk 向量和 query 向量必须**同一个 embedding 模型**。`embed.Embedder.Model()` 已存在。建议在 `Chunk` 或 cache 头部记录 embedding model id;`newEmbedder` 的模型变更时，把 `Schema`/model 不匹配的 chunk 判为需重嵌（可与 §2.2 的 Schema 合并处理）。

---

## 6. 注入 prompt 结构设计（落地到代码）

`MatchKnowledge` 的 `Context` 构造（`graphbuild.go:531-559`）改为两段式（对应 §1.4 的排版）：

1. 抬头保留：`以下是该项目的相关背景知识…`
2. **## 结构（实体与关系）**：现有实体循环 + 关系循环，原样。observations 可适度精简（内容让位给 chunk）。
3. **## 相关原文片段（细节与理由，最权威，优先据此判断）**：遍历 RRF 选出的 chunk，每块：
   ```
   〔来源：<resolveSessionTitles 渲染的标题>〕
   <chunk.Text 原文>
   ───
   ```
4. `Sources` 字段合并实体来源 + chunk 来源，去重。

三个调用方（`orchestrate.go` runEnrich、`commit.go` GenerateCommit、writeback 的召回如未来需要）**无需改动**——它们只消费 `KnowledgeMatch.Context`，两段式对它们透明。

**可选但推荐**：给 chat 也接上注入。目前 `MatchKnowledge` 只被 enrich/commit 调用，没有实时 chat 注入路径。如果产品有 chat 界面，应在 chat 发送前调用 `MatchKnowledge(projectSlug, userMsg)` 把 `Context` 作为一条 system message 前置——这才是最接近 Obsidian「边聊边召回原文」的形态。（本方案不强制，取决于 axon 是否有 chat 入口。）

---

## 7. 存储 / 迁移影响

- **体积**：每 chunk = 原文（~600 字 ≈ 1.2KB）+ 向量（1536 float32 ≈ 6KB JSON）。估算：50 会话 × 15 块 ≈ 750 块 ≈ **5～6MB/项目**。JSON 可接受;若某项目会话极多（数千），再考虑 §2.3-B 的独立向量文件。
- **回填**：靠 §2.2 的 `Schema` 版本触发。**关键优化**：chunk 生成**不需要 `graphModel` LLM**（切块是本地字符串操作，只需 embedder）。所以升级回填时，可以**只重跑「切块 + embed」，跳过已缓存的实体蒸馏**——把 `IndexProject` 改成：实体部分按 mtime 判新鲜（不变则复用），chunk 部分按 `Schema` 判缺失（缺则补）。这样老用户升级只花 embedding 钱，不花 LLM 钱。
- **原子写**:`SaveCache` 现在直接 `WriteFile`,加了大字段后建议改成 `tmp + Rename`（`graph.Save` 已经是这个模式，抄过来），避免写一半损坏。
- **向后兼容**:`Chunks omitempty`、`LoadAllCache` 对老缓存照常解析(无 chunk 就是空 slice),`MatchKnowledge` chunk 通道遇空自动退化为纯实体注入,不报错。

---

## 8. 迭代补全：chunk 与实体并行长大

三条回写入口，chunk 与实体**同源同时**生成,并行积累:

| 入口 | 现有实体回写 | 新增 chunk 回写 |
|---|---|---|
| **对话索引** `IndexProject` | `extractFromText` → 实体 | `chunkTranscript(msgs)` → chunks，同存 `SessionCache` |
| **代码索引** `BuildGraphFromCode` | 骨架 + LLM 业务富化 | `chunkCodeFile` → code chunks（`code:<path>` 溯源） |
| **任务采纳** `writeBackTaskKnowledge` | `extractFromText` → 实体 | `chunkTaskTranscript` → task chunks（`task:<id>` 溯源） |

- 三处都已有「组装 transcript / 读文件内容」的现成代码，切块只是在**同一份原文**上再走一遍，边际成本极低。
- writeback 的 chunk 存进它那条 `SessionCache{SessionID: "task:"+id}`（现有就是这么存实体的），`MatchKnowledge` 的 `loadChunks` 自然把它一起捞出来。
- 增量语义不变：mtime/Schema 判新鲜，新会话/新任务才产出新 chunk,已有的复用。

---

## 9. 为什么这样原理上就有效

**核心论证:信息不该被压缩两次。**

1. **信息论视角**:当前链路是「对话(高熵原文)→ LLM 蒸馏成实体(有损压缩#1)→ 召回时只取几条 observation(有损#2)」。两次有损后,AI 拿到的是「支付服务:用了幂等键」——结论在,但**推导结论所需的信息已被丢弃**,AI 无法复原「为什么、怎么实现、涉及哪段代码」,只能基于残缺上下文再合成一次,极易跑偏。原文 chunk 通道把「未经压缩的原始证据」直接送到模型面前,消除了信息塌缩。

2. **RAG 的既有结论**:纯向量 RAG 在「答案就在某几段原文里」的场景表现最好;它恰恰是 axon 缺的那半。Obsidian 系工具(Smart Connections/Copilot Vault QA)之所以让 AI 显著变聪明,**唯一的秘密就是检索并注入真实文字块**,没有「先蒸馏成实体再丢原文」这一步。axon 不是方向错,是**把 GraphRAG 里信息量最低的那半单独拎出来做,还砍掉了必配的原文通道**。

3. **结构 + 内容互补,不是二选一**:成熟 GraphRAG 实现同时存图节点 embedding 和 document chunk embedding。实体图谱擅长「全局导航、多跳、溯源」,原文 chunk 擅长「细节、理由、代码」。两者叠加,模型既有地图又有实景——这是「喂干瘪实体」永远给不了的。

4. **对比会怎样**:同一个「幂等键」问题——
   - *喂干瘪实体*:模型看到「用了幂等键」,不知道键是什么、为何选它,大概率重新发明一套(可能用订单号+时间戳),与既有实现冲突。
   - *喂原文 chunk + 结构*:模型看到当时的原始讨论「用 bizOrderId,因为回调会重复投递」+ 实际代码片段 + 「支付服务依赖订单服务」的结构,直接对齐既有设计,产出与项目一致。

**这就是「用得越多越懂」从口号变成机制的关键一步**:积累的不再只是被压干的结论,而是可随时重新解读的原始证据。

---

## 10. 落地步骤清单（改哪些文件，按依赖顺序）

```
1. internal/embed/embed.go + openai.go
   → 加 EmbedBatch（数组 input、分批、退避）           验证: openai_test.go 加批量用例
2. internal/graph/graph.go（或 cache.go）
   → 定义 Chunk struct                                验证: 编译通过
3. internal/graph/cache.go
   → SessionCache 加 Chunks + Schema；LoadCache 用 Schema 判 stale；SaveCache 改 tmp+Rename
                                                      验证: cache 读写测试（新旧结构互兼容）
4. graphbuild.go
   → chunkTranscript() 切对话；embedChunks()；
     IndexProject 并行产出+存 chunk（实体按 mtime 复用、chunk 按 Schema 补）；
     loadChunks(); MatchKnowledge 接 chunk 向量召回 + RRF 融合 + 两段式注入；
     常量区按 §4.4 更新                                验证: MatchKnowledge 单测（构造带 chunk 的 cache，断言注入含原文段+来源）
5. codegraphbuild.go
   → chunkCodeFile() 按函数切；BuildGraphFromCode 产出 code chunks
6. writeback.go
   → chunkTaskTranscript()；writeBackTaskKnowledge 产出 task chunks
7. 前端（settings/enrich/commit 结果页）
   → Method=="keyword" 明确告警；建索引时 emb==nil warning
8. （里程碑2，可选）internal/embed/local.go
   → 内置本地 embedding（ONNX bge-small-zh），零配置
```

每步遵守全局质量规范:改 Go 文件后 `go build ./...`;改召回/切块逻辑补单测(JUnit 对等的 Go table-driven test),确认注入块真的含原文片段与来源标签。

---

## 11. 关键数值速查

- 对话块:~600 中文字符(≈450 token),按轮次切,重叠 1 轮/~100 字
- 代码块:按函数边界,超长再按 ~500 token 二切
- chunk 召回候选:30;最终注入:5 块;chunk cosine 下限:0.30
- 实体种子:top 8;实体 cosine 下限:0.30(从 0.35 下调)
- 关系扩展:1 跳
- 注入总预算:~3200 token(结构 ~800 + 原文 ~2000 + 抬头/分隔余量)
- RRF 常数:k=60
- embed 批量:64~128 条/批
- 单项目 chunk 存储估算:50 会话 ≈ 5~6MB

---

## 参考

- Obsidian Smart Connections（block-level 本地语义检索、注入原文）：https://smartconnections.app/smart-connections/ ; https://github.com/brianpetro/obsidian-smart-connections
- 为什么用本地 embedding（零配置）：https://smartconnections.app/smart-connections/why-local-embeddings/
- Copilot for Obsidian Vault QA（笔记切块 RAG + 来源）：https://www.obsidiancopilot.com/docs/vault-qa
- GraphRAG vs RAG（各擅长、非替代）：https://www.cognee.ai/blog/deep-dives/graphrag-vs-rag ; https://www.tigergraph.com/blog/graphrag-vs-vector-rag/
- GraphRAG 实践（图节点向量与 chunk 向量并存）：https://towardsdatascience.com/graphrag-in-practice-how-to-build-cost-efficient-high-recall-retrieval-systems/
- 只留摘要导致上下文不完整：https://sumantthakur.substack.com/p/rag-vs-graphrag-vs-vectorless-rag
- chunk size / overlap 最佳实践：https://www.firecrawl.dev/blog/best-chunking-strategies-rag ; https://www.premai.io/blog/rag-chunking-strategies-the-2026-benchmark-guide/ ; https://developer.nvidia.com/blog/finding-the-best-chunking-strategy-for-accurate-ai-responses/ ; https://www.digitalapplied.com/blog/rag-chunking-strategies-2026-retrieval-quality-playbook
- RRF 融合（k=60）：https://docs.microsoft.com/en-us/azure/search/hybrid-search-ranking ; https://docs.lancedb.com/reranking/rrf ; https://www.digitalapplied.com/blog/hybrid-search-bm25-vector-reranking-reference-2026
</content>
</invoke>
