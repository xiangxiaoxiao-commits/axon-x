# Axon 转向 — AI 辅助提交工具(事实源)

> 全体协作角色开工前先读本文件。不要推翻这里已确认的方向。

## 新定位(一句话)
把 axon 重构成一个**便捷、懂业务、多模型的 AI 辅助 git 提交工具**:读代码变更 + 项目业务背景 → 生成规范、贴合业务的 commit message(和 PR 描述),用户审阅微调后提交。

## 为什么"懂业务"是核心差异点
市面 AI commit 工具大多只看 diff,生成的信息干巴巴(如 "update handler")。axon 已经积累了**项目知识图谱**(模块/服务/概念、关系、决策、踩过的坑,还带别名归一和会话溯源)。把它作为**业务背景注入**给模型,生成的 commit 能用对业务术语、点明"为什么改",而不只是"改了什么"。这是我们相对通用工具的护城河。

## 核心流程(MVP)
1. 用户在某 git 仓库有改动(staged / unstaged)
2. axon 读取 diff(优先 staged;可选全部)
3. 组装:diff + 匹配到的项目业务知识(HybridRAG 从知识图谱召回相关实体/事实)
4. 发给用户选的模型(Claude / GPT / GLM)
5. 生成 commit message(遵循规范,见下),可含 PR 描述
6. 用户**审阅 + 可编辑** → 确认 → 执行 commit(不自动 push)

## 多模型支持(必须)
- **Claude**:Anthropic 原生协议(已有 provider)
- **GPT**:OpenAI 协议(已有 provider)
- **GLM(智谱)**:走 OpenAI 兼容端点(`https://open.bigmodel.cn/api/paas/v4`,模型如 glm-4.6/glm-4);技术研究确认后落地。可能复用 openai 协议 provider,只是 baseURL/模型不同 → 需验证兼容性。
- 用户能在设置里配多家 key、选默认模型、按需切换。

## 提交规范(来自用户全局 CLAUDE.md,必须遵守)
- 格式:`type(scope): description`
- type:feat / fix / refactor / docs / test / chore
- **不自动 push**;commit 前让用户确认 message
- PR 描述写清楚:改了什么、为什么改、怎么测试的
- commit message 用英文

## "便捷"的要求
- 步骤最少:打开 → 选仓库/自动识别当前仓库 → 一键生成 → 审阅 → 提交
- 生成要快,给"生成中"反馈
- 结果可编辑(不是只能接受/拒绝)
- 好看、简洁(延续现在的暖色极简 UI)

## 可复用的现有资产(在 /Users/xiangxiao/xx/axon)
- **provider 包**:OpenAI 兼容 + Anthropic 流式,错误脱敏、Keychain 存 key。GLM 可能直接复用。
- **config / secret**:多 provider 配置 + Keychain。
- **知识图谱(graph 包 + graphbuild.go)**:业务背景来源。MatchKnowledge(HybridRAG)可复用做"按 diff 召回相关业务知识"。
- **term 包**:内嵌 shell,可用于跑 git 命令。
- **claudedata / search**:历史会话知识(也是业务背景的一部分)。
- 前端:Wails + Svelte,神经中枢导航 + 暖色设计系统。

## 重构取向
- **提交工具成为主视图/主功能**(第一入口),知识图谱降为"业务背景引擎"在背后支撑 + 一个可选的知识浏览入口。
- 保留但弱化:纯粹的会话浏览/搜索等(作为辅助),别喧宾夺主。
- 具体保留/精简由 PRD + 项目经理规划确定。

## 工程约定
- Go 后端 + Svelte 前端;改 Go 必 `CGO_ENABLED=1 go build ./...` 通过;改导出方法要 `wails generate module` 重新生成绑定。
- 打包必须 `cd /Users/xiangxiao/xx/axon` 再 `wails build`,装完验证二进制时间戳更新;wails build 会重置图标,重装后重贴 `build/iconfile.icns`。
- 注释/commit/标识符英文,和用户沟通中文。
- 每阶段可独立验证;分工靠共享文档(docs/)传递。
