package claude

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// optionRe matches a numbered choice in Claude Code's dialogs, with or without
// the selection cursor: "❯ 1. Yes", "  2. Yes, and don't ask again for …".
var optionRe = regexp.MustCompile(`^[\s│|]*(?:[❯›>]\s*)?(\d{1,2})[.)]\s+(.+?)[\s│|]*$`)

// ParseOptions extracts the last run of numbered options (1, 2, 3 …) from a
// captured pane. Box-drawing borders and the cursor are ignored.
func ParseOptions(pane string) []model.Option {
	lines := strings.Split(pane, "\n")
	var best []model.Option
	var run []model.Option
	for _, l := range lines {
		m := optionRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		switch {
		case n == 1:
			run = []model.Option{{Key: "1", Label: cleanLabel(m[2])}}
		case len(run) > 0 && n == len(run)+1:
			run = append(run, model.Option{Key: m[1], Label: cleanLabel(m[2])})
		default:
			continue
		}
		if len(run) >= 2 {
			best = append([]model.Option(nil), run...)
		}
	}
	if len(best) > 0 {
		best[0].Primary = true
	}
	return best
}

func cleanLabel(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "(esc)")
	s = strings.TrimSpace(strings.TrimRight(s, "│| "))
	return lower(s)
}

// RefineOptions replaces a prompt's guessed options with the ones on screen,
// keeping the guess when the pane shows nothing usable.
func RefineOptions(p *model.Prompt, pane string) bool {
	if p == nil || p.Kind == "input" {
		return false
	}
	opts := ParseOptions(pane)
	if len(opts) < 2 {
		return false
	}
	if p.Kind == "question" && len(opts) < len(p.Options) {
		return false
	}
	if p.Kind == "question" {
		// Our labels (with descriptions) come from the tool input. The screen adds
		// free-text choices ("type something", "chat about this") that the panel
		// can't answer; "type in terminal" covers those.
		return false
	}
	same := len(opts) == len(p.Options)
	for i := range opts {
		if same && opts[i].Label != p.Options[i].Label {
			same = false
		}
	}
	p.Options = opts
	return !same
}

// cursorRe matches one row of an arrow-key menu: "❯ No, exit" (selected) or
// "  Yes, I trust this folder".
var cursorRe = regexp.MustCompile(`^\s{0,3}(❯\s|\s\s)\s*(\S.*?)\s*$`)

// ParseCursorMenu reads an unnumbered arrow-key menu that ends with an
// "Enter to confirm" footer (e.g. the workspace-trust dialog). title is the
// dialog's first line.
func ParseCursorMenu(pane string) (title string, opts []model.Option, ok bool) {
	lines := strings.Split(pane, "\n")
	footer := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "Enter to confirm") {
			footer = i
			break
		}
	}
	if footer < 0 {
		return "", nil, false
	}
	start := footer
	for i := footer - 1; i >= 0 && footer-i <= 8; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if !cursorRe.MatchString(lines[i]) || len(lines[i])-len(strings.TrimLeft(lines[i], " ")) > 4 {
			break
		}
		start = i
	}
	selected := false
	for _, l := range lines[start:footer] {
		m := cursorRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if strings.HasPrefix(strings.TrimLeft(l, " "), "❯") {
			selected = true
		}
		opts = append(opts, model.Option{Key: strconv.Itoa(len(opts) + 1), Label: lower(m[2])})
	}
	if len(opts) < 2 || !selected {
		return "", nil, false
	}
	// Title: the first text line after the dialog's top rule.
	top := 0
	for i := start - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "────") {
			top = i + 1
			break
		}
	}
	for _, l := range lines[top:start] {
		if t := strings.TrimSpace(strings.Trim(l, "─│╭╮╰╯ ")); t != "" {
			title = strings.TrimSuffix(t, ":")
			break
		}
	}
	if strings.Contains(pane, "trust this folder") {
		title = "trust this folder?"
	}
	primary := 0
	for i, o := range opts {
		if strings.HasPrefix(o.Label, "yes") {
			primary = i
			break
		}
	}
	opts[primary].Primary = true
	return lower(title), opts, true
}
