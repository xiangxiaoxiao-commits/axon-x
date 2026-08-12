# Axon 编排后端技术方案(TECH_ORCH)

> 面向 Go + Wails 落地。本文只讲后端:任务补全、并行执行、持久化、review 迭代。
> 遵循 BRIEF_ORCH.md 已确认方向,不推翻。前端事件契约见"事件设计"一节。

## 0. 复用清单(不重造)

| 现有资产 | 在本方案里的角色 |
|---|---|
| `internal/provider`(`Chat` 流式 + `collectReply` 收全文) | 补全走 `collectReply`(要完整 JSON);执行走流式 `Chat`(要实时推送) |
| `app.go` 的 `newProvider` / `providerForProtocol` | 按任务选定的 provider/model 建实例,逻辑照搬 |
| `app.go` 的 `emit` + `cancels map` + `runStream` 模式 | 直接复刻成任务级:`taskCancels` + `runTask` |
| `internal/db`(embedded migration 框架) | 加 `0003_tasks.sql`,零改动框架 |
| `internal/store`(SQLite) | **新增** `TaskStore`,复用同一个 `*sql.DB`,不塞进现有 conversation Store |
| `graphbuild.go` 的 `MatchKnowledge(projectSlug, text)` | 补全时用粗略任务当 query 召回业务背景;空图谱已自带降级 |
| `graphbuild.go` 的 `parseExtracted`(容错抽 JSON) | 补全结果解析照搬同一套(容忍代码围栏/多余散文) |

核心判断:补全和执行是**两种 LLM 调用**——补全要一次性拿到结构化全文(`collectReply`),执行要流式推进度(`Chat`)。两者都已有现成通道,不用改 provider 层。

---

## 1. 任务信息自动补全

### 1.1 输出结构:选 JSON(不选带标记文本)

结论:**JSON**。理由——前端要把补全结果渲染成"可编辑表单"(BRIEF 第 2 步明确"给用户 review/编辑"),JSON 的字段能直接映射成表单控件(数组字段=可增删的列表项),编辑后回存也是结构化的。带标记文本(Markdown 段落)让前端只能给一个大 textarea,改完还得重新解析,得不偿失。

规格结构(Go,JSON tag 即前端契约,camelCase):

```go
// TaskSpec 是补全后的结构化规格,可被用户编辑后再执行。
type TaskSpec struct {
    Goal               string   `json:"goal"`               // 目标(一句话)
    Background         string   `json:"background"`         // 背景/上下文
    Constraints        []string `json:"constraints"`        // 约束
    Scope              []string `json:"scope"`              // 涉及文件或范围
    AcceptanceCriteria []string `json:"acceptanceCriteria"` // 验收标准
    Pitfalls           []string `json:"pitfalls"`           // 易遗漏点
    Steps              []string `json:"steps"`              // 建议执行步骤
}
```

数组字段而非长文本:每条独立 → 前端渲染成清单、用户逐条增删改。存库时整体 `json.Marshal` 进 `tasks.spec` 列。

### 1.2 Prompt 设计要点

系统 prompt 固定,用户 prompt = 召回背景 + 粗略任务。要点:

1. **角色**:"你是资深工程师,把用户潦草的任务描述补全成一份结构完整、可直接派给执行者的规格。"
2. **强约束输出**:只输出一个 JSON 对象,字段固定为上述七个,不要代码围栏外的任何散文。给出字段中文语义说明。
3. **补人写时漏的**(BRIEF 动机):明确要求"推断用户没写但执行者必须知道的:隐含约束、验收标准、易踩坑点"。这是本工具的差异点,prompt 必须点名。
4. **诚实标注**:推断出来但不确定的内容,在该条目里用"(推断)"前缀,让用户 review 时一眼看到哪些是模型脑补的。
5. **温度低**(0.2 左右),要稳定结构不要发散。

prompt 骨架:

```go
const completeSpecPrompt = `你是资深工程师。用户会给一段潦草的任务描述,可能还带项目背景知识。
把它补全成一份结构完整、可直接派给执行者的规格。重点补齐用户没写、但执行者必须知道的信息:
隐含约束、验收标准、容易遗漏或踩坑的点。

