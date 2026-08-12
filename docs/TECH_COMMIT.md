# Axon 提交工具 — 技术预研(GLM 接入 / git 集成 / 业务知识注入)

> 面向 Go 落地。基于现有代码(`internal/provider`、`internal/config`、`internal/secret`、`internal/term`、`graphbuild.go`)与 GLM 官方文档实测确认。

---

## 一、GLM(智谱)接入

### 1.1 结论(先给判断)

**GLM 可以直接复用现有的 `provider.NewOpenAI`,不需要单独写 GLM provider。**

GLM 的 v4 开放平台提供**标准 OpenAI 兼容端点**:同样的 `/chat/completions` 路径、同样的请求体、同样的 SSE 流式格式(`data:` 行 + `[DONE]` 结束)、同样的 `Authorization: Bearer <key>` 鉴权。现有 `openai.go` 里的 `stream()` / `parseOpenAIStream()` 逻辑对它完全适用。

落地只需在配置里加一条 provider:

```go
provider.Config{
    Name:     "glm",
    Protocol: "openai",                                  // 复用现有分支,app.go:181 无需改
    BaseURL:  "https://open.bigmodel.cn/api/paas/v4",     // 末尾无 /,NewOpenAI 会 TrimRight 再拼 /chat/completions
    KeyRef:   "provider:glm",
}
```

模型名直接填 `glm-4.6`(或 `glm-4-plus` / `glm-4-flash` 等,见 1.4)。`app.go` 的 `newProvider` 里 `case "openai"` 分支原样命中,**Go 后端一行代码都不用改**,只是多一条配置数据。

### 1.2 关键事实(来自官方文档实测)

| 项 | 值 |
|---|---|
| Base URL | `https://open.bigmodel.cn/api/paas/v4/`(海外品牌 z.ai 为 `https://api.z.ai/api/paas/v4/`) |
| Chat 端点 | `POST {baseURL}/chat/completions` |
| 鉴权 header | `Authorization: Bearer <API_KEY>` |
| API Key 形态 | **直接用原始 key 做 Bearer,v4 端点不再需要 JWT 签名**(见下方"鉴权坑") |
| 是否 OpenAI 兼容 | 官方明确"可用现有 OpenAI SDK,只改 key 和 baseURL" |
| 是否支持 stream | 支持,`"stream": true`,SSE `data:` 行,以 `data: [DONE]` 结束 |
| 流式 chunk 结构 | `choices[].delta.content`(与 OpenAI 一致) |
| Content-Type | `application/json` |

现有 `openai.go` 发的请求头正好是 `Authorization: Bearer`(openai.go:139)、`Accept: text/event-stream`(openai.go:138),`parseOpenAIStream` 也正好按 `data:` 前缀 + `[DONE]` 解析(openai.go:171-183)。**逐条对上,无缝。**

### 1.3 鉴权坑(重要,但对我们无影响)

历史上智谱老 SDK 要求把 API Key 按 `{id}.{secret}` 拆开、用 HS256 **签一个带过期时间的 JWT** 再放进 `Authorization`。这是很多老教程/老代码的做法。

**但 v4 的 OpenAI 兼容端点已经支持直接用原始 API Key 作为 Bearer token**(z.ai 官方 HTTP 文档、bigmodel 的 curl 示例均为 `Authorization: Bearer <token>`,无签名步骤)。所以我们**不需要引入任何 JWT 库,现有 `NewOpenAI` 直接可用**。

> 落地提示:用户在设置里粘贴的就是控制台给的那串完整 key,存进 Keychain(现有 `secret` 流程),运行时 `Bearer <key>` 直发即可。

### 1.4 可用模型(填进 `DefaultModel` / 模型下拉)

GLM 系列迭代很快,截至当前常见可用文本模型(具体以控制台"我的模型"为准):

- `glm-4.6` — 当前主力,强推理/代码,长上下文(约 200K)。**建议作为 GLM 默认模型。**
- `glm-4.5` / `glm-4.5-air` — 上一代旗舰 / 轻量版。
- `glm-4-plus` — GLM-4 时代旗舰。
- `glm-4-air` / `glm-4-flash` — 轻量、便宜/免费档,适合"生成 commit"这类高频短任务,**成本敏感时首选**。
- 更新的还有 `glm-4.7-flash`(免费档)、`glm-5` 等,按控制台实际开通为准。

