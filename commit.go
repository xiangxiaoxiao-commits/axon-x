package main

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"axon/internal/gitx"
	"axon/internal/provider"
)

// CommitDraft is the generated commit content returned to the frontend for
// review/editing before an explicit commit. Message follows the
// `type(scope): description` convention; PR fields are filled only when
// requested. UsedKnowledge/KnowledgeSources expose what business background was
// injected (empty when none). Truncated flags that the diff was shortened.
// Warnings surfaces safety notes (e.g. a possible secret spotted in the diff).
type CommitDraft struct {
	Message          string   `json:"message"`
	PRTitle          string   `json:"prTitle"`
	PRBody           string   `json:"prBody"`
	UsedKnowledge    []string `json:"usedKnowledge"`
	KnowledgeSources []string `json:"knowledgeSources"`
	Truncated        bool     `json:"truncated"`
	Warnings         []string `json:"warnings,omitempty"`
}

// commitSystemPrompt instructs the model to follow the team's commit convention.
// The business background (if any) is injected as a separate system message.
const commitSystemPrompt = `You are a commit-message assistant for a senior engineer.
Write a git commit message that STRICTLY follows: type(scope): description
- type must be one of: feat, fix, refactor, docs, test, chore
- scope is the affected module/component inferred from the changed paths (omit the parens if unclear)
- description is a concise English summary in the imperative mood, no trailing period
- keep the subject line under 72 characters
- when the change warrants it, add a blank line then a body explaining WHAT changed and WHY (not how), in English

If project background knowledge is provided, use its terminology and explain the motivation accurately; never invent facts not supported by the diff or the background.

Output format — use these exact delimiters and nothing else outside them:
<COMMIT>
<the full commit message: subject line, optional blank line + body>
</COMMIT>`

// commitPRExtra is appended to the system prompt when a PR description is wanted.
const commitPRExtra = `
Also produce a pull-request description with three sections (What changed / Why / How tested), in English, wrapped as:
<PR_TITLE>
<a concise PR title>
</PR_TITLE>
<PR_BODY>
<the PR description>
</PR_BODY>`

// RepoStatus returns the working-tree state for dir (branch, changed files,
// staged flag). dir must be a path inside the target repository. A non-repo dir
// yields RepoStatus{IsRepo:false} without error.
func (a *App) RepoStatus(dir string) (gitx.RepoStatus, error) {
	if strings.TrimSpace(dir) == "" {
		return gitx.RepoStatus{}, fmt.Errorf("repository directory is required")
	}
	return gitx.New().Status(dir)
}

// GenerateCommit reads the diff for the given scope, optionally injects matched
// business knowledge, and asks the selected model for a spec-compliant commit
// message (and, if withPR, a PR description). It never commits — it only drafts.
//
// Degradation: if projectSlug is empty, the graph is empty, or recall fails, it
// silently falls back to pure-diff generation (no error) — the MVP cold-start
// path where an empty knowledge graph must still work.
func (a *App) GenerateCommit(dir, scope, providerName, model, projectSlug string, withPR bool) (CommitDraft, error) {
	if strings.TrimSpace(dir) == "" {
		return CommitDraft{}, fmt.Errorf("repository directory is required")
	}

	// 1. Read the (filtered, size-bounded) diff for the requested scope.
	git := gitx.New()
	diff, truncated, err := git.Diff(dir, diffScope(scope))
	if err != nil {
		return CommitDraft{}, fmt.Errorf("read diff: %w", err)
	}
	if strings.TrimSpace(diff) == "" {
		return CommitDraft{}, fmt.Errorf("没有可提交的变更")
	}

	// 2. Business-knowledge injection (enhancement layer; degrades silently).
	var knowledge KnowledgeMatch
	if strings.TrimSpace(projectSlug) != "" {
		query := buildKnowledgeQuery(diff)
		if m, mErr := a.MatchKnowledge(projectSlug, query); mErr == nil {
			knowledge = m
		}
	}

	// 3. Assemble the prompt: convention system prompt, optional background,
	//    then the diff as the primary input.
	sysPrompt := commitSystemPrompt
	if withPR {
		sysPrompt += commitPRExtra
	}
	msgs := []provider.ChatMessage{{Role: provider.RoleSystem, Content: sysPrompt}}
	if strings.TrimSpace(knowledge.Context) != "" {
		msgs = append(msgs, provider.ChatMessage{Role: provider.RoleSystem, Content: knowledge.Context})
	}
	msgs = append(msgs, provider.ChatMessage{
		Role:    provider.RoleUser,
		Content: "Here is the diff to describe:\n\n" + diff,
	})

	// 4. Build the provider and collect the (non-streamed) reply.
	prov, modelID, err := a.providerForGeneration(providerName, model)
	if err != nil {
		return CommitDraft{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 90*time.Second)
	defer cancel()
	reply, err := collectReply(ctx, prov, provider.ChatRequest{
		Model:       modelID,
		Messages:    msgs,
		Temperature: 0.2, // low temp: commit messages should not wander
		MaxTokens:   1500,
	})
	if err != nil {
		return CommitDraft{}, fmt.Errorf("generate commit message: %w", err)
	}

	draft := parseCommitReply(reply)
	draft.UsedKnowledge = knowledge.Names
	draft.KnowledgeSources = knowledge.Sources
	draft.Truncated = truncated
	draft.Warnings = scanSecrets(diff)
	return draft, nil
}

// DoCommit creates the commit in dir using the (possibly user-edited) message.
// stageAll=true stages the whole working tree first. It only commits when
// called — never automatically — and never pushes. Returns the new short hash.
func (a *App) DoCommit(dir, message string, stageAll bool) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("repository directory is required")
	}
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("commit message is empty")
	}
	return gitx.New().Commit(dir, message, stageAll)
}

