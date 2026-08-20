package main

import (
	"regexp"
	"strings"
)

// isSnapshotObs returns true if an observation is a one-time snapshot that
// should NOT be persisted into the knowledge graph. Snapshots are specific
// deployment states, pipeline IDs, image tags, commit hashes — information
// that becomes stale immediately and can mislead agents when used as facts.
func isSnapshotObs(obs string) bool {
	// Explicit rule indicators: keep even if other patterns match.
	for _, r := range ruleIndicators {
		if strings.Contains(obs, r) {
			return false
		}
	}

	// Check regex patterns.
	if reImageTag.MatchString(obs) {
		return true
	}
	if reSnapshotIP.MatchString(obs) {
		return true
	}
	if rePipeline.MatchString(obs) {
		return true
	}
	if reImageRepo.MatchString(obs) {
		return true
	}

	// Commit hash with context.
	if reCommitHash.MatchString(obs) {
		ol := strings.ToLower(obs)
		for _, ctx := range commitContexts {
			if strings.Contains(ol, ctx) {
				return true
			}
		}
	}

	// Keyword matches.
	for _, kw := range snapshotKeywords {
		if strings.Contains(obs, kw) {
			return true
		}
	}

	return false
}

// Compiled regex patterns for snapshot detection.
var (
	// Image tags: x.y.z-beta-<hex> or x.y.z-stable-<hex>
	reImageTag = regexp.MustCompile(`\d+\.\d+\.\d+-(?:beta|stable)-[0-9a-f]{6,}`)
	// Internal/private IP addresses (deployment-specific)
	reSnapshotIP = regexp.MustCompile(`(?:10\.12\.0\.78|10\.99\.\d+|172\.20\.\d+|39\.105\.\d+)`)
	// Pipeline IDs: "pipeline 20xxxx" or "pipeline#20xxxx"
	rePipeline = regexp.MustCompile(`(?i)pipeline\s*#?\s*\d{5,}`)
	// Internal image repo
	reImageRepo = regexp.MustCompile(`10\.12\.0\.78:5000/`)
	// Git commit hashes (7+ hex chars)
	reCommitHash = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
)

var commitContexts = []string{
	"提交", "commit", "push", "合入", "cherry-pick", "rebase", "已推送",
}

var snapshotKeywords = []string{
	"单副本 Running",
	"Pod 状态为 Running",
	"Pod 状态为",
	"已扩容",
	"已推送到 release/",
	"已合入 release/",
	"已 push 到",
	"已推送远程",
	"提交为 ",
	"最终提交为",
	"修复提交为",
	"当前镜像为",
	"修复镜像为",
	"构建镜像为",
	"部署镜像为",
	"pipeline 均成功",
	"构建成功",
	"CI 成功后",
	"pipeline 构建成功",
}

var ruleIndicators = []string{
	"push 后自动触发",
	"push 后会自动",
	"自动触发构建",
	"约定", "约束", "必须", "不能", "不允许",
	"规则", "规范", "流程", "架构", "职责", "负责",
	"设计决策", "原因是", "因为", "为了",
}
