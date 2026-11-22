package server

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// MaxPromptBytes is the longest prompt POST /v1/agents/{id}/prompt takes.
const MaxPromptBytes = 4000

// CleanPrompt checks a prompt's text before it is typed into a terminal:
// not empty, at most MaxPromptBytes of UTF-8, and no control characters but
// newlines and tabs (an escape sequence could press keys of its own). CRLF
// becomes LF.
func CleanPrompt(text string) (string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return "", errors.New("the prompt is empty")
	}
	if len(text) > MaxPromptBytes {
		return "", errors.New("the prompt is longer than 4000 bytes")
	}
	if !utf8.ValidString(text) {
		return "", errors.New("the prompt is not valid UTF-8")
	}
	for _, c := range text {
		if c == '\n' || c == '\t' {
			continue
		}
		if c < 0x20 || c == 0x7f || (c >= 0x80 && c < 0xa0) {
			return "", errors.New("the prompt has control characters: only text, newlines and tabs can be typed")
		}
	}
	return strings.TrimRight(text, " \t\n"), nil
}
