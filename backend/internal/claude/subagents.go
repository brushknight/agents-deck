package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// Background subagents (the Agent tool with run_in_background, and Workflow
// runs) keep working after the main thread's turn is over. The main
// transcript records each one's life:
//
//   - launch: a tool result (toolUseResult) with status "async_launched" and
//     agentId (a subagent) or taskId (a workflow run);
//   - resume: a SendMessage result with resumedAgentId: it works again;
//   - finish: a <task-notification> with its <task-id> and a <status>
//     (completed, failed, killed, stopped), queued (queue-operation) and then
//     delivered (a user line, or a queued_command attachment);
//   - stop: a TaskStop result with task_id;
//   - a foreground subagent's own result (agentId, status "completed").
//
// Background shell commands (backgroundTaskId) are not subagents: a dev
// server left running is not an agent at work.

const (
	// SubagentCap is the last resort against a subagent shown working
	// forever (a notification Claude Code never wrote): one with no sign of
	// life for this long (no launch, resume or write to its own transcript)
	// is over. Subagents go quiet for long stretches (a long build, a long
	// reply): the longest seen in real sessions was about an hour.
	SubagentCap = 6 * time.Hour
	// subagentEndGrace: a subagent whose own transcript ends with its final
	// reply this long ago is over, notification or not (it comes within
	// seconds of that reply).
	subagentEndGrace = 5 * time.Minute
	// The fallback's windows for subagents the main transcript says nothing
	// about (see recentSubagents).
	subagentWindow     = 30 * time.Minute
	subagentToolWindow = 30 * time.Minute
	lifeChunk          = 8 << 20
	lifeIdle           = time.Hour // a transcript not asked about for this long is forgotten
)

// life is what the main transcript says about one subagent or workflow run.
type life struct {
	working  bool
	at       time.Time // its latest launch or resume
	title    string
	workflow bool
	order    int
}

// lifeSeen is what was read of one main transcript: up to where (a whole
// number of lines), the bytes just before that point (to tell an appended
// file from a replaced one), and every subagent it mentions.
type lifeSeen struct {
	mu    sync.Mutex
	size  int64
	edge  []byte
	subs  map[string]*life
	notes map[uint64]bool // notifications already counted (each is written more than once)
	n     int
	used  time.Time
}

var lifeCache = struct {
	sync.Mutex
	m map[string]*lifeSeen
}{m: map[string]*lifeSeen{}}

// Prefilter: only lines with one of these can carry a record above.
var lifeNeedles = [][]byte{[]byte(`async_launched`), []byte(`resumedAgentId`), []byte(`task-notification`),
	[]byte(`"task_type"`), []byte(`"agentId"`)}

// lifeOf reads what was appended to the main transcript at path since the
// last call and returns its subagents (a copy). ok is false when the file
// can't be read.
func lifeOf(path string, now time.Time) (subs map[string]life, ok bool) {
	lifeCache.Lock()
	s := lifeCache.m[path]
	if s == nil {
		s = &lifeSeen{}
		lifeCache.m[path] = s
	}
	for p, x := range lifeCache.m {
		if x != s && x.mu.TryLock() {
			if now.Sub(x.used) > lifeIdle {
				delete(lifeCache.m, p)
			}
			x.mu.Unlock()
		}
	}
	lifeCache.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.used = now
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, false
	}
	if st.Size() < s.size || s.subs == nil || !bytes.Equal(readEdge(f, s.size), s.edge) {
		s.size, s.subs, s.notes, s.n = 0, map[string]*life{}, map[uint64]bool{}, 0 // new, truncated or replaced
	}
	s.catchUp(f, st.Size())
	s.edge = readEdge(f, s.size)
	subs = make(map[string]life, len(s.subs))
	for id, l := range s.subs {
		subs[id] = *l
	}
	return subs, true
}

// catchUp reads whole lines from s.size up to size. A last line with no
// newline yet is still being written and is read next time.
func (s *lifeSeen) catchUp(f *os.File, size int64) {
	off := s.size
	var carry []byte
	for off < size {
		buf := make([]byte, min(lifeChunk, size-off))
		n, _ := f.ReadAt(buf, off)
		if n == 0 {
			return
		}
		off += int64(n)
		data := append(carry, buf[:n]...)
		last := bytes.LastIndexByte(data, '\n')
		if last < 0 {
			carry = data // one long line: read on
			continue
		}
		for _, ln := range bytes.Split(data[:last], []byte("\n")) {
			s.line(ln)
		}
		s.size = off - int64(len(data)-last-1)
		carry = append([]byte(nil), data[last+1:]...)
	}
}

// lifeLine is the part of a transcript line the records above use.
type lifeLine struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Sidechain bool            `json:"isSidechain"`
	Content   json.RawMessage `json:"content"` // queue-operation: the queued text
	Message   *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Result     json.RawMessage `json:"toolUseResult"`
	Attachment *struct {
		Type   string          `json:"type"`
		Prompt json.RawMessage `json:"prompt"`
	} `json:"attachment"`
}