只输出一个 JSON 对象,不要任何解释文字、不要代码围栏。字段:
- goal: 目标,一句话说清要达成什么
- background: 背景/上下文,结合给出的项目知识
- constraints: 约束数组(技术栈、兼容性、性能、安全等)
- scope: 涉及的文件或范围数组
- acceptanceCriteria: 验收标准数组,可检验
- pitfalls: 易遗漏点/易踩坑点数组
- steps: 建议执行步骤数组,有序

推断出的、不确定的条目,在文本开头加 "(推断)"。`
```

### 1.3 结合项目知识(MatchKnowledge)

用粗略任务当 query 调 `MatchKnowledge`,把召回的 `Context` 拼进用户 prompt:

```go
func (a *App) CompleteTask(taskID, projectSlug, rough string) (TaskSpec, error) {
    // 1) 召回业务背景(空图谱/无 embedder 时 km.Context 为空,自动降级)
    var bg string
    if km, err := a.MatchKnowledge(projectSlug, rough); err == nil {
        bg = km.Context // "以下是该项目的相关背景知识……" 或空串
    }

    // 2) 建 provider(用任务选定的,或默认)
    prov, err := a.providerForTask(taskID) // 复刻 newProvider 选择逻辑
    if err != nil {
        return TaskSpec{}, err
    }

    // 3) 拼 user prompt
    var ub strings.Builder
    if strings.TrimSpace(bg) != "" {
        ub.WriteString(bg)
        ub.WriteString("\n\n---\n\n")
    }
    ub.WriteString("任务描述:\n")
    ub.WriteString(rough)

    // 4) 一次性收全文(补全不需要流式)
    reply, err := collectReply(a.ctx, prov, provider.ChatRequest{
        Model:       a.taskModel(taskID),
        Messages:    []provider.ChatMessage{
            {Role: provider.RoleSystem, Content: completeSpecPrompt},
            {Role: provider.RoleUser, Content: ub.String()},
        },
        Temperature: 0.2, MaxTokens: 2000,
    })
    if err != nil {
        return TaskSpec{}, err
    }

    // 5) 容错解析(照搬 parseExtracted 的抽 JSON 逻辑)
    spec := parseTaskSpec(reply)

    // 6) 存中间产物,状态 completing -> awaiting_confirmation
    a.tasks.SaveSpec(a.ctx, taskID, spec)
    a.tasks.SetStatus(a.ctx, taskID, StatusAwaitingConfirmation)
    return spec, nil
}
```

`parseTaskSpec` 复用 `parseExtracted` 那套:找首个 `{` 到末个 `}`,`json.Unmarshal`,失败返回空 `TaskSpec`(降级为"给用户一张空表单自己填")。

### 1.4 中间产物存储

补全结果**立即落库**(`tasks.spec`,JSON 文本),状态置 `awaiting_confirmation`。用户在前端编辑表单后,调 `UpdateTaskSpec(taskID, spec)` 覆盖 `tasks.spec`。执行时读的是库里当前的 spec,保证"用户改过的版本才是被执行的版本"。


---

## 2. 多任务并行执行架构

### 2.1 状态机

八个状态(存 `tasks.status` 文本列):

```
draft            草稿:刚建,只有粗略输入
completing       补全中:LLM 正在生成 spec
awaiting_confirm 待确认:spec 已生成,等用户 review/编辑
queued           排队中:已提交执行但并发已满,在等 worker
executing        执行中:goroutine 正在流式生成结果
awaiting_review  待审阅:结果生成完,等用户裁决
adopted          已采纳(终态)
rejected         已打回:带反馈,可再次进入 queued/executing 迭代
failed           执行失败(可重跑)
```

流转:

```
draft ──CompleteTask──▶ completing ──ok──▶ awaiting_confirm
                            └──err─────────▶ draft(附错误)
awaiting_confirm ──UpdateSpec──▶ awaiting_confirm(自环,存编辑)
awaiting_confirm ──SubmitTask──▶ queued ──worker空闲──▶ executing
executing ──done──▶ awaiting_review
executing ──err───▶ failed
executing ──Cancel─▶ awaiting_confirm(丢弃本次输出,可重提)
awaiting_review ──Adopt──▶ adopted(终态)
awaiting_review ──Reject(反馈)──▶ queued(带反馈重跑,记新 run)
failed ──Retry──▶ queued
```

关键点:`queued` 和 `executing` 分开,因为有并发上限,提交后不一定马上跑。前端据此显示"排队中/执行中"。

### 2.2 并发管理:带上限的 worker 派发

