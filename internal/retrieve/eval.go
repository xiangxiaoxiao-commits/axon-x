package retrieve

import (
	"strings"

	"axon/internal/graph"
)

// EvalCase is one deterministic offline recall expectation. QueryVector is
// supplied by the fixture so evaluation never depends on a network model.
type EvalCase struct {
	Name        string
	Query       string
	QueryVector []float32
	Expected    []string
	Forbidden   []string
	TopK        int
}

type EvalReport struct {
	Cases              int
	PrecisionAtK       float64
	Recall             float64
	ErrorExpansionRate float64
	SourceCoverage     float64
}

// Evaluate runs a fixed corpus through the production recall path and returns
// aggregate quality metrics suitable for CI regression thresholds.
func Evaluate(g *graph.Graph, chunks []graph.Chunk, cases []EvalCase, opts RecallOpts) EvalReport {
	report := EvalReport{Cases: len(cases)}
	if len(cases) == 0 {
		return report
	}
	var precision, recall, errorRate, coverage float64
	for _, c := range cases {
		result := RecallWithOpts(g, chunks, c.QueryVector, c.Query, opts)
		k := c.TopK
		if k <= 0 || k > len(result.OrderedHits) {
			k = len(result.OrderedHits)
		}
		expected := normalizedSet(c.Expected)
		forbidden := normalizedSet(c.Forbidden)
		correct := 0
		for _, id := range result.OrderedHits[:k] {
			if expected[id] {
				correct++
			}
		}
		if k > 0 {
			precision += float64(correct) / float64(k)
		}
		found := 0
		for id := range expected {
			if result.Hit[id] {
				found++
			}
		}
		if len(expected) > 0 {
			recall += float64(found) / float64(len(expected))
		}
		bad := 0
		for id := range forbidden {
			if result.Hit[id] {
				bad++
			}
		}
		if len(forbidden) > 0 {
			errorRate += float64(bad) / float64(len(forbidden))
		}
		if len(result.Chunks) > 0 {
			withSource := 0
			for _, chunk := range result.Chunks {
				if strings.TrimSpace(chunk.Source) != "" {
					withSource++
				}
			}
			coverage += float64(withSource) / float64(len(result.Chunks))
		}
	}
	n := float64(len(cases))
	report.PrecisionAtK = precision / n
	report.Recall = recall / n
	report.ErrorExpansionRate = errorRate / n
	report.SourceCoverage = coverage / n
	return report
}

func normalizedSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[strings.ToLower(strings.TrimSpace(value))] = true
	}
	return out
}
