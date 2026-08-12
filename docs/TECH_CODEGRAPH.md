# Axon 代码知识抽取 — 技术预研(从代码/仓库抽业务实体与关系,并入知识图谱)

> 面向 Go 落地。基于现有代码(`internal/graph`、`internal/gitx`、`internal/provider`、`internal/embed`、`graphbuild.go`)。目标:让图谱不只从「历史对话」蒸馏,也能「从代码抽知识」,让 AI 同时懂业务与懂代码。

---

## 零、结论速览(先给判断)

1. **抽取方式:混合(静态骨架 + LLM 补业务),推荐。** 静态分析出准确的结构骨架(文件/包/函数/import 依赖),只对少量「关键文件」调 LLM 补业务含义写进 `Observations`。既准又省,幻觉可控。
2. **多语言策略:分两层。** 通用层(语言无关)靠「目录 + 文件 + import 正则 + 符号正则」覆盖所有语言;精解析层先只做 **Go**(用标准库 `go/ast`,零依赖、最准),其他语言按需增量补。**不引入 tree-sitter/cgo**(见 §1.4)。
3. **模型映射:代码实体复用现有 `Entity`,新增 `type` 取值 `file|package|function|type`。** 关系复用 `Relation`,`label` 取 `imports|contains|calls`。见 §2 映射表。
4. **融合:直接复用 `merge.go` 的别名归一。** 让代码实体携带 `Aliases`(短名 / 路径 / 驼峰拆词),让 LLM 蒸馏对话时也给业务实体挂上代码符号别名,`Graph.Merge` 自动把 `PaymentService`(代码)和「支付服务」(对话)并成一个节点。见 §2.3。
5. **规模控制:先过滤后预算。** 白名单源码目录 + 黑名单 `vendor/node_modules/dist/...`(黑名单直接复用 `gitx` 已有的 `skipPathGenerated`),静态骨架全量扫(快),LLM 补充按预算只挑 Top-N 关键文件(改动频率 / 大小 / 是否入口)。
6. **增量:复用 gitx diff + 现有 cache 机制。** 只对 `git diff` 命中的文件重抽,按文件粒度存 cache(类比 `SessionCache` 按会话粒度),`Merge` 累加。

---

## 一、抽取方式选型

### 1.1 三种方式对比

| 方式 | 抽到什么 | 准确度 | 成本/速度 | 懂业务 | 结论 |
|---|---|---|---|---|---|
| **纯静态分析** | 文件/包/函数/类型、import 依赖、目录结构、调用关系 | 结构层面高(确定性) | 快、免费 | 否(只有名字,没含义) | 做骨架 |
| **纯 LLM** | 实体 + 关系 + 业务职责/含义 | 业务层面高,但结构会漏/错、有幻觉 | 慢、贵 | 是 | 太贵不可全量 |
| **混合(推荐)** | 静态出骨架,LLM 只补关键文件的业务含义 | 结构准 + 关键处有业务 | 可控 | 关键处懂 | **采用** |

业界近期实证也支持这个判断:AST 派生的确定性图谱在覆盖率、多跳可靠性上强于纯 LLM 抽取,且索引成本低很多(arXiv 2601.08773);主流工具(Sourcegraph SCIP、CodeAtlas、code-graph-mcp、code-graph-rag)全部以 AST/tree-sitter 的结构抽取为骨架,LLM/embedding 只做补充与检索。

### 1.2 混合方案的具体设计

分三段流水线,和现有 `IndexProject`(对话蒸馏)是**并列的第二条数据源**,最终都汇入同一张 `Graph`:

```
BuildGraphFromCode(repoDir, projectSlug)
  ├─ 阶段A 扫描 + 过滤     → 得到待处理文件清单(源码目录、跳过 vendor 等)
  ├─ 阶段B 静态抽骨架       → []Entity{file/package/function/type} + []Relation{imports/contains/calls}
  │        (语言无关正则 + Go 走 go/ast 精解析)                       ← 快、全量、免费
  ├─ 阶段C LLM 补业务       → 只挑 Top-N 关键文件,喂内容让模型写 Observations + Aliases + 业务实体
  │        (预算受限,见 §3)                                          ← 慢、少量、花钱
  └─ 阶段D Merge 并入图谱   → graph.Merge(entities, relations) + Save   ← 复用现有,和对话知识共存
```

关键点:**阶段 B 决定「图里有哪些节点/边」(骨架必须准),阶段 C 只往已有节点上「贴业务标签」(Observations/Aliases),不新增结构。** 这样即使 C 出幻觉,最多是某个文件的职责描述不准,不会污染依赖关系这类硬事实。

