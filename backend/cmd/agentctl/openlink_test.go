package main

import "testing"

func TestLinkTarget(t *testing.T) {
	for _, c := range []struct{ link, word, want string }{
		{"https://example.com/a?b=1", "", "https://example.com/a?b=1"},
		{"file:///Users/sam/dev/app/main.go", "", "file:///Users/sam/dev/app/main.go"},
		{"", "(https://example.com/docs).", "https://example.com/docs"},
		{"", `"http://localhost:7340/x",`, "http://localhost:7340/x"},
		{"", "hello", ""},
		{"", "src/auth/session.ts", ""},
		{"", "", ""},
		{"javascript:alert(1)", "", ""},
		{"vscode://file/x", "", ""},
		{"https://", "", ""},
		{"", "ftp://example.com", ""},
		{"https://example.com/a b", "", ""},
	} {
		if got := linkTarget(c.link, c.word); got != c.want {
			t.Errorf("linkTarget(%q, %q) = %q, want %q", c.link, c.word, got, c.want)
		}
	}
}