// providerForGeneration resolves the provider + model to use, falling back to
// configured defaults when either is empty.
func (a *App) providerForGeneration(providerName, model string) (provider.Provider, string, error) {
	cfg := a.cfg.Get()
	if providerName == "" {
		providerName = cfg.DefaultProvider
	}
	if model == "" {
		model = cfg.DefaultModel
	}
	pc, ok := a.cfg.Provider(providerName)
	if !ok {
		return nil, "", fmt.Errorf("provider %q not configured", providerName)
	}
	prov, err := a.newProvider(pc)
	if err != nil {
		return nil, "", err
	}
	return prov, model, nil
}

// diffScope maps the frontend's scope string to a gitx.DiffScope, defaulting to
// staged (the MVP-preferred scope).
func diffScope(scope string) gitx.DiffScope {
	switch gitx.DiffScope(scope) {
	case gitx.ScopeUnstaged:
		return gitx.ScopeUnstaged
	case gitx.ScopeAll:
		return gitx.ScopeAll
	default:
		return gitx.ScopeStaged
	}
}

// hunkHeaderRe captures the trailing context (function/class name) git puts
// after the "@@ -a,b +c,d @@" of each hunk.
var hunkHeaderRe = regexp.MustCompile(`(?m)^@@ .* @@\s*(.+)$`)

// buildKnowledgeQuery builds a dense recall query from the diff: changed file
// paths (+ their dirs and base names) and the symbol names in hunk headers.
// This is information-dense and hits module/service/concept entities far better
// than the raw diff would (see docs/TECH_COMMIT.md §3.2).
func buildKnowledgeQuery(diff string) string {
	var b strings.Builder
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			b.WriteString(s)
			b.WriteString(" ")
		}
	}
	for _, ln := range strings.Split(diff, "\n") {
		if strings.HasPrefix(ln, "+++ b/") {
			p := strings.TrimPrefix(ln, "+++ b/")
			add(p)
			add(filepath.Dir(p))
			add(strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)))
		}
	}
	for _, m := range hunkHeaderRe.FindAllStringSubmatch(diff, -1) {
		add(m[1])
	}
	return strings.TrimSpace(b.String())
}

// parseCommitReply extracts Message / PRTitle / PRBody from the model reply. If
// the <COMMIT> delimiters are absent, the whole trimmed reply is treated as the
// message (tolerant of models that ignore the format).
func parseCommitReply(reply string) CommitDraft {
	d := CommitDraft{}
	d.Message = strings.TrimSpace(extractTag(reply, "COMMIT"))
	if d.Message == "" {
		d.Message = strings.TrimSpace(reply)
	}
	d.PRTitle = strings.TrimSpace(extractTag(reply, "PR_TITLE"))
	d.PRBody = strings.TrimSpace(extractTag(reply, "PR_BODY"))
	return d
}

// extractTag returns the content between <name> and </name>, or "" if absent.
func extractTag(s, name string) string {
	openTag, closeTag := "<"+name+">", "</"+name+">"
	i := strings.Index(s, openTag)
	if i < 0 {
		return ""
	}
	i += len(openTag)
	j := strings.Index(s[i:], closeTag)
	if j < 0 {
		return strings.TrimSpace(s[i:])
	}
	return s[i : i+j]
}

// secretPatterns are conservative signatures of credentials that must not slip
// into a commit. We report the pattern name only — never the matched value.
var secretPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"OpenAI-style API key (sk-)", regexp.MustCompile(`sk-[A-Za-z0-9]{16,}`)},
	{"AWS access key id (AKIA…)", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"private key block", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"GitHub token (ghp_/gho_…)", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`)},
	{"Slack token (xox…)", regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)},
}

// scanSecrets checks only added lines of the diff for secret-like patterns and
// returns human-readable warnings (no secret values). Nil when nothing matches.
func scanSecrets(diff string) []string {
	var warnings []string
	seen := map[string]bool{}
	for _, ln := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(ln, "+") || strings.HasPrefix(ln, "+++") {
			continue
		}
		for _, p := range secretPatterns {
			if !seen[p.name] && p.re.MatchString(ln) {
				seen[p.name] = true
				warnings = append(warnings, "diff 中检测到疑似"+p.name+"，请确认后再提交")
			}
		}
	}
	return warnings
}