### 1.3 阶段 C 的 prompt 形态(仿 `extractPrompt`)

给模型喂「文件路径 + 静态已知的符号列表 + 文件内容(截断)」,让它只输出业务层信息:

```
你是代码理解器。下面是一个源码文件及其已解析出的符号骨架。
请只提炼「这个文件/其中符号在业务上负责什么」的持久知识,不要复述语法结构。

【输出】对已知符号补充:
- observations:它在业务/系统里的职责(如"负责订单支付回调校验")
- aliases:它的业务别名(中文名/简称/领域术语,如 PaymentService → ["支付服务","支付"])
若某符号纯属技术样板(getter/DTO 等)无业务含义,可跳过。

只输出 JSON:
{"entities":[{"name":"符号名","type":"function|type","aliases":[...],"observations":[...]}],"relations":[...]}
```

温度沿用 0.1、`MaxTokens` 2000,和现有蒸馏一致。输出复用 `parseExtracted` 解析。

### 1.4 多语言:务实建议(不上 tree-sitter)

现状:项目已用 cgo(`go-sqlite3`),技术上能接 `go-tree-sitter`。但**不建议现在上**,理由:

- tree-sitter 每种语言一个 grammar,依赖膨胀、编译变慢、跨平台打包(Wails 桌面应用)复杂度陡增,收益前期不明显。
- 骨架的 80% 价值(有哪些文件/包、谁 import 谁、有哪些顶层函数/类型)靠**行级正则**就能拿到,语言无关。

**两层策略:**

1. **通用层(所有语言)——正则启发式,不建 AST:**
   - 文件 / 目录:直接来自扫描,天然是 `file`/`package(=目录)` 实体与 `contains` 关系。
   - import 依赖:每种语言 import 语法就几种,一组正则覆盖主流:
     - Go `import ( "x" )` / `import "x"`;JS/TS `import ... from 'x'` / `require('x')`;Python `import x` / `from x import y`;Java `import a.b.C;`。
     - → `file --imports--> 依赖目标`。
   - 顶层符号:`func|def|function|class|type|interface|struct` 开头的行,正则抓名字 → `function`/`type` 实体 + `file --contains--> symbol`。
   - 语言判定:按扩展名(`.go/.ts/.tsx/.js/.py/.java/.rs/...`)。
2. **精解析层(先只 Go)——标准库 `go/parser`+`go/ast`,零第三方依赖:**
   - 准确拿到包名、导出/非导出、函数签名、类型、方法归属、**函数内的调用**(`calls` 关系,正则做不到)。
   - Go 之外的语言:先只吃通用层的结果;等某语言成为主力项目时,再单独补它的精解析(可届时再评估 tree-sitter)。

> 判断:通用层保证「任何语言都不空图」,Go 精解析保证「axon 自身及 Go 项目质量最高」。这是最小可用且可迭代的路线。

---

## 二、抽什么 + 怎么映射到现有 graph 模型

### 2.1 Entity 映射表

现有 `Entity{Name, Type, Observations, Aliases, ObsSources, Embedding}` 完全够用,只需约定新的 `Type` 取值(和对话来源的 `module|service|concept|decision|constraint` 并存):

| 代码里的东西 | Entity.Type | Name(唯一键)约定 | Aliases(用于归一) | Observations 来源 |
|---|---|---|---|---|
| 目录 / 包 | `package` | 相对仓库根的目录路径,如 `internal/graph` | 包名 `graph`、末段 | LLM 补:这个包整体职责 |
| 源码文件 | `file` | 相对路径,如 `internal/graph/merge.go` | 文件名 `merge.go` | LLM 补:文件职责 |
| 函数 / 方法 | `function` | `包路径.符号`,如 `graph.Merge`(去重友好) | 短名 `Merge`、`(*Graph).Merge` | LLM 补:业务职责 |
| 类型 / 结构体 / 接口/类 | `type` | `包路径.类型`,如 `graph.Entity` | 短名 `Entity` | LLM 补:领域含义 |

**Name 唯一键的取舍(需注意):** 现有 `Merge` 用 `normKey`(lowercase+trim)做唯一键,大小写不敏感。代码里 `getUser` 与 `GetUser` 会被并成一个 —— 对「懂业务」这个目标基本无害(甚至是我们想要的归一),但若未来要做精确跳转,需要在 Name 里带上包路径前缀(上表已用 `包.符号` 规避同名冲突)。**这一点建议架构师确认:代码实体的 Name 是否统一加包/路径前缀。**

### 2.2 Relation 映射表

