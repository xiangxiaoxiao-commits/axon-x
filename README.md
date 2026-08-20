# Axon-x

> 让 AI 在动手前，先读懂你这个项目的**业务背景**和**代码结构**。

Axon-x 是一个跨平台桌面应用（macOS + Windows），它把散落在历史对话、代码仓库、Obsidian 笔记里的"隐性项目知识"，沉淀成**按命名空间隔离的知识图谱**，再通过 **MCP（Model Context Protocol）** 暴露给所有主流 AI 编码助手（Claude Code、OpenAI Codex、WorkBuddy、CodeBuddy）。AI 每次干活时，都能自动召回"这个项目以前的设计决策、踩过的坑、接口约定、业务约束"，而不是每次从零开始。

它的角色是**给 agent 用的上下文增强器**：你在 GUI 里养知识图谱，一键接入所有 Agent，agent 就懂你的项目。

一句话：**用得越多越懂你的项目。**

---

## 下载安装

最新版本在 **[Releases](https://github.com/xiangxiaoxiao-commits/axon-x/releases/latest)** 直接下载：

| 平台 | 下载 | 安装 |
| --- | --- | --- |
| **macOS**（Intel + Apple Silicon 通用） | `Axon-x-*.dmg` | 打开 dmg，把 Axon-x 拖进 Applications |
| **Windows** x64 | `Axon-x-*-windows-amd64.zip` | 解压到任意文件夹，双击 `Axon-x.exe` |
| **Windows** 32 位 | `Axon-x-*-windows-386.zip` | 同上 |
| **Windows** ARM | `Axon-x-*-windows-arm64.zip` | 同上 |

> **macOS 首次打开**：ad-hoc 签名，首次打开被拦时右键 → 打开，或 `xattr -dr com.apple.quarantine /Applications/Axon-x.app`。
>
> **Windows 首次打开**：`Axon-x.exe` 与 `axon-mcp.exe` 放同一文件夹；SmartScreen 点「更多信息」→「仍要运行」。

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

```
┌──────────────────────────────────────────────────────────────┐
│                        建图 (Ingest)                          │
│  代码仓库 ──┐                                                 │
│  历史对话 ──┤── 模型蒸馏 ──→ 实体 + 有向关系 + 原文片段        │
│  笔记/手动 ─┘              ↓                                  │
│                    graphcache/<namespace>/*.json               │
└──────────────────────────────┬───────────────────────────────┘
                               │
┌──────────────────────────────▼───────────────────────────────┐
│                       检索 (Recall)                            │
│                                                               │
│  query ──→ ┌─ 结构通道: embedding cosine → 语义种子            │
│            │              ↓                                    │
│            │         正向关系扩展 (A→B only)                    │
│            │              ↓                                    │
│            │         RRF 融合 ────────────→ 命中实体集          │
│            │                                                  │
│            └─ 原文通道: chunk embedding → top-K 片段            │
│                              ↓                                │
│                         RRF 融合 ────────→ 排序片段            │
└──────────────────────────────┬───────────────────────────────┘
                               │
┌──────────────────────────────▼───────────────────────────────┐
│                       注入 (Inject)                            │
│                                                               │
│  MCP stdio server ←── Agent 调用 search_knowledge             │
│       ↓                                                       │
│  返回: 实体事实(带溯源) + 原文片段(带来源) → Agent 上下文       │
│       ↓                                                       │
│  Agent 学到新知识 → remember_knowledge → 回写图谱 (闭环)       │
└──────────────────────────────────────────────────────────────┘
```

### 建图（Ingest）

从四类来源抽取知识，合并进按命名空间隔离的图谱：

| 来源 | 处理方式 | 产出 |
|------|---------|------|
| 代码仓库 | 静态分析（目录/包/import + Go AST 函数/类型/调用） | 代码结构实体 + 依赖关系 |
| 历史对话 | 模型蒸馏（发给 LLM 提取实体/关系/事实） | 业务决策、约束、接口约定 |
| Obsidian 笔记 | chunk + embed + wikilink 解析 | 笔记知识 + 链接关系 |
| MCP 实时写入 | agent 调 `remember_knowledge` 直接写 | 即时沉淀的新知识 |

每个来源产出的实体通过**别名归一**合并：`OrderService`、`订单服务`、`order-service` 会折叠为同一个节点。

### 检索（Recall）

采用 HybridRAG 双通道并行召回：

- **结构通道**：query embedding 与实体 embedding 做 cosine 相似度，取 top-K 语义种子 → 沿**正向**有向关系扩展 1 跳（A→B，不反向）→ 与关键词命中实体做 RRF 融合
- **原文通道**：query embedding 与 chunk embedding 做 cosine → 与关键词命中 chunk 做 RRF 融合 → top-N 片段

两个通道独立打分，最终合并返回"相关实体事实 + 原文片段"。

### 注入（Inject）

通过 stdio MCP server 暴露给 Agent。Agent 发起 `search_knowledge` 调用 → server 执行双通道召回 → 返回结构化结果。Agent 在对话中学到新知识后调 `remember_knowledge` 回写 → 图谱增长 → 下次召回能查到 → **闭环**。

---

## 命名空间

知识图谱按**命名空间**隔离管理，每个命名空间对应一个逻辑项目。

在项目根目录放一个 `.axon-project` 文件（内容为命名空间名），即可声明归属：

```bash
echo "wanlian" > ~/projects/monorepo-gaia/.axon-project
echo "wanlian" > ~/projects/mono-glite/.axon-project
echo "axon" > ~/projects/axon/.axon-project
```

- 同一命名空间可以对应多个目录（业务耦合深的多仓库用同一个命名空间，如万联业务域下的所有仓库统一用 `wanlian`）
- `_global_` 命名空间存放跨项目的平台通用知识（部署规范、CI 约束等）
- `search_knowledge` 默认搜索所有命名空间；可传 `project` 参数限定范围
- `remember_knowledge` 写入时从 `.axon-project` 自动路由到正确命名空间

---

## 接入 AI Agent（MCP）

在 **设置 → 接入 AI Agent** 里点**「一键接入所有 Agent」**即可。

| Agent | 配置文件 | 格式 |
|-------|---------|------|
| Claude Code | `~/.claude.json` | JSON |
| OpenAI Codex | `~/.codex/config.toml`（支持 `CODEX_HOME` 环境变量） | TOML |
| WorkBuddy | `~/.workbuddy/.mcp.json` | JSON |
| CodeBuddy | `~/.codebuddy/.mcp.json` | JSON |

也可以手动注册：

```bash
# Claude Code
claude mcp add axon-knowledge -s user /path/to/axon-mcp

# OpenAI Codex
codex mcp add axon-knowledge -- /path/to/axon-mcp

# WorkBuddy / CodeBuddy: 编辑 .mcp.json，添加 mcpServers.axon-knowledge
```

### MCP 工具集（9 个）

| 工具 | 作用 |
| --- | --- |
| `project_overview` | 拿到当前项目的知识骨架（核心模块 / 关键决策 / 约束）。冷启动首选 |
| `search_knowledge` | 自然语言查询，返回相关实体、事实与原文片段。默认搜全部命名空间 |
| `get_entity` | 查看某实体的全部事实 + 关系 + 别名 |
| `remember_knowledge` | 写入持久知识（实体/有向关系），别名归一并入已有节点 |
| `delete_entity` | 删除噪音/过期实体及其关系 |
| `set_scope` | 锁定本次会话到指定命名空间，后续操作只在范围内 |
| `clear_scope` | 清除范围锁定，恢复搜索全部 |
| `move_entity` | 跨命名空间移动实体，纠正归属 |
| `list_projects` | 列出所有命名空间及实体数 |

### 关系方向性

所有关系是有向的：`From —Label→ To`（From 是主语，To 是宾语，Label 是动词）。召回时只沿正向扩展，不会反向污染无关知识。

### 让图谱越用越全

1. **让 agent 主动查/写/改** — 在项目指令文件（CLAUDE.md / Codex instructions / WorkBuddy rules）里加以下内容：

   ```markdown
   ## 项目知识图谱（axon-knowledge MCP）

   ### 查（动手前）

   用 search_knowledge 查相关知识，query 用模块名、功能关键词或接口名。

   - 图谱是参考，与代码冲突时以代码为准。⚠️标记（>90天）需验证时效性。
   - 查无结果不代表没约束，正常继续。

   ### 写（结论敲定后立即写，不攒不拖）

   触发时机：
   - "用 X 方案（因为 Y，放弃 Z）"→ 写
   - "这里不能 W（因为会导致 V）"→ 写
   - "A 调 B 的接口是 /path，字段含义是 ..."→ 写

   规范：
   - 一实体一概念（判断：两条事实如果会独立过时就拆开）
   - observations 用断言：`已决定X（理由：Y）`/`必须X`/`不能X（因为Y）`
   - 禁止：可能/大概/也许/好像
   - 关系有方向，读成通顺的话：from=主语 —label(动词)→ to=宾语
     - ✅ `gaia-lite —部署于→ sit-14`
     - ❌ `sit-14 —部署→ gaia-lite`
   - 给别名：`aliases: ["OrderService", "订单服务"]`

   不写：pipeline/镜像tag/commit/部署状态（自动过滤）、临时调试、代码里能看到的事实、
   通用技术常识、讨论中间态。

   ### 改（发现过时知识）

   delete_entity 删旧的 → remember_knowledge 写修正后的完整版本。不追加矛盾 observation。
   归属错误用 move_entity 迁移。

   ### 范围

   通常不需要操作。用户明确说"只关注 X"时 set_scope(["wanlian"])，完成后 clear_scope。

   ### 原则

   只沉淀"代码里看不出来、忘了会重复踩坑"的知识。宁少而准。
   ```

2. **定期建索引** — 增量蒸馏新会话，不重复烧 token。

3. **人工校正** — 删噪音、纠错、合并别名。**图谱质量 > 数量。**

---

## 桌面端功能

界面四个入口——**知识**、**会话**、**终端**、**设置**：

- **知识图谱**：力导向可视化，按命名空间切换查看；支持节点交互、事实编辑、实体删除。
- **建索引**：从代码建图 / 从历史对话蒸馏 / 吸收 Obsidian。支持 Claude Code、Codex、WorkBuddy、CodeBuddy 四种 Agent 的会话。
- **会话浏览**：所有 Agent 的历史会话按工作目录分组展示，带 model 标签（Opus / GPT / WorkBuddy 等）；详情页展示最后进度；点「▶ 恢复」在内置终端新开标签恢复会话。
- **会话溯源与知识剔除**：看到每个会话产出了哪些知识；对噪音知识「✕ 剔除」，重建索引也不会回来。
- **多标签内置终端**：PTY 驱动的真实 shell，多个会话在 app 内并排跑。
- **设置**：配置 Provider、Embedding、一键接入所有 Agent。

---

## 快速上手

1. **配 Embedding Provider**（设置 → 召回方式 → 语义模型）。推荐 `text-embedding-3-small` 或 `embedding-3`。
2. **创建 `.axon-project`**：在项目根目录写入命名空间名。
3. **建索引**：知识视图 → 「+ 知识来源」，从代码/对话/笔记建图。
4. **一键接入**：设置 → 接入 AI Agent → 一键接入所有 Agent。
5. **在 Agent 指令里加查询提示**：让 agent 动手前先查知识图谱。

---

## 技术栈

- **框架**：[Wails v2](https://wails.io)（Go 后端 + Web 前端）
- **前端**：Svelte 5 + Vite + TypeScript
- **后端**：Go 1.25
- **存储**：SQLite 纯 Go 驱动（无 cgo）
- **数据目录**：macOS `~/Library/Application Support/axon/`，Windows `%AppData%\axon\`
- **密钥**：macOS Keychain / Windows Credential Manager
- **模型接入**：OpenAI-compatible + Anthropic 原生
- **向量**：云端 embedding，无 key 时本地词面兜底

### 后端模块

| 模块 | 职责 |
| --- | --- |
| `graph` | 知识图谱模型：实体 / 有向关系 / 别名归一 / 溯源 / embedding |
| `retrieve` | 召回核心：HybridRAG 双通道 + RRF 融合 + 正向关系扩展 |
| `claudedata` | 读取 Claude Code / Codex / WorkBuddy / CodeBuddy 会话数据 |
| `codegraph` | 从代码仓库抽取结构骨架 |
| `embed` | embedding 接口（云端 + 本地兜底） |
| `mcpinstall` | 多 Agent 一键接入（JSON/TOML 配置读写） |
| `provider` | 各家 API 流式调用 |
| `secret` | OS 凭证库密钥存取 |

---

## 从源码构建

```bash
# 实时开发
wails dev

# 生产构建
wails build -clean

# 编译 MCP server
go build -o build/bin/axon-mcp ./cmd/axon-mcp          # macOS/Linux
GOOS=windows go build -o build/bin/axon-mcp.exe ./cmd/axon-mcp  # Windows

# 测试
go test ./...
```

---

## 数据与隐私

- 所有图谱、会话、配置存于用户级数据目录，升级不丢。
- API key **只存 OS 凭证库**，不落明文。
- 单机单用户，无云同步、无多人协作。
