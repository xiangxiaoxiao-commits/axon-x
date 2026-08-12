// Package embed abstracts text embedding behind one interface so the vector
// backend (currently the OpenAI embeddings API) can be swapped for a local
// model later without touching callers.
package embed

import (
	"context"
	"math"
)

// Embedder turns text into a dense vector. Implementations are expected to be
// safe for concurrent use.
type Embedder interface {
	// Embed returns the embedding for a single text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch returns embeddings for many texts in one (or a few) request(s),
	// preserving input order. Implementations are expected to internally split
	// the input into vendor-friendly batches. Chunk indexing produces far more
	// vectors than entity distillation, so batching is required to keep it usable.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Model reports the embedding model id (stored alongside vectors so a model
	// change can be detected and old vectors re-embedded).
	Model() string
}

// Cosine returns the cosine similarity of two equal-length vectors in [-1, 1].
// Returns 0 when either vector is zero-length, mismatched, or zero-magnitude.
func Cosine(a, b []float32) float32 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}
