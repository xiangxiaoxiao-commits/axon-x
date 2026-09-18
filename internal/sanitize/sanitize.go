// Package sanitize removes machine-specific and personal details from knowledge
// before it leaves the machine that produced it.
//
// The use case is sharing a distilled knowledge graph with a colleague: the
// business facts, decisions and constraints are the product, while absolute
// code paths, credentials, account identifiers and session ids are the author's
// private context. This package is deliberately rule-based and deterministic —
// a shared graph should be reproducible, and every removal should be countable,
// so an exporter can show what was stripped instead of asking for trust.
package sanitize

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Level selects how aggressive the rule set is.
type Level string

const (
	// LevelStandard strips what identifies a person or a machine: absolute
	// paths, credentials, contact details, session/account ids and private
	// network addresses. Business vocabulary is left alone.
	LevelStandard Level = "standard"
	// LevelStrict additionally strips anything shaped like an internal
	// identifier (ticket keys, host names, ports), trading business context for
	// a smaller footprint.
	LevelStrict Level = "strict"
)

// ParseLevel maps a UI value onto a Level, defaulting to standard.
func ParseLevel(s string) Level {
	if strings.EqualFold(strings.TrimSpace(s), string(LevelStrict)) {
		return LevelStrict
	}
	return LevelStandard
}

// LocalPathPlaceholder marks where a machine-specific path used to be. The last
// path element is kept after it, because a directory or file name usually
// carries real meaning ("mono-glite", "merge.go") while the prefix above it
// only describes one person's disk layout.
const LocalPathPlaceholder = "<本地路径>"

// SecretPlaceholder marks where a credential used to be.
const SecretPlaceholder = "<已脱敏>"

// minUserNameLen guards the user-name rule: short names would match ordinary
// two-letter tokens all over a Chinese sentence.
const minUserNameLen = 3

// pathSegment is one run of characters inside a path: everything except
// whitespace and the punctuation that ends a clause in Chinese or English prose.
const pathSegment = `[^\s"'` + "`" + `:;,\)\]，。；：（）【】、《》]+`

// pathAtom is a path element. SPACES are allowed only when the following word
// starts with an upper-case letter or a digit, which is how real multi-word
// directory names look ("Application Support", "My Documents"). Prose that
// follows a path in a sentence starts lower-case, so "/Users/me/proj contains
// the merge logic" stops at "proj" instead of swallowing the rest of the
// sentence. The rule is case-sensitive on purpose: the (?i) flag would defeat it.
const pathAtom = pathSegment + `(?: [A-Z0-9](?:` + pathSegment + `)*)*`

// pathPrefix is the machine-specific head of an absolute path: a home
// directory, a Windows drive (which must sit on a word boundary, or the "l:" in
// "mysql://" would look like one), a UNC host, or a temp directory. It stops
// before the first separator so the element pattern below can require at least
// one real path element — a bare "~/" or "D:" is left alone.
const pathPrefix = `(?:~|/[Uu]sers|/[Hh]ome|/[Rr]oot|/private/var/folders|/var/folders|/private/tmp|\b[A-Za-z]:|\\\\[^\s\\]+)`

// rule is one redaction rule. Rules run in order, so broad structural patterns
// are applied before narrow ones and later rules see the already-redacted text.
type rule struct {
	name        string
	description string
	re          *regexp.Regexp
	// sub rewrites one match. A nil sub replaces the match with constant.
	sub      func(match string) string
	constant string
}

// replace rewrites every match in s and returns the new text plus the matches it
// rewrote, so the caller can both count and sample them.
func (r rule) replace(s string) (string, []string) {
	idx := r.re.FindAllStringIndex(s, -1)
	if len(idx) == 0 {
		return s, nil
	}
	var b strings.Builder
	b.Grow(len(s))
	matches := make([]string, 0, len(idx))
	prev := 0
	for _, m := range idx {
		b.WriteString(s[prev:m[0]])
		match := s[m[0]:m[1]]
		matches = append(matches, match)
		if r.sub != nil {
			b.WriteString(r.sub(match))
		} else {
			b.WriteString(r.constant)
		}
		prev = m[1]
	}
	b.WriteString(s[prev:])
	return b.String(), matches
}

// keepPrefixThen redacts the value half of a `key<sep>value` match, keeping the
// key so the fact stays readable ("password=<已脱敏>" rather than a bare hole).
func keepPrefixThen(seps string) func(string) string {
	return func(m string) string {
		if i := strings.IndexAny(m, seps); i >= 0 {
			return m[:i+1] + SecretPlaceholder
		}
		return SecretPlaceholder
	}
}

