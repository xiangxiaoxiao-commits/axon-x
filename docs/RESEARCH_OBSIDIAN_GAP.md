# 为什么 Obsidian 那套能让 AI 变聪明，而 axon 现在效果不佳

> 诚实的根因调查 + 可落地改进。结论先行，证据在后。

## TL;DR（结论先行）

1. **axon 最根本的问题：双重有损蒸馏，且把原文彻底丢掉了。**
   对话 → LLM 提炼成「实体 + 几条 observation」→ 存进 cache，**原始对话/代码原文不落盘**（`SessionCache` 只有 `Entities/Relations`，见 `internal/graph/cache.go`）。召回时注入给 AI 的又只是这些干瘪 observation。AI 拿到的是「支付服务：用了幂等键」这种抽象结论，而**当时为什么这么做、具体怎么实现、涉及哪段代码，全没了**。信息量塌缩了两次。

2. **Obsidian 那套之所以有效，恰恰是它从不做这件事。**
   Smart Connections / Copilot 的检索单元是**笔记原文的块（block/chunk）**，embedding 建在**真实的、人写的高密度文字**上，召回后**把原文段落塞给 AI**（还能拖进正文）。没有「先蒸馏成实体再丢原文」这一步。AI 拿到的是有血有肉的上下文。

3. **最高性价比的改进（具体到 axon）：在 `SessionCache` 里多存一份「原文 chunk + embedding」，`MatchKnowledge` 召回时把命中的原文片段一起注入**（实体图谱保留做「结构/关系」用，原文 chunk 做「内容」用）。这是把当前的 GraphRAG 退化实现补成「GraphRAG + 经典向量 RAG」双通道，性价比最高、改动集中在两三个文件。

---

## 一、Obsidian + AI 到底靠什么让 AI 变聪明

拆成三个机制，逐个说，并对照 axon。

### 机制 A：检索并注入的是「原文块」，不是「提炼实体」

