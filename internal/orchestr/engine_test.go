package orchestr

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"axon/internal/provider"
	"axon/internal/task"
)

// --- fakes ---

// fakeProvider streams a canned reply. It picks the enrichment reply when the
// request carries the enrichment system prompt, otherwise the execution reply.
type fakeProvider struct {
	enrich string
	exec   string
	err    error

	mu       sync.Mutex
	requests []provider.ChatRequest
}

func (p *fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) Chat(ctx context.Context, req provider.ChatRequest) (<-chan provider.ChatChunk, <-chan error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.mu.Unlock()

	reply := p.exec
	if len(req.Messages) > 0 && req.Messages[0].Content == EnrichPrompt {
		reply = p.enrich
	}
	chunks := make(chan provider.ChatChunk, 1)
	errs := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(errs)
		if p.err != nil {
			errs <- p.err
			return
		}
		chunks <- provider.ChatChunk{Delta: reply}
		errs <- nil
	}()
	return chunks, errs
}

func (p *fakeProvider) lastRequest() provider.ChatRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) == 0 {
		return provider.ChatRequest{}
	}
	return p.requests[len(p.requests)-1]
}

// fakeStore is an in-memory task.Store.
type fakeStore struct {
	mu      sync.Mutex
	tasks   map[string]task.Task
	runs    map[string][]task.Run
	seqTask int
	seqRun  int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{tasks: map[string]task.Task{}, runs: map[string][]task.Run{}}
}

func (s *fakeStore) CreateTask(t task.Task) (task.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seqTask++
	t.ID = fmt.Sprintf("t%d", s.seqTask)
	now := time.Now().UnixMilli()
	t.CreatedAt, t.UpdatedAt = now, now
	s.tasks[t.ID] = t
	return t, nil
}

func (s *fakeStore) GetTask(id string) (task.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return task.Task{}, fmt.Errorf("task %q not found", id)
	}
	return t, nil
}

func (s *fakeStore) ListTasks() ([]task.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]task.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t)
	}
	return out, nil
}

func (s *fakeStore) UpdateTask(t task.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[t.ID]; !ok {
		return fmt.Errorf("task %q not found", t.ID)
	}
	t.UpdatedAt = time.Now().UnixMilli()
	s.tasks[t.ID] = t
	return nil
}

func (s *fakeStore) DeleteTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, id)
	delete(s.runs, id)
	return nil
}

func (s *fakeStore) AddRun(r task.Run) (task.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seqRun++
	r.ID = s.seqRun
	r.Seq = len(s.runs[r.TaskID]) + 1
	r.CreatedAt = time.Now().UnixMilli()
	s.runs[r.TaskID] = append(s.runs[r.TaskID], r)
	return r, nil
}

func (s *fakeStore) UpdateRun(r task.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.runs[r.TaskID] {
		if existing.ID == r.ID {
			s.runs[r.TaskID][i] = r
			return nil
		}
	}
	return fmt.Errorf("run %d not found", r.ID)
}

func (s *fakeStore) ListRuns(taskID string) ([]task.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]task.Run(nil), s.runs[taskID]...), nil
}

// recordWriteback captures acceptance writebacks.
type recordWriteback struct {
	mu    sync.Mutex
	calls []string
}

func (w *recordWriteback) WriteBack(_ context.Context, t task.Task, result string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, t.ID+"|"+result)
	return nil
}

func (w *recordWriteback) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.calls)
}

// --- helpers ---

func newTestEngine(t *testing.T, prov *fakeProvider, opts ...func(*Deps)) (*Engine, *fakeStore) {
	t.Helper()
	store := newFakeStore()
	d := Deps{
		Ctx:   context.Background(),
		Store: store,
		Provider: func(context.Context, string, string) (provider.Provider, string, error) {
			return prov, "fake-model", nil
		},
	}
	for _, o := range opts {
		o(&d)
	}
	e, err := New(d)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e, store
}

// waitStatus polls until the task reaches want, failing the test on timeout.
// Enrichment and execution both run in goroutines, so every assertion on their
// outcome has to wait for the transition rather than read it immediately.
func waitStatus(t *testing.T, e *Engine, id string, want task.Status) task.Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := e.GetTask(id)
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		if got.Status == want {
			return got
		}
		time.Sleep(2 * time.Millisecond)
	}
	got, _ := e.GetTask(id)
	t.Fatalf("task %s status = %q, want %q", id, got.Status, want)
	return task.Task{}
}

const validSpecJSON = `{"goal":"加超时重试","background":"订单服务","constraints":["不改接口"],"scope":["svc/order.go"],"acceptCriteria":["超时后重试3次"],"missedPoints":["(推断)幂等性"],"steps":["改配置"]}`

// --- tests ---

