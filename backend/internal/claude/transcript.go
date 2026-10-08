package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/pricing"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// maxRead bounds one tail pass so a huge transcript can't stall the daemon.
const maxRead = 8 << 20

type line struct {
	Type      string `json:"type"`
	GitBranch string `json:"gitBranch"`
	AITitle   string `json:"aiTitle"`
	Custom    string `json:"customTitle"`
	IsAPIErr  bool   `json:"isApiErrorMessage"`
	Message   *struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
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
	} else if st.Size() == e.Offset {
		return false
	}
	if _, err := f.Seek(e.Offset, io.SeekStart); err != nil {
		return false
	}
	buf, _ := io.ReadAll(io.LimitReader(f, maxRead))
	e.Offset += int64(len(buf))
	data := append([]byte(e.Pending), buf...)
	last := bytes.LastIndexByte(data, '\n')
	if last < 0 {
		e.Pending = string(data)
		return false
	}
	e.Pending = string(data[last+1:])

	changed := false
	var lastCtx int64
	var lastModel string
	sc := bufio.NewScanner(bytes.NewReader(data[:last]))
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		var l line
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		if l.GitBranch != "" && l.GitBranch != e.A.Branch {
			e.A.Branch, changed = l.GitBranch, true
		}
		switch l.Type {
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
				e.SetStatus(model.Error)
				e.A.Error = &model.ErrInfo{Message: apiErrorText(l.Message.Content)}
				changed = true
				continue
			}
			if l.Message.Usage == nil || l.Message.ID == "" || l.Message.Model == "<synthetic>" {
				continue
			}
			u := l.Message.Usage
			e.SetUsage(l.Message.ID, l.Message.Model, model.Tokens{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite})
			lastCtx = u.Input + u.CacheRead + u.CacheWrite
			lastModel = l.Message.Model
			if e.A.Status == model.Error {
				e.SetStatus(model.Running) // recovered: a real reply arrived
			}
			changed = true
		}
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