- **Smart Connections**：本地 embedding 建在每篇笔记及其**块（block）**上，按语义相似度把**相关笔记/段落原文**展示在侧栏，可预览、可拖进当前笔记（[smartconnections.app](https://smartconnections.app/smart-connections/)、[GitHub README](https://github.com/brianpetro/obsidian-smart-connections)）。它明确是 **block-level** 检索，注入的是原文。
- **Copilot for Obsidian（Vault QA）**：典型 RAG——把笔记切块、向量化、召回相关块**连同来源链接**喂给 LLM 回答（[Vault QA 文档](https://www.obsidiancopilot.com/docs/vault-qa)）。
- 关键点：**embedding 和注入的对象都是「真实文字」**。笔记本身就是用户精心写的、信息密度极高的内容，原样召回原样注入，AI 能读到细节、语气、理由、代码。

对照 axon：`entityEmbedText` 把 embedding 建在 `name + observations` 上（`graphbuild.go:212`），注入的 `Context` 也是 `【实体名】\n- observation` 的列表（`graphbuild.go:531-546`）。**embedding 和注入的对象都是「二手摘要」**，不是原文。

### 机制 B：块级（chunk）粒度 vs 实体级粒度

- 业界共识：**纯向量 RAG 擅长「答案就在某几段原文里」的场景**；GraphRAG（实体+关系）擅长**多跳推理、溯源、跨文档模式**（[cognee](https://www.cognee.ai/blog/deep-dives/graphrag-vs-rag)、[tigergraph](https://www.tigergraph.com/blog/graphrag-vs-vector-rag/)）。二者**不是替代关系**——成熟 GraphRAG 实现会**同时存**图节点 embedding **和** document chunk embedding（[Towards Data Science: GraphRAG in Practice](https://towardsdatascience.com/graphrag-in-practice-how-to-build-cost-efficient-high-recall-retrieval-systems/)）。
- 一句有代表性的警告：「**如果检索破坏了原始文档结构（只留摘要），模型就会基于不完整的上下文去合成**」（[substack: Retrieval Is Now an Architecture Decision](https://sumantthakur.substack.com/p/rag-vs-graphrag-vs-vectorless-rag)）。

对照 axon：**只有实体级，没有 chunk 级**。等于把 GraphRAG 砍掉了它必须搭配的那半（原文 chunk 通道），只留了信息量最低的实体摘要。这是最伤的设计取舍。

### 机制 C：人工确认的双向链接 = 高质量、被人背书的关系

- Obsidian 的 `[[双链]]` 是**用户手动建立、人脑确认过的关系**，几乎无噪音；笔记内容也是**人主动写下的**，天然高密度、高相关。
- 就算是「零链接」的 Smart Connections，它的底料也是**人写的笔记**，而非机器从对话里蒸馏的抽象条目。

对照 axon：实体、关系、observation **全部由 LLM 从对话/代码自动抽取**。自动抽取必然带来：命名漂移、关系稀疏、observation 空泛。虽然有 `Merge` 做别名归一（`internal/graph/merge.go`）、`graphedit.go` 提供了增删改实体/关系的能力，但**默认没有「人确认」这一环**，图谱质量天然低于人工笔记。

---

## 二、axon 为什么效果不佳（对照代码，逐条证伪/坐实）

| 怀疑方向 | 结论 | 证据 |
|---|---|---|
| **提炼太抽象/丢原文** | ✅ **坐实，且是头号根因** | `extractPrompt` 明确要求「宁可少而准」「丢弃工具调用/报错/一次性内容」，把对话压成实体+几条 observation（`graphbuild.go:46-69`）。`SessionCache` 结构里**根本没有原文字段**（`cache.go:13-18`），`buildTranscript` 读出的原文用完即弃（`graphbuild.go:267`）。 |
| **召回粒度不对（无原文/代码片段）** | ✅ **坐实** | `MatchKnowledge` 全程只在 `g.Entities` 上操作，注入 `Context` 只有实体名+observation+关系（`graphbuild.go:531-559`）。没有任何原始片段。 |
| **注入信息量太低** | ✅ **坐实** | 注入块形如 `【支付服务】\n- 用了幂等键`。AI 拿到的是结论，没有推理链、没有代码、没有当时的权衡。 |
| **图谱质量差（命名乱/关系少/observation 空）** | ⚠️ **部分成立** | 有 `Merge` 别名归一缓解命名漂移，`aliases` 机制也在（`merge.go`）。但关系全靠 LLM 自动抽，且 `extractPrompt` 追求「少而准」，实际关系往往稀疏，`expandAlongRelations` 只走 1 跳（`knowledgeExpandHops=1`），扩不出多少东西。 |
| **embedding 缺失 → 降级关键词** | ⚠️ **真实存在的隐患** | `MatchKnowledge` 里 `a.newEmbedder()` 失败就**静默降级为纯 substring 匹配**（`graphbuild.go:485-493`）。用户若没配 OpenAI-协议 embedding provider，语义召回直接失效，退化成「实体名字面出现在文中才命中」——而中文对话里精确出现实体名的概率很低。这条会让效果雪上加霜，但**不是根因**（就算 embedding 正常，注入的还是干瘪摘要）。 |
| **检索没召回到真正相关的东西** | ⚠️ **症状，非根因** | 阈值 `knowledgeSeedMinScore=0.35`、`topK=5`、1 跳扩展都偏保守。但核心问题是：**就算召回对了实体，实体本身信息量也不够**。 |

### 一个被忽视的强证据：PRD 本来就想做 chunk 检索，实现时跑偏了

- `docs/PRD.md` US-3.1：「自动从我过去的会话里找出**语义相关的片段**」。
- `docs/PRD.md` F4.3：「摘要/**片段**向量化后存入 sqlite-vec，支持近邻检索」。

**设计意图本来是「召回原文片段」**，但落地成了「蒸馏实体 + 实体向量」，把「片段」这一层弄丢了。这不是要不要做的问题，是**已经规划过、实现时退化了**。

---

## 三、可落地改进（按性价比排序，具体到改哪）

### 🥇 改进 1：存原文 chunk + embedding，召回时注入原文片段（性价比最高）

**这是根治「信息量塌缩」的关键一步，把 axon 从「只有实体摘要」补成「实体图谱 + 原文 RAG」双通道。**

改动点：
1. `internal/graph/cache.go` 的 `SessionCache` 增加一个 `Chunks []Chunk` 字段：
   ```go
   type Chunk struct {
       ID        string    `json:"id"`        // sessionID#序号，做溯源
       Text      string    `json:"text"`      // 原文片段（对话/代码原样，600~1000 字一块）
       Embedding []float32 `json:"embedding,omitempty"`
   }
   ```
2. `IndexProject`（`graphbuild.go:127`）在 `extractFromText` 之外，**并行把 transcript 切块 + embed**，一起存进 cache。切块可以简单按字符窗口 + 重叠（先别过度设计）。
3. `MatchKnowledge`（`graphbuild.go:460`）新增一路：把 query embedding 和所有 chunk embedding 算 cosine，取 topN 原文块，拼进注入 `Context`（放在实体摘要之后，标注来源会话）。
4. 注入块结构变成：`结构化知识（实体/关系）` + `相关原文片段（有血有肉的上下文）`。

收益：AI 第一次能读到「当时到底怎么讨论的、代码长什么样、为什么这么定」。这是 Obsidian 有效的核心机制在 axon 的直接移植。
成本：中。改 3 个文件；需要注意 cache 体积增大（原文入库）和 embedding 调用量上升（切块后向量数变多）。

### 🥈 改进 2：修掉 embedding 静默降级这个坑

现状：没配 embedder 就默默退化成 substring 关键词匹配，中文场景基本召回不到东西，用户却不知道自己在用「阉割版」。

改动点（`graphbuild.go` / 设置页）：
- **首选**：内置一个本地 embedding（如 `bge-small` / `gte` 一类小模型，走 onnx/本地进程），像 Smart Connections 那样「零配置、无需 API key」（[why local embeddings](https://smartconnections.app/smart-connections/why-local-embeddings/)）。这直接对标 Obsidian「开箱即用」的体验。
- **兜底**：若坚持只用远程 embedding，则在 UI 上把 `KnowledgeMatch.Method == "keyword"` 明确告警「当前为降级关键词召回，建议配置 embedding」——代码里 `Method` 字段已经算好了（`graphbuild.go:574`），前端用起来即可。

收益：让语义召回真正生效。成本：低（告警）到中（本地模型）。

### 🥉 改进 3：extractPrompt 保留「原文引用」而不只是抽象结论

即使不做改进 1，也可以让每条 observation **带一句原文摘录/关键代码**，而不是只有一句抽象总结。

改动点：`extractPrompt`（`graphbuild.go:46`）要求 observation 尽量**保留具体细节和一句原文佐证**（例如把「用了幂等键」升级为「支付创建接口用 `bizOrderId` 做幂等键去重，避免重复扣款——见当时讨论」）。同时可放宽「宁可少而准」的力度。

收益：低成本提升单条 observation 的信息密度。成本：低（只改 prompt）。
注意：这只是缓解，治标不治本——真正有血有肉的原文还是得靠改进 1。

### 改进 4：把「人确认关系」做成正循环（对标双向链接）

`graphedit.go` 已经有增删改实体/关系、`UpdateEntityObservations` 的能力，缺的是**引导用户去确认/补关系**的产品动作，以及**人工确认的关系应享有更高召回权重/更多跳扩展**。

改动点：
- 召回时对「人工确认过的关系」放宽跳数（当前 `knowledgeExpandHops=1` 对全体一刀切）。
- UI 上把自动抽取的关系标为「待确认」，让用户一键确认——确认过的关系视作高质量边。

收益：让图谱质量随使用向「人工笔记」靠拢，是 Obsidian 双链价值的补法。成本：中（涉及前端交互 + 数据结构加 `Confirmed` 标记）。优先级低于 1/2。

### 改进 5：召回参数调优（顺手做）

`knowledgeSeedMinScore=0.35` 偏高（中文 + 二手摘要 embedding，相似度普遍偏低），`topK=5`、1 跳扩展偏保守。做完改进 1 后，用真实数据重新标定这几个阈值（`graphbuild.go:25-35`）。成本：极低。

---

## 四、一句话总结给决策

axon 的知识图谱不是「图谱」这个方向错了，而是**只做了 GraphRAG 里信息量最低的那半（实体摘要），把必须搭配的原文 chunk 通道整个丢了**——而 Obsidian 有效恰恰是因为它专注做好了那半（原文块召回 + 原文注入）。**先做改进 1（存原文 chunk + 召回注入原文）+ 改进 2（修 embedding 降级），就能把「AI 拿到干瘪结论」变成「AI 拿到有血有肉的上下文」，这是投入产出比最高的一步。**

## 参考

- Smart Connections（block-level 本地语义检索）：https://smartconnections.app/smart-connections/ ，https://github.com/brianpetro/obsidian-smart-connections
- 为什么用本地 embedding（零配置）：https://smartconnections.app/smart-connections/why-local-embeddings/
- Copilot for Obsidian Vault QA（笔记切块 RAG + 来源）：https://www.obsidiancopilot.com/docs/vault-qa
- GraphRAG vs RAG（各自擅长场景，非替代）：https://www.cognee.ai/blog/deep-dives/graphrag-vs-rag ，https://www.tigergraph.com/blog/graphrag-vs-vector-rag/
- GraphRAG 实践（图节点 embedding 与 chunk embedding 并存）：https://towardsdatascience.com/graphrag-in-practice-how-to-build-cost-efficient-high-recall-retrieval-systems/
- 检索若只留摘要会导致上下文不完整：https://sumantthakur.substack.com/p/rag-vs-graphrag-vs-vectorless-rag
- Context-graph-grounded RAG 相比扁平 chunk 检索的增益：https://atlan.com/know/chunking-strategies-rag/