| 代码里的关系 | Relation.Label | From → To | 来源 |
|---|---|---|---|
| 目录包含文件 / 子目录 | `contains`(现有中文风格可用「包含」) | `package` → `file`/子`package` | 静态,确定 |
| 文件/包 import 另一个包 | `imports`(「依赖」) | `file` 或 `package` → 目标 `package` | 静态(正则/AST) |
| 文件包含符号 | `contains`(「包含」) | `file` → `function`/`type` | 静态 |
| 函数调用函数 | `calls`(「调用」) | `function` → `function` | Go 走 AST;其他语言暂缺 |
| 类型实现接口 | `implements`(「实现」,可选) | `type` → `type` | Go AST 可选,后期 |

> `label` 用中文还是英文:现有对话关系用中文(「依赖/调用/属于」)。**建议代码关系也用中文**(「依赖/包含/调用」),这样 `MatchKnowledge` 注入给模型时读起来一致,`relKey` 去重也不会因中英文并存而漏合。

### 2.3 和对话业务实体的融合(核心)——直接复用别名归一

这是「懂代码 + 懂业务」打通的关键。**不需要改 `merge.go`,只要在两边都填好 `Aliases`:**

- 代码侧(阶段 C):让 LLM 给 `PaymentService` 打上 `aliases: ["支付服务","支付","payment"]`。
- 对话侧(现有 `extractPrompt` 已经在做):让蒸馏时给「支付服务」打上 `aliases: ["PaymentService","payment"]`。

`Graph.Merge` 的 `entityKeys` 会把 name+所有 alias 都注册进 `idx`,只要两个节点的 name/alias **有任一交集**,就折叠成一个节点,`Observations` 取并集(代码职责 + 对话里的决策/踩坑合并到同一个实体上)。→ 一个 `PaymentService` 节点上,既有静态的 `imports/calls` 结构,又有对话沉淀的「为什么这么设计、踩过什么坑」。

**只需一点小增强(可选,提高命中率):** 在阶段 B 静态生成实体时,就把「符号名的驼峰/下划线拆词」「文件名去扩展名」自动塞进 `Aliases`(如 `payment_service.go` → alias `payment service`),让对话里的自然语言更容易撞上。这段是纯字符串处理,放在 Go 侧,不花模型钱。

### 2.4 溯源(ObsSources)

现有 `ObsSources[i]` 存的是 session id。代码来源建议存一个**特殊前缀的 source**,如 `code:internal/graph/merge.go`,这样 `resolveSessionTitles` 能识别并显示成「来自代码 merge.go」而不是当成 session id 查不到。需要在 `resolveSessionTitles` 加一个 `strings.HasPrefix(id, "code:")` 分支(小改动)。

---

## 三、规模与性能

### 3.1 过滤(先砍掉不该看的)

**白名单 + 黑名单双重过滤,黑名单直接复用 `gitx` 已有能力:**

- 目录黑名单:`node_modules / vendor / dist / build / .next / target / .git` —— `gitx/client.go` 的 `skipPathGenerated` 已经覆盖这批,**抽出来复用**(可提到 `gitx` 导出或复制常量)。
- 文件黑名单:lockfile、`.min.js`、`.snap`、二进制(靠扩展名 + 首 KB 是否含 NUL 判定)、secret 文件(复用 `skipPathSecret`,**代码抽取同样绝不读 `.env/*.pem/*.key`**)。
- 扩展名白名单:只处理源码扩展名(`.go .ts .tsx .js .jsx .py .java .rs .rb .php .c .cpp .h ...`),其余(`.md .json .png .lock`)跳过。
- 单文件大小上限:`> 256KB` 的文件跳过内容(多半是生成物 / 数据文件),但仍可留一个 `file` 节点。

### 3.2 预算(LLM 补充只挑关键文件)

静态骨架**全量**跑(纯 CPU,几千文件也就秒级)。花钱的是阶段 C,必须限量。

**关键文件评分(Top-N 选取):**

| 信号 | 说明 | 权重 |
|---|---|---|
| 改动频率 | `git log --format= --name-only -n 300` 统计各文件出现次数,越常改越核心 | 高 |
| 被依赖度 | 阶段 B 已算出 `imports` 入度,被多处引用的文件更重要 | 高 |
| 是否入口 | `main.go`、`app.go`、`cmd/*`、`*controller*`/`*service*`/`*handler*` 命名 | 中 |
| 文件大小 | 适中优先(太小没内容,太大多半是生成物) | 低 |

**具体预算建议(初始值,可调):**

