package sanitize

import (
	"strings"
	"testing"
)

func scrub(t *testing.T, level Level, in string) (string, Report) {
	t.Helper()
	s := New(Options{Level: level, Home: "/Users/testuser"})
	out := s.Text(in)
	return out, s.Report()
}

func TestScrubsPathsButKeepsMeaningfulTail(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/Users/xiangxiao/projects/mono-glite 是连接器仓库", "<本地路径>/mono-glite 是连接器仓库"},
		{"/Users/xiangxiao/xx/axon/internal/graph/merge.go 做了合并", "<本地路径>/merge.go 做了合并"},
		{`D:\work\glite-ui\src\index.ts`, `<本地路径>/index.ts`},
		{`\\fileserver\share\proj\a.txt`, `<本地路径>/a.txt`},
		{`/var/folders/ab/cd/T/axon-build`, `<本地路径>/axon-build`},
		{`~/Library/Application Support/axon/graphcache`, `<本地路径>/graphcache`},
	}
	for _, c := range cases {
		got, _ := scrub(t, LevelStandard, c.in)
		if got != c.want {
			t.Errorf("Text(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestKeepsRelativeRepoPathsAndBusinessVocabulary(t *testing.T) {
	// Relative paths and domain words are the product, not private context.
	in := "internal/graph/merge.go 与 mono-glite 的 wanlian 命名空间通过 PaymentService 关联"
	got, rep := scrub(t, LevelStandard, in)
	if got != in {
		t.Fatalf("Text() = %q, want the input untouched", got)
	}
	if !rep.Skipped {
		t.Fatalf("Report.Skipped = false, hits = %+v", rep.Hits)
	}
}

// The path rule must not swallow the prose that follows a path, or sharing a
// graph would quietly cost the colleague the sentence around every file name.
func TestPathRuleStopsAtFollowingProse(t *testing.T) {
	in := "查看 /Users/me/proj contains the merge logic 即可"
	got, _ := scrub(t, LevelStandard, in)
	if !strings.Contains(got, "contains the merge logic") {
		t.Fatalf("Text() = %q, want the following prose preserved", got)
	}
	if strings.Contains(got, "/Users/me") {
		t.Fatalf("Text() = %q still leaks the home path", got)
	}
}

// A URL is not a Windows path: without the word boundary the "l:" of
// "mysql:" would be read as a drive letter and the URL would be mangled.
func TestUrlsAreNotMistakenForWindowsPaths(t *testing.T) {
	in := "地址是 http://root@gaia-host/api 和 https://example.com/a/b"
	got, _ := scrub(t, LevelStandard, in)
	if got != in {
		t.Fatalf("Text() = %q, want %q untouched", got, in)
	}
}

func TestScrubsCredentials(t *testing.T) {
	cases := []string{
		"用 sk-ant-api03-AbCdEf0123456789 调用模型",
		"token ghp_0123456789abcdefghijklmnopqrstuvwx 过期了",
		"AWS key AKIAIOSFODNN7EXAMPLE 已轮换",
		"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcd",
		`api_key = "s3cr3t-value-1234"`,
		"DB: mysql://root:SuperSecret123@10.0.0.5:3306/gaia",
	}
	for _, in := range cases {
		got, rep := scrub(t, LevelStandard, in)
		if rep.Total == 0 {
			t.Errorf("Text(%q) scrubbed nothing", in)
		}
		for _, leak := range []string{"sk-ant", "ghp_0123", "AKIAIOSFODNN7EXAMPLE", "eyJhbGciOiJIUzI1NiJ9", "s3cr3t-value-1234", "SuperSecret123"} {
			if strings.Contains(got, leak) {
				t.Errorf("Text(%q) = %q still leaks %q", in, got, leak)
			}
		}
	}
}

func TestKeepsTheFieldNameWhenRedactingAValue(t *testing.T) {
	got, _ := scrub(t, LevelStandard, `password = "hunter2hunter2"`)
	if !strings.HasPrefix(got, "password =") {
		t.Fatalf("Text() = %q, want the key preserved so the fact stays readable", got)
	}
	if strings.Contains(got, "hunter2hunter2") {
		t.Fatalf("Text() = %q still leaks the password", got)
	}
}

func TestScrubsContactDetailsAndIDs(t *testing.T) {
	cases := []struct{ in, mustNotContain string }{
		{"联系 xiangxiao.xiang@trustbe.cn 处理", "xiangxiao.xiang@trustbe.cn"},
		{"手机 13800138000 是他", "13800138000"},
		{"会话 7f8a9b0c-1d2e-4f3a-8b4c-5d6e7f8a9b0c 的结论", "7f8a9b0c-1d2e-4f3a-8b4c-5d6e7f8a9b0c"},
		{"user_id = 882311 有权限", "882311"},
		{"订单 100000123456 已支付", "100000123456"},
		{"内网 192.168.31.77:8080 上的服务", "192.168.31.77"},
	}
	for _, c := range cases {
		got, rep := scrub(t, LevelStandard, c.in)
		if rep.Total == 0 {
			t.Errorf("Text(%q) scrubbed nothing", c.in)
		}
		if strings.Contains(got, c.mustNotContain) {
			t.Errorf("Text(%q) = %q still contains %q", c.in, got, c.mustNotContain)
		}
	}
}

func TestScrubsStandaloneUserNameButNotLongerTokens(t *testing.T) {
	got, _ := scrub(t, LevelStandard, "由 testuser 提交，影响 testing 计划")
	if strings.Contains(got, "testuser ") {
		t.Fatalf("Text() = %q, want the bare login name redacted", got)
	}
	if !strings.Contains(got, "testing") {
		t.Fatalf("Text() = %q, want a longer token containing the name left alone", got)
	}
}

// The login name also appears embedded in path-encoded slugs, where "-" is the
// separator. Excluding "-" from the boundary let it survive in exactly the form
// a real knowledge fact uses ("-Users-alice-xx").
func TestScrubsUserNameInsidePathSlug(t *testing.T) {
	s := New(Options{Level: LevelStandard, Home: "/Users/testuser"})
	for _, in := range []string{
		"graphcache 目录从 -Users-testuser-xx 重命名为 axon",
		"/private/tmp/-Users-testuser-projects-mono-glite",
		"仓库目录名 testuser_projects 已废弃",
	} {
		out := s.Text(in)
		if strings.Contains(out, "testuser") {
			t.Errorf("Text(%q) = %q still contains the login name", in, out)
		}
	}
}

// Rule descriptions are embedded in the exported report, so they must not read
// like leftover secrets or addresses.
func TestRuleDescriptionsCarryNoSampleData(t *testing.T) {
	s := New(Options{Level: LevelStrict, Home: "/Users/testuser"})
	s.Text("/Users/testuser/a sk-ant-api03-abcdefghijklmn 在 192.168.1.9:8080")
	for _, h := range s.Report().Hits {
		for _, bad := range []string{"sk-", "ghp_", "AKIA", "192.168", "10.", "/Users"} {
			if strings.Contains(h.Description, bad) {
				t.Errorf("rule %q description %q reads like sample data (%q)", h.Rule, h.Description, bad)
			}
		}
	}
}

func TestShortUserNamesAreNotScrubbed(t *testing.T) {
	// A two-character login would match ordinary tokens everywhere; the rule is
	// skipped rather than allowed to shred the text.
	s := New(Options{Level: LevelStandard, Home: "/home/xx"})
	out := s.Text("xx 与 yy 都是占位符")
	if out != "xx 与 yy 都是占位符" {
		t.Fatalf("Text() = %q, want the text untouched for a too-short user name", out)
	}
}

func TestStrictLevelAddsTicketAndHostRules(t *testing.T) {
	in := "PLJI-2928 在 glite-gateway:10443 上修复"
	standard, _ := scrub(t, LevelStandard, in)
	if standard != in {
		t.Fatalf("standard level changed %q to %q, want no change", in, standard)
	}
	strict, rep := scrub(t, LevelStrict, in)
	if strings.Contains(strict, "PLJI-2928") || strings.Contains(strict, "glite-gateway:10443") {
		t.Fatalf("strict Text() = %q, want ticket and host removed", strict)
	}
	if rep.Total == 0 {
		t.Fatal("strict level reported no redactions")
	}
}

func TestReportCarriesCountsAndSamples(t *testing.T) {
	s := New(Options{Level: LevelStandard, Home: "/Users/testuser"})
	s.Text("/Users/testuser/a and /Users/testuser/b")
	rep := s.Report()
	if rep.Total == 0 || len(rep.Hits) == 0 {
		t.Fatalf("Report() = %+v, want hits", rep)
	}
	found := false
	for _, h := range rep.Hits {
		if h.Rule == "本地绝对路径" {
			found = true
			if h.Count != 2 {
				t.Errorf("hit count = %d, want 2", h.Count)
			}
			if len(h.Samples) == 0 || h.Samples[0].Before == h.Samples[0].After {
				t.Errorf("samples = %+v, want a before/after pair", h.Samples)
			}
		}
	}
	if !found {
		t.Fatalf("Report() = %+v, want a 本地绝对路径 hit", rep)
	}
}

func TestExtraTermsAndStringSlices(t *testing.T) {
	s := New(Options{Level: LevelStandard, Home: "/Users/testuser", Terms: []string{"内部代号凤凰"}})
	out := s.Text("内部代号凤凰 项目已立项")
	if strings.Contains(out, "凤凰") {
		t.Fatalf("Text() = %q, want the extra term removed", out)
	}
	got := s.Strings([]string{"  正常事实  ", "  ", "/Users/testuser/x"})
	if len(got) != 2 {
		t.Fatalf("Strings() = %v, want blanks dropped", got)
	}
}

func TestIdsInProseAreNotOverScrubbed(t *testing.T) {
	// Short numbers, ports and versions are legitimate business detail.
	in := "接口返回 200，重试 3 次，端口 8080，版本 v1.2.3"
	got, _ := scrub(t, LevelStandard, in)
	if got != in {
		t.Fatalf("Text() = %q, want %q untouched", got, in)
	}
}
