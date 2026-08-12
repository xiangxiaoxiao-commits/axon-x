# Axon 产品需求文档 —— AI 辅助 git 提交工具 (PRD_COMMIT)

> 事实源:`docs/BRIEF_COMMIT.md`。本 PRD 在其已确认方向上细化到可开发、可测试的粒度。
> 架构与排期由架构师/项目经理最终裁定;本文的"重构范围"只给建议。

---

## 1. 产品概述

Axon 是一个**桌面端、懂业务、多模型的 AI 辅助 git 提交工具**。它读取你当前仓库的代码变更(diff),结合从**项目知识图谱**召回的业务背景,调用你选定的大模型(Claude / GPT / GLM),一键生成**规范且贴合业务**的 commit message(及可选的 PR 描述);你审阅、随手微调后确认提交。全程不自动 push。

- **给谁用**:重度使用 git + AI 的工程师(主语言 Java/Go,重视工程质量与提交规范)。
- **解决什么**:手写 commit message 费神且容易写成流水账;通用 AI commit 工具只看 diff,产出"update handler"这类干瘪信息,说不清"为什么改"、用不对业务术语。
- **差异点(护城河)**:Axon 已沉淀了每个项目的**知识图谱**(模块/服务/概念、关系、决策、踩过的坑,带别名归一与会话溯源)。生成时把与本次 diff 相关的业务知识作为背景注入模型,commit message 能用对业务名词、点明变更动机,而不只是罗列改了哪些文件。相较 aicommits / opencommit 等"纯 diff"工具,这是"懂业务"的关键。
- **形态**:延续现有 Wails(Go 后端)+ Svelte(前端)桌面应用与暖色极简 UI,复用已有的 provider / config / secret / graph 资产。

---

## 2. 用户画像与核心场景

### 2.1 主用户 —「阿轩」资深后端工程师(P0,产品为他而造)
- 每天多次提交,在意 commit 历史的可读性,团队要求 `type(scope): description` 规范。
- 常在多个项目/微服务间切换,记不住每个模块的业务上下文。
- 手上配了多家模型 key,想按场景切换(日常用便宜快的,复杂改动用强模型)。
- 讨厌被打断:希望"改完代码 → 生成 → 扫一眼微调 → 提交"一气呵成,步骤越少越好。

### 2.2 核心场景(主线)
改完代码 → 打开 Axon(或它已停在当前仓库)→ 选提交范围 → 一键生成规范且贴业务的 commit message → 审阅并微调 → 确认提交(不 push)。

### 2.3 用户故事

1. 作为工程师,我想让工具自动识别我当前所在的 git 仓库并展示改动状态,以便我不用手动指定路径就能开始。
2. 作为工程师,我想选择本次提交的范围(仅 staged / 全部改动 / 勾选具体文件),以便一次提交只包含我想要的内容。
3. 作为工程师,我想一键根据 diff + 项目业务背景生成规范的 commit message,以便不用自己组织措辞就能写出说清"为什么改"的提交。
4. 作为工程师,我想在提交前直接编辑生成的 message,以便修正模型的措辞或补充细节,而不是只能全盘接受或拒绝。
5. 作为工程师,我想按需切换用于生成的模型(Claude/GPT/GLM),以便根据改动复杂度权衡质量与成本。
6. 作为工程师,我想在确认后由工具执行 `git commit`(绝不自动 push),以便我始终掌控代码何时进入远端。
7. 作为工程师(P1),我想为一组改动生成 PR 描述(改了什么/为什么/怎么测),以便快速发起评审。
8. 作为工程师,我想配置多家模型的 key、设默认模型和生成偏好(语言/emoji/长度),以便工具产出符合我团队规范的结果。

---

## 3. 功能需求(按优先级)

> 图例:P0 = MVP 必做;P1 = 首版之后紧接着做;P2 = 可延后。
> 每条给出"用户点什么 / 看到什么",并标注可复用的现有资产。

