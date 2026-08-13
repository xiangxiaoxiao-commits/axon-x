# Axon-x

> 让 AI 在动手前，先读懂你这个项目的**业务背景**和**代码结构**。

Axon-x 是一个跨平台桌面应用（macOS + Windows），它把散落在历史对话、代码仓库、Obsidian 笔记里的"隐性项目知识"，沉淀成一张**可迭代补全的知识图谱**，再通过 **MCP（Model Context Protocol）** 暴露给 Claude Code 之类的 AI 编码助手。这样 AI 每次干活时，都能自动召回"这个项目以前的设计决策、踩过的坑、接口约定、业务约束"，而不是每次从零开始、需要你反复解释。

它的角色是**给 agent 用的上下文增强器**：你在 GUI 里养一张项目知识图谱，一键接入 Claude Code，agent 就懂你的项目。

一句话：**用得越多越懂你的项目。**

---

## 解决什么问题

用 AI 写代码时，最大的摩擦不是模型不够聪明，而是它**不懂上下文**：

- 它不知道你三个月前为什么选了这套幂等方案，于是又给你出了个会重复扣款的设计。
- 它不知道 `OrderService` 依赖了哪些下游，改一处就漏改一片。
- 你自己"大概有印象但记不清关键词"，翻不回当初那段对话。

Axon-x 把这些知识固化下来，让 AI 主动来查，而不是等你手动喂。

---

## 工作原理

三个环节构成一个闭环：

1. **建图（Ingest）** — 从三类来源抽取业务实体、关系与原文片段，合并进同一张按项目隔离的知识图谱：
   - **代码仓库**：语言无关层（目录 / 文件 / 包 / import 关系）+ Go AST 层（函数 / 类型 / 调用关系）。
   - **历史对话**：Claude Code 的会话记录，蒸馏出设计决策与结论。
   - **Obsidian 笔记**：把 vault 里的知识并入图谱。

2. **检索（Recall）** — **HybridRAG 双通道**：实体结构通道（语义 seed + 关系扩展）与原文 chunk 通道并行召回，用 **RRF（Reciprocal Rank Fusion）** 融合排序。embedding 默认走云端（OpenAI-compatible），无 key 时自动降级到**本地词面兜底**，保证离线可用。

3. **注入（Inject）** — 通过 stdio MCP server 把知识图谱暴露给 Claude Code，AI 在会话中直接调用工具查询，带**来源溯源**。

---

## 接入 Claude Code（MCP）

在 **设置 → 接入 Claude Code** 里点一下**「一键接入」**即可。Axon-x 会定位随应用分发的 `axon-mcp` 二进制，把 `axon-knowledge` 这个 stdio server 写进 Claude Code 的用户级配置（`~/.claude.json` 的 `mcpServers`），保留其它条目不动。设置页会实时显示「已接入 / 未接入」状态，并可一键移除或更新路径。

> 从 Finder / 资源管理器启动的 GUI 进程往往拿不到完整 PATH，因此一键接入**不依赖** `claude` CLI，而是直接、原子地读写配置文件，macOS 与 Windows 行为一致。

也可以手动用 CLI 注册：

```bash
claude mcp add axon-knowledge -s user /path/to/axon-mcp
```

接入后，AI 在会话中可调用四个工具：

| 工具 | 作用 |
| --- | --- |
| `list_projects` | 列出所有已建图谱的项目（slug + 路径 + 实体数） |
| `search_knowledge` | 给一段自然语言 query，返回相关实体、事实与原文片段，带来源标注 |
| `get_entity` | 查看某实体的全部 observations（事实）+ 关系 + 别名，支持别名与大小写不敏感匹配 |
| `remember_knowledge` | 把本次对话学到的持久知识写回图谱（实体/关系），通过别名归一并入已有节点——MCP 模式下"越用越懂"的写入闭环 |

`axon-mcp` 是一个独立二进制，不依赖 GUI，直接读 GUI 建好的 `graphs/` 与 `graphcache/`，与 App 共用 `internal/retrieve` 里的召回核心。

### 让图谱在 MCP 模式下越用越全

查询（`search_knowledge`）是**只读**的，查再多也不会自动沉淀。让图谱在纯 MCP 使用下持续长大，靠这几条：

