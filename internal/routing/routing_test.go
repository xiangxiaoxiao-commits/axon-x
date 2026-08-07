package routing

import (
	"strings"
	"testing"
)

// TestDefaultTableWellFormed guards the hand-written defaults.json: it must
// parse, expose exactly the four task types, keep Order consistent with the
// Profiles key set, and carry sane recommendation values.
func TestDefaultTableWellFormed(t *testing.T) {
	tbl, err := Default()
	if err != nil {
		t.Fatalf("Default() returned error: %v", err)
	}

	wantTypes := []string{
		TaskDailyDevelopment,
		TaskHardProblems,
		TaskBackgroundAutomation,
		TaskLobster,
	}

	if len(tbl.Order) != len(wantTypes) {
		t.Errorf("Order has %d entries, want %d: %v", len(tbl.Order), len(wantTypes), tbl.Order)
	}
	if len(tbl.Profiles) != len(wantTypes) {
		t.Errorf("Profiles has %d entries, want %d", len(tbl.Profiles), len(wantTypes))
	}

	// Order and Profiles must describe the same key set.
	orderSet := make(map[string]bool, len(tbl.Order))
	for _, k := range tbl.Order {
		if orderSet[k] {
			t.Errorf("Order contains duplicate key %q", k)
		}
		orderSet[k] = true
		if _, ok := tbl.Profiles[k]; !ok {
			t.Errorf("Order key %q has no matching profile", k)
		}
	}
	for k := range tbl.Profiles {
		if !orderSet[k] {
			t.Errorf("Profiles key %q missing from Order", k)
		}
	}

	// Every expected task type must be present.
	for _, k := range wantTypes {
		if _, ok := tbl.Profiles[k]; !ok {
			t.Errorf("missing profile for task type %q", k)
		}
	}

	// Validate each recommendation pair.
	for key, prof := range tbl.Profiles {
		if prof.Key != key {
			t.Errorf("profile %q has mismatched Key field %q", key, prof.Key)
		}
		checkRecommendation(t, key, "primary", prof.Primary)
		checkRecommendation(t, key, "alternate", prof.Alternate)
	}
}

func checkRecommendation(t *testing.T, taskKey, tier string, r Recommendation) {
	t.Helper()
	if strings.TrimSpace(r.Provider) == "" {
		t.Errorf("%s/%s: Provider is empty", taskKey, tier)
	}
	if strings.TrimSpace(r.Model) == "" {
		t.Errorf("%s/%s: Model is empty", taskKey, tier)
	}
	if r.MaxTokens <= 0 {
		t.Errorf("%s/%s: MaxTokens = %d, want > 0", taskKey, tier, r.MaxTokens)
	}
	if r.IQ <= 0 {
		t.Errorf("%s/%s: IQ = %g, want > 0", taskKey, tier, r.IQ)
	}
}

// classifySample is one labelled input for the accuracy suite.
type classifySample struct {
	input string
	want  string
}

// classifySamples is a table of typical phrasings the heuristic is expected to
// handle, spread across the four task types (both Chinese and English).
var classifySamples = []classifySample{
	// Hard problems: architecture, refactors, deep debugging, very long input.
	{"帮我重构支付模块的架构", TaskHardProblems},
	{"排查这个死锁问题", TaskHardProblems},
	{"分析一下这段代码的性能瓶颈", TaskHardProblems},
	{"这个高并发场景应该怎么设计", TaskHardProblems},
	{"optimize the query performance", TaskHardProblems},
	{"help me debug this race condition", TaskHardProblems},
	{"find the root cause of this issue", TaskHardProblems},
	// Overlong input should route to hard problems via the length branch.
	{strings.Repeat("这是一段很长的需求描述文本，包含很多背景信息和上下文说明。", 20), TaskHardProblems},

	// Background automation: scripts, batch jobs, cron, CI, pipelines, crawling.
	{"写个批量迁移脚本", TaskBackgroundAutomation},
	{"配置 CI pipeline", TaskBackgroundAutomation},
	{"定时爬取数据", TaskBackgroundAutomation},
	{"写一个自动化的数据处理脚本", TaskBackgroundAutomation},
	{"帮我做一次数据迁移", TaskBackgroundAutomation},
	{"write a batch script to process the files", TaskBackgroundAutomation},
	{"set up a cron job for nightly runs", TaskBackgroundAutomation},

	// Lobster: trivial, repetitive, throwaway edits.
	{"把这段翻译成英文", TaskLobster},
	{"格式化这个 JSON", TaskLobster},
	{"帮我改个变量名", TaskLobster},
	{"这只是个简单的小改动", TaskLobster},
	{"rename these variables", TaskLobster},
	{"fix a typo in the readme", TaskLobster},
	{"translate this comment to English", TaskLobster},

	// Daily development: ordinary coding requests without special keywords.
	{"给这个函数加个参数", TaskDailyDevelopment},
	{"写一个 REST handler", TaskDailyDevelopment},
	{"帮我加一个用户登录接口", TaskDailyDevelopment},
	{"把这个方法拆成两个", TaskDailyDevelopment},
	{"给列表加一个分页功能", TaskDailyDevelopment},
	{"实现一个购物车的增删改查", TaskDailyDevelopment},
	{"add a new field to the user model", TaskDailyDevelopment},
	{"写个单元测试覆盖这个分支", TaskDailyDevelopment},
}

// TestClassifyAccuracy is the core check for PRD F3.5 / NFR 6.4: the heuristic
// classifier must reach at least a 0.70 hit rate on the labelled sample set.
func TestClassifyAccuracy(t *testing.T) {
	const threshold = 0.70

	if len(classifySamples) < 20 {
		t.Fatalf("sample set too small: %d, want >= 20", len(classifySamples))
	}

	hits := 0
	for _, s := range classifySamples {
		got := Classify(s.input)
		if got == s.want {
			hits++
			continue
		}
		// Report every miss so the keyword set can be tuned.
		t.Logf("MISS: input=%q want=%s got=%s", preview(s.input), s.want, got)
	}

	rate := float64(hits) / float64(len(classifySamples))
	t.Logf("classify accuracy: %d/%d = %.2f (threshold %.2f)", hits, len(classifySamples), rate, threshold)

	if rate < threshold {
		t.Errorf("classify accuracy %.2f below threshold %.2f", rate, threshold)
	}
}

// preview shortens long inputs for readable failure logs.
func preview(s string) string {
	const max = 40
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// TestClassifyDeterministic verifies Classify is a pure function: repeated calls
// on the same input always yield the same task type.
func TestClassifyDeterministic(t *testing.T) {
	inputs := []string{
		"帮我重构支付模块的架构",
		"写个批量迁移脚本",
		"把这段翻译成英文",
		"给这个函数加个参数",
		"",
	}
	for _, in := range inputs {
		first := Classify(in)
		for i := 0; i < 5; i++ {
			if got := Classify(in); got != first {
				t.Errorf("Classify(%q) not deterministic: got %s then %s", preview(in), first, got)
			}
		}
	}
}