- `maxLLMFiles = 40`(每次全量构建最多对 40 个文件调 LLM),按上面评分降序取前 40。
- 单文件内容截断 `maxCodeChars = 8000`(比对话的 16000 小,代码密度高;取文件头 + 导出符号附近)。
- 估算成本:40 文件 × ~10K tokens ≈ 中小仓库一次几毛到几块钱,和一次对话蒸馏同量级,可接受。
- 大仓库(文件 > 2000):`maxLLMFiles` 不随规模线性涨,固定 40~60,靠评分聚焦,保证成本封顶。

### 3.3 增量(和 gitx diff 结合 + cache)

复用现有「按单位存 cache、Merge 累加」的思路,把粒度从 session 换成 file:

- 新增 `CodeFileCache{Path, Mtime/BlobHash, Entities, Relations}`,存在 `<dataDir>/codegraphcache/<slug>/<pathhash>.json`(类比 `SessionCache`)。
- 判新旧:文件 mtime 或 `git hash-object` 的 blob hash 与 cache 里的比,变了才重抽(骨架必重抽,LLM 补充只在文件进入 Top-N 且内容变了才重跑)。
- 增量入口:`git diff --name-only <lastIndexed>..HEAD`(或工作区 diff)拿到改动文件集,只对这批走流水线;删除的文件从 cache 删并在下次 `assemble` 时消失。
- 组装:和 `assembleGraph` 一样,`LoadAll` code cache → 逐个 `Merge` → 与对话 cache 合并进同一张图。

> 结果:首次全量索引一次(骨架秒级 + 40 次 LLM 调用),之后每次只处理改动文件,commit 后触发增量几乎无感。

---

## 四、落地骨架(Go 侧)

新增包 `internal/codegraph`(和 `internal/graph` 分工:`codegraph` 负责「从代码产出 entities/relations」,产出物喂给 `graph.Merge`,复用现有存储/归一/embedding)。

```go
// internal/codegraph/codegraph.go
package codegraph

// Extract 扫描 repoDir,产出可直接喂给 graph.Merge 的实体与关系。
// 不碰存储、不调 LLM —— 纯静态骨架。LLM 补充与合并在上层(App)编排。
func ExtractSkeleton(repoDir string, files []string) ([]graph.Entity, []graph.Relation, error)

// SelectKeyFiles 按改动频率/被依赖度/入口/大小评分,返回 Top-N 关键文件路径。
func SelectKeyFiles(repoDir string, files []string, rels []graph.Relation, n int) []string

// listSourceFiles 扫描 + 过滤(白名单扩展名、黑名单目录、大小上限、secret 跳过)。
func listSourceFiles(repoDir string) ([]string, error)
```

上层编排(放在 `graphbuild.go` 旁,复用 `provider`/`embedder`/cache):

```go
// BuildGraphFromCode 从代码构建知识并入项目图谱(对话知识的并列数据源)。
func (a *App) BuildGraphFromCode(repoDir, projectSlug string) error {
    dataDir, _ := db.AppDataDir()

    files, _ := codegraph.listSourceFiles(repoDir)              // 阶段A 扫描+过滤
    ents, rels, _ := codegraph.ExtractSkeleton(repoDir, files) // 阶段B 静态骨架(全量,免费)

    // 阶段C LLM 补业务:只挑关键文件,受预算限制
    prov, _ := a.newProvider(/* openai-protocol provider */)
    key := codegraph.SelectKeyFiles(repoDir, files, rels, maxLLMFiles)
    for _, f := range key {
        if fresh(codeCache(dataDir, projectSlug, f)) { continue } // 增量:未变则跳过
        biz, _ := a.extractBizFromCode(prov, repoDir, f)          // 仿 extractFromText
        stampObsSourcesCode(biz.Entities, "code:"+f)              // 溯源前缀 code:
        ents = graph.MergeExtracted(ents, biz.Entities)           // 把业务标签贴到骨架符号上
        rels = append(rels, biz.Relations...)
        saveCodeCache(dataDir, projectSlug, f, biz)
    }

    if emb, err := a.newEmbedder(); err == nil {
        a.embedEntities(emb, ents)                                // 复用现有 embedding
    }

    g, _ := graph.Load(dataDir, projectSlug)
    g.Merge(ents, rels)                                           // 阶段D 与对话知识共存、别名归一
    g.UpdatedAt = time.Now().UnixMilli()
    return graph.Save(dataDir, g)
}
```

要点:阶段 B/D 都走现成的 `graph.Entity/Relation` 与 `graph.Merge`;LLM 调用完全复用 `collectReply`/`parseExtracted`/`extractPrompt` 风格;embedding 复用 `embedEntities`;溯源用 `code:` 前缀区分来源。**几乎不改现有代码,只是新增一条数据入口。**