1. **让 agent 主动写回（核心）** — 在 `CLAUDE.md` 里写一条指令，让 agent 开工前先查、学到持久知识后调 `remember_knowledge` 写回。这样每次对话的结论**实时**并入图谱（别名归一到已有实体），不用回 GUI。可直接粘贴：

   ```markdown
   ## 项目知识图谱（axon-knowledge MCP）

   如果当前会话挂载了 axon-knowledge 这个 MCP 工具：

   - 开工前（改代码/做设计/排查前）先用 search_knowledge 查本项目的既有决策、
     约束、坑、接口约定，不要臆测历史设计。project slug 用 list_projects 拿到，
     选与当前工作目录匹配的那个（slug = 工作目录绝对路径里的 / 换成 -）。
   - 敲定一个非显然的持久知识后（设计决策及理由 / 约束与坑 / 接口约定 / 模块职责与关系），
     调用 remember_knowledge 写回（project 用同一个 slug）：一个实体一个概念，
     observations 一句话一条，给出别名，能表达关系就带 relations。
   - 不要写回：临时调试、一次性操作、寒暄、能从代码或 git log 直接看出的事实、
     被否定的方案。只沉淀“代码里看不出来、忘了会重复踩坑”的东西，宁可少而准。
   ```

   > 改了 `axon-mcp`（如升级本应用）后，到 **设置 → 接入 Claude Code** 点一次「重新接入 / 更新路径」，并**重启正在运行的 Claude Code 会话**，新工具才会加载。

2. **定期重新「建索引」** — 你用 Claude Code 会不断在本地积累新会话文件，回 Axon 点一次建索引即可**增量**蒸馏进图（旧的跳过，不重复烧 token）。

3. **代码 / 笔记变了就重跑**「🧬 从代码建图」「📓 吸收 Obsidian」，让结构与笔记知识保持最新。

4. **人工校正** — 在知识视图里删噪音、纠错、合并别名。**图谱质量比数量重要。**

---

## 桌面端功能

界面收敛为四个入口——**知识**、**会话**、**终端**与**设置**：

- **知识图谱**：可视化查看实体 / 关系 / 别名 / 溯源；支持人工确认、修正、去噪（保证图谱质量）。
- **建索引**：对一个真实仓库跑"从代码建图"；对话与笔记同样并入。
- **回写闭环**：新学到的业务事实增量合并进图谱，持续长大。
- **会话浏览与一键恢复**：浏览 Claude Code 存在本地的历史会话（按项目隔离，标签页关了也不丢）；详情页顶部展示「最后进度」（最后一轮你的提问 + AI 回复尾部），一眼看清停在哪；点「▶ 恢复」直接在内置终端里 `cd` 回原工作目录并 `claude --resume`，异常时快速接回。
- **内置终端**：PTY 驱动的真实 shell，跨标签页常驻（恢复起来的会话切走再切回仍存活）。
- **设置**：配置模型 Provider、embedding，以及一键接入 Claude Code。

---

## 最佳实践

让 agent 真正吃到项目知识，关键不在功能多，而在**把图谱养好、让 agent 主动查**。下面是推荐的工作流。

### 快速上手（5 步）

1. **配一个 embedding provider**（设置 → Embedding）。语义召回的精度几乎全靠它——没有 embedding 时会降级成关键词匹配，"记不清关键词"的场景就召回不到。推荐 `text-embedding-3-small` 或智谱 `embedding-3`。
2. **建索引**：对你的仓库跑一次"从代码建图"，把历史对话、Obsidian 笔记也并入。
3. **人工过一遍图谱**：删掉噪音实体、合并同义别名（`OrderService` / `订单服务`）、修正错误事实。**图谱质量 > 图谱数量**。
4. **一键接入 Claude Code**（设置 → 接入 Claude Code）。
5. **重启正在运行的 Claude Code 会话**，让它重新加载 MCP server。

### 让 agent 主动查（关键）

接入只是让工具**可用**，不等于 agent **会用**。最有效的一步：在项目的 `CLAUDE.md` 里写一条指令，告诉 agent 动手前先查。

```markdown
## 项目知识
动手改代码或做设计前，先用 axon-knowledge 的 search_knowledge 工具
查询本项目相关的既有决策、约束与接口约定（project slug: <你的-slug>），
不要臆测历史设计。
```

用 `list_projects` 拿到你的 project slug 填进去。这条指令能把"偶尔查一下"变成"每次动手前必查"。

### 养图谱的习惯

- **趁热沉淀**：一个非显然的设计决策拍板后，顺手让它进图谱（回写闭环）。事后补总是补不全。
- **一个实体一个概念**：别把"支付"和"支付回调幂等"塞进同一个实体，拆开，关系用 relation 表达。
- **事实要能溯源**：图谱里每条 observation 都带来源，评审时优先信有溯源的。
- **定期去噪**：代码删了、方案废了，对应实体也清掉，别让 agent 拿着过期知识干活。

### 什么该进图谱，什么不该

