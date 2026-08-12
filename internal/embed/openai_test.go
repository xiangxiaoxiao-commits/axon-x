package embed

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIEmbedder_Embed(t *testing.T) {
	const secretKey = "sk-super-secret-key-value"
	want := []float32{0.1, -0.2, 0.3}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/embeddings") {
			t.Errorf("path = %s, want .../embeddings", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+secretKey {
			t.Errorf("Authorization = %q, want bearer key", got)
		}

		var body openaiEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Input != "hello" {
			t.Errorf("input = %q, want hello", body.Input)
		}
		if body.EncodingFormat != "float" {
			t.Errorf("encoding_format = %q, want float", body.EncodingFormat)
		}

		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[{"embedding":[0.1,-0.2,0.3]}]}`)
	}))
	defer srv.Close()

	e := NewOpenAI(srv.URL, secretKey, "")
	if e.Model() != defaultOpenAIModel {
		t.Errorf("Model() = %q, want default %q", e.Model(), defaultOpenAIModel)
	}

	got, err := e.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestOpenAIEmbedder_EmbedBatch(t *testing.T) {
	// Return embeddings out of index order to verify the client re-orders them.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body openaiBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode batch request: %v", err)
		}
		if len(body.Input) != 3 {
			t.Errorf("input len = %d, want 3", len(body.Input))
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[
			{"index":2,"embedding":[0.3]},
			{"index":0,"embedding":[0.1]},
			{"index":1,"embedding":[0.2]}
		]}`)
	}))
	defer srv.Close()

	e := NewOpenAI(srv.URL, "sk-key", "")
	got, err := e.EmbedBatch(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	want := []float32{0.1, 0.2, 0.3}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i := range want {
		if len(got[i]) != 1 || got[i][0] != want[i] {
			t.Errorf("got[%d] = %v, want [%v] (order not restored by index)", i, got[i], want[i])
		}
	}
}

func TestOpenAIEmbedder_EmbedBatchEmpty(t *testing.T) {
	e := NewOpenAI("http://unused", "sk-key", "")
	got, err := e.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("EmbedBatch(nil): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestOpenAIEmbedder_AuthErrorHidesKey(t *testing.T) {
	const secretKey = "sk-super-secret-key-value"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer srv.Close()

	e := NewOpenAI(srv.URL, secretKey, "custom-model")
	if e.Model() != "custom-model" {
		t.Errorf("Model() = %q, want custom-model", e.Model())
	}

	_, err := e.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "authentication failed") {
		t.Errorf("error = %q, want readable auth message", msg)
	}
	if strings.Contains(msg, secretKey) {
		t.Errorf("error leaked API key: %q", msg)
	}
	if strings.Contains(msg, "Bearer") {
		t.Errorf("error leaked Authorization header: %q", msg)
	}
}