对"生成 commit message"这个场景:输入是 diff(可能几千 token)、输出很短(几行),**`glm-4-flash` / `glm-4.6` 性价比最高**,不必上最贵的。

### 1.5 需要实测验证的两个兼容性细节(低风险,列出以防踩坑)

现有 `openai.go` 的请求体带了两个字段,少数 OpenAI 兼容实现会挑剔,建议接入时用真 key 跑一次冒烟:

1. **`stream_options: {include_usage: true}`**(openai.go:77-87、126)
   OpenAI 用它在最后一个 chunk 回 token usage。GLM 若不认这个字段,轻则忽略、重则报 400。若报错,退路:GLM 侧把 `StreamOptions` 置空(它多半会在末尾 chunk 里自带 `usage`),usage 解析逻辑(openai.go:189-192)照样能读到。
   → **建议:接入时抓一次响应确认;必要时给 openai provider 加个"是否发 stream_options"的开关。**

2. **`temperature` 取值域**
   OpenAI 允许 0~2;GLM 部分模型历史上要求 `0 < temperature < 1`(开区间,不接受 0 或 1)。若前端默认传 `temperature=0` 可能被拒。
   → **建议:GLM 场景把默认温度设成 `0.2~0.3`;commit 生成本来就该低温少发散。**

3. **`/models` 列表端点**
   `app.go:ListModels` 对 openai 协议会 `GET {baseURL}/models`(openai.go:41)。GLM v4 是否暴露该端点不确定。若不支持,设置页的"模型下拉"会取不到。
   → **建议:GLM 与 Anthropic 一样走"curated 静态列表"(参考 app.go:200-204 的 anthropic 分支),或允许用户手填模型名。这是产品/前端决策点。**

### 1.6 Claude / GPT 现有 provider 是否够用

**够用。**
- Claude 走 `NewAnthropic`(原生 Messages API + SSE,anthropic.go),已实现 system 拆分、usage、错误脱敏,现成。
- GPT 走 `NewOpenAI`(OpenAI 官方端点),现成。
- 三家统一在 `provider.Provider` 接口 + `ChatRequest`/`ChatChunk` 之下,提交工具只面向这个抽象拼 prompt、收流,不关心厂商差异。

---

## 二、git 集成方案(Go 后端读 diff / 执行 commit)

### 2.1 选型结论:调 `git` CLI(`exec.Command`),不用 go-git

**推荐直接 `os/exec` 调用系统 `git`。** 理由:

| 维度 | git CLI(推荐) | go-git 库 |
|---|---|---|
| diff 生成 | 与用户终端**完全一致**(尊重 `.gitattributes`、`diff.*` 配置、rename 检测) | 自己实现的 diff,复杂场景(rename/mode change/子模块)有差异或缺陷 |
| 行为可预期 | 用户在终端看到什么就是什么 | 可能和用户认知不符,难排查 |
| staged/unstaged/status | 一条命令搞定 | 需自己拼 index 对比 |
| hooks / commit 签名 | 原生走 `pre-commit`/`commit-msg`/GPG 签名 | 需自己处理,易漏 |
| 依赖 | 零(用户装了 git) | 引入一个较重的库 |
| 缺点 | 依赖环境有 git | 纯 Go、无外部依赖 |

对本工具的需求(读 diff、读文件列表、读分支/根、执行 commit),**git CLI 更简单、更可靠、行为和用户终端一致**,这正是提交工具最需要的属性。项目里已有 `internal/term`(PTY 跑 shell)证明"调外部命令"是既定路线,但 git 操作**不要走 PTY/shell**,直接 `exec.Command` 传参数数组即可(见 2.4 安全)。

### 2.2 需要的命令清单