| 适合沉淀 | 不必沉淀 |
| --- | --- |
| 为什么这么设计（决策与权衡） | 代码结构本身（agent 直接读代码更准） |
| 踩过的坑与约束（"这里不能并发"） | 能从 git log / 注释直接得到的事实 |
| 跨服务的接口约定、字段语义 | 一次性的临时调试信息 |
| 业务规则（"未婚用户走 B 流程"） | 通用技术知识（agent 本来就会） |

> 一句话原则：**沉淀那些"只有你知道、代码里看不出来、忘了会重复踩坑"的东西。**

---

## 技术栈

- **框架**：[Wails v2](https://wails.io)（Go 后端 + Web 前端）
- **前端**：Svelte 5 + Vite + TypeScript
- **后端**：Go 1.25（`GOTOOLCHAIN=auto` 自动拉取）
- **存储**：SQLite，纯 Go 驱动 [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite)（无 cgo，Windows / macOS 免工具链交叉编译），WAL 即时落盘
- **数据目录**：`os.UserConfigDir()/axon`（macOS 为 `~/Library/Application Support/axon`，Windows 为 `%AppData%\axon`）
- **密钥**：OS 凭证库——macOS Keychain / Windows Credential Manager，不落明文
- **模型接入**：OpenAI-compatible + Anthropic 原生，流式输出
- **向量**：云端 embedding（如 `text-embedding-3-small`），无 key 时本地词面兜底

### 后端模块（`internal/`）

| 模块 | 职责 |
| --- | --- |
| `codegraph` | 从代码仓库抽取结构骨架（文件 / 包 / 函数 / 类型 + 关系） |
| `graph` | 知识图谱模型：实体 / 关系 / 别名归一 / 溯源 / embedding |
| `retrieve` | App 无关的召回核心：HybridRAG 双通道 + RRF 融合 |
| `embed` | embedding 抽象接口（云端 + 本地兜底） |
| `claudedata` | 读取 Claude Code 会话数据（列表 / 全文 / 最后进度 / 精确工作目录），支撑会话浏览与一键恢复 |
| `provider` | 各家 API 流式调用 |
| `secret` | OS 凭证库密钥存取（Keychain / Credential Manager，按平台分派） |
| `mcpinstall` | 一键接入：读写 Claude Code 的 `~/.claude.json` |
| `db` / `store` / `config` | 迁移 / 持久化 / 配置 |

---

## 从源码构建

前置：Go 1.25、Node、[Wails CLI](https://wails.io/docs/gettingstarted/installation)。sqlite 已换成纯 Go 驱动，**后端本身不再需要 cgo**（Wails 打包仍依赖各平台的 WebView 运行时）。

```bash
# 实时开发（热重载）
wails dev

# 后端测试
go test ./...

# 生产构建
wails build -clean               # -> build/bin/Axon-x.app（macOS）
                                 #    build/bin/Axon-x.exe（Windows）

# 编译 MCP server（随应用分发，一键接入会定位它）
go build -o build/bin/axon-mcp ./cmd/axon-mcp          # macOS/Linux
GOOS=windows go build -o build/bin/axon-mcp.exe ./cmd/axon-mcp

# 打包 dmg（macOS）
bash scripts/make-dmg.sh
```

> 注意：编译产物 `axon` / `axon-mcp`(`.exe`) 已在 `.gitignore` 中，不要提交进仓库。
>
> **一键接入的前提**：`axon-mcp` 二进制需与主程序位于同一目录（打包时一并放入 `build/bin/`，或 .app 的可执行目录），否则设置页会提示找不到二进制。

---

## 数据与隐私

- 所有图谱、会话、配置存于用户级数据目录（macOS `~/Library/Application Support/axon/`，Windows `%AppData%\axon\`），升级 / 重装不丢。
- API key **只存 OS 凭证库**（Keychain / Credential Manager），不落明文、不进上述目录。
- 单机单用户，无云同步、无多人协作。

---

## 已知边界

- 支持 macOS（Apple Silicon / Intel）与 Windows；Linux 可编译但无原生凭证库接入。
- macOS 为 ad-hoc 签名（非 Apple 付费公证），首次打开需右键 → 打开。
- AST 级抽取目前以 Go 为主；其它语言走语言无关的目录 / import 层。
- 需自备各家 API key；语义召回的最佳精度依赖一个 OpenAI-compatible 的 embedding provider。

---

## 文档

设计与技术细节见 `docs/`：迭代计划（`ITERATION_PLAN.md`）、上下文引擎方案（`PLAN_CONTEXT_ENGINE.md`）、codegraph 技术设计（`TECH_CODEGRAPH.md`）、各能力的 PRD 等。

