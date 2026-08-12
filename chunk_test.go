package main

import (
	"strings"
	"testing"

	"axon/internal/claudedata"
)

func TestIsNoiseChunk(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"greeting", "user: 好的", true},
		{"short", "user: 嗯嗯", true},
		{"thanks", "assistant: thanks", true},
		{"substantive", "user: 幂等键到底用订单号还是单独生成？我担心回调会重复投递导致重复扣款，需要一个稳妥的方案", false},
		{"log block", "assistant:\nat foo.bar(x)\nat baz.qux(y)\ngoroutine 1 [running]:\nerror: boom", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isNoiseChunk(c.text); got != c.want {
				t.Errorf("isNoiseChunk(%q) = %v, want %v", c.text, got, c.want)
			}
		})
	}
}

func TestChunkTranscript_SkipsNoiseAndKeepsPrefix(t *testing.T) {
	long := strings.Repeat("这是一段有意义的关于支付服务幂等设计的讨论内容。", 20)
	msgs := []claudedata.SessionMessage{
		{Role: "user", Text: "好的"}, // noise, dropped
		{Role: "user", Text: long},
		{Role: "assistant", Text: long},
	}
	chunks := chunkTranscript("sess-1", msgs)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk from substantive turns")
	}
	for _, ch := range chunks {
		if ch.Source != "sess-1" || ch.Kind != "chat" {
			t.Errorf("bad chunk metadata: %+v", ch)
		}
		if strings.TrimSpace(ch.Text) == "好的" {
			t.Error("noise-only chunk should have been dropped")
		}
		if !strings.Contains(ch.Text, "user:") && !strings.Contains(ch.Text, "assistant:") {
			t.Errorf("chunk lost speaker prefix: %q", ch.Text)
		}
	}
}

func TestChunkCodeFile(t *testing.T) {
	content := "package foo\n\nfunc A() {\n\treturn\n}\n\nfunc B() {\n\treturn\n}\n"
	chunks := chunkCodeFile("foo/bar.go", content)
	if len(chunks) == 0 {
		t.Fatal("expected code chunks")
	}
	for _, ch := range chunks {
		if ch.Kind != "code" || ch.Source != "code:foo/bar.go" {
			t.Errorf("bad code chunk metadata: %+v", ch)
		}
		if !strings.Contains(ch.Text, "foo/bar.go") {
			t.Errorf("code chunk missing path marker: %q", ch.Text)
		}
	}
}

func TestRRFFuse(t *testing.T) {
	// "b" is rank 1 in list two and rank 2 in list one, so it should beat "a"
	// (rank 1 in one list only) and "c" (rank 3, one list).
	got := rrfFuse(
		[]string{"a", "b", "c"},
		[]string{"b", "d"},
	)
	if len(got) != 4 {
		t.Fatalf("fused len = %d, want 4", len(got))
	}
	if got[0] != "b" {
		t.Errorf("top = %q, want b (appears high in both lists)", got[0])
	}
	// Every input id must appear exactly once.
	seen := map[string]int{}
	for _, id := range got {
		seen[id]++
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if seen[id] != 1 {
			t.Errorf("id %q appears %d times, want 1", id, seen[id])
		}
	}
}

func TestQueryTerms(t *testing.T) {
	got := queryTerms("orchestrate.go runEnrich a")
	// "a" is dropped (len < 2). Punctuation-trimmed tokens survive.
	want := map[string]bool{"orchestrate.go": true, "runenrich": true}
	if len(got) != len(want) {
		t.Fatalf("terms = %v, want keys %v", got, want)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected term %q", g)
		}
	}
}
