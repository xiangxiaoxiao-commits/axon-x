package claudedata

import "strings"

// A session listing can be asked for one of three reaches, all expressed as a
// single string so the UI has one parameter to pass:
//
//	"*"            every session on the machine
//	"wanlian"      only sessions belonging to that namespace
//	"@unassigned"  only sessions that belong to no namespace
//
// A namespace is a product concept ("which body of work is this"), while Claude
// Code's own directories are path-encoded and Codex/Buddy keep their identity
// elsewhere — so a scope is resolved through the cwd each transcript records for
// itself, never by matching directory names.
const (
	// ScopeAll is the scope that reaches every session.
	ScopeAll = "*"
	// ScopeUnassigned is the scope of sessions whose cwd sits under no
	// .axon-project marker, so no namespace claims them.
	ScopeUnassigned = "@unassigned"
)

// NormalizeScope maps the empty string — the value a not-yet-initialised
// selection has — onto ScopeAll, so callers always see an explicit reach.
func NormalizeScope(scope string) string {
	s := strings.TrimSpace(scope)
	if s == "" {
		return ScopeAll
	}
	return s
}

// IsNamespaceScope reports whether the scope names a single namespace (as
// opposed to the two wildcard scopes). Only a real namespace allows the
// cheap cwd probe to reject transcripts before they are parsed.
func IsNamespaceScope(scope string) bool {
	s := NormalizeScope(scope)
	return s != ScopeAll && s != ScopeUnassigned
}

// MatchesScope reports whether a session falls inside a scope.
func MatchesScope(m SessionMeta, scope string) bool {
	switch NormalizeScope(scope) {
	case ScopeAll:
		return true
	case ScopeUnassigned:
		return m.Namespace == ""
	default:
		return m.Namespace == NormalizeScope(scope)
	}
}