// pathTail redacts an absolute path down to its last element.
func pathTail(m string) string {
	trimmed := strings.TrimRight(m, `/\ `)
	name := ""
	if i := strings.LastIndexAny(trimmed, `/\`); i >= 0 && i+1 < len(trimmed) {
		name = trimmed[i+1:]
	}
	if name == "" {
		return LocalPathPlaceholder
	}
	return LocalPathPlaceholder + "/" + name
}

// urlCredential redacts the password inside `scheme://user:password@host`.
func urlCredential(m string) string {
	i := strings.Index(m, "://")
	if i < 0 {
		return SecretPlaceholder
	}
	return m[:i+3] + SecretPlaceholder + "@"
}

// rules run in this order. Credentials are claimed first: a connection string
// such as "mysql://user:pw@host" also looks a little like a path, and whichever
// rule runs first gets to decide how it is read.
var rules = []rule{
	{
		name: "服务凭据",
		// Descriptions travel in the export report, so they must not contain the
		// literal prefixes they remove: a colleague scanning the file would read
		// them as leftover credentials.
		description: "常见服务商的 API key、token、JWT 前缀",
		re:          regexp.MustCompile(`(?i)\b(?:sk|rk|pk|ghp|gho|ghs|ghr|github_pat|xox[baprs]|glpat|AKIA|ASIA|AIza|lsk)[-_A-Za-z0-9]{12,}\b|\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{4,}`),
		constant:    SecretPlaceholder,
	},
	{
		name:        "账号密码字段",
		description: "api_key / secret / password / token 等字段后面的具体值",
		re:          regexp.MustCompile(`(?i)\b(?:api[_-]?key|secret|passwd|password|pwd|token|access[_-]?key|private[_-]?key|auth)\s*[:=]\s*["']?[^\s"',;]{4,}["']?`),
		sub:         keepPrefixThen(":="),
	},
	{
		name:        "连接串凭据",
		description: "数据库与消息队列连接串里内嵌的密码",
		re:          regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s:/@]+:[^\s:/@]{3,}@`),
		sub:         urlCredential,
	},
	{
		name:        "本地绝对路径",
		description: "你电脑上的绝对路径（家目录 / 盘符 / 临时目录 / UNC），只保留最后一级名字",
		re:          regexp.MustCompile(pathPrefix + `(?:[/\\]` + pathAtom + `)+`),
		sub:         pathTail,
	},
	{
		name:        "邮箱",
		description: "个人或企业邮箱地址",
		re:          regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`),
		constant:    "<邮箱>",
	},
	{
		name:        "手机号",
		description: "中国大陆手机号",
		re:          regexp.MustCompile(`(?:\+?86[-\s]?)?\b1[3-9]\d{9}\b`),
		constant:    "<手机号>",
	},
	{
		name:        "会话与请求 ID",
		description: "UUID 形式的会话 / 请求 / 追踪 ID",
		re:          regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`),
		constant:    "<ID>",
	},
	{
		name:        "账号与工号",
		description: "user_id / uid / 工号 / 员工号 / 账号 等字段的值",
		re:          regexp.MustCompile(`(?i)\b(?:user[_-]?id|uid|account[_-]?id|employee[_-]?id|tenant[_-]?id|工号|员工号|账号)\s*[:=]?\s*[A-Za-z0-9_.@-]{3,}`),
		constant:    "<ID>",
	},
	{
		name:        "长数字 ID",
		description: "9 位以上的纯数字标识（业务主键、时间戳等）",
		re:          regexp.MustCompile(`\b\d{9,}\b`),
		constant:    "<ID>",
	},
	{
		name:        "内网地址",
		description: "私有网段与回环地址",
		re:          regexp.MustCompile(`\b(?:10\.\d{1,3}|127\.\d{1,3}|192\.168\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3})\.\d{1,3}(?::\d{2,5})?\b`),
		constant:    "<内网地址>",
	},
	{
		name:        "主目录用户名",
		description: "本机登录用户名（作为独立词出现时）",
		// Built in New() from the actual home directory.
	},
}

// strictRules only run at LevelStrict: they remove internal identifiers that
// carry real business meaning, so they stay opt-in.
var strictRules = []rule{
	{
		name:        "工单号",
		description: "形如 ABC-1234 的内部工单 / 需求号",
		re:          regexp.MustCompile(`\b[A-Z]{2,6}-\d{2,6}\b`),
		constant:    "<工单号>",
	},
	{
		name:        "内部主机与端口",
		description: "*.internal / *.local / *.corp 主机名，以及 host:port",
		re:          regexp.MustCompile(`(?i)\b[a-z0-9][a-z0-9-]*(?:\.[a-z0-9-]+)*\.(?:internal|local|corp|lan|intra)\b(?::\d{2,5})?|\b[a-z][a-z0-9-]{2,}:\d{4,5}\b`),
		constant:    "<主机>",
	},
}

// Sample is one redacted snippet, kept so a caller can show what a rule actually
// caught rather than only how many times it fired.
type Sample struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

// Hit reports one rule's activity.
type Hit struct {
	Rule        string   `json:"rule"`
	Description string   `json:"description"`
	Count       int      `json:"count"`
	Samples     []Sample `json:"samples,omitempty"`
}

// Report is the full account of one scrubbing pass.
type Report struct {
	Level   Level `json:"level"`
	Hits    []Hit `json:"hits"`
	Total   int   `json:"total"`
	Skipped bool  `json:"skipped"` // true when nothing matched
}

const maxSamplesPerRule = 3

// Scrubber redacts text while counting what it removed. One Scrubber covers a
// whole export so the report aggregates across every field.
type Scrubber struct {
	level   Level
	rules   []rule
	counts  map[string]int
	samples map[string][]Sample
}

// Options configures a Scrubber.
type Options struct {
	Level Level
	// Home is the path whose final element (the login name) is scrubbed wherever
	// it appears as a standalone word. Defaults to the real home directory.
	Home string
	// Terms are extra literal strings to redact, e.g. an internal project key the
	// caller knows about. Terms shorter than two runes are ignored.
	Terms []string
}

// New builds a Scrubber for one export.
func New(opts Options) *Scrubber {
	home := strings.TrimSpace(opts.Home)
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	user := ""
	if home != "" {
		user = filepath.Base(filepath.Clean(home))
	}

	active := make([]rule, 0, len(rules)+len(strictRules)+len(opts.Terms))
	for _, r := range rules {
		if r.re == nil {
			// The user-name rule is the only one whose pattern depends on the
			// machine, so it is materialised here.
			if len([]rune(user)) < minUserNameLen || user == "." || user == string(filepath.Separator) {
				continue // nothing safe to scrub
			}
			r = userNameRule(user)
		}
		active = append(active, r)
	}
	if opts.Level == LevelStrict {
		active = append(active, strictRules...)
	}
	for _, t := range opts.Terms {
		t = strings.TrimSpace(t)
		if len([]rune(t)) < 2 {
			continue
		}
		active = append(active, rule{
			name:        "自定义词条",
			description: "调用方指定要去除的词条",
			re:          regexp.MustCompile(regexp.QuoteMeta(t)),
			constant:    "<已去除>",
		})
	}

	return &Scrubber{
		level:   opts.Level,
		rules:   active,
		counts:  make(map[string]int),
		samples: make(map[string][]Sample),
	}
}

// userNameRule matches a login name standing alone — never as part of a longer
// token, so a business word that merely contains those letters is left intact —
// and keeps the surrounding punctuation.
func userNameRule(user string) rule {
	// The boundary excludes only alphanumerics. An earlier version also excluded
	// "-", "_" and ".", which let the login name survive inside path-encoded
	// slugs such as "-Users-alice-xx" -- exactly the form it appears in when a
	// fact mentions a Claude Code project directory.
	re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])` + regexp.QuoteMeta(user) + `([^A-Za-z0-9]|$)`)
	return rule{
		name:        "主目录用户名",
		description: "本机登录用户名（作为独立词出现时）",
		re:          re,
		sub: func(m string) string {
			return re.ReplaceAllString(m, "${1}<用户>${2}")
		},
	}
}

