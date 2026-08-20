# Changelog

## v0.4.0 (2026-08-19)

### 搜索精度优化

- **Stop words 过滤**：通用动词（更新/修改/处理/使用等）不再作为关键词匹配，大幅减少无关命中
- **Chunk 匹配门槛提升**：从"命中任意一个词"改为"至少命中一半 query 词"
- **短实体名跳过**：名字 < 3 字符的实体不参与关键词匹配
- **输出总量上限 8000 字符**：超出截断并提示缩小范围

### 知识归属优化

- **业务域合并**：gaia/gaiac/glite 合为统一的 `wanlian` 命名空间，解决跨项目归属错误
- 业务耦合深的多仓库共用一个命名空间，不再强拆

### 写入质量把关

- **快照自动拦截**：remember_knowledge 写入时检测并过滤 pipeline ID、镜像 tag、commit hash、部署状态等一次性记录
- **模糊措辞警告**：含"可能/大概/也许"时提示改为确定性陈述
- **关系方向约束**：from=主语, to=宾语, label=动词，强制有向语义

### 输出准确性

- **免责前缀**：搜索结果标注"仅供参考，以代码为准"
- **时效标注**：MCP 写入的知识显示"N天前"，超 90 天加 ⚠️ 警示
- **正向关系扩展**：只沿 A→B 方向拉入关联实体，不反向污染

### 数据清洗

- 删除 359 条快照 observation（pipeline ID、镜像 tag 等）
- 删除 20 个纯快照实体
- 49 条"部署"关系重命名为"承载"（方向明确化）
- 清理旧路径编码命名空间残留目录

---

## v0.3.1 (2026-08-18)

### 新特性

- **会话级范围锁定**：新增 `set_scope`/`clear_scope` MCP 工具，可在会话中锁定只操作指定命名空间，用完恢复全量搜索。
- **跨命名空间实体移动**：新增 `move_entity` MCP 工具，把误放的实体从一个命名空间移到另一个。
- **默认搜索全量**：`search_knowledge` 省略 `project` 时搜索所有命名空间，确保信息完整不遗漏。
- **多 Agent 会话浏览**：会话列表同时展示 Claude Code、Codex、WorkBuddy、CodeBuddy 的会话，按工作目录分组。
- **多 Agent 一键接入 UI**：设置页展示 4 个 Agent 状态，一键全部注册。

### 修复

- **关系方向性**：召回扩展改为只沿正向（From→To），不再反向拉入无关实体。修复了"搜 glite 误拉出客户仓"类问题。
- **关系写入约束**：`remember_knowledge` 的 relations 描述强化方向性语义（from=主语, to=宾语, label=动词）。
- **49 条"部署"关系**：label 从语义模糊的"部署"改为方向明确的"承载"。
- **MCP type=stdio**：修复 installJSON 写入空 type 导致 Claude Code 拒绝连接。
- **会话恢复分派**：按 agent 类型生成正确的 resume 命令（claude/codex/cd only）。
- **终端 unicode11 addon**：try-catch 保护，避免 addon 加载失败导致终端无法启动。
- **Windows 兼容**：resume 命令使用平台感知的引号、cd /d、链接符。

### 改进

- 会话列表按工作目录分组展示，不再按时间分桶。
- `delete_entity` 工具支持从图谱中删除噪音实体。
- WorkBuddy 会话解析修复 timestamp 数字类型和 `<user_query>` 标签提取。

---

## v0.3.0 (2026-08-18)

### 新特性

- **命名空间机制**：用 `.axon-project` 文件声明项目命名空间，取代旧的路径编码 slug。每个项目根目录放一个文件（内容为命名空间名如 `gaia`），MCP 调用时自动路由。
- **多命名空间搜索**：`search_knowledge` 支持逗号分隔多命名空间（如 `"gaia,gaiac"`）、`"*"` 通配搜全部；省略时自动搜当前项目 + `_global_`。
- **`_global_` 命名空间**：存放跨项目平台级通用知识（Hippo 部署约束、CI 规范、K8s 集群信息等），所有项目查询时自动参与召回。
- **`delete_entity` MCP 工具**：从图谱中删除噪音/过期实体及其关系，支持别名匹配。
- **多 Agent 一键接入**：设置页支持同时接入 Claude Code、Codex、WorkBuddy、CodeBuddy，显示每个 Agent 的接入状态。
- **多 Agent 会话归档**：建索引时自动扫描 Claude Code、Codex、WorkBuddy、CodeBuddy 四种 Agent 的本地会话记录并蒸馏进图谱。
- **GUI 命名空间切换**：侧边栏项目选择器改为标签式，点击切换查看不同命名空间的图谱。
- **会话按目录分组**：会话浏览页从按时间分桶改为按工作目录分组，所有 Agent 的会话按项目目录归类展示。
- **多 Agent 会话浏览**：会话列表同时展示 Claude Code、Codex、WorkBuddy、CodeBuddy 的会话，带 model 标签区分来源。

### 修复

- **Windows NTFS 兼容**：cache 文件名中的冒号（如 `mcp:xxx`）替换为下划线，修复 Windows 上 `os.Rename` 失败。
- **命名空间索引失败**：`_global_` 等命名空间没有对应的 Claude Code session 目录时不再报错，优雅跳过。
- **终端 CJK 对齐**：加载 xterm.js unicode11 addon，修复中文和 box-drawing 字符宽度计算错误。
- **WorkBuddy 解析**：修复 `timestamp` 字段为数字类型导致 JSON 反序列化失败的问题。

### 改进

- `remember_knowledge` 描述强调知识归属方向，防止跨项目误写。
- 支持 `CODEX_HOME` 环境变量自定义 Codex 数据目录。
- 各 Agent 配置路径全部使用 `os.UserHomeDir()` + `filepath.Join()`，Windows/macOS/Linux 通用。

---

## v0.2.0 (2026-08-17)

- 知识图谱可视化（力导向布局 + 节点交互）
- HybridRAG 双通道召回（语义 + 关键词 + RRF 融合）
- MCP server 一键接入 Claude Code
- 会话溯源与知识剔除
- 从代码建图 / 吸收 Obsidian
- 内置多标签终端
- 实体编辑与删除