| 需求 | 命令 |
|---|---|
| 是否在 git 仓库内 | `git rev-parse --is-inside-work-tree` |
| 仓库根 | `git rev-parse --show-toplevel` |
| 当前分支 | `git rev-parse --abbrev-ref HEAD`(空仓库/detached 时回 `HEAD`,可用 `git branch --show-current` 兜底) |
| 改动文件列表(状态机可解析) | `git status --porcelain=v1` (稳定格式,勿依赖人类可读 `git status`) |
| staged 文件 + 状态 | `git diff --cached --name-status` |
| **staged diff** | `git diff --cached` |
| **unstaged diff** | `git diff` |
| 每文件增删行数 + 二进制探测 | `git diff --cached --numstat`(二进制文件显示 `-\t-\t<path>`) |
| 执行 commit(见 2.3) | `git commit -F -`(message 从 stdin 传) |

MVP 优先 **staged**(`git diff --cached`);"包含未暂存"作为可选项。

### 2.3 执行 commit:用 `-F -` 从 stdin 传 message

commit message 含换行(标题 + 空行 + 正文 + PR 描述)、可能含引号/反引号/`$`/中文。**绝不要拼进 `-m "..."` 字符串**,尤其绝不要经过 shell。

推荐:`git commit -F -`,message 写进进程 stdin:

```go
func gitCommit(ctx context.Context, repoRoot, message string) error {
    cmd := exec.CommandContext(ctx, "git", "commit", "-F", "-")
    cmd.Dir = repoRoot
    cmd.Stdin = strings.NewReader(message) // message 原样进 stdin,无任何转义/注入面
    var stderr bytes.Buffer
    cmd.Stderr = &stderr
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("git commit failed: %s: %w", strings.TrimSpace(stderr.String()), err)
    }
    return nil
}
```

要点:
- `-F -` 让 git 把 stdin 全文当 message,换行/特殊字符零处理成本。
- `cmd.Dir = repoRoot`:显式指定仓库,避免依赖进程 cwd。
- 因为用 `exec.Command`(参数数组)**不经过 shell**,本就没有 shell 注入;`-F -` 更进一步连"参数里塞恶意内容"都免了。
- 只提交已 staged 的内容;要不要顺带 `git add` 由产品决定(建议 MVP 不自动 add,尊重用户的暂存区)。

### 2.4 通用执行封装(统一 Dir / context / stderr)

```go
// runGit 在 repoRoot 下执行 git,返回 stdout。绝不经过 shell。
func runGit(ctx context.Context, repoRoot string, args ...string) (string, error) {
    cmd := exec.CommandContext(ctx, "git", args...)
    if repoRoot != "" {
        cmd.Dir = repoRoot
    }
    var stdout, stderr bytes.Buffer
    cmd.Stdout, cmd.Stderr = &stdout, &stderr
    if err := cmd.Run(); err != nil {
        return "", fmt.Errorf("git %s: %s: %w", args[0], strings.TrimSpace(stderr.String()), err)
    }
    return stdout.String(), nil
}
```

所有读操作(root/branch/status/diff)都走它,只有 commit 因为要喂 stdin 单独写。

### 2.5 安全红线

- **绝不自动 push。** 工具里根本不提供 push 命令入口;需要 push 让用户回自己终端做。
- **commit 只在用户确认后。** 流程必须是:生成 → 用户审阅/可编辑 → 显式点"提交"才 `git commit`。
- **不经过 shell**:一律 `exec.Command("git", args...)`,不用 `sh -c`,天然免注入。
- **不把敏感文件 diff 发给模型**(见 2.6 过滤)。

### 2.6 diff 过滤与截断(防止把密钥/大文件/噪音喂给模型)

**先用 `git diff --cached --numstat` 拿到"每文件增删行数 + 是否二进制",据此决定发什么,再去取具体 diff。** 规则:

1. **跳过二进制**:numstat 里 `-\t-` 的文件,只在列表里列个文件名,不发内容。
2. **跳过锁文件 / 生成物**(按路径黑名单):
   `package-lock.json`、`yarn.lock`、`pnpm-lock.yaml`、`go.sum`、`Cargo.lock`、`poetry.lock`、`composer.lock`、`*.min.js`、`dist/`、`build/`、`vendor/`、`node_modules/`、`*.snap` 等。只在列表提一句"lockfile 变更,内容略"。
