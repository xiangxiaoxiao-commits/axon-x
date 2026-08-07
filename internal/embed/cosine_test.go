package embed

import (
	"math"
	"testing"
)

// cosineTol is the floating-point tolerance for cosine similarity comparisons.
const cosineTol = 1e-6

// almostEqual reports whether two float32 values are within cosineTol.
func almostEqual(a, b float32) bool {
	return math.Abs(float64(a)-float64(b)) <= cosineTol
}

// TestCosineBasicRelations covers the defining properties of cosine similarity:
// orthogonal vectors score ~0, identical directions ~1 and opposite ~-1.
func TestCosineBasicRelations(t *testing.T) {
	cases := []struct {
		name string
		a, b []float32
		want float32
	}{
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0},
		{"same direction", []float32{1, 2, 3}, []float32{2, 4, 6}, 1},
		{"identical", []float32{0.3, -0.7, 0.1}, []float32{0.3, -0.7, 0.1}, 1},
		{"opposite", []float32{1, 2, 3}, []float32{-1, -2, -3}, -1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Cosine(tc.a, tc.b)
			if !almostEqual(got, tc.want) {
				t.Errorf("Cosine(%v, %v) = %v, want %v (tol %g)", tc.a, tc.b, got, tc.want, cosineTol)
			}
		})
	}
}

// TestCosineDegenerate verifies the documented guard cases return exactly 0:
// zero vectors, mismatched lengths and empty input.
func TestCosineDegenerate(t *testing.T) {
	cases := []struct {
		name string
		a, b []float32
	}{
		{"zero vector a", []float32{0, 0, 0}, []float32{1, 2, 3}},
		{"zero vector b", []float32{1, 2, 3}, []float32{0, 0, 0}},
		{"both zero", []float32{0, 0}, []float32{0, 0}},
		{"length mismatch", []float32{1, 2, 3}, []float32{1, 2}},
		{"empty a", []float32{}, []float32{}},
		{"nil inputs", nil, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Cosine(tc.a, tc.b); got != 0 {
				t.Errorf("Cosine(%v, %v) = %v, want 0", tc.a, tc.b, got)
			}
		})
	}
}

// TestCosineSymmetric checks that similarity does not depend on argument order.
func TestCosineSymmetric(t *testing.T) {
	a := []float32{0.2, 0.9, -0.4, 0.1}
	b := []float32{-0.5, 0.3, 0.8, 0.2}
	if ab, ba := Cosine(a, b), Cosine(b, a); !almostEqual(ab, ba) {
		t.Errorf("Cosine not symmetric: Cosine(a,b)=%v, Cosine(b,a)=%v", ab, ba)
	}
}

// TestCosineSemanticRanking models the core of US-3.4 recall: a query vector is
// scored against several "memory" vectors, and the one pointing in a similar
// direction (semantically related, not lexically identical) must rank strictly
// above the unrelated ones. This is the deterministic stand-in for real
// embedding recall, which relies on the same similarity-ordering logic.
func TestCosineSemanticRanking(t *testing.T) {
	// query: an arbitrary direction in 4-space.
	query := []float32{0.9, 0.1, 0.4, 0.2}

	// related points in nearly the same direction as the query (a small
	// perturbation), standing in for a paraphrase that shares meaning but not
	// the exact words.
	related := []float32{0.85, 0.15, 0.45, 0.25}

	// unrelated vectors point elsewhere, including one nearly orthogonal and one
	// pointing away.
	unrelatedOrthogonal := []float32{-0.2, 0.9, -0.3, 0.1}
	unrelatedOpposite := []float32{-0.9, -0.1, -0.4, -0.2}

	sRelated := Cosine(query, related)
	sOrtho := Cosine(query, unrelatedOrthogonal)
	sOpposite := Cosine(query, unrelatedOpposite)

	if sRelated <= sOrtho {
		t.Errorf("related (%v) should outrank orthogonal (%v)", sRelated, sOrtho)
	}
	if sRelated <= sOpposite {
		t.Errorf("related (%v) should outrank opposite (%v)", sRelated, sOpposite)
	}
	// Sanity: the related score should be close to a perfect match.
	if sRelated < 0.9 {
		t.Errorf("related score %v unexpectedly low; expected a near-parallel match", sRelated)
	}
}
