package redact

import (
	"strings"
	"testing"
)

// fake builds a made-up secret: prefix plus n characters of a fixed
// mixed-case pattern. The secrets are assembled at run time so no
// key-shaped string sits in the source (and no secret scanner trips on it).
// None of them is, or was, a real key.
func fake(prefix string, n int) string {
	const pat = "Q7mZp2Xk9RtB4wNc6YhLd3Vf8GsJ1aUe5"
	var b strings.Builder
	b.WriteString(prefix)
	for i := 0; i < n; i++ {
		b.WriteByte(pat[(i*7+3)%len(pat)])
	}
	return b.String()
}

func TestStringRedacts(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"anthropic", "use " + fake("sk-"+"ant-api03-", 40) + " for it", "use [redacted:anthropic] for it"},
		{"openai", "key " + fake("sk-"+"proj-", 40), "key [redacted:openai]"},
		{"github classic", fake("gh"+"p_", 36), "[redacted:github]"},
		{"github fine-grained", fake("github"+"_pat_", 60), "[redacted:github]"},
		{"slack", "bot " + fake("xo"+"xb-", 30), "bot [redacted:slack]"},
		{"aws", "id " + "AK" + "IA" + strings.ToUpper(fake("", 16)) + " ok", "id [redacted:aws] ok"},
		{"google", fake("AI"+"za", 35), "[redacted:google]"},
		{"stripe", fake("sk"+"_live_", 24), "[redacted:stripe]"},
		{"paddle", "new token " + fake("pdl"+"_live_apikey_", 30), "new token [redacted:paddle]"},
		{"agents deck", fake("ad"+"k_", 32), "[redacted:agents-deck]"},
		{"jwt", fake("ey"+"J", 20) + "." + fake("ey"+"J", 30) + "." + fake("", 24), "[redacted:jwt]"},
		{"bearer", `curl -H "Authorization: Bearer ` + fake("", 20) + `"`, `curl -H "Authorization: Bearer [redacted:bearer]"`},
		{"url password", "git clone https://bot:" + fake("", 12) + "@git.example.com/r.git", "git clone https://bot:[redacted:password]@git.example.com/r.git"},
		{"name equals", "export API_KEY=" + fake("", 20), "export API_KEY=[redacted:secret]"},
		{"name colon quoted", `password: "hunter2-but-longer"`, `password: "[redacted:secret]"`},
		{"json", `{"client_secret":"` + fake("", 18) + `"}`, `{"client_secret":"[redacted:secret]"}`},
		{"private key", "-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY----- done", "[redacted:private-key] done"},
		{"private key cut off", "here: -----BEGIN OPENSSH PRIVATE KEY-----\nb3Blbn", "here: [redacted:private-key]"},
		{"random", "it is " + fake("", 40) + " now", "it is [redacted:token] now"},
		{"named kind wins", "GITHUB_TOKEN=" + fake("gh"+"p_", 36), "GITHUB_TOKEN=[redacted:github]"},
		{"two in one", fake("gh"+"p_", 36) + " and " + fake("sk"+"_test_", 20), "[redacted:github] and [redacted:stripe]"},
	}
	for _, c := range cases {
		if got := String(c.in); got != c.want {
			t.Errorf("%s:\n got  %q\n want %q", c.name, got, c.want)
		}
	}
}

func TestStringKeepsOrdinaryText(t *testing.T) {
	for _, s := range []string{
		"",
		"fix the login bug and run the tests",
		"rename max_tokens to maxTokens in config.go",
		"tokens: 120000 used today",
		"Bearer authentication is described in docs/protocol.md",
		"the task-runner-for-the-nightly-build is slow",
		"look at commit 179515df6857650a44c78dba114bb8587a73bfc3",
		"agent 857bfa4b-3232-40c4-8230-3c8505bf7d72 is done",
		"open backend/internal/server/queue_test.go and clients/flutter/agents_ui/lib/src/agents_app.dart",
		"the function TestStringKeepsOrdinaryTextWhenItIsLong passes",
		"sha256 e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"https://github.com/example/repo/pull/90",
		"password reset email copy: keep it short",
		"Already [redacted:github] here",
	} {
		if got := String(s); got != s {
			t.Errorf("changed ordinary text:\n in  %q\n out %q", s, got)
		}
	}
}

func TestStringIsIdempotent(t *testing.T) {
	in := "token=" + fake("", 20) + " and " + fake("gh"+"p_", 36) + " and Bearer " + fake("", 20)
	once := String(in)
	if twice := String(once); twice != once {
		t.Errorf("second pass changed it:\n once  %q\n twice %q", once, twice)
	}
}