3. **跳过疑似敏感文件**(按路径,绝不发内容):
   `.env`、`.env.*`、`*.pem`、`*.key`、`id_rsa*`、`*credential*`、`*secret*`、`*.p12`、`*.keystore`。列表里也只提文件名,不发 diff。
4. **大小预算**(建议初值,可配):
   - **单文件 diff 上限 ≈ 400 行 / 20 KB**:超出则只取前 N 行 + 尾部标注 `... (truncated, +X/-Y lines)`。
   - **整体 diff 预算 ≈ 60 KB(约 1.5 万 token)**:所有文件累加超预算时,按"改动行数从小到大"优先保留小改动完整 diff,大文件降级为"文件名 + numstat 摘要"。
   - 这些数值给的是保守起点(GLM/GPT 上下文都能装更多,但发太多既慢又贵、还稀释重点)。

5. **diff 太大时的分级降级策略**:
   - **一级(默认)**:按上面预算,小文件全发、大文件截断、二进制/锁文件/敏感文件只列名。
   - **二级(改动很大,超预算数倍)**:不发逐行 diff,改发"结构化摘要":文件列表 + 每文件 `+X/-Y` + hunk 头(`git diff` 里 `@@ ... @@` 后面带的函数/上下文名)。让模型基于"改了哪些文件、哪些函数、增删规模"生成 message。
   - **三级(仍然过大)**:提示用户"改动过多,建议拆分提交",或让用户勾选只针对部分文件生成。
   - 可选增强:一次性大改时按目录/模块聚类,生成多段 message 供选。

> 二进制探测优先信 `--numstat`;`git diff` 对二进制也会输出 `Binary files a/x and b/x differ` 这类行,可作二次兜底识别。

---

## 三、业务知识注入(复用 `MatchKnowledge` HybridRAG)

### 3.1 现有能力回顾

`(a *App) MatchKnowledge(projectSlug, text string) (KnowledgeMatch, error)`(graphbuild.go:438)做三路召回并合并:
1. **语义种子**:对 `text` 求 embedding,取与实体向量 cosine 最近的 top-K(需配置了 embedder);
2. **关系扩展**:对种子实体沿知识图谱关系走几跳,带出关联知识;
3. **子串兜底**:`text` 里字面出现的实体名/别名,始终生效(没 embedder 时降级为纯子串)。

返回 `KnowledgeMatch{ Names, Context, Sources }`,其中 `Context` 已经是拼好的可注入文本块(带"以下是该项目的相关背景知识…"前缀 + 每个实体的 observations + 实体间关系)。**这块能直接塞进 prompt,无需二次加工。**

### 3.2 接入思路:把"变更"当 query

核心是构造一个**信息密度高、能命中实体名/语义的 `text`**,喂给 `MatchKnowledge`。建议 query 由三部分拼成:

1. **改动文件路径**(全路径 + 目录名 + 文件名去扩展名)
   → 目录/模块名(如 `order-service`、`payment`、`auth`)最容易命中知识图谱里的模块/服务实体(子串路命中)。
2. **hunk 头里的符号名**:`git diff` 每个 `@@ ... @@` 后面 git 会带上所在函数/类名;把这些抽出来。
   → 命中"概念/接口/关键类"这类实体。
3. **新增行里的关键标识符**(可选,量大时省略):新增代码里的类名/方法名/常量。
   → 补充语义召回的输入,让 embedding 更贴近具体改动。

```go
// 伪代码:构造召回 query
func buildKnowledgeQuery(files []ChangedFile, diffText string) string {
    var b strings.Builder
    for _, f := range files {
        b.WriteString(f.Path + " ")
        b.WriteString(filepath.Dir(f.Path) + " ")
        b.WriteString(strings.TrimSuffix(filepath.Base(f.Path), filepath.Ext(f.Path)) + " ")
    }
    b.WriteString(extractHunkHeaders(diffText))   // @@ 后的函数/上下文名
    // 可选:b.WriteString(extractAddedIdentifiers(diffText))
    return b.String()
}
```

