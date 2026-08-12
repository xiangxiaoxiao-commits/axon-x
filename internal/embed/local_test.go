package embed

import (
	"context"
	"math"
	"testing"
)

// l2 returns the Euclidean norm of a vector, for normalization assertions.
func l2(v []float32) float64 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return math.Sqrt(sum)
}

func TestLocalEmbedder_Deterministic(t *testing.T) {
	e := NewLocal()
	a, _ := e.Embed(context.Background(), "订单服务的超时降级策略")
	b, _ := e.Embed(context.Background(), "订单服务的超时降级策略")
	if len(a) != len(b) {
		t.Fatalf("length mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic at %d: %v vs %v", i, a[i], b[i])
		}
	}
}

func TestLocalEmbedder_Dimension(t *testing.T) {
	e := NewLocal()
	v, _ := e.Embed(context.Background(), "hello world")
	if len(v) != localDim {
		t.Fatalf("expected dim %d, got %d", localDim, len(v))
	}
	if e.Model() != LocalModelID {
		t.Fatalf("expected model %q, got %q", LocalModelID, e.Model())
	}
}

func TestLocalEmbedder_UnitNorm(t *testing.T) {
	e := NewLocal()
	for _, text := range []string{"hello world", "订单服务的超时降级策略", "func main() { fmt.Println(1) }"} {
		v, _ := e.Embed(context.Background(), text)
		if n := l2(v); math.Abs(n-1) > 1e-5 {
			t.Errorf("norm of %q = %v, want ~1", text, n)
		}
	}
}

func TestLocalEmbedder_EmptyIsZero(t *testing.T) {
	e := NewLocal()
	for _, text := range []string{"", "   ", "\n\t"} {
		v, _ := e.Embed(context.Background(), text)
		if n := l2(v); n != 0 {
			t.Errorf("norm of empty %q = %v, want 0", text, n)
		}
	}
}

func TestLocalEmbedder_SimilarBeatsUnrelated(t *testing.T) {
	e := NewLocal()
	// English: shared subject should score much higher than an unrelated topic.
	q, _ := e.Embed(context.Background(), "how to configure the payment service timeout")
	similar, _ := e.Embed(context.Background(), "configuring timeout for the payment service")
	unrelated, _ := e.Embed(context.Background(), "the cat sat quietly on a warm windowsill")

	sSim := Cosine(q, similar)
	sUnrel := Cosine(q, unrelated)
	if sSim <= sUnrel {
		t.Errorf("similar (%.3f) should beat unrelated (%.3f)", sSim, sUnrel)
	}
	if sSim < 0.3 {
		t.Errorf("similar cosine too low: %.3f", sSim)
	}
}

func TestLocalEmbedder_Chinese(t *testing.T) {
	e := NewLocal()
	// Chinese without word boundaries: char n-grams should still separate a
	// related sentence from an unrelated one.
	q, _ := e.Embed(context.Background(), "订单服务在高并发下的超时和熔断降级方案")
	similar, _ := e.Embed(context.Background(), "订单服务超时熔断降级怎么配置")
	unrelated, _ := e.Embed(context.Background(), "今天天气很好适合去公园散步")

	sSim := Cosine(q, similar)
	sUnrel := Cosine(q, unrelated)
	if sSim <= sUnrel {
		t.Errorf("chinese similar (%.3f) should beat unrelated (%.3f)", sSim, sUnrel)
	}
	if sSim < 0.3 {
		t.Errorf("chinese similar cosine too low: %.3f", sSim)
	}
}

func TestLocalEmbedder_Batch(t *testing.T) {
	e := NewLocal()
	texts := []string{"alpha beta", "gamma delta", "订单服务"}
	batch, err := e.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != len(texts) {
		t.Fatalf("expected %d vectors, got %d", len(texts), len(batch))
	}
	// Batch results must equal the per-text Embed results (order preserved).
	for i, text := range texts {
		single, _ := e.Embed(context.Background(), text)
		if Cosine(batch[i], single) < 0.9999 {
			t.Errorf("batch[%d] differs from single Embed of %q", i, text)
		}
	}
}