不为每个任务无脑起 goroutine(用户可能一次提十几个,打爆 API 限流)。用**信号量 channel 限并发**,超出的排队。这是对现有 `runStream` 单流模式的自然扩展。

```go
// TaskManager 管理并行任务的生命周期。挂在 App 上(App.tasks 之外再加 App.taskMgr)。
type TaskManager struct {
    app  *App
    sem  chan struct{}          // 缓冲=最大并发数,占坑即限流
    mu   sync.Mutex
    running map[string]context.CancelFunc // taskID -> cancel(复刻 app.cancels)
}

func NewTaskManager(app *App, maxConcurrency int) *TaskManager {
    return &TaskManager{
        app:     app,
        sem:     make(chan struct{}, maxConcurrency), // 建议默认 3
        running: make(map[string]context.CancelFunc),
    }
}

// Submit 把任务推入执行队列。立即返回(非阻塞),真正执行在 goroutine 里等信号量。
func (m *TaskManager) Submit(taskID string, feedback string) error {
    spec, err := m.app.tasks.GetSpec(m.app.ctx, taskID)
    if err != nil {
        return err
    }
    // 新建一个 run 记录(见第 4 节 task_runs),拿到 runID
    runID, err := m.app.tasks.StartRun(m.app.ctx, taskID, spec, feedback)
    if err != nil {
        return err
    }
    m.app.tasks.SetStatus(m.app.ctx, taskID, StatusQueued)
    m.app.emitTask(taskID, EventTaskStatus, taskStatusEvent{Status: StatusQueued})

    go func() {
        // 等一个并发名额(阻塞在这,天然形成队列)
        m.sem <- struct{}{}
        defer func() { <-m.sem }()

        // 建可取消 ctx,登记以便 Cancel
        ctx, cancel := context.WithCancel(m.app.ctx)
        m.mu.Lock()
        m.running[taskID] = cancel
        m.mu.Unlock()
        defer func() {
            m.mu.Lock(); delete(m.running, taskID); m.mu.Unlock()
        }()

        m.app.runTask(ctx, taskID, runID, spec, feedback)
    }()
    return nil
}

// Cancel 取消执行中的任务(执行中或排队中皆可)。
func (m *TaskManager) Cancel(taskID string) {
    m.mu.Lock()
    cancel := m.running[taskID]
    m.mu.Unlock()
    if cancel != nil {
        cancel()
    }
}
```

`runTask` 是 `runStream` 的任务版:流式消费、累积、按 taskID 推事件、落库结果。

```go
func (m *TaskManager) app_runTask_sketch() {} // 见下

func (a *App) runTask(ctx context.Context, taskID, runID string, spec TaskSpec, feedback string) {
    a.tasks.SetStatus(a.ctx, taskID, StatusExecuting)
    a.emitTask(taskID, EventTaskStatus, taskStatusEvent{Status: StatusExecuting})

    prov, err := a.providerForTask(taskID)
    if err != nil {
        a.failRun(taskID, runID, err); return
    }

    // 执行 prompt = spec(序列化成可读文本) + 打回反馈(若有)
    msgs := buildExecMessages(spec, feedback)
    chunks, errs := prov.Chat(ctx, provider.ChatRequest{
        Model: a.taskModel(taskID), Messages: msgs,
        Temperature: 0.3, MaxTokens: 4000,
    })

    var b strings.Builder
    for chunk := range chunks {
        if chunk.Delta != "" {
            b.WriteString(chunk.Delta)
            a.emitTask(taskID, EventTaskDelta, taskDeltaEvent{RunID: runID, Delta: chunk.Delta})
        }
    }
    var streamErr error
    select { case streamErr = <-errs: default: }

    // 落库(累积文本,即使中途取消也保留 —— 照搬 runStream 语义)
    a.tasks.FinishRun(a.ctx, runID, b.String(), streamErr)

    switch {
    case streamErr != nil && errors.Is(streamErr, context.Canceled):
        // 用户取消:退回待确认,输出作废
        a.tasks.SetStatus(a.ctx, taskID, StatusAwaitingConfirm)
        a.emitTask(taskID, EventTaskStatus, taskStatusEvent{Status: StatusAwaitingConfirm})
    case streamErr != nil:
        a.tasks.SetStatus(a.ctx, taskID, StatusFailed)
        a.emitTask(taskID, EventTaskError, taskErrorEvent{RunID: runID, Error: streamErr.Error()})
    default:
        a.tasks.SetStatus(a.ctx, taskID, StatusAwaitingReview)
        a.emitTask(taskID, EventTaskDone, taskDoneEvent{RunID: runID})
    }
}
```