> 注意:**query 用"路径 + 符号"而不是整段 diff。** 整段 diff 噪音大(空白/括号/import),会稀释语义召回、也拖慢 embedding。路径和符号名信息密度最高,最能命中业务实体。

### 3.3 拼进生成 prompt

召回结果直接接到现有注入通道。`SendMessage` 已经有 `injectContext string` 参数(app.go:247),会被当 system message 前置。提交场景组装:

```
[system] 你是提交信息助手。严格遵循 type(scope): description,type ∈
         feat/fix/refactor/docs/test/chore,message 用英文。
         正文说清"改了什么、为什么改";需要时给 PR 描述(改了什么/为什么/怎么测)。
[system] <= KnowledgeMatch.Context 原样注入(该项目相关业务背景)
[user]   变更文件列表 + 过滤/截断后的 diff
```

即:`match, _ := a.MatchKnowledge(projectSlug, query)`,把 `match.Context` 作为 `injectContext`(或直接拼进 system 段),diff 作为 user 段发出。生成后 `match.Sources` 还能在 UI 标注"本次背景取自 xx 会话",延续现有溯源体验。

### 3.4 一个待定项(需产品/架构确认)

`MatchKnowledge` 需要 `projectSlug`,而入口是一个 **git 仓库路径**。**"git 仓库 ↔ 知识图谱 project"的映射关系没有现成约定**,可选:
- 让用户在打开仓库时选/绑定对应的 project;
- 用仓库名(`filepath.Base(repoRoot)`)去匹配 project slug;
- 支持无绑定时降级:不注入知识,退化成"只看 diff"的通用生成(仍能用,只是没了业务护城河)。

**建议:MVP 支持"手动绑定 + 无绑定降级",映射策略作为产品决策点。**

---

## 四、需要架构师拍板的取舍(汇总)

1. **GLM 的 `stream_options` / temperature 兼容性**:现有 openai provider 无条件发 `stream_options.include_usage` 且可能默认温度 0。GLM 若挑剔,要不要给 openai provider 加"发不发 stream_options""默认温度"的按 provider 开关?(建议:先实测,报错再加开关。)

2. **GLM 模型下拉的来源**:`/models` 端点 GLM 可能不支持。走静态 curated 列表(同 Anthropic 分支)还是允许手填?(建议:curated + 可手填。)

3. **git 仓库 ↔ 知识图谱 project 的映射**:手动绑定 / 按仓库名匹配 / 无绑定降级。影响"懂业务"这个核心卖点的开箱体验。(建议:手动绑定 + 无绑定降级。)

4. **diff 预算的默认值**(单文件 400 行/20KB、整体 60KB)与敏感文件/锁文件黑名单:是否需要做成用户可配?(建议:先硬编码保守值,后续再放到设置。)

5. **是否自动 `git add`**:MVP 只提交已暂存内容(尊重用户暂存区),还是提供"暂存全部并提交"的便捷开关?(建议:默认只提交 staged,便捷开关可选。)

---

## 参考链接

- 智谱 OpenAI 兼容接口说明:https://docs.bigmodel.cn/cn/guide/develop/openai/introduction
- 智谱 HTTP API(base URL `https://open.bigmodel.cn/api/paas/v4/`):https://docs.bigmodel.cn/cn/guide/develop/http/introduction
- z.ai(海外品牌)HTTP 接入,`Authorization: Bearer YOUR_API_KEY`:https://docs.z.ai/guides/develop/http/introduction
- z.ai Chat Completion API 参考:https://docs.z.ai/api-reference/llm/chat-completion
- GLM API base URL + OpenAI 兼容(第三方指南):https://apidog.com/blog/how-to-use-glm-5-1-api
- curl 示例(`.../api/paas/v4/chat/completions` + Bearer):https://gist.github.com/excitedplus1s
- GLM-5 模型说明:https://docs.bigmodel.cn/cn/guide/models/text/glm-5
- 智谱 Python SDK(历史 JWT 鉴权背景参考):https://github.com/MetaGLM/zhipuai-sdk-python-v4
