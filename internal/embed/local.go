package embed

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

// LocalModelID is the model id reported by LocalEmbedder. Stored alongside
// vectors (like any other model id) and used by callers to detect that recall
// ran on the local, dependency-free fallback rather than a cloud model.
const LocalModelID = "local-hashing-v1"

// localDim is the fixed vector dimension. A power of two so bucket selection by
// masking the low bits stays independent of the sign bit taken from the top.
const localDim = 1024

// LocalEmbedder is a pure-Go, zero-dependency, offline embedder used as a
// fallback when no cloud embedding service is configured. It turns text into a
// deterministic L2-normalized vector via character n-gram + word feature
// hashing (the "hashing trick") with sublinear term frequencies.
//
// This is a LEXICAL vector, NOT a neural embedding: it captures surface overlap
// (shared characters, n-grams, tokens) so it can fuzzily recall related text —
// far better than exact substring matching, and it works for Chinese (character
// n-grams need no word boundaries) and code. It does NOT capture cross-lingual
// or deep semantic meaning the way a trained model does. It is deterministic,
// CPU-only, extremely fast, and never touches the network.
//
// Safe for concurrent use (no mutable state).
type LocalEmbedder struct {
	dim int
}

// NewLocal builds a LocalEmbedder with the default dimension.
func NewLocal() *LocalEmbedder {
	return &LocalEmbedder{dim: localDim}
}

// Model implements Embedder.
func (e *LocalEmbedder) Model() string { return LocalModelID }

// Embed implements Embedder. It is deterministic: the same text always yields
// the same vector. The context is unused (no I/O) but kept for interface
// compatibility. Empty/whitespace text yields a zero vector.
func (e *LocalEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	vec := make([]float32, e.dim)

	// Normalize: lowercase (case-insensitive matching, mainly for English/code)
	// and collapse runs of whitespace to a single space so char n-grams that span
	// a word boundary are stable.
	norm := strings.Join(strings.Fields(strings.ToLower(text)), " ")
	if norm == "" {
		return vec, nil
	}

	// Count raw term frequencies, then apply the hashing trick with sublinear TF.
	tf := make(map[string]int)
	addCharNGrams(norm, tf)
	addWordTokens(norm, tf)

	for term, count := range tf {
		// Sublinear TF (1 + log(count)) dampens very frequent features.
		weight := 1 + math.Log(float64(count))
		sum := hash64(term)
		bucket := int(sum % uint64(e.dim))
		// Signed hashing: the top bit picks +/- so distinct features that collide
		// in the same bucket tend to cancel rather than always reinforce, reducing
		// collision bias without a vocabulary.
		if sum>>63&1 == 0 {
			vec[bucket] += float32(weight)
		} else {
			vec[bucket] -= float32(weight)
		}
	}

	l2Normalize(vec)
	return vec, nil
}

// EmbedBatch implements Embedder. Local embedding has no network cost, so it
// simply maps Embed over the inputs, preserving order.
func (e *LocalEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := e.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// addCharNGrams adds unigram+bigram+trigram character features. Character
// n-grams are the workhorse for Chinese (which has no word boundaries) and give
// English/code robust subword overlap. N-grams that are entirely whitespace are
// skipped as noise. Features are prefixed by length so they occupy distinct
// hash spaces.
func addCharNGrams(s string, tf map[string]int) {
	runes := []rune(s)
	for n := 1; n <= 3; n++ {
		if len(runes) < n {
			break
		}
		prefix := string(rune('0'+n)) + ":"
		for i := 0; i+n <= len(runes); i++ {
			gram := runes[i : i+n]
			if allSpace(gram) {
				continue
			}
			tf[prefix+string(gram)]++
		}
	}
}

// addWordTokens adds whole-token features (runs of letters/digits), giving an
// exact-word signal on top of the fuzzy n-grams. Prefixed "w:" to stay in a
// separate hash space from character n-grams.
func addWordTokens(s string, tf map[string]int) {
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			tf["w:"+b.String()]++
			b.Reset()
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
}

func allSpace(rs []rune) bool {
	for _, r := range rs {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// hash64 returns the FNV-1a 64-bit hash of s. The low bits select the bucket
// and the top bit selects the sign; both are derived from one hash.
func hash64(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// l2Normalize scales v in place to unit length so Cosine reduces to a dot
// product and similarity is comparable across texts of different sizes. A
// zero vector is left unchanged.
func l2Normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}

// Compile-time check that LocalEmbedder satisfies the Embedder interface.
var _ Embedder = (*LocalEmbedder)(nil)
