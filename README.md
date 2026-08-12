# Axon-x

> 让 AI 在动手前，先读懂你这个项目的**业务背景**和**代码结构**。

Axon-x 是一个 macOS 桌面应用，它把散落在历史对话、代码仓库、Obsidian 笔记里的"隐性项目知识"，沉淀成一张**可迭代补全的知识图谱**，再通过 **MCP（Model Context Protocol）** 暴露给 Claude Code 之类的 AI 编码助手。这样 AI 每次干活时，都能自动召回"这个项目以前的设计决策、踩过的坑、接口约定、业务约束"，而不是每次从零开始、需要你反复解释。

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

## MCP 工具

把 Axon-x 的 MCP server 注册给 Claude Code：

```bash
claude mcp add axon-knowledge /path/to/axon-mcp
```

注册后，AI 在会话中可调用三个工具：

| 工具 | 作用 |
| --- | --- |
| `list_projects` | 列出所有已建图谱的项目（slug + 路径 + 实体数） |
| `search_knowledge` | 给一段自然语言 query，返回相关实体、事实与原文片段，带来源标注 |
| `get_entity` | 查看某实体的全部 observations（事实）+ 关系 + 别名，支持别名与大小写不敏感匹配 |

`axon-mcp` 是一个独立二进制，不依赖 GUI，直接读 GUI 建好的 `graphs/` 与 `graphcache/`，与 App 共用 `internal/retrieve` 里的召回核心。

---

## 桌面端功能

Wails 应用提供图形界面完成"建图 + 管理 + 辅助"：

- **知识图谱**：可视化查看实体 / 关系 / 别名 / 溯源；支持人工确认、修正、去噪（保证图谱质量）。
- **建索引**：对一个真实仓库跑"从代码建图"；对话与笔记同样并入。
- **回写闭环**：任务 / review 采纳后，把新学到的业务事实增量合并进图谱，持续长大。
- **辅助工具**：AI 辅助 git commit、多任务并行编排等（服从"懂业务"这一核心目标）。

---

## 技术栈

- **框架**：[Wails v2](https://wails.io)（Go 后端 + Web 前端）
- **前端**：Svelte 5 + Vite + TypeScript
- **后端**：Go 1.25（`GOTOOLCHAIN=auto` 自动拉取）
- **存储**：SQLite（WAL，即时落盘）；数据存于 `~/Library/Application Support/axon/`
- **密钥**：macOS Keychain，不落明文
- **模型接入**：OpenAI-compatible + Anthropic 原生，流式输出
- **向量**：云端 embedding（如 `text-embedding-3-small`），无 key 时本地词面兜底

### 后端模块（`internal/`）

| 模块 | 职责 |
| --- | --- |
| `codegraph` | 从代码仓库抽取结构骨架（文件 / 包 / 函数 / 类型 + 关系） |
| `graph` | 知识图谱模型：实体 / 关系 / 别名归一 / 溯源 / embedding |
| `retrieve` | App 无关的召回核心：HybridRAG 双通道 + RRF 融合 |
| `embed` | embedding 抽象接口（云端 + 本地兜底） |
| `claudedata` | 读取 Claude Code 会话数据 |
| `provider` | 各家 API 流式调用 |
| `secret` | Keychain 密钥存取 |
| `db` / `store` / `config` | 迁移 / 持久化 / 配置 |

---

## 从源码构建

前置：Go 1.25、Node、[Wails CLI](https://wails.io/docs/gettingstarted/installation)、CGO（Xcode Command Line Tools）。

```bash
# 实时开发（热重载）
wails dev

# 后端测试（CGO + race）
CGO_ENABLED=1 go test -race ./...

# 生产构建 -> build/bin/Axon-x.app
wails build -clean

# 单独编译 MCP server
go build -o axon-mcp ./cmd/axon-mcp

# 打包 dmg
bash scripts/make-dmg.sh
```

> 注意：编译产物 `axon` / `axon-mcp` 已在 `.gitignore` 中，不要提交进仓库。

---

## 数据与隐私

- 所有图谱、会话、配置存于 `~/Library/Application Support/axon/`，升级 / 重装不丢。
- API key **只存 macOS Keychain**，不落明文、不进上述目录。
- 单机单用户，无云同步、无多人协作。

---

## 已知边界

- 仅 macOS（Apple Silicon / Intel）。
- ad-hoc 签名（非 Apple 付费公证），首次打开需右键 → 打开。
- AST 级抽取目前以 Go 为主；其它语言走语言无关的目录 / import 层。
- 需自备各家 API key；语义召回的最佳精度依赖一个 OpenAI-compatible 的 embedding provider。

---

## 文档

设计与技术细节见 `docs/`：迭代计划（`ITERATION_PLAN.md`）、上下文引擎方案（`PLAN_CONTEXT_ENGINE.md`）、codegraph 技术设计（`TECH_CODEGRAPH.md`）、各能力的 PRD 等。