### F1 仓库识别与 diff 读取 (P0)
- **F1.1 自动识别当前仓库**:应用启动时探测工作目录/最近使用目录是否为 git 仓库;是则直接展示。用户也可手动选择/切换仓库目录。列出最近使用的仓库便于切换。
- **F1.2 展示仓库状态**:显示当前分支、staged / unstaged / untracked 文件清单及每个文件的增删行数。
- **F1.3 选择提交范围**:提供三档 —— 仅已暂存(staged,默认)/ 全部改动(自动 `git add -A` 语义,提交前确认)/ 勾选具体文件。切换范围时 diff 预览与后续生成随之更新。
- **F1.4 diff 读取**:读取所选范围的 diff 文本用于生成。大 diff 需截断/摘要(见 NFR 性能),并在 UI 提示"diff 过大,已截断/仅取摘要"。
- **F1.5 二进制/超大文件处理**:二进制文件与超大文件不纳入 diff 正文,仅以"文件名 + 变更类型"列出。
- *复用*:`term`(跑 git 命令)。**需新增**:结构化 git 封装(status/diff/commit 解析),不要只靠拼终端输出。

### F2 业务背景注入 (P0,核心差异点)
- **F2.1 按 diff 召回业务知识**:从本次 diff 抽取信号(改动的文件路径、包/类/函数名、关键标识符),用其调用 `MatchKnowledge`(HybridRAG:语义 seed + 关系扩展 + 子串兜底)召回相关实体/事实/关系。
- **F2.2 组装注入上下文**:把召回的业务知识 + diff 组装成发给模型的 prompt;业务背景作为 system 段注入(复用现有 `SendMessage` 的 injectContext 思路)。
- **F2.3 背景可见与可关闭**:生成结果旁展示"参考了哪些业务背景"(实体名 + 会话溯源来源),让用户知道 message 依据何在;并提供开关,可关闭注入做纯 diff 生成对照。
- **F2.4 优雅降级**:无 embedder(未配 OpenAI 兼容 key)或项目无图谱时,自动退化为纯 diff 生成,不报错、不阻塞。
- *复用*:`graph` 包、`graphbuild.go` 的 `MatchKnowledge` / `assembleGraph` / `semanticSeeds` / `expandAlongRelations`;`embed` 包。

### F3 多模型生成 (P0)
- **F3.1 provider 配置**:用户在设置里配置多家 provider(名称、协议 openai/anthropic、baseURL、key)。key 存 Keychain,永不落配置文件。
- **F3.2 GLM 接入**:GLM(智谱)走 OpenAI 兼容端点(`https://open.bigmodel.cn/api/paas/v4`,模型如 glm-4.6/glm-4),复用 openai 协议 provider,仅 baseURL/模型不同。**落地前需技术验证兼容性**(流式、models 列表、错误格式)。
- **F3.3 生成时选模型**:主界面可直接选择本次生成用的 provider + 模型;缺省用默认模型。切换后重新生成即可。
- **F3.4 流式反馈**:生成过程流式输出到结果区,并有"生成中"状态与可取消(Stop)。
- **F3.5 错误脱敏**:任何模型错误提示都不得泄露 key 或内部细节(复用 provider 层已有脱敏)。
- *复用*:`provider`(OpenAI/Anthropic 流式)、`config`、`secret`、`app.go` 的 `newProvider` / `runStream` / `StopGeneration`。

### F4 commit message 生成 (P0)
- **F4.1 规范遵循**:严格产出 `type(scope): description`;type 取值 feat/fix/refactor/docs/test/chore;description 用英文;必要时生成多行 body 说明"为什么改"。
- **F4.2 scope 推断**:结合改动文件路径与业务图谱推断 scope(如模块/服务名),而非留空或瞎填。
- **F4.3 生成偏好生效**:遵循设置里的语言(body 语言可选)、是否带 emoji(如 Gitmoji,默认关)、message 长度上限。
- **F4.4 多候选(可选增强,P1)**:一次生成 2-3 个候选供选择。首版可只出 1 个。
- *复用*:provider + 注入上下文;prompt 模板为**新增**。

