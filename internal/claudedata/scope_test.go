package claudedata

import "testing"

func TestNormalizeScopeTreatsEmptyAsEverything(t *testing.T) {
	// A selection that has not been initialised arrives as "", and the browser
	// must show everything rather than nothing.
	if got := NormalizeScope(""); got != ScopeAll {
		t.Fatalf("NormalizeScope(\"\") = %q, want %q", got, ScopeAll)
	}
	if got := NormalizeScope("  "); got != ScopeAll {
		t.Fatalf("NormalizeScope(\"  \") = %q, want %q", got, ScopeAll)
	}
	if got := NormalizeScope(" wanlian "); got != "wanlian" {
		t.Fatalf("NormalizeScope(\" wanlian \") = %q, want \"wanlian\"", got)
	}
}

func TestIsNamespaceScope(t *testing.T) {
	cases := map[string]bool{
		"":              false,
		ScopeAll:        false,
		ScopeUnassigned: false,
		"wanlian":       true,
		"_global_":      true,
	}
	for scope, want := range cases {
		if got := IsNamespaceScope(scope); got != want {
			t.Errorf("IsNamespaceScope(%q) = %v, want %v", scope, got, want)
		}
	}
}

func TestMatchesScope(t *testing.T) {
	wanlian := SessionMeta{ID: "a", Namespace: "wanlian"}
	orphan := SessionMeta{ID: "b", Namespace: ""}

	cases := []struct {
		scope string
		meta  SessionMeta
		want  bool
	}{
		// Everything reaches both an attributed and an unattributed session.
		{ScopeAll, wanlian, true},
		{ScopeAll, orphan, true},
		{"", wanlian, true},
		// A namespace reaches only its own.
		{"wanlian", wanlian, true},
		{"wanlian", orphan, false},
		{"axon", wanlian, false},
		// Unassigned is the complement of "some namespace claims it".
		{ScopeUnassigned, wanlian, false},
		{ScopeUnassigned, orphan, true},
	}
	for _, c := range cases {
		if got := MatchesScope(c.meta, c.scope); got != c.want {
			t.Errorf("MatchesScope(%q, ns=%q) = %v, want %v", c.scope, c.meta.Namespace, got, c.want)
		}
	}
	// A namespace must not be matched by a prefix of its name.
	if MatchesScope(SessionMeta{Namespace: "wanlian-x"}, "wanlian") {
		t.Error("MatchesScope matched a namespace by prefix")
	}
}
