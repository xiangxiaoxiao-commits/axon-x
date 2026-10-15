package graph

import "time"

type HealthIssue struct {
	Code    string `json:"code"`
	Level   string `json:"level"` // info | warning | error
	Count   int    `json:"count"`
	Message string `json:"message"`
}

type GraphHealth struct {
	Score                   int           `json:"score"`
	Entities                int           `json:"entities"`
	Assertions              int           `json:"assertions"`
	TrustedAssertions       int           `json:"trustedAssertions"`
	CandidateAssertions     int           `json:"candidateAssertions"`
	ExpiredAssertions       int           `json:"expiredAssertions"`
	ConflictingAssertions   int           `json:"conflictingAssertions"`
	SourcelessAssertions    int           `json:"sourcelessAssertions"`
	Relations               int           `json:"relations"`
	TrustedRelations        int           `json:"trustedRelations"`
	CandidateRelations      int           `json:"candidateRelations"`
	SourcelessRelations     int           `json:"sourcelessRelations"`
	OversizedRelationLabels int           `json:"oversizedRelationLabels"`
	IsolatedEntities        int           `json:"isolatedEntities"`
	GovernanceActions       int           `json:"governanceActions"`
	CanUndo                 bool          `json:"canUndo"`
	Issues                  []HealthIssue `json:"issues"`
}

func AssessHealth(g *Graph, now time.Time) GraphHealth {
	h := GraphHealth{Issues: []HealthIssue{}}
	if g == nil {
		return h
	}
	h.Entities = len(g.Entities)
	connected := make(map[string]bool, len(g.Entities))
	for i := range g.Entities {
		NormalizeEntityAssertions(&g.Entities[i])
		conflicts := assertionConflictIndexesAt(g.Entities[i], now)
		for assertionIndex, a := range g.Entities[i].Assertions {
			h.Assertions++
			if conflicts[assertionIndex] {
				h.ConflictingAssertions++
			}
			if !conflicts[assertionIndex] && AssertionUsable(a, now) {
				h.TrustedAssertions++
			}
			if NormalizeAssertion(a).State == RelationCandidate {
				h.CandidateAssertions++
			}
			if a.ValidUntil > 0 && now.UnixMilli() > a.ValidUntil {
				h.ExpiredAssertions++
			}
			if len(a.Sources) == 0 {
				h.SourcelessAssertions++
			}
		}
	}
	for _, relation := range g.Relations {
		r := NormalizeRelation(relation)
		h.Relations++
		connected[normKey(r.From)] = true
		connected[normKey(r.To)] = true
		if RelationUsableForRecall(r) {
			h.TrustedRelations++
		}
		if r.State == RelationCandidate {
			h.CandidateRelations++
		}
		if len(r.Sources) == 0 {
			h.SourcelessRelations++
		}
		if IsOversizedRelationLabel(r.Label) {
			h.OversizedRelationLabels++
		}
	}
	for _, e := range g.Entities {
		if !connected[normKey(e.Name)] {
			h.IsolatedEntities++
		}
	}
	addIssue := func(code, level string, count int, message string) {
		if count > 0 {
			h.Issues = append(h.Issues, HealthIssue{Code: code, Level: level, Count: count, Message: message})
		}
	}
	addIssue("candidate_relations", "warning", h.CandidateRelations, "待确认的候选关系不会参与召回")
	addIssue("sourceless_assertions", "warning", h.SourcelessAssertions, "缺少来源的事实难以核验")
	addIssue("sourceless_relations", "warning", h.SourcelessRelations, "缺少来源的关系难以核验")
	addIssue("oversized_relation_labels", "warning", h.OversizedRelationLabels, "关系标签写成了整句而非谓词，无法归一、复用或参与推理")
	addIssue("expired_assertions", "warning", h.ExpiredAssertions, "已过有效期的事实不会参与召回")
	addIssue("conflicting_assertions", "error", h.ConflictingAssertions, "检测到直接冲突的事实，审核前均不会参与召回")
	addIssue("isolated_entities", "info", h.IsolatedEntities, "孤立实体仍可按语义或关键词召回")

	if h.Entities == 0 {
		return h
	}
	penalty := ratioPenalty(h.SourcelessAssertions, h.Assertions, 25) +
		ratioPenalty(h.SourcelessRelations, h.Relations, 20) +
		ratioPenalty(h.CandidateRelations, h.Relations, 15) +
		ratioPenalty(h.OversizedRelationLabels, h.Relations, 10) +
		ratioPenalty(h.ExpiredAssertions, h.Assertions, 20) +
		ratioPenalty(h.ConflictingAssertions, h.Assertions, 30) +
		ratioPenalty(h.IsolatedEntities, h.Entities, 10)
	h.Score = 100 - penalty
	if h.Score < 0 {
		h.Score = 0
	}
	return h
}

func ratioPenalty(part, total, weight int) int {
	if part <= 0 || total <= 0 {
		return 0
	}
	return (part*weight + total - 1) / total
}