### F5 审阅与编辑 (P0)
- **F5.1 可编辑文本框**:生成结果落入一个可编辑区(subject 行 + body 分离更佳),用户可任意修改。
- **F5.2 规范校验提示**:实时提示是否符合 `type(scope): description`(如 subject 超长、type 非法、缺冒号),仅提示不强制拦截。
- **F5.3 重新生成**:一键用当前范围/模型重新生成(可换模型再生成);编辑中的内容重生成前给出覆盖确认。
- **F5.4 不是接受/拒绝二选一**:明确以"编辑后提交"为主路径。

### F6 执行提交 (P0)
- **F6.1 提交动作**:用户确认后执行 `git commit`,使用编辑区最终文本作为 message。范围为"勾选文件/全部"时,提交前先 stage 对应文件。
- **F6.2 绝不自动 push / 不 force**:提交后停在本地;不执行任何 push、force、reset --hard 等破坏性操作。
- **F6.3 提交结果反馈**:显示新 commit 的 hash、message、包含文件数;失败(如无改动、pre-commit hook 拒绝、冲突)给出清晰错误与原状态。
- **F6.4 保留 hook**:默认不加 `--no-verify`,尊重项目 git hook。
- *复用*:`term` / 新增结构化 git 封装。

### F7 PR 描述生成 (P1)
- **F7.1 生成 PR 描述**:基于一组 commit / 分支相对 base 的 diff,生成含"改了什么 / 为什么改 / 怎么测试"三段的 PR 描述,注入业务背景。
- **F7.2 可编辑 + 复制**:结果可编辑、一键复制到剪贴板。首版不直接调 gh/glab 创建 PR(见边界)。

### F8 便捷性 (P0 贯穿)
- **F8.1 最少步骤**:默认路径 = 打开(自动停在当前仓库)→ 一键生成 → 微调 → 提交,理想 3 次点击内完成。
- **F8.2 生成中反馈**:流式输出 + loading 态 + 可取消。
- **F8.3 快捷键**:生成(如 Cmd+Enter)、提交(如 Cmd+Shift+Enter)、切换范围、重新生成;快捷键在界面可见/可查。

### F9 设置 (P0)
- **F9.1 模型/key 管理**:增删改 provider,输入/更新 key(写 Keychain),展示 key 是否已配置(不回显明文)。
- **F9.2 默认模型**:设默认 provider + model。
- **F9.3 生成偏好**:body 语言、是否带 emoji(默认关)、message 长度上限、是否默认注入业务背景、默认提交范围。
- *复用*:`config`、`secret`、现有 SettingsView / ProviderForm / SecretInput 组件。

---

## 4. 主界面设想(提交工具作为第一入口)

打开应用后**直接进入"提交视图"**(不再先落在神经中枢导航)。单屏、左右布局,暖色极简:

- **顶部条**:当前仓库名 + 分支(可点击切换仓库/分支只读展示);右侧为模型选择器(provider + model 下拉)与设置入口。
- **左栏 —— 变更区**:
  - 提交范围切换(仅 staged / 全部 / 自定义)。
  - 文件列表:文件名、变更类型(A/M/D)、增删行数、勾选框(自定义范围时可勾)。
  - 点击文件在右栏预览其 diff。
- **右栏(上)—— diff 预览**:所选文件/范围的 diff,语法高亮,超大截断提示。
- **右栏(中)—— 生成区**:醒目的"生成 commit message"主按钮;下方一行显示"将参考的业务背景"(命中的实体标签,可点开看详情与会话来源,可开关注入)。
- **右栏(下)—— 结果编辑区**:可编辑的 message(subject + body),实时规范校验提示;旁边有"重新生成""复制"。生成时此区流式填充并显示生成中。
- **底部操作条**:主按钮"提交"(不 push),旁注当前范围包含 N 个文件;可选"生成 PR 描述"(P1)。
- **辅助入口**:知识图谱浏览、历史会话搜索、记忆管理、终端等作为**次级入口**(顶部菜单或折叠侧栏),默认不占主视图。神经中枢导航降级为可选入口而非启动首屏。

