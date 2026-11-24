package model

import (
	"strings"
	"testing"
)

func TestAnswerText(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Done. The tests pass.", "Done. The tests pass."},
		{"## Summary\n\nFixed the **refresh-token race** in `session.ts`.\n\n- added a lock\n- added a *regression* test\n", "Summary Fixed the refresh-token race in session.ts. • added a lock • added a regression test"},
		{"Run this:\n\n```sh\ngo test ./...\n```\n\nThen push.", "Run this: go test ./... Then push."},
		{"See [the guide](https://example.com/guide) and ![chart](c.png).", "See the guide and chart."},
		{"| file | lines |\n|---|---:|\n| a.go | 12 |\n", "file · lines a.go · 12"},
		{"> quoted\n> > twice\n\n---\n\n1. first\n2. second", "quoted twice 1. first 2. second"},
		{"keep snake_case_names, *.go globs and 2*3*4 as they are", "keep snake_case_names, *.go globs and 2*3*4 as they are"},
		{"`**not bold**` stays, ~~gone~~ goes, _a_ _b_", "**not bold** stays, gone goes, a b"},
		{"  lots   of\n\n\n   space  ", "lots of space"},
		{"", ""},
	} {
		if got := AnswerText(c.in); got != c.want {
			t.Errorf("AnswerText(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
	long := AnswerText(strings.Repeat("word ", 400))
	if r := []rune(long); len(r) != LastAnswerMax || !strings.HasSuffix(long, "…") {
		t.Errorf("long answer: %d runes, ends %q", len(r), string(r[len(r)-1]))
	}
}
