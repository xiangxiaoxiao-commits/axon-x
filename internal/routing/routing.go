// Package routing maps a task type to a recommended provider/model and request
// parameters, and heuristically classifies free-form input into a task type.
// The reference IQ/cost/minutes figures come from the user's codexradar
// measurements and are shown to explain a recommendation; the model ids are
// mapped to the official APIs axon actually calls (Phase 2).
package routing

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// Task type identifiers (stable keys, also stored on conversations).
const (
	TaskDailyDevelopment     = "daily_development"
	TaskHardProblems         = "hard_problems"
	TaskBackgroundAutomation = "background_automation"
	TaskLobster              = "lobster_tasks"
)

// Recommendation is one concrete model choice for a task tier.
type Recommendation struct {
	Provider    string  `json:"provider"`    // provider Name (config), e.g. "anthropic"
	Model       string  `json:"model"`       // official model id, e.g. "claude-sonnet-5"
	Temperature float64 `json:"temperature"` // mapped from the original effort tier
	MaxTokens   int     `json:"maxTokens"`   // mapped from the original effort tier
	// Reference metrics from codexradar, for display only.
	IQ      float64 `json:"iq"`
	CostUSD float64 `json:"costUsd"`
	Minutes float64 `json:"minutes"`
}

// TaskProfile is the recommendation pair for one task type.
type TaskProfile struct {
	Key       string         `json:"key"`
	Title     string         `json:"title"`
	Primary   Recommendation `json:"primary"`
	Alternate Recommendation `json:"alternate"`
}

// Table is the full routing table keyed by task type.
type Table struct {
	Profiles map[string]TaskProfile `json:"profiles"`
	// Order preserves a stable display order for the four task types.
	Order []string `json:"order"`
}

//go:embed defaults.json
var defaultsJSON []byte

// Default returns the built-in routing table.
func Default() (Table, error) {
	var t Table
	if err := json.Unmarshal(defaultsJSON, &t); err != nil {
		return Table{}, fmt.Errorf("parse default routing table: %w", err)
	}
	return t, nil
}

// Classify guesses a task type from free-form input using simple, transparent
// heuristics (keywords + length). It returns a task key and never fails; the
// caller may always override. This is intentionally cheap and explainable
// rather than an LLM call.
func Classify(input string) string {
	s := strings.ToLower(input)

	// Hard problems: architecture, debugging tough issues, refactors, design.
	hardKeywords := []string{
		"重构", "架构", "设计", "疑难", "排查", "性能", "并发", "死锁", "内存泄漏",
		"refactor", "architect", "design", "debug", "race", "deadlock", "optimize", "root cause",
	}
	// Background automation: scripts, batch, cron, CI, pipelines.
	bgKeywords := []string{
		"脚本", "批量", "定时", "自动化", "迁移", "爬",
		"script", "batch", "cron", "pipeline", "ci", "automation", "migrate",
	}
	// Lobster: trivial, repetitive, high-volume throwaway tasks.
	lobsterKeywords := []string{
		"翻译", "格式化", "重命名", "改个", "简单",
		"translate", "rename", "format", "typo", "trivial",
	}

	switch {
	case containsAny(s, hardKeywords) || len(input) > 600:
		return TaskHardProblems
	case containsAny(s, bgKeywords):
		return TaskBackgroundAutomation
	case containsAny(s, lobsterKeywords):
		return TaskLobster
	default:
		return TaskDailyDevelopment
	}
}

func containsAny(s string, keywords []string) bool {
	for _, k := range keywords {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}