// lifeResult is a tool result's fields that start, resume or end a subagent.
type lifeResult struct {
	Status       string `json:"status"`
	AgentID      string `json:"agentId"`
	TaskID       string `json:"taskId"`
	Description  string `json:"description"`
	Summary      string `json:"summary"`
	WorkflowName string `json:"workflowName"`
	Error        string `json:"error"`
	Resumed      string `json:"resumedAgentId"`
	Success      *bool  `json:"success"`
	StopID       string `json:"task_id"`
	StopType     string `json:"task_type"`
}

func (s *lifeSeen) line(b []byte) {
	hit := false
	for _, n := range lifeNeedles {
		if bytes.Contains(b, n) {
			hit = true
			break
		}
	}
	if !hit {
		return
	}
	var l lifeLine
	if json.Unmarshal(b, &l) != nil || l.Sidechain {
		return
	}
	at, _ := time.Parse(time.RFC3339Nano, l.Timestamp)
	switch l.Type {
	case "user":
		if l.Message != nil {
			s.notifications(l.Message.Content)
		}
		if len(l.Result) > 0 && l.Result[0] == '{' {
			var r lifeResult
			if json.Unmarshal(l.Result, &r) == nil {
				s.result(r, at)
			}
		}
	case "queue-operation":
		s.notifications(l.Content)
	case "attachment":
		if l.Attachment != nil && l.Attachment.Type == "queued_command" {
			s.notifications(l.Attachment.Prompt)
		}
	}
}

func (s *lifeSeen) get(id string) *life {
	l := s.subs[id]
	if l == nil {
		s.n++
		l = &life{order: s.n}
		s.subs[id] = l
	}
	return l
}

func (s *lifeSeen) result(r lifeResult, at time.Time) {
	switch {
	case r.Status == "async_launched" && r.Error == "" && (r.AgentID != "" || r.TaskID != ""):
		id, title := r.AgentID, r.Description
		if id == "" {
			id, title = r.TaskID, r.Summary
			if title == "" {
				title = r.WorkflowName
			}
		}
		l := s.get(id)
		l.working, l.at, l.workflow = true, at, r.AgentID == ""
		if title != "" {
			l.title = title
		}
	case r.Resumed != "":
		if r.Success != nil && !*r.Success {
			return
		}
		l := s.get(r.Resumed)
		l.working, l.at = true, at
	case r.StopID != "" && r.StopType != "": // TaskStop
		s.get(r.StopID).working = false
	case r.AgentID != "" && r.Status != "": // a foreground subagent's result
		s.get(r.AgentID).working = false
	}
}

var (
	noteRe   = regexp.MustCompile(`(?s)<task-notification>(.*?)</task-notification>`)
	taskIDRe = regexp.MustCompile(`<task-id>\s*([^<\s]+)\s*</task-id>`)
	statusRe = regexp.MustCompile(`<status>\s*([^<]*?)\s*</status>`)
)

// notifications ends the subagents whose task notification raw (a JSON
// string, or text blocks) starts with. Each notification is written up to
// three times (queued, then delivered); only its first counts, so one
// delivered after the subagent was resumed doesn't end the new run. Text
// that only quotes a notification (a file read, a command's output) is a
// tool result, never read here.
func (s *lifeSeen) notifications(raw json.RawMessage) {
	if !bytes.Contains(raw, []byte("task-notification")) { // ("<" and ">" may be written \u003c, \u003e)
		return
	}
	var texts []string
	var str string
	if json.Unmarshal(raw, &str) == nil {
		texts = []string{str}
	} else {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		_ = json.Unmarshal(raw, &blocks)
		for _, b := range blocks {
			if b.Type == "text" {
				texts = append(texts, b.Text)
			}
		}
	}
	for _, t := range texts {
		if !strings.HasPrefix(strings.TrimSpace(t), "<task-notification>") {
			continue
		}
		for _, m := range noteRe.FindAllStringSubmatch(t, -1) {
			id, st := taskIDRe.FindStringSubmatch(m[1]), statusRe.FindStringSubmatch(m[1])
			if id == nil || st == nil || !taskOver(st[1]) {
				continue // an event of a task still running (a monitor's line)
			}
			h := fnv.New64a()
			h.Write([]byte(m[1]))
			if k := h.Sum64(); !s.notes[k] {
				s.notes[k] = true
				s.get(id[1]).working = false // (one launched in an earlier transcript is known, and done)
			}
		}
	}
}

// taskOver reports whether a notification's status ends the task.
func taskOver(status string) bool {
	switch strings.ToLower(status) {
	case "", "running", "pending", "started", "in_progress":
		return false
	}
	return true
}

// subInfo is what a subagent's own transcript says, cached by size and time.
type subInfo struct {
	size       int64
	mod, start time.Time
	done       bool // ends with its final reply
	inTool     bool
	tool       string
	title, typ string // from its .meta.json
}

var subCache = struct {
	sync.Mutex
	m map[string]subInfo
}{m: map[string]subInfo{}}