---

## 5. 验收标准(P0 功能,可测)

### AC-F1 仓库识别与 diff 读取
- Given 工作目录是一个含改动的 git 仓库,When 打开应用,Then 自动展示该仓库、当前分支与文件变更清单(区分 staged/unstaged/untracked)。
- Given 目录不是 git 仓库,When 打开,Then 提示"非 git 仓库"并允许手动选择仓库,不崩溃。
- Given 范围切到"仅 staged",When 无已暂存文件,Then 提示"没有已暂存的改动",生成按钮禁用。
- Given 一个二进制/超大文件改动,When 查看 diff,Then 仅列文件名与变更类型,不加载其正文。

### AC-F2 业务背景注入
- Given 项目已有知识图谱且 diff 触及某已知模块,When 生成,Then "参考的业务背景"区展示至少 1 个命中实体及其来源。
- Given 未配置 embedder 或项目无图谱,When 生成,Then 走纯 diff 生成、正常出结果、无报错。
- Given 关闭"注入业务背景"开关,When 生成,Then prompt 不含业务背景段(可由生成结果/日志验证)。

### AC-F3 多模型生成
- Given 已配置 Claude 与一个 OpenAI 兼容 provider,When 切换模型并生成,Then 使用所选模型且结果流式返回。
- Given GLM 按 OpenAI 兼容端点配置,When 生成,Then 能正常返回结果(兼容性通过技术验证)。
- Given 生成进行中,When 点击 Stop,Then 生成停止、已产出的文本保留、界面回到可操作态。
- Given key 错误或网络失败,When 生成,Then 显示可读错误且不含 key/内部堆栈。

### AC-F4 commit message 生成
- Given 一次正常改动,When 生成,Then 结果首行匹配 `type(scope): description`,type 属于 {feat,fix,refactor,docs,test,chore},description 为英文。
- Given 偏好设为"带 emoji 关闭 / subject 上限 72",When 生成,Then 结果不含 emoji 且 subject 不超过上限。

### AC-F5 审阅与编辑
- Given 已生成 message,When 用户在编辑区修改文本,Then 修改被保留并用于后续提交。
- Given 编辑后的 subject 缺少冒号/type 非法,When 编辑,Then 出现规范提示,但仍允许用户选择提交(不硬拦截)。
- Given 已有编辑内容,When 点"重新生成",Then 先确认是否覆盖,确认后用当前范围/模型重生。

### AC-F6 执行提交
- Given 编辑区有合法 message 且范围有内容,When 点"提交",Then 执行 `git commit`,成功后显示新 commit hash 与包含文件数,工作区状态刷新。
- Given 提交完成,Then 不发生任何 push/force;远端无变化。
- Given pre-commit hook 拒绝或无改动,When 提交,Then 显示清晰失败原因,仓库状态不被破坏。
- Given 范围为"勾选文件",When 提交,Then 仅所勾文件进入本次 commit。

### AC-F8 便捷性
- Given 当前仓库有已暂存改动且已配默认模型,When 依次执行 生成→提交,Then 主路径可在 3 次点击内完成。
- Given 生成中,Then 有明确的"生成中"反馈且按钮防重复触发。

### AC-F9 设置
- Given 新增 provider 并填 key,When 保存,Then key 写入 Keychain,列表显示"已配置 key"但不回显明文。
- Given 设了默认 provider/model,When 重开应用,Then 生成默认使用该模型。

---

## 6. 重构范围建议(仅建议,不下最终结论)

原则:让提交工具当主角,知识图谱退居"业务背景引擎"+ 可选浏览入口,砍掉与提交主线无关的重前端。