func TestEnrichProducesEditableSpec(t *testing.T) {
	prov := &fakeProvider{enrich: validSpecJSON, exec: "done"}
	e, _ := newTestEngine(t, prov)

	tk, err := e.CreateTask("给订单服务加个超时重试", "", "", "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if tk.Status != task.StatusDraft {
		t.Fatalf("new task status = %q, want draft", tk.Status)
	}
	if tk.Title != "给订单服务加个超时重试" {
		t.Fatalf("title = %q, want the input's first line", tk.Title)
	}

	if err := e.EnrichTask(tk.ID); err != nil {
		t.Fatalf("EnrichTask: %v", err)
	}
	got := waitStatus(t, e, tk.ID, task.StatusReviewSpec)

	if got.Spec.Goal != "加超时重试" {
		t.Errorf("goal = %q", got.Spec.Goal)
	}
	if len(got.Spec.AcceptCriteria) != 1 || got.Spec.AcceptCriteria[0] != "超时后重试3次" {
		t.Errorf("acceptCriteria = %v", got.Spec.AcceptCriteria)
	}
	if len(got.Spec.Scope) != 1 {
		t.Errorf("scope = %v", got.Spec.Scope)
	}
	if got.FailedStage != "" {
		t.Errorf("failedStage = %q, want empty on a successful parse", got.FailedStage)
	}
}

func TestEnrichParseFailureDegradesNotFails(t *testing.T) {
	prov := &fakeProvider{enrich: "抱歉，我无法输出 JSON。", exec: "done"}
	e, _ := newTestEngine(t, prov)

	tk, _ := e.CreateTask("模糊需求", "", "", "")
	_ = e.EnrichTask(tk.ID)
	got := waitStatus(t, e, tk.ID, task.StatusReviewSpec)

	// A parse failure must still leave the task usable: empty spec, marker set,
	// no automatic retry (which would burn tokens on a doomed call).
	if got.FailedStage != "enrich" {
		t.Errorf("failedStage = %q, want \"enrich\"", got.FailedStage)
	}
	if got.Spec.Goal != "" {
		t.Errorf("goal = %q, want empty spec", got.Spec.Goal)
	}
}

func TestRunStreamsAndReviews(t *testing.T) {
	prov := &fakeProvider{enrich: validSpecJSON, exec: "第一版产出"}
	e, _ := newTestEngine(t, prov)

	tk, _ := e.CreateTask("给订单服务加个超时重试", "", "", "")
	_ = e.EnrichTask(tk.ID)
	waitStatus(t, e, tk.ID, task.StatusReviewSpec)

	if err := e.RunTask(tk.ID); err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	got := waitStatus(t, e, tk.ID, task.StatusReviewResult)
	if got.Status != task.StatusReviewResult {
		t.Fatalf("status = %q, want review_result", got.Status)
	}
	runs, _ := e.ListRuns(tk.ID)
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if runs[0].Result != "第一版产出" {
		t.Errorf("run result = %q", runs[0].Result)
	}

	// The execution prompt must carry the confirmed spec, not the raw input.
	last := prov.lastRequest()
	if !strings.Contains(last.Messages[1].Content, "超时后重试3次") {
		t.Errorf("exec prompt missing acceptance criteria:\n%s", last.Messages[1].Content)
	}
}

func TestRejectOpensNewRunWithFeedback(t *testing.T) {
	prov := &fakeProvider{enrich: validSpecJSON, exec: "v1"}
	e, _ := newTestEngine(t, prov)

	tk, _ := e.CreateTask("给订单服务加个超时重试", "", "", "")
	_ = e.EnrichTask(tk.ID)
	waitStatus(t, e, tk.ID, task.StatusReviewSpec)
	_ = e.RunTask(tk.ID)
	waitStatus(t, e, tk.ID, task.StatusReviewResult)

	if err := e.ReviewTask(tk.ID, "reject", "验收标准要补上回滚"); err != nil {
		t.Fatalf("ReviewTask reject: %v", err)
	}
	waitStatus(t, e, tk.ID, task.StatusReviewResult)

	runs, _ := e.ListRuns(tk.ID)
	if len(runs) != 2 {
		t.Fatalf("runs = %d, want 2 (append-only iteration history)", len(runs))
	}
	if runs[1].Feedback != "验收标准要补上回滚" {
		t.Errorf("second run feedback = %q", runs[1].Feedback)
	}

	// The iterate prompt must carry both the previous result and the feedback so
	// the model knows what to improve on.
	last := prov.lastRequest()
	body := last.Messages[1].Content
	if !strings.Contains(body, "v1") {
		t.Errorf("iterate prompt missing previous result:\n%s", body)
	}
	if !strings.Contains(body, "验收标准要补上回滚") {
		t.Errorf("iterate prompt missing feedback:\n%s", body)
	}
}