要点:

- **并发上限**用 `sem chan struct{}`,不用第三方 pool 库,轻量够用。默认 3,可做成配置项。
- **取消**复刻 `app.cancels` 那套 map + `context.CancelFunc`。取消执行中或排队中(排队中的 goroutine 阻塞在 `m.sem <-`,cancel 后需在拿到名额前检查 ctx;实现时在 `m.sem <-` 后先 `if ctx.Err()!=nil { return }`)。
- **崩溃恢复**:启动时把库里残留的 `executing`/`queued` 任务重置为 `awaiting_confirm`(desktop 单机,goroutine 不跨进程存活),让用户重新提交。放在 `startup` 里扫一次。

### 2.3 进度/结果推送(复用 Wails EventsEmit)

完全复刻 `chat:delta` 那套,只是事件名换成 `task:*`,payload 带 `taskId` 供前端路由到对应任务卡片。

事件定义:

```go
const (
    EventTaskStatus = "task:status" // 状态变化(状态机每次流转都推)
    EventTaskDelta  = "task:delta"  // 执行流式增量
    EventTaskDone   = "task:done"   // 执行完成 -> 待审阅
    EventTaskError  = "task:error"  // 执行失败
    EventTaskSpec   = "task:spec"   // 补全完成,推 spec 供前端渲染表单
)

type taskStatusEvent struct {
    TaskID string `json:"taskId"`
    Status string `json:"status"`
}
type taskDeltaEvent struct {
    TaskID string `json:"taskId"`
    RunID  string `json:"runId"`
    Delta  string `json:"delta"`
}
type taskDoneEvent struct {
    TaskID string `json:"taskId"`
    RunID  string `json:"runId"`
}
type taskErrorEvent struct {
    TaskID string `json:"taskId"`
    RunID  string `json:"runId"`
    Error  string `json:"error"`
}
type taskSpecEvent struct {
    TaskID string   `json:"taskId"`
    Spec   TaskSpec `json:"spec"`
}

// emitTask 统一给 payload 塞 taskId(payload 结构体已含 TaskID 字段)。
func (a *App) emitTask(taskID, event string, payload interface{}) {
    a.emit(event, payload)
}
```

前端订阅这五个事件,用 `taskId` 分发到对应任务卡片:多个任务并行流式,各推各的,互不干扰(和现在多会话流式同理)。

### 2.4 取消 / 打回重跑

- **取消执行**:`TaskManager.Cancel(taskID)` → ctx cancel → `runTask` 走 `context.Canceled` 分支 → 退回 `awaiting_confirm`,本次 run 标记 canceled(见第 4 节)。
- **打回重跑**:`RejectTask(taskID, feedback)` → 存反馈 → 状态回 `queued` → 再次 `Submit(taskID, feedback)`。反馈作为额外消息拼进执行 prompt,并**新开一条 run**(不覆盖上次结果,保留迭代历史)。

---

## 3. 任务持久化(SQLite)

### 3.1 新建 TaskStore(不复用 conversation Store)

建议**新建** `internal/store` 下的 `TaskStore`(实现放 `internal/store/sqlite`,复用同一个 `*sql.DB`)。理由:

- 现有 `store.Store` 接口语义是"append-only 的会话消息归档",任务是"有状态、可变更(status/spec 反复改)、有生命周期"的实体,塞进去会污染接口。
- 但**复用同一个 `db.Open` 的连接和 migration 框架**,不新开数据库文件。`sqlite.NewTaskStore(sqlDB)` 和现有 `sqlite.New(sqlDB)` 并列。

App 上挂两个:`a.store`(会话)+ `a.tasks`(任务)。

### 3.2 migration:`0003_tasks.sql`

放 `internal/db/migrations/0003_tasks.sql`,框架零改动自动应用。

