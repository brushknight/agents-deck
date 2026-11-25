package model

import (
	"regexp"
	"strings"
)

// AnswerText turns an agent's reply, written in Markdown, into the plain
// text the deck shows as its last answer: one line, at most LastAnswerMax
// characters (ending in "…" when cut).
//
//   - code fences go, the code inside them stays
//   - headings, quotes and horizontal rules lose their marks
//   - list bullets become "•"; numbered items keep their numbers
//   - table rows become their cells joined by " · "; separator rows go
//   - **bold**, __bold__, *italic*, `code`, ~~struck~~ lose their marks
//   - links and images keep their text: [text](url) is "text"
//   - all whitespace, line breaks included, collapses to single spaces
func AnswerText(md string) string {
	var out []string
	for _, ln := range strings.Split(md, "\n") {
		t := strings.TrimSpace(ln)
		switch {
		case t == "":
			continue
		case strings.HasPrefix(t, "```"), strings.HasPrefix(t, "~~~"):
			continue // a fence line; the code between fences stays as text
		case ruleLine.MatchString(t), tableRule.MatchString(t):
			continue
		}
		for strings.HasPrefix(t, ">") { // quotes, nested ones too
			t = strings.TrimSpace(t[1:])
		}
		if m := heading.FindStringSubmatch(t); m != nil {
			t = m[1]
		}
		if m := bullet.FindStringSubmatch(t); m != nil {
			t = "• " + m[1]
		}
		if strings.HasPrefix(t, "|") && strings.HasSuffix(t, "|") && len(t) > 1 {
			var cells []string
			for _, c := range strings.Split(t[1:len(t)-1], "|") {
				if c = strings.TrimSpace(c); c != "" {
					cells = append(cells, c)
				}
			}
			t = strings.Join(cells, " · ")
		}
		out = append(out, inline(t))
	}
	s := strings.Join(strings.Fields(strings.Join(out, " ")), " ")
	if r := []rune(s); len(r) > LastAnswerMax {
		s = string(r[:LastAnswerMax-1]) + "…"
	}
	return s
}

var (
	ruleLine  = regexp.MustCompile(`^([-*_])(\s*[-*_]){2,}$`)
	tableRule = regexp.MustCompile(`^\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?$`)
	heading   = regexp.MustCompile(`^#{1,6}\s+(.*?)\s*#*$`)
	bullet    = regexp.MustCompile(`^[-*+]\s+(.*)$`)

	image  = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	link   = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	code   = regexp.MustCompile("`+([^`]+)`+")
	strong = regexp.MustCompile(`(\*\*|__)(\S(?:.*?\S)?)(\*\*|__)`)
	struck = regexp.MustCompile(`~~(\S(?:.*?\S)?)~~`)
	// *italic* and _italic_ only between word edges, so snake_case names,
	// globs like *.go and 2*3*4 keep their characters.
	em = regexp.MustCompile(`(^|[^\w*])[*_](\S(?:[^*_]*?\S)?)[*_]([^\w*]|$)`)
)

func inline(t string) string {
	t = image.ReplaceAllString(t, "$1")
	t = link.ReplaceAllString(t, "$1")
	// Code spans first, kept as they are: their insides are not formatting.
	var spans []string
	t = code.ReplaceAllStringFunc(t, func(m string) string {
		spans = append(spans, code.FindStringSubmatch(m)[1])
		return "\x00" + string(rune('0'+len(spans)-1)) + "\x00"
	})
	t = strong.ReplaceAllString(t, "$2")
	t = struck.ReplaceAllString(t, "$1")
	t = em.ReplaceAllString(t, "$1$2$3")
	t = em.ReplaceAllString(t, "$1$2$3") // again: a match takes the character after it, which may start the next one
	for i, s := range spans {
		t = strings.Replace(t, "\x00"+string(rune('0'+i))+"\x00", s, 1)
	}
	return t
}