### 4.1 注入增强(让代码相关 query 召回更准)

任务/提交时,`MatchKnowledge` 的 query 目前只喂用户文本。增强:把「涉及的代码文件路径 + 符号名」拼进 query。

- **commit 场景:** `gitx.Diff` 已知改动文件与 hunk 头(函数上下文)。把改动的 `文件路径 + 从 hunk header 抓到的函数名`(`diffSummary` 已在提取 `@@ ... @@`)附加到 query 文本里,再调 `MatchKnowledge`。→ 语义 seed 和 alias 子串匹配都会命中对应的 `file`/`function` 代码实体,连带 `expandAlongRelations` 拉出它的依赖与对话沉淀的决策。
- **task 场景:** `Spec.Scope`(涉及范围:文件/模块)已存在,把 `Scope` 里的路径/模块名并进 `MatchKnowledge` 的输入即可。
- 无需改 `MatchKnowledge` 内部逻辑:它已经是「子串命中 name/alias + 语义 seed + 关系扩展」的混合召回,喂进更多结构化线索(路径、符号)天然提升命中。唯一小增强:query 里若含 `/` 路径,可顺手拆出文件名(去目录、去扩展名)一起匹配。

---

## 五、需要架构师拍板的取舍(汇总)

1. **代码实体 Name 是否统一加包/路径前缀(`graph.Merge` vs `Merge`)。** 加前缀避免同名冲突、利于精确定位;不加则更容易和对话里的自然语言归一。倾向:**加前缀,但把短名放进 Aliases**(两全)。
2. **是否上 tree-sitter。** 本预研建议**先不上**,Go 走标准库 AST、其他语言走正则。若很快要深度支持 JS/TS/Python 的调用图,再评估 tree-sitter(cgo/打包成本)。
3. **LLM 预算上限 `maxLLMFiles`(建议 40)与关键文件评分权重。** 直接决定成本与业务覆盖度,需按典型仓库规模拍板。
4. **关系 label 用中文还是英文。** 建议中文(和对话关系一致,注入/去重更统一),但会和「英文更通用」的直觉冲突,请确认。
5. **触发时机。** 手动「从代码建索引」按钮 vs commit 后自动增量。倾向:首次手动全量,之后 commit 钩子自动增量(和现有 commit 工具联动)。
6. **`Merge` 大小写不敏感 + 代码同名折叠的副作用。** 目前对「懂业务」有利,但要确认不会把语义不同的 `getUser`/`GetUser` 误并造成困惑(带包前缀可缓解)。

---

## 参考链接

- [SCIP - a better code indexing format than LSIF(Sourcegraph)](https://sourcegraph.com/blog/announcing-scip) — 语言无关的符号索引协议,definitions/references 的工业级做法
- [sourcegraph/scip(GitHub)](https://github.com/sourcegraph/scip/) — SCIP 协议实现,可参考其符号命名与关系建模
- [AST-Derived Graphs vs LLM-Extracted Knowledge Graphs(arXiv 2601.08773)](https://arxiv.org/abs/2601.08773) — 实证:AST 派生图在覆盖率/多跳/成本上优于纯 LLM 抽取(支撑「混合」结论)
- [Tree-Sitter-Based Knowledge Graphs for LLM Code Exploration via MCP(arXiv 2603.27277)](https://arxiv.org/html/2603.27277v1) — 66 语言、多阶段流水线的代码知识图谱
- [CodeAtlas(GitHub)](https://github.com/AryanSaini26/CodeAtlas) — tree-sitter 实时代码知识图谱 MCP,给 AI agent 用
- [code-graph-mcp(GitHub)](https://github.com/sdsrss/code-graph-mcp) — AST 知识图谱 + 调用图遍历 + 影响分析,10 语言
- [cognitx-leyton/codegraph(GitHub)](https://github.com/cognitx-leyton/codegraph) — TS/React 代码图谱入 Neo4j,Cypher 查架构(关系建模参考)
- [Hierarchical Context-Aware Graph RAG(Google Research)](https://research.google/pubs/beyond-vector-similarity-hierarchical-context-aware-graph-rag-vs-standard-rag-in-enterprise-code-migration/) — AST 抽取 + 属性图 + 分层上下文检索,企业代码迁移场景
- [code-chunk(GitHub)](https://github.com/supermemoryai/code-chunk) — tree-sitter 按语义边界(函数/类)切块,含 scope/imports 上下文(阶段 C 截断策略参考)