func TestAcceptTriggersWritebackOnlyWithProject(t *testing.T) {
	wb := &recordWriteback{}
	prov := &fakeProvider{enrich: validSpecJSON, exec: "ok"}
	e, _ := newTestEngine(t, prov, func(d *Deps) { d.Writeback = wb })

	// No project bound: acceptance must not write back to any graph.
	bare, _ := e.CreateTask("无项目任务", "", "", "")
	_ = e.EnrichTask(bare.ID)
	waitStatus(t, e, bare.ID, task.StatusReviewSpec)
	_ = e.RunTask(bare.ID)
	waitStatus(t, e, bare.ID, task.StatusReviewResult)
	_ = e.ReviewTask(bare.ID, "accept", "")
	time.Sleep(20 * time.Millisecond)
	if wb.count() != 0 {
		t.Errorf("writeback fired %d times without a project, want 0", wb.count())
	}

	bound, _ := e.CreateTask("有项目任务", "", "", "my-project")
	_ = e.EnrichTask(bound.ID)
	waitStatus(t, e, bound.ID, task.StatusReviewSpec)
	_ = e.RunTask(bound.ID)
	waitStatus(t, e, bound.ID, task.StatusReviewResult)
	_ = e.ReviewTask(bound.ID, "accept", "")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && wb.count() == 0 {
		time.Sleep(2 * time.Millisecond)
	}
	if wb.count() != 1 {
		t.Fatalf("writeback fired %d times with a project, want 1", wb.count())
	}
}

func TestEnrichInjectsRecalledKnowledge(t *testing.T) {
	recall := recallerFunc(func(_ context.Context, slug, _ string) (Knowledge, error) {
		if slug != "proj" {
			t.Errorf("recall slug = %q, want proj", slug)
		}
		return Knowledge{
			Context: "以下是该项目背景：订单服务使用 gRPC。",
			Names:   []string{"订单服务"},
			Sources: []string{"历史会话A"},
			Method:  "semantic",
		}, nil
	})
	prov := &fakeProvider{enrich: validSpecJSON, exec: "ok"}
	e, _ := newTestEngine(t, prov, func(d *Deps) { d.Recall = recall })

	tk, _ := e.CreateTask("加超时重试", "", "", "proj")
	_ = e.EnrichTask(tk.ID)
	got := waitStatus(t, e, tk.ID, task.StatusReviewSpec)

	// Provenance is recorded on the spec so the host can show what was injected.
	if len(got.Spec.InjectedKnowledge) != 1 || got.Spec.InjectedKnowledge[0] != "订单服务" {
		t.Errorf("injectedKnowledge = %v", got.Spec.InjectedKnowledge)
	}
	if got.Spec.RecallMethod != "semantic" {
		t.Errorf("recallMethod = %q", got.Spec.RecallMethod)
	}
	// And the background block must actually reach the model.
	first := prov.requests[0]
	if !strings.Contains(first.Messages[1].Content, "gRPC") {
		t.Errorf("enrich prompt missing recalled background:\n%s", first.Messages[1].Content)
	}
}

func TestUpdateSpecKeepsProvenance(t *testing.T) {
	prov := &fakeProvider{enrich: validSpecJSON, exec: "ok"}
	e, _ := newTestEngine(t, prov)

	tk, _ := e.CreateTask("x", "", "", "")
	_ = e.EnrichTask(tk.ID)
	got := waitStatus(t, e, tk.ID, task.StatusReviewSpec)

	// Simulate a host that saved provenance at enrich time.
	got.Spec.InjectedKnowledge = []string{"订单服务"}
	got.Spec.RecallMethod = "semantic"
	_ = e.store.UpdateTask(got)

	edited := got.Spec
	edited.Goal = "改过的目标"
	if err := e.UpdateSpec(tk.ID, edited); err != nil {
		t.Fatalf("UpdateSpec: %v", err)
	}

	after, _ := e.GetTask(tk.ID)
	if after.Spec.Goal != "改过的目标" {
		t.Errorf("goal = %q, want the edit to persist", after.Spec.Goal)
	}
	// Editing the form must not silently drop where the knowledge came from.
	if len(after.Spec.InjectedKnowledge) != 1 || after.Spec.RecallMethod != "semantic" {
		t.Errorf("provenance lost on spec edit: %+v", after.Spec)
	}
}

func TestRecoverStaleTasks(t *testing.T) {
	prov := &fakeProvider{enrich: validSpecJSON, exec: "ok"}
	e, store := newTestEngine(t, prov)

	mk := func(status task.Status) string {
		tk, _ := e.CreateTask("x", "", "", "")
		tk.Status = status
		_ = store.UpdateTask(tk)
		return tk.ID
	}
	executing := mk(task.StatusExecuting)
	queued := mk(task.StatusQueued)
	enriching := mk(task.StatusEnriching)

	e.RecoverStaleTasks()

	if got, _ := e.GetTask(executing); got.Status != task.StatusFailed || got.FailedStage != "execute" {
		t.Errorf("executing -> %q/%q, want failed/execute", got.Status, got.FailedStage)
	}
	if got, _ := e.GetTask(queued); got.Status != task.StatusFailed {
		t.Errorf("queued -> %q, want failed", got.Status)
	}
	if got, _ := e.GetTask(enriching); got.Status != task.StatusDraft {
		t.Errorf("enriching -> %q, want draft", got.Status)
	}
}

// recallerFunc adapts a function to Recaller.
type recallerFunc func(context.Context, string, string) (Knowledge, error)

func (f recallerFunc) Recall(ctx context.Context, slug, query string) (Knowledge, error) {
	return f(ctx, slug, query)
}
