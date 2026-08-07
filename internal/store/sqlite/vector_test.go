package sqlite

import (
	"math"
	"testing"
)

func TestVectorRoundTrip(t *testing.T) {
	cases := [][]float32{
		nil,
		{},
		{0},
		{1, -1, 0.5, -0.5},
		{math.MaxFloat32, -math.MaxFloat32, math.SmallestNonzeroFloat32},
		{float32(math.Inf(1)), float32(math.Inf(-1))},
	}

	for _, want := range cases {
		got, err := decodeVector(encodeVector(want))
		if err != nil {
			t.Fatalf("decodeVector(encodeVector(%v)) returned error: %v", want, err)
		}
		if len(got) != len(want) {
			t.Fatalf("length mismatch: got %d, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("element %d: got %v, want %v", i, got[i], want[i])
			}
		}
	}
}

func TestDecodeVectorNaN(t *testing.T) {
	// NaN survives the bit-level round trip even though NaN != NaN.
	in := []float32{float32(math.NaN())}
	got, err := decodeVector(encodeVector(in))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || !math.IsNaN(float64(got[0])) {
		t.Fatalf("expected a single NaN, got %v", got)
	}
}

func TestDecodeVectorBadLength(t *testing.T) {
	for _, n := range []int{1, 2, 3, 5, 7} {
		if _, err := decodeVector(make([]byte, n)); err == nil {
			t.Errorf("decodeVector with %d bytes: expected error, got nil", n)
		}
	}
}
