package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/pricing"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// One tail call reads up to maxChunks × maxRead (1 GiB): a whole long
// session in one go, without holding it all in memory at once.
const (
	maxRead   = 8 << 20
	maxChunks = 128
)

type line struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	GitBranch string `json:"gitBranch"`
	AITitle   string `json:"aiTitle"`
	Custom    string `json:"customTitle"`
	IsAPIErr  bool   `json:"isApiErrorMessage"`
	Effort    string `json:"effort"` // the reasoning effort of an assistant turn (low … max)
	Timestamp string `json:"timestamp"`
	IsMeta    bool   `json:"isMeta"`                    // text Claude Code adds for itself (command caveats, reminders)
	Sidechain bool   `json:"isSidechain"`               // a subagent's message
	Summary   bool   `json:"isCompactSummary"`          // the summary a compaction leaves: not a prompt
	ShownOnly bool   `json:"isVisibleInTranscriptOnly"` // shown in the transcript, never sent to the model
	Message   *struct {
		ID         string          `json:"id"`
		Model      string          `json:"model"`
		StopReason string          `json:"stop_reason"`
		Content    json.RawMessage `json:"content"`
		Usage      *struct {
			Input      int64 `json:"input_tokens"`
			Output     int64 `json:"output_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// Tail reads transcript lines appended since the last call and folds usage,
// model, branch, Claude's own title and API errors into the agent.
func Tail(e *store.Entry) bool {
	if e.Transcript == "" {
		return false
	}
	f, err := os.Open(e.Transcript)
	if err != nil {
		return false
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || st.Size() < e.Offset {
		e.Offset, e.Usage, e.Pending = 0, nil, "" // truncated or replaced
		e.Answer, e.AnswerID = "", ""
	} else if st.Size() == e.Offset {
		return false
	}
	if _, err := f.Seek(e.Offset, io.SeekStart); err != nil {
		return false
	}
	// Catch up in one call, chunk by chunk, so the board never shows the
	// numbers of half-read history (a restart re-reads from the start).
	changed := false
	var lastCtx int64
	var lastModel, lastEffort string
	// API errors are judged on the whole read, not line by line: re-reading a
	// transcript after a restart must not replay an old error and its recovery
	// into a status ("recovered: running") for a turn that ended long ago.
	wasErr := e.A.Status == model.Error
	var lastErr json.RawMessage // the latest API error, nil once a real reply follows it
	replied, over := false, false
	for range maxChunks {
		buf, _ := io.ReadAll(io.LimitReader(f, maxRead))
		if len(buf) == 0 {
			break
		}
		e.Offset += int64(len(buf))
		data := append([]byte(e.Pending), buf...)
		last := bytes.LastIndexByte(data, '\n')
		if last < 0 {
			e.Pending = string(data)
			continue
		}
		e.Pending = string(data[last+1:])

		sc := bufio.NewScanner(bytes.NewReader(data[:last]))
		sc.Buffer(make([]byte, 64<<10), 16<<20)
		for sc.Scan() {
			var l line
			if json.Unmarshal(sc.Bytes(), &l) != nil {
				continue
			}
			if t, ok := l.activeAt(); ok && e.MarkActive(t) {
				changed = true
			}
			if decides, ends := l.turnState(); decides {
				over = ends
				if followAnswer(e, l, ends) {
					changed = true
				}
			}
			if l.GitBranch != "" && l.GitBranch != e.A.Branch {
				e.A.Branch, changed = l.GitBranch, true
			}
			switch l.Type {
			case "system":
				// The compaction finished (also caught by SessionStart "compact").
				if l.Subtype == "compact_boundary" && EndCompact(e) {
					changed = true
				}
			case "ai-title":
				if l.AITitle != "" && l.AITitle != e.A.AITitle {
					e.A.AITitle, changed = l.AITitle, true
				}
			case "custom-title":
				if l.Custom != e.CustomTitle {
					e.CustomTitle, changed = l.Custom, true
				}
			case "assistant":
				if l.Message == nil {
					continue
				}
				if l.IsAPIErr {
					lastErr = l.Message.Content
					continue
				}
				if l.Message.Usage == nil || l.Message.ID == "" || l.Message.Model == "<synthetic>" {
					continue
				}
				lastErr, replied = nil, true
				u := l.Message.Usage
				e.SetUsage(l.Message.ID, l.Message.Model, model.Tokens{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite})
				lastCtx = u.Input + u.CacheRead + u.CacheWrite
				lastModel = l.Message.Model
				if l.Effort != "" {
					lastEffort = l.Effort
				}
				changed = true
			}
		}
	}
	switch {
	case lastErr != nil: // the read ends on an API error
		e.SetStatus(model.Error)
		e.A.Error = &model.ErrInfo{Message: apiErrorText(lastErr)}
		changed = true
	case wasErr && replied: // recovered: a real reply arrived after the error
		if over {
			e.SetStatus(model.Idle) // ...and the turn it belonged to is over
		} else {
			e.SetStatus(model.Running)
		}
		changed = true
	}
	if lastEffort != "" && lastEffort != e.A.Effort {
		e.A.Effort, changed = lastEffort, true
	}
	if lastModel != "" {
		e.A.Model = lastModel
		e.A.ModelLabel = pricing.Label(lastModel)
		e.A.Context = model.Context{Used: lastCtx, Window: pricing.Window(lastModel)}
		if lastCtx > e.A.Context.Window { // e.g. a 1M beta on a model we think is 200k
			e.A.Context.Window = 1_000_000
		}
	}
	if changed {
		e.A.Tokens, e.A.CostUSD = e.Totals(pricing.Cost)
		e.A.Title = e.DisplayTitle() // names follow Claude's titles live
	}
	return changed
}

// followAnswer follows the reply of the turn in progress, for a line of the main
// conversation: the text of the latest API message, which becomes the
// agent's LastAnswer when the line ends the turn. A message that only calls
// tools leaves no text, so what counts is the text after the last tool call.
// Any message sent to it (your prompt, a tool's result, an interrupt) starts
// over: an interrupted turn leaves no answer, and the one before it stays. It reports whether LastAnswer
// changed.
func followAnswer(e *store.Entry, l line, ends bool) bool {
	if l.Type == "user" { // your prompt, a tool's result or an interrupt
		e.Answer, e.AnswerID = "", ""
		return false
	}
	if l.IsAPIErr || l.Message.Model == "<synthetic>" {
		return false
	}
	text := replyText(l.Message.Content)
	switch {
	case l.Message.ID == "" || l.Message.ID != e.AnswerID:
		e.Answer, e.AnswerID = text, l.Message.ID
	case text != "":
		e.Answer = strings.TrimSpace(e.Answer + "\n\n" + text) // one message, written a block per line
	}
	if t, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil && text != "" {
		e.AnswerAt = t
	}
	if !ends {
		return false
	}
	return CommitAnswer(e)
}

// CommitAnswer makes the reply of the turn that just ended the agent's
// LastAnswer, when it has any text. It reports whether that changed.
func CommitAnswer(e *store.Entry) bool {
	text := model.AnswerText(e.Answer)
	e.Answer, e.AnswerID = "", ""
	if text == "" || text == e.A.LastAnswer && e.AnswerAt.Equal(e.A.LastAnswerAt) {
		return false
	}
	e.A.LastAnswer, e.A.LastAnswerAt = text, e.AnswerAt
	if e.A.LastAnswerAt.IsZero() {
		e.A.LastAnswerAt = Now()
	}
	return true
}

// replyText is the text of an assistant message's text blocks (thinking and
// tool calls left out).
func replyText(content json.RawMessage) string {
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		var s string
		_ = json.Unmarshal(content, &s)
		return strings.TrimSpace(s)
	}
	var parts []string
	for _, b := range blocks {
		if t := strings.TrimSpace(b.Text); b.Type == "text" && t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n\n")
}

// activeAt is when a line counts as the agent's activity: a message sent to
// it (your prompt, a tool's result) or received from it (a reply, a tool
// call). Everything else in a transcript does not count: titles, summaries,
// snapshots, hook and system notes, text Claude Code adds for itself
// (isMeta), and subagents' messages.
func (l line) activeAt() (time.Time, bool) {
	if l.Type != "user" && l.Type != "assistant" || l.IsMeta || l.Sidechain || l.Message == nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, l.Timestamp)
	return t, err == nil
}

// turnState is what a line means for the turn in progress: whether it is a
// message of the main conversation (decides), and whether the turn is over
// once it is the latest one (ends): Claude's final reply, an API error that
// stopped the turn, or a turn interrupted with Esc, which runs no hook.
// Text Claude Code adds for itself, a compaction's summary and subagents'
// messages decide nothing.
func (l line) turnState() (decides, ends bool) {
	if l.Type != "user" && l.Type != "assistant" || l.Message == nil || l.IsMeta || l.Sidechain || l.Summary || l.ShownOnly {
		return false, false
	}
	if l.Type == "assistant" {
		return true, l.Message.StopReason == "end_turn" || l.IsAPIErr
	}
	return true, interruptText(l.Message.Content)
}

// interruptText reports whether a user message is Claude Code's note that
// you interrupted the turn ("[Request interrupted by user]", "… for tool use]").
func interruptText(content json.RawMessage) bool {
	if !bytes.Contains(content, []byte("[Request interrupted by user")) {
		return false
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal(content, &blocks) // a typed prompt is a plain string: not an interrupt
	for _, b := range blocks {
		if b.Type == "text" && strings.HasPrefix(b.Text, "[Request interrupted by user") {
			return true
		}
	}
	return false
}

func apiErrorText(content json.RawMessage) string {
	var blocks []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(content, &blocks)
	for _, b := range blocks {
		if t := strings.TrimSpace(b.Text); t != "" {
			t = strings.TrimPrefix(t, "API Error: ")
			return clip(lower(oneLine(t)), 120)
		}
	}
	return "api error"
}