| 现有功能 | 建议 | 理由 |
|---|---|---|
| provider / config / secret | **保留(核心复用)** | 多模型生成直接依赖。 |
| graph + graphbuild(MatchKnowledge/HybridRAG) | **保留但转为后台引擎** | 从"聊天注入"改为"按 diff 召回"的业务背景来源,是差异点。 |
| embed | **保留** | HybridRAG 语义召回所需。 |
| term(PTY shell) | **弱化/按需保留** | 提交可用结构化 git 封装而非 PTY;终端作为次级"高级"入口即可,非主路径。 |
| SessionsView(会话浏览)/ SearchView(搜索)/ claudedata | **弱化为辅助入口** | 是业务背景的数据来源之一,但不该占主视图;保留最小浏览/检索能力。 |
| MemoryManagerView(记忆管理) | **弱化或延后** | 与提交主线弱相关;可作为知识浏览的一部分,首版不主推。 |
| GraphView(图谱可视化) | **弱化为可选入口** | 让用户能查看/信任业务背景来源即可,不必做重可视化。 |
| NeuralHub(神经中枢导航) | **降级** | 不再作为启动首屏;可作为"更多功能"入口或直接移除,启动直达提交视图。 |
| ReplView(聊天) / SendMessage 聊天链路 | **弱化/复用底层** | 通用聊天不是本产品定位;底层 provider/流式复用到提交生成,聊天 UI 可移除或降为次级。 |
| SQLite 归档(db/store) | **保留(降载)** | 仍用于会话数据/业务背景;首版不主推归档浏览体验。 |

**需新增**:结构化 git 封装(仓库识别、status、范围化 diff、commit、结果解析),这是当前代码缺口(现只有 PTY shell,无 diff 解析)。

---

## 7. 非功能需求

### 7.1 性能
- 生成首字节(diff 已就绪时)目标 P50 < 3s、P95 < 8s(取决于模型),流式尽快出首 token。
- diff 读取与业务背景召回(缓存命中)应"近即时"(< 500ms 量级);超大 diff 需截断/摘要,避免超模型上下文与卡顿。
- 大仓库 status/diff 读取不阻塞 UI(后台执行 + loading)。

### 7.2 安全
- **key 只存 macOS Keychain**,永不写配置文件、日志、导出;错误提示脱敏(复用 provider 层)。
- **diff 泄密防护**:发送前对 diff 做敏感信息扫描(常见密钥/token 模式),命中则提示并允许用户剔除该文件/片段后再生成;至少在 UI 明确告知"diff 将发送给所选模型"。
- 提交范围与 stage 行为对用户透明,提交前可见将包含哪些文件。
- 外发数据仅限用户主动发起的模型生成请求;不向第三方端点上传代码/密钥。

### 7.3 不破坏 git 状态
- 只在用户显式确认后执行 `git commit`;**绝不自动 push、force、reset --hard、clean -f、branch -D**。
- 提交失败时保持仓库原状态,不做隐式修复。
- 默认保留 git hook(不加 `--no-verify`),除非用户显式选择跳过。
- 不修改 git config、不切换分支(首版)。

### 7.4 工程质量(承接用户偏好)
- Go 改动须 `CGO_ENABLED=1 go build ./...` 通过;改导出方法须 `wails generate module` 重生成绑定。
- 关键路径(git 封装、召回、生成、提交)有单元测试;git 提交流程用临时仓库做集成测试。

---

## 8. 明确边界(第一版不做)

1. **不自动 push,不创建远端 PR/MR**(不集成 gh/glab 创建动作);PR 描述仅生成文本供复制。
2. **不做多仓库批量提交 / monorepo 拆分提交**。
3. **不做 commit 拆分建议 / 交互式 rebase / squash / amend 编排**(amend 非主路径)。
4. **不做本地模型 / Ollama 接入**(仅 API provider;embedder 抽象保留但不落地本地)。
5. **不做团队协作、云同步、账号体系**。
6. **不做 Windows/Linux 打包保证**(首版聚焦 macOS,Keychain/签名为 macOS 路径)。
7. **不做知识图谱的重编辑/可视化增强**(仅保留只读浏览与来源溯源)。
8. **不做提交历史分析、changelog / release notes 生成**(可作为后续)。

