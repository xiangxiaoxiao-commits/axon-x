# Changelog

## v0.3.0 (2026-08-18)

### 新特性

- **命名空间机制**：用 `.axon-project` 文件声明项目命名空间，取代旧的路径编码 slug。每个项目根目录放一个文件（内容为命名空间名如 `gaia`），MCP 调用时自动路由。
- **多命名空间搜索**：`search_knowledge` 支持逗号分隔多命名空间（如 `"gaia,gaiac"`）、`"*"` 通配搜全部；省略时自动搜当前项目 + `_global_`。
- **`_global_` 命名空间**：存放跨项目平台级通用知识（Hippo 部署约束、CI 规范、K8s 集群信息等），所有项目查询时自动参与召回。
- **`delete_entity` MCP 工具**：从图谱中删除噪音/过期实体及其关系，支持别名匹配。
- **多 Agent 一键接入**：设置页支持同时接入 Claude Code、Codex、WorkBuddy、CodeBuddy，显示每个 Agent 的接入状态。
- **多 Agent 会话归档**：建索引时自动扫描 Claude Code、Codex、WorkBuddy、CodeBuddy 四种 Agent 的本地会话记录并蒸馏进图谱。
- **GUI 命名空间切换**：侧边栏项目选择器改为标签式，点击切换查看不同命名空间的图谱。

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