```sql
-- Phase 编排:多任务并行执行 + review。
-- tasks 是任务主体(可变状态);task_runs 是每次执行的历史(append-only)。

CREATE TABLE tasks (
    id            TEXT PRIMARY KEY,            -- UUID v4
    project_slug  TEXT NOT NULL DEFAULT '',    -- 关联项目(给 MatchKnowledge 召回用)
    rough_input   TEXT NOT NULL DEFAULT '',    -- 用户原始粗略输入
    spec          TEXT NOT NULL DEFAULT '',    -- 补全规格 TaskSpec 的 JSON(用户可编辑)
    status        TEXT NOT NULL DEFAULT 'draft',
    provider_name TEXT NOT NULL DEFAULT '',    -- 选用的 provider 实例名(空=用默认)
    model         TEXT NOT NULL DEFAULT '',    -- 选用的 model id(空=用默认)
    review_note   TEXT NOT NULL DEFAULT '',    -- 最终 review 结论/备注
    last_error    TEXT NOT NULL DEFAULT '',    -- 最近一次失败信息(脱敏,provider 层已处理)
    created_at    INTEGER NOT NULL,            -- unix epoch millis
    updated_at    INTEGER NOT NULL             -- 驱动列表排序
);

CREATE INDEX idx_tasks_updated_at ON tasks (updated_at DESC);
CREATE INDEX idx_tasks_status ON tasks (status);

-- task_runs: 一个任务的每次执行都是一条 run,不覆盖,保留迭代历史。
CREATE TABLE task_runs (
    id           TEXT PRIMARY KEY,             -- UUID v4(= 事件里的 runId)
    task_id      TEXT NOT NULL,
    seq          INTEGER NOT NULL,             -- 第几次执行(1,2,3...),同 task 内递增
    spec_snapshot TEXT NOT NULL DEFAULT '',    -- 本次执行时的 spec 快照(spec 可能后续再改)
    feedback     TEXT NOT NULL DEFAULT '',     -- 本次是打回重跑时,附带的用户反馈(首次为空)
    result       TEXT NOT NULL DEFAULT '',     -- 模型产出(流式累积的全文)
    status       TEXT NOT NULL DEFAULT 'executing', -- executing | done | failed | canceled
    error        TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    finished_at  INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE
);

CREATE INDEX idx_task_runs_task ON task_runs (task_id, seq);
```

设计取舍:

- **spec 存 JSON 文本列**,不拆成关系表。spec 是整体读写的表单产物,拆表纯属自找麻烦(和现有 memories 存 BLOB embedding 一个思路——单机、量小、Go 侧处理)。
- **result 存在 task_runs 不存在 tasks**:一个任务多次执行(打回迭代),结果天然属于某次 run。tasks 主体只留状态和当前 spec。
- **spec_snapshot**:执行时把当时的 spec 拷进 run,因为用户之后可能再改 spec 重跑,历史 run 要能还原"当时是拿什么规格跑的"。
- 时间戳沿用现有约定:`INTEGER` unix millis,`time.Now().UnixMilli()`。
- 外键 `ON DELETE CASCADE`,删任务连带删 runs,和现有 conversations→messages 一致。

### 3.3 TaskStore 接口草案

```go
type TaskStore interface {
    CreateTask(ctx, projectSlug, roughInput, providerName, model string) (Task, error)
    GetTask(ctx, id string) (Task, error)
    ListTasks(ctx) ([]Task, error)                 // updated_at DESC
    SaveSpec(ctx, id string, spec TaskSpec) error   // 补全结果 / 用户编辑
    GetSpec(ctx, id string) (TaskSpec, error)
    SetStatus(ctx, id, status string) error         // 状态流转,同时 bump updated_at
    SetProviderModel(ctx, id, providerName, model string) error
    SetReviewNote(ctx, id, note string) error

    // runs
    StartRun(ctx, taskID string, spec TaskSpec, feedback string) (runID string, err error) // seq 自增
    FinishRun(ctx, runID, result string, err error) error // 置 done/failed
    CancelRun(ctx, runID string) error
    ListRuns(ctx, taskID string) ([]TaskRun, error)  // seq ASC,给 review 看迭代历史
}
```

写操作均参数化(照搬现有 sqlite.go 风格),`SetStatus` 等在同一语句里 bump `updated_at` 让列表重排。

---

## 4. Review / 打回迭代

### 4.1 数据模型:多 run 历史,不覆盖

结论:**每次执行开一条 `task_runs`,保留完整迭代历史,不覆盖**。理由:

- Review 的价值就在于对比"打回前 vs 反馈后"的产出,覆盖了就没法比。
- 单机 SQLite,几十条 run 的存储成本可忽略。
- tasks 主体只指向"当前状态",历史在 runs 里按 `seq` 排。

一次打回迭代的数据流:

