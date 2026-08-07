# Axon

多模型 AI 桌面客户端(macOS)。把散落在终端里的 AI 对话,收拢成一个能记住你、帮你省钱、装得上传得出去的本地工作台。

## 解决什么

- **会话归档** — 每句对话即时落盘(SQLite WAL),关窗、崩溃、断电都不丢。
- **按任务自动选模型** — 输入内容启发式分类到任务类型,推荐最合适的模型并预估成本。
- **跨会话语义记忆** — 对过往会话生成摘要与向量,新提问时召回语义相关的历史,一键注入上下文。
- **直连各家 API** — OpenAI-compatible 与 Anthropic 原生,流式输出;API key 存 macOS Keychain,不落明文。

## 系统要求

- macOS(Apple Silicon / Intel)
- 自备各家 API key(在应用设置里配置)

## 安装(使用打包好的 .dmg)

1. 双击 `Axon-0.1.0.dmg`,把 `Axon.app` 拖到 `Applications`。
2. **首次打开**:因为使用 ad-hoc 签名(未做 Apple 付费公证),直接双击会被 Gatekeeper 拦。
   **右键点击 Axon → 选择「打开」→ 在弹窗里再点「打开」**。之后就能正常双击启动。
   > 命令行等效操作:`xattr -dr com.apple.quarantine /Applications/Axon.app`
3. 首次启动会引导你配置第一个 Provider 和 API key(存入 Keychain)。
   - 语义记忆需要一个 **OpenAI-compatible** 的 Provider(用于 embedding,如 `text-embedding-3-small`)。

## 数据位置

所有数据存于 `~/Library/Application Support/axon/`(SQLite 数据库 + 配置)。
升级/重装不影响该目录,历史不丢。API key 不在这里,只存 Keychain。

## 从源码开发

前置:Go(项目要求 1.25,已配 `GOTOOLCHAIN=auto` 自动拉取)、Node、Wails CLI、CGO(Xcode CLT)。

```bash
# 实时开发(热重载)
wails dev

# 后端测试(CGO + race)
CGO_ENABLED=1 go test -race ./...

# 生产构建 -> build/bin/Axon.app
wails build -clean

# 打包 dmg(在 build/bin/ 生成 Axon-<version>.dmg)
# 见 scripts/make-dmg.sh
```

## 架构

- Go 后端(`internal/`):`db`(SQLite+迁移)、`store`(归档持久化)、`provider`(流式 API)、
  `secret`(Keychain)、`config`、`routing`(任务→模型)、`embed`(向量);`app.go`/`memory.go` 是 Wails 绑定层。
- Svelte 前端(`frontend/src/`):四视图 —— 聊天 / 归档 / 记忆 / 设置。
- 详见 `docs/`(BRIEF / PRD / UX)。

## 已知边界(第一版)

仅 macOS;单机单用户,无云同步/多人协作;ad-hoc 签名(非 Apple 公证);
不做工具调用/多模态。分类器为启发式关键词匹配。
