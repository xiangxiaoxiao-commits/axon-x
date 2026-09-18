package graph

import (
	"testing"
	"time"
)

func TestUsableAssertionsWithholdsDirectContradictions(t *testing.T) {
	e := Entity{Name: "发布规则", Assertions: []Assertion{
		{Text: "生产发布允许跳过审批", State: RelationAsserted, Confidence: .9},
		{Text: "生产发布不允许跳过审批", State: RelationAsserted, Confidence: .9},
		{Text: "回滚必须保留审计记录", State: RelationVerified, Confidence: 1},
	}}
	got := UsableAssertions(e, time.Now())
	if len(got) != 1 || got[0].Text != "回滚必须保留审计记录" {
		t.Fatalf("usable assertions = %+v", got)
	}
}

func TestConflictDetectionDoesNotGuessRelatedFacts(t *testing.T) {
	e := Entity{Assertions: []Assertion{
		{Text: "支付服务使用 Redis 缓存"},
		{Text: "订单服务不允许跳过审批"},
	}}
	if got := AssertionConflictIndexes(e); len(got) != 0 {
		t.Fatalf("unrelated facts marked conflicting: %v", got)
	}
}

func TestCandidateContradictionDoesNotSuppressTrustedFact(t *testing.T) {
	e := Entity{Assertions: []Assertion{
		{Text: "生产发布允许跳过审批", State: RelationVerified, Confidence: 1},
		{Text: "生产发布不允许跳过审批", State: RelationCandidate, Confidence: .45},
	}}
	got := UsableAssertions(e, time.Now())
	if len(got) != 1 || got[0].State != RelationVerified {
		t.Fatalf("candidate suppressed trusted fact: %+v", got)
	}
}
