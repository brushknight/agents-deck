// Package redact takes secrets out of text the deck keeps about an agent:
// its last prompt, its last answer, what it is doing, the question it asks.
// That text goes out to the browser, the boards, MCP clients and logs, so a
// key pasted into a prompt must not travel with it.
//
// String replaces each secret it recognises with "[redacted:<kind>]":
//
//   - keys with a known shape: Anthropic, OpenAI, GitHub, Slack, AWS, Google,
//     Stripe, Paddle, agents deck tokens (adk_), JSON Web Tokens and private
//     key blocks;
//   - the value after "Bearer", after a password, token, secret or API key
//     name ("token=…", "API_KEY: …"), and a password in a URL;
//   - any other long run of letters and digits that looks random (mixed case
//     and digits, high entropy), the shape most keys have.
//
// It keeps what names the secret ("Bearer ", "token=", the URL's user) so the
// text still reads. Ordinary prompts, paths, commit hashes and UUIDs come
// back unchanged.
package redact

import (
	"math"
	"regexp"
	"strings"
)

// Mark is what a secret of the given kind turns into.
func Mark(kind string) string { return "[redacted:" + kind + "]" }

type rule struct {
	kind string
	re   *regexp.Regexp
	// keep is how many leading submatches to keep in front of the mark (the
	// name of the secret: "Bearer ", "token=", "https://user:").
	keep int
	// ok, when set, says whether the secret itself (the last submatch, or the
	// whole match) is one: a value with no digit after "Bearer" is a word.
	ok func(string) bool
}

// rules run in order: specific shapes first, so a GitHub token says
// "github" and not "token".
var rules = []rule{
	{"private-key", regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|$)`), 0, nil},
	{"anthropic", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{16,}`), 0, nil},
	{"openai", regexp.MustCompile(`\bsk-(?:proj-|svcacct-|admin-)?[A-Za-z0-9_-]{20,}`), 0, hasDigit},
	{"github", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,})`), 0, nil},
	{"slack", regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}`), 0, nil},
	{"aws", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), 0, nil},
	{"google", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`), 0, nil},
	{"stripe", regexp.MustCompile(`\b(?:sk|rk|pk)_(?:live|test)_[A-Za-z0-9]{16,}`), 0, nil},
	{"paddle", regexp.MustCompile(`\bpdl_(?:live|sdbx|test)_[A-Za-z0-9_]{10,}`), 0, nil},
	{"agents-deck", regexp.MustCompile(`\badk_[A-Za-z0-9_-]{16,}`), 0, nil},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), 0, nil},
	{"bearer", regexp.MustCompile(`(?i)(\bbearer\s+)([A-Za-z0-9._~+/=-]{12,})`), 1, hasDigit},
	{"password", regexp.MustCompile(`([a-z][a-z0-9+.-]*://[^\s:/@]+:)([^\s@/\[][^\s@/]{2,})(@)`), 2, nil},
	{"secret", regexp.MustCompile(`(?i)(\b[a-z0-9_.-]*(?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|auth[_-]?key|private[_-]?key)["']?\s*[:=]\s*["']?)([^\s"',;\[][^\s"',;]{5,})`), 1, notNumber},
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func hasDigit(s string) bool { return strings.ContainsAny(s, "0123456789") }

// notNumber: "tokens: 120000" is a count, not a secret.
func notNumber(s string) bool { return strings.Trim(s, "0123456789.,_kKmM") != "" }

// long is a candidate for the entropy check: a run of key characters.
var long = regexp.MustCompile(`[A-Za-z0-9_+=-]{32,}`)

// String returns s with every secret it finds replaced by its Mark.
func String(s string) string {
	if s == "" {
		return s
	}
	for _, r := range rules {
		if !r.re.MatchString(s) {
			continue
		}
		s = r.re.ReplaceAllStringFunc(s, func(m string) string {
			sub := r.re.FindStringSubmatch(m)
			if r.ok != nil && !r.ok(sub[len(sub)-1-boolInt(r.keep == 2)]) {
				return m
			}
			switch r.keep {
			case 1:
				return sub[1] + Mark(r.kind)
			case 2:
				return sub[1] + Mark(r.kind) + sub[3]
			}
			return Mark(r.kind)
		})
	}
	return long.ReplaceAllStringFunc(s, func(m string) string {
		if looksRandom(m) {
			return Mark("token")
		}
		return m
	})
}

// looksRandom reports whether a long run of key characters reads like a
// generated key: upper and lower case letters and digits, and close to the
// entropy of random base62. Words joined by underscores or dashes, hex
// (commit hashes, digests) and UUIDs fall short of it.
func looksRandom(s string) bool {
	if strings.Contains(s, "redacted") {
		return false
	}
	var lower, upper, digit bool
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
			lower = true
		case c >= 'A' && c <= 'Z':
			upper = true
		case c >= '0' && c <= '9':
			digit = true
		}
	}
	if !lower || !upper || !digit {
		return false
	}
	return entropy(s) >= 4.0
}

// entropy is the Shannon entropy of s in bits per character.
func entropy(s string) float64 {
	n := float64(len(s))
	counts := map[rune]int{}
	for _, c := range s {
		counts[c]++
	}
	var h float64
	for _, k := range counts {
		p := float64(k) / n
		h -= p * math.Log2(p)
	}
	return h
}