// Text returns the scrubbed form of s.
func (s *Scrubber) Text(in string) string {
	if s == nil || in == "" {
		return in
	}
	out := in
	for _, r := range s.rules {
		next, matches := r.replace(out)
		if len(matches) == 0 {
			continue
		}
		s.counts[r.name] += len(matches)
		for _, m := range matches {
			if len(s.samples[r.name]) >= maxSamplesPerRule {
				break
			}
			after := r.rewrite(m)
			if after == m {
				continue
			}
			s.samples[r.name] = append(s.samples[r.name], Sample{Before: m, After: after})
		}
		out = next
	}
	return out
}

// rewrite applies a rule to a single match.
func (r rule) rewrite(match string) string {
	if r.sub != nil {
		return r.sub(match)
	}
	return r.constant
}

// Strings scrubs every element of a slice, dropping any that become empty.
func (s *Scrubber) Strings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v = strings.TrimSpace(s.Text(v)); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Report returns the aggregated outcome, noisiest rule first. It is meant for
// the operator who is doing the export, on their own machine.
func (s *Scrubber) Report() Report {
	rep := Report{Level: s.level, Hits: []Hit{}}
	for _, r := range s.rules {
		n := s.counts[r.name]
		if n == 0 {
			continue
		}
		rep.Hits = append(rep.Hits, Hit{
			Rule:        r.name,
			Description: r.description,
			Count:       n,
			Samples:     s.samples[r.name],
		})
		rep.Total += n
	}
	sort.SliceStable(rep.Hits, func(i, j int) bool { return rep.Hits[i].Count > rep.Hits[j].Count })
	rep.Skipped = rep.Total == 0
	return rep
}

// Shareable returns a copy safe to embed in a file that leaves the machine: the
// per-rule counts and descriptions stay, the samples go.
//
// This matters more than it looks. A sample holds the text a rule matched —
// which is by definition the text that was supposed to disappear. Shipping the
// report alongside the scrubbed knowledge would hand the recipient back exactly
// what was redacted, inside the very file whose purpose is to not contain it.
func (r Report) Shareable() Report {
	out := Report{Level: r.Level, Total: r.Total, Skipped: r.Skipped, Hits: make([]Hit, 0, len(r.Hits))}
	for _, h := range r.Hits {
		out.Hits = append(out.Hits, Hit{Rule: h.Rule, Description: h.Description, Count: h.Count})
	}
	return out
}