// subFile reads the end of a subagent's transcript (and its .meta.json),
// again only when it changed. ok is false when there is no such file.
func subFile(path string) (subInfo, bool) {
	st, err := os.Stat(path)
	if err != nil {
		subCache.Lock()
		delete(subCache.m, path)
		subCache.Unlock()
		return subInfo{}, false
	}
	subCache.Lock()
	c, hit := subCache.m[path]
	subCache.Unlock()
	if hit && c.size == st.Size() && c.mod.Equal(st.ModTime()) {
		return c, true
	}
	c = subInfo{size: st.Size(), mod: st.ModTime()}
	c.done, c.inTool, c.tool, c.start = subagentTail(path, st.Size())
	var meta struct {
		Type        string `json:"agentType"`
		Description string `json:"description"`
	}
	if b, err := os.ReadFile(strings.TrimSuffix(path, ".jsonl") + ".meta.json"); err == nil && json.Unmarshal(b, &meta) == nil {
		c.title, c.typ = clip(oneLine(meta.Description), 80), meta.Type
	}
	subCache.Lock()
	subCache.m[path] = c
	subCache.Unlock()
	return c, true
}

// Subagents lists the subagents (and workflow runs) of the session at
// transcript that are working now, oldest first. since is when the session's
// Claude Code process started (zero: not known): subagents run inside it, so
// one launched before it, in an earlier run of the session, is not working.
// The caller asks only while that process is alive.
//
// The main transcript's records decide (see the top of this file); what
// it never mentions falls back to each subagent's own transcript (a
// foreground subagent, an older Claude Code that wrote no such records).
func Subagents(transcript string, since, now time.Time) []model.Subagent {
	if transcript == "" {
		return nil
	}
	lives, _ := lifeOf(transcript, now)
	dir := filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents")
	type ranked struct {
		s     model.Subagent
		order int
	}
	var out []ranked
	known := make(map[string]bool, len(lives))
	for id, l := range lives {
		known[id] = true
		if !l.working || !since.IsZero() && l.at.Before(since) {
			continue
		}
		s := model.Subagent{ID: id, Title: clip(oneLine(l.title), 80)}
		last := l.at
		if l.workflow {
			s.Type = "workflow"
		} else if info, ok := subFile(filepath.Join(dir, "agent-"+id+".jsonl")); ok {
			if info.done && info.mod.After(l.at) && now.Sub(info.mod) > subagentEndGrace {
				continue // its final reply came long ago: over, though no notification says so
			}
			if info.mod.After(last) {
				last = info.mod
			}
			s.Tool, s.Type = info.tool, info.typ
			if s.Title == "" {
				s.Title = info.title
			}
		}
		if now.Sub(last) > SubagentCap {
			continue
		}
		if s.Title == "" {
			s.Title = "subagent"
		}
		out = append(out, ranked{s, l.order})
	}
	// Order: launch order, then the fallback's by when their files were made.
	sort.Slice(out, func(i, j int) bool { return out[i].order < out[j].order })
	more := recentSubagents(transcript, subagentWindow, subagentToolWindow, known, now)
	sort.Slice(more, func(i, j int) bool { return more[i].since.Before(more[j].since) })
	if len(out)+len(more) == 0 {
		return nil
	}
	subs := make([]model.Subagent, 0, len(out)+len(more))
	for _, r := range out {
		subs = append(subs, r.s)
	}
	for _, f := range more {
		subs = append(subs, f.s)
	}
	return subs
}

// SubagentsDetail is the activity line of an agent whose main thread is
// between turns while its subagents work.
func SubagentsDetail(n int) string {
	if n == 1 {
		return "1 subagent working"
	}
	return fmt.Sprintf("%d subagents working", n)
}

// ShowBackground shows the agent running for its subagents alone
// (e.A.Subagents): its main thread is between turns.
func ShowBackground(e *store.Entry) {
	e.SetStatus(model.Running)
	e.Background = true
	e.A.Activity = &model.Activity{Tool: "Task", Detail: SubagentsDetail(len(e.A.Subagents))}
}

// FollowSubagents applies the agent's working subagents (e.A.Subagents, just
// read) to its status: an idle agent with subagents at work is running, and
// once the last one is done it is idle again, unseen (they finished, look at
// it) like any agent that just finished. A question, a permission prompt or
// an error is left as it is: those still show. It reports whether the agent
// changed.
func FollowSubagents(e *store.Entry) bool {
	n := len(e.A.Subagents)
	switch {
	case n > 0 && e.A.Status == model.Idle:
		ShowBackground(e)
		return true
	case n == 0 && e.Background:
		e.SetStatus(model.Idle)
		e.A.Unseen = true
		return true
	case e.Background && (e.A.Activity == nil || e.A.Activity.Detail != SubagentsDetail(n)):
		ShowBackground(e)
		return true
	case e.A.Status == model.Waiting && e.WaitAgent != "" && !slices.ContainsFunc(e.A.Subagents, func(s model.Subagent) bool { return s.ID == e.WaitAgent }):
		// The subagent that asked is gone (it stopped, or went on without
		// a hook we saw): nobody waits on that prompt any more.
		switch {
		case !e.WaitBackground:
			e.SetStatus(model.Running) // the main thread goes on
		case n > 0:
			ShowBackground(e)
		default:
			e.SetStatus(model.Idle)
			e.A.Unseen = true
		}
		return true
	}
	return false
}
