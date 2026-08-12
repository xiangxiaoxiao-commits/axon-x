package embed

import (
	"context"
	"errors"
	"testing"
)

// fakeEmbedder counts Embed calls and can simulate an embedding-incapable
// endpoint (e.g. a gateway that 503s on /v1/embeddings).
type fakeEmbedder struct {
	calls int
	err   error
	empty bool // return an empty vector even without an error
}

func (f *fakeEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.empty {
		return []float32{}, nil
	}
	return []float32{0.1, 0.2}, nil
}

func (f *fakeEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	return make([][]float32, len(texts)), nil
}

func (f *fakeEmbedder) Model() string { return "fake" }

func TestProbe(t *testing.T) {
	if err := Probe(context.Background(), &fakeEmbedder{}); err != nil {
		t.Fatalf("healthy endpoint: unexpected error: %v", err)
	}
	if err := Probe(context.Background(), &fakeEmbedder{err: errors.New("503")}); err == nil {
		t.Fatal("failing endpoint: expected error, got nil")
	}
	if err := Probe(context.Background(), &fakeEmbedder{empty: true}); err == nil {
		t.Fatal("empty-vector endpoint: expected error, got nil")
	}
}

func TestProbeCache_MemoizesAndReportsProbed(t *testing.T) {
	c := NewProbeCache()
	e := &fakeEmbedder{}

	usable, probed, err := c.Usable(context.Background(), e, "k")
	if !usable || !probed || err != nil {
		t.Fatalf("first call: usable=%v probed=%v err=%v", usable, probed, err)
	}
	usable, probed, err = c.Usable(context.Background(), e, "k")
	if !usable || probed || err != nil {
		t.Fatalf("cached call: usable=%v probed=%v err=%v", usable, probed, err)
	}
	if e.calls != 1 {
		t.Fatalf("expected exactly 1 network probe, got %d", e.calls)
	}
}

func TestProbeCache_CachesFailure(t *testing.T) {
	c := NewProbeCache()
	e := &fakeEmbedder{err: errors.New("503")}

	usable, probed, err := c.Usable(context.Background(), e, "k")
	if usable || !probed || err == nil {
		t.Fatalf("first call: usable=%v probed=%v err=%v", usable, probed, err)
	}
	usable, probed, _ = c.Usable(context.Background(), e, "k")
	if usable || probed {
		t.Fatalf("cached failure: usable=%v probed=%v", usable, probed)
	}
	if e.calls != 1 {
		t.Fatalf("expected exactly 1 network probe, got %d", e.calls)
	}

	// A different endpoint key re-probes rather than reusing the cached result.
	if _, probed, _ := c.Usable(context.Background(), &fakeEmbedder{}, "other"); !probed {
		t.Fatal("new key should trigger a fresh probe")
	}
}