```
awaiting_review
   │ RejectTask(taskID, "反馈:X 没考虑到,Y 要改成 Z")
   ▼
tasks.review_note 追加反馈 / tasks.status = queued
   │ Submit(taskID, feedback)
   ▼
StartRun: 新 run,seq = 上次+1,feedback = 用户反馈,spec_snapshot = 当前 spec
   ▼
runTask: 执行 prompt = spec + "上一版产出的问题反馈:" + feedback
   │  (可选:把上一条 run 的 result 也带进 prompt,让模型知道要改什么)
   ▼
awaiting_review(新结果)→ 用户再裁决
```

### 4.2 执行 prompt 如何带反馈

```go
func buildExecMessages(spec TaskSpec, feedback string) []provider.ChatMessage {
    sys := "你是资深工程师,按给定规格产出结果(方案/代码/答案)。严格满足验收标准。"
    var u strings.Builder
    u.WriteString(spec.render()) // 把 TaskSpec 各字段拼成可读文本
    if strings.TrimSpace(feedback) != "" {
        u.WriteString("\n\n---\n上一版产出被打回,请针对以下反馈重做:\n")
        u.WriteString(feedback)
    }
    return []provider.ChatMessage{
        {Role: provider.RoleSystem, Content: sys},
        {Role: provider.RoleUser, Content: u.String()},
    }
}
```

MVP 阶段"执行"= 调 LLM 生成结果文本(BRIEF 边界),不落盘改代码。所以 result 就是一段文本,存 `task_runs.result`,前端渲染给用户 review。

### 4.3 采纳 / 打回 API

```go
func (a *App) AdoptTask(taskID, note string) error   // status=adopted, review_note=note
func (a *App) RejectTask(taskID, feedback string) error {
    a.tasks.SetReviewNote(a.ctx, taskID, feedback)
    return a.taskMgr.Submit(taskID, feedback) // 内部置 queued 并新开 run
}
func (a *App) RetryTask(taskID string) error         // failed -> 重新 Submit(无新反馈)
```

---

## 5. App 接线小结

`App` 结构新增:

```go
type App struct {
    // ... 现有字段 ...
    tasks   store.TaskStore     // 新增:任务持久化
    taskMgr *TaskManager        // 新增:并发执行管理
}
```

`startup` 里:

```go
a.tasks = sqlite.NewTaskStore(sqlDB)      // 复用同一个 sqlDB
a.taskMgr = NewTaskManager(a, 3)          // 默认并发 3
a.recoverStaleTasks()                      // executing/queued -> awaiting_confirm
```

导出给前端的方法(Wails 绑定,`wails generate module` 刷新):
`CreateTask` / `CompleteTask` / `UpdateTaskSpec` / `SubmitTask` / `CancelTask` / `AdoptTask` / `RejectTask` / `RetryTask` / `ListTasks` / `GetTask` / `ListTaskRuns` / `SetTaskProviderModel`。

---

## 6. 需要架构师拍板的技术取舍

1. **默认并发上限 = 3?** 太高易触发各家 API 限流(尤其 GLM/免费额度),太低并行感弱。建议 3,做成配置项。是否要按 provider 分别限流(不同厂商各一个信号量)?——建议 MVP 先全局一个,够用。
2. **补全失败的降级**:LLM 返回的 JSON 解析失败时,是给用户一张**空表单**自己填,还是**重试一次**?建议先空表单(简单、不烧钱),重试后续加。
3. **打回重跑要不要把上一版 result 喂给模型**:喂了模型知道改什么(效果好),但 token 成本高、可能被旧输出带偏。建议**只喂 feedback + spec**,把旧 result 作为可选开关。
4. **崩溃恢复策略**:残留 `executing` 任务重置为 `awaiting_confirm`(丢弃半成品)是否可接受?另一选择是保留半成品 result 让用户看。建议丢弃(半成品 LLM 文本价值低),但 run 标 `canceled` 留痕。
5. **spec 是否需要版本历史**:目前用户编辑 spec 是**覆盖** `tasks.spec`,只在 run 里留 `spec_snapshot`。若要"spec 编辑历史"需单独表——建议 MVP 不做,snapshot 够追溯。
6. **project_slug 从哪来**:任务要关联到某个项目才能 `MatchKnowledge` 召回。建新任务时用户选项目?还是全局当前项目?需前端交互确认(与 UX 对齐)。

