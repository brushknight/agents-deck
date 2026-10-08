package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Config for one simulated agent (flags of `agentctl sim-agent`).
type Config struct {
	ID         string // AGENTSTERM_ID
	Socket     string // daemon unix socket
	Dir        string // project folder
	Transcript string // where to write the Claude-style JSONL
	Seed       int64
	Speed      float64       // >1 = faster
	Patience   time.Duration // auto-answer prompts after this long (0 = never)
}

type agent struct {
	cfg     Config
	p       Project
	r       *rand.Rand
	session string
	model   string
	ctx     int64
	msgN    int
	keys    chan byte
	hc      *http.Client
	titled  bool
}

const (
	cOrange = "\x1b[38;5;209m"
	cDim    = "\x1b[38;5;245m"
	cBold   = "\x1b[1m"
	cGreen  = "\x1b[38;5;114m"
	cRed    = "\x1b[38;5;203m"
	cReset  = "\x1b[0m"
)

var models = []struct {
	id, label string
	weight    int
}{{"claude-opus-5-5", "Opus 5.5", 6}, {"claude-sonnet-5", "Sonnet 5", 3}, {"claude-haiku-4-5", "Haiku 4.5", 1}}

// Run drives one fake Claude until the session is killed.
func Run(cfg Config) error {
	if cfg.Speed <= 0 {
		cfg.Speed = 1
	}
	a := &agent{cfg: cfg, r: rand.New(rand.NewSource(cfg.Seed)), keys: make(chan byte, 16),
		hc: &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", cfg.Socket)
			}}}}
	a.p = Scan(cfg.Dir)
	a.session = uuid(a.r)
	w := a.r.Intn(10)
	for _, m := range models {
		if w -= m.weight; w < 0 {
			a.model = m.id
			a.header(m.label)
			break
		}
	}
	a.ctx = 18_000 + int64(a.r.Intn(9000))
	if a.r.Intn(4) == 0 { // a long-running session, close to its first auto-compact
		a.ctx = a.window() * int64(80+a.r.Intn(9)) / 100
	}

	restore := rawMode()
	defer restore()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	go func() { <-sig; a.hook("SessionEnd", nil); restore(); os.Exit(0) }()
	go a.readKeys()

	_ = os.MkdirAll(filepath.Dir(cfg.Transcript), 0o700)
	a.line(map[string]any{"type": "user", "cwd": cfg.Dir, "gitBranch": a.p.Branch, "sessionId": a.session})
	a.hook("SessionStart", map[string]any{"source": "startup"})
	a.sleep(1, 8) // stagger agents
	for {
		a.task()
		a.idle()
	}
}

func (a *agent) header(model string) {
	home, _ := os.UserHomeDir()
	dir := strings.Replace(a.cfg.Dir, home, "~", 1)
	a.print("")
	a.print(" " + cOrange + "✻" + cReset + cBold + " Claude Code" + cReset + cDim + " · simulated" + cReset)
	a.print("   " + cDim + model + " · " + dir + cReset)
	a.print("")
}

// ---- terminal ---------------------------------------------------------------

func rawMode() func() {
	cmd := exec.Command("stty", "raw", "-echo")
	cmd.Stdin = os.Stdin
	if cmd.Run() != nil {
		return func() {}
	}
	return func() {
		c := exec.Command("stty", "sane")
		c.Stdin = os.Stdin
		_ = c.Run()
	}
}

func (a *agent) readKeys() {
	buf := make([]byte, 16)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		for _, b := range buf[:n] {
			select {
			case a.keys <- b:
			default:
			}
		}
	}
}

func (a *agent) print(s string) { fmt.Fprint(os.Stdout, strings.ReplaceAll(s, "\n", "\r\n")+"\r\n") }

func (a *agent) drainKeys() {
	for {
		select {
		case <-a.keys:
		default:
			return
		}
	}
}

func (a *agent) dur(min, max float64) time.Duration {
	s := min + a.r.Float64()*(max-min)
	return time.Duration(s / a.cfg.Speed * float64(time.Second))
}

// sleep waits min..max seconds (scaled); it returns true if Esc interrupted it.
func (a *agent) sleep(min, max float64) bool {
	t := time.NewTimer(a.dur(min, max))
	defer t.Stop()
	for {
		select {
		case <-t.C:
			return false
		case k := <-a.keys:
			if k == 0x1b {
				return true
			}
		}
	}
}

// spin shows Claude's working line while a step "runs"; true = interrupted.
func (a *agent) spin(verb string, min, max float64) bool {
	end := time.Now().Add(a.dur(min, max))
	start := time.Now()
	frames := []string{"·", "✢", "✳", "✶", "✻", "✽"}
	tick := time.NewTicker(180 * time.Millisecond)
	defer tick.Stop()
	defer fmt.Fprint(os.Stdout, "\r\x1b[K")
	for i := 0; ; i++ {
		el := time.Since(start).Seconds()
		fmt.Fprintf(os.Stdout, "\r\x1b[K%s%s %s…%s %s(%ds · esc to interrupt)%s", cOrange, frames[i%len(frames)], verb, cReset, cDim, int(el), cReset)
		select {
		case <-tick.C:
			if time.Now().After(end) {
				return false
			}
		case k := <-a.keys:
			if k == 0x1b {
				return true
			}
		}
	}
}

// waitChoice waits for a digit 1..n (or Esc = n, the "no" option). After the
// configured patience the simulated user answers option 1 at the terminal.
func (a *agent) waitChoice(n int) int {
	a.drainKeys()
	var timeout <-chan time.Time
	if a.cfg.Patience > 0 {
		t := time.NewTimer(a.cfg.Patience + time.Duration(a.r.Intn(20))*time.Second)
		defer t.Stop()
		timeout = t.C
	}
	for {
		select {
		case <-timeout:
			return 1
		case k := <-a.keys:
			if k == 0x1b {
				return n
			}
			if k >= '1' && int(k-'0') <= n {
				return int(k - '0')
			}
		}
	}
}

// ---- daemon + transcript --------------------------------------------------------

func (a *agent) hook(event string, extra map[string]any) {
	payload := map[string]any{"hook_event_name": event, "session_id": a.session,
		"transcript_path": a.cfg.Transcript, "cwd": a.cfg.Dir}
	for k, v := range extra {
		payload[k] = v
	}
	b, _ := json.Marshal(payload)
	resp, err := a.hc.Post("http://agentd/hook?agent="+a.cfg.ID, "application/json", bytes.NewReader(b))
	if err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func (a *agent) line(v map[string]any) {
	f, err := os.OpenFile(a.cfg.Transcript, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(v)
	_, _ = f.Write(append(b, '\n'))
}

// usage records one API turn: the context grows by delta tokens.
func (a *agent) usage(delta int64) {
	a.msgN++
	read := a.ctx
	a.ctx += delta
	if a.ctx > a.window()*9/10 {
		a.compact()
		read = a.ctx
		a.ctx += delta
	}
	a.line(map[string]any{"type": "assistant", "gitBranch": a.p.Branch, "message": map[string]any{
		"id": fmt.Sprintf("msg_sim_%s_%d", a.cfg.ID, a.msgN), "model": a.model,
		"usage": map[string]any{"input_tokens": 3 + a.r.Intn(40), "output_tokens": 60 + a.r.Intn(900),
			"cache_read_input_tokens": read, "cache_creation_input_tokens": delta},
	}})
}

func (a *agent) window() int64 {
	if strings.Contains(a.model, "haiku") {
		return 200_000
	}
	return 1_000_000
}

// compact plays an auto-compact: the hook, a few seconds of summarising,
// then the boundary line and the fresh session start, like Claude Code.
func (a *agent) compact() {
	a.hook("PreCompact", map[string]any{"trigger": "auto"})
	a.spin("Compacting conversation", 4, 8)
	a.ctx = 40_000 + int64(a.r.Intn(30_000))
	a.line(map[string]any{"type": "system", "subtype": "compact_boundary", "content": "Conversation compacted"})
	a.hook("SessionStart", map[string]any{"source": "compact"})
	a.print(cDim + "✻ Conversation compacted" + cReset)
}

// ---- behaviour --------------------------------------------------------------------

func (a *agent) task() {
	t := a.p.Task(a.r)
	a.print(cDim + "────────────────────────────────────────────────────────────" + cReset)
	a.print(cBold + "> " + cReset + t.Prompt)
	a.print("")
	a.hook("UserPromptSubmit", map[string]any{"prompt": t.Prompt})
	if a.spin("Thinking", 2, 5) {
		a.interrupted()
		return
	}
	a.usage(int64(2000 + a.r.Intn(4000)))
	if !a.titled || a.r.Intn(3) == 0 {
		a.line(map[string]any{"type": "ai-title", "aiTitle": t.Title})
		a.titled = true
	}
	steps := 4 + a.r.Intn(7)
	// At most one interruption per task: ~15% a permission prompt, ~10% a
	// question, the rest run straight through.
	promptAt, kind := -1, a.r.Intn(100)
	if kind < 25 {
		promptAt = 1 + a.r.Intn(steps-1)
	}
	for i := 0; i < steps; i++ {
		if a.r.Intn(40) == 0 {
			a.apiError()
		}
		if i == promptAt && kind >= 15 {
			if !a.question() {
				return
			}
			continue
		}
		if !a.step(i, i == promptAt) {
			return
		}
	}
	a.print(cOrange + "⏺" + cReset + " " + summary(a.r, t))
	a.print("")
	a.usage(int64(800 + a.r.Intn(2500)))
	a.hook("Stop", nil)
}

// step performs one tool call; false means the turn ended early.
func (a *agent) step(i int, ask bool) bool {
	roll := a.r.Intn(100)
	if ask && roll < 48 {
		roll = 48 + a.r.Intn(52) // a prompt needs an Edit or a Bash step
	}
	var tool, label, result string
	var input map[string]any
	needsOK := false
	switch {
	case i == 0 && a.r.Intn(3) == 0:
		n := 3 + a.r.Intn(4)
		tool, label, result = "TodoWrite", "Update Todos", fmt.Sprintf("☐ %d tasks planned", n)
		todos := make([]map[string]any, n)
		for j := range todos {
			todos[j] = map[string]any{"content": "step", "status": "pending"}
		}
		input = map[string]any{"todos": todos}
	case roll < 33:
		f := a.p.file(a.r)
		tool, label, result = "Read", "Read("+f+")", fmt.Sprintf("Read %d lines", 40+a.r.Intn(400))
		input = map[string]any{"file_path": filepath.Join(a.cfg.Dir, f)}
	case roll < 48:
		pat := patterns[a.r.Intn(len(patterns))]
		tool, label, result = "Grep", fmt.Sprintf("Search(pattern: %q)", pat), fmt.Sprintf("Found %d files", 1+a.r.Intn(14))
		input = map[string]any{"pattern": pat}
	case roll < 75:
		f := a.p.file(a.r)
		add, del := 2+a.r.Intn(40), a.r.Intn(18)
		tool, label, result = "Edit", "Update("+f+")", fmt.Sprintf("Updated %s with %d additions and %d removals", f, add, del)
		input = map[string]any{"file_path": filepath.Join(a.cfg.Dir, f)}
		needsOK = ask
	default:
		cmds := a.p.Commands()
		c := cmds[a.r.Intn(len(cmds))]
		tool, label = "Bash", "Bash("+c+")"
		result = []string{fmt.Sprintf("✓ %d passed", 12+a.r.Intn(300)), "no issues found", "done in " + fmt.Sprint(1+a.r.Intn(40)) + "s"}[a.r.Intn(3)]
		input = map[string]any{"command": c, "description": "run " + strings.Fields(c)[0]}
		needsOK = ask
	}
	a.print(cOrange + "⏺" + cReset + " " + label)
	a.hook("PreToolUse", map[string]any{"tool_name": tool, "tool_input": input})
	if needsOK && !a.permission(tool, input) {
		return false
	}
	if a.spin(verbs[a.r.Intn(len(verbs))], 1.5, 6) {
		a.interrupted()
		return false
	}
	a.print("  " + cDim + "⎿  " + result + cReset)
	a.print("")
	a.hook("PostToolUse", map[string]any{"tool_name": tool})
	a.usage(int64(1200 + a.r.Intn(9000)))
	return true
}

var verbs = []string{"Working", "Pondering", "Cooking", "Crunching", "Tinkering", "Noodling", "Brewing", "Synthesizing"}

func (a *agent) permission(tool string, input map[string]any) bool {
	a.hook("PermissionRequest", map[string]any{"tool_name": tool, "tool_input": input})
	home, _ := os.UserHomeDir()
	dir := strings.Replace(a.cfg.Dir, home, "~", 1)
	a.print(cDim + "────────────────────────────────────────────────────────────" + cReset)
	if tool == "Bash" {
		c := input["command"].(string)
		a.print(" " + cBold + "Bash command" + cReset)
		a.print("   " + c)
		a.print("   " + cDim + input["description"].(string) + cReset)
		a.print(" Do you want to proceed?")
		a.print(" " + cOrange + "❯ 1. Yes" + cReset)
		a.print("   2. Yes, and don't ask again for " + strings.Fields(c)[0] + " commands in " + dir)
	} else {
		a.print(" " + cBold + "Edit file" + cReset)
		a.print("   " + strings.Replace(input["file_path"].(string), home, "~", 1))
		a.print(" Do you want to make this edit?")
		a.print(" " + cOrange + "❯ 1. Yes" + cReset)
		a.print("   2. Yes, allow all edits during this session")
	}
	a.print("   3. No, and tell Claude what to do differently (esc)")
	a.print(" " + cDim + "Esc to cancel" + cReset)
	if a.waitChoice(3) == 3 {
		a.print("  " + cRed + "⎿  Interrupted · What should Claude do instead?" + cReset)
		a.print("")
		a.hook("Stop", nil)
		return false
	}
	return true
}

var questions = []struct {
	q, header string
	opts      [3][2]string
}{
	{"Which approach should I take?", "Approach", [3][2]string{{"Minimal fix", "smallest change that makes it pass"}, {"Proper refactor", "cleaner, touches more files"}, {"Add a flag", "keep old behaviour behind a setting"}}},
	{"Should I keep backwards compatibility?", "Compat", [3][2]string{{"Yes, keep it", "add a shim for old callers"}, {"No, break it", "update all callers now"}, {"Deprecate first", "warn for one release"}}},
	{"Where should the new tests live?", "Tests", [3][2]string{{"Next to the code", "same package, _test file"}, {"Integration suite", "slower, more realistic"}, {"Both", "unit + one integration case"}}},
}

func (a *agent) question() bool {
	q := questions[a.r.Intn(len(questions))]
	opts := make([]map[string]any, 3)
	for i, o := range q.opts {
		opts[i] = map[string]any{"label": o[0], "description": o[1]}
	}
	input := map[string]any{"questions": []any{map[string]any{"question": q.q, "header": q.header, "options": opts, "multiSelect": false}}}
	a.hook("PreToolUse", map[string]any{"tool_name": "AskUserQuestion", "tool_input": input})
	a.hook("PermissionRequest", map[string]any{"tool_name": "AskUserQuestion", "tool_input": input})
	a.print(cDim + "────────────────────────────────────────────────────────────" + cReset)
	a.print(" ☐ " + q.header)
	a.print(q.q)
	for i, o := range q.opts {
		if i == 0 {
			a.print(cOrange + "❯ 1. " + o[0] + cReset)
		} else {
			a.print(fmt.Sprintf("  %d. %s", i+1, o[0]))
		}
		a.print("     " + cDim + o[1] + cReset)
	}
	a.print("  4. Type something.")
	a.print("  5. Chat about this")
	a.print(cDim + "Enter to select · ↑/↓ to navigate · Esc to cancel" + cReset)
	c := a.waitChoice(3)
	a.print(cOrange + "⏺" + cReset + " User answered Claude's questions:")
	a.print("  " + cDim + "⎿  · " + q.q + " → " + q.opts[c-1][0] + cReset)
	a.print("")
	a.hook("PostToolUse", map[string]any{"tool_name": "AskUserQuestion"})
	a.usage(int64(600 + a.r.Intn(1500)))
	return true
}

func (a *agent) apiError() {
	a.msgN++
	a.line(map[string]any{"type": "assistant", "isApiErrorMessage": true, "message": map[string]any{
		"id": fmt.Sprintf("err_sim_%s_%d", a.cfg.ID, a.msgN), "model": "<synthetic>",
		"content": []any{map[string]any{"type": "text", "text": "API Error: 529 Overloaded"}}}})
	a.print("  " + cRed + "⎿  API Error: 529 Overloaded · retrying…" + cReset)
	a.sleep(8, 18)
}

func (a *agent) interrupted() {
	a.print("  " + cRed + "⎿  Interrupted · What should Claude do instead?" + cReset)
	a.print("")
	a.hook("Stop", nil)
}

// idle waits for the "user" to come back with the next task.
func (a *agent) idle() {
	a.print(cDim + "> " + cReset)
	min, max := 15.0, 70.0
	if a.r.Intn(5) == 0 {
		min, max = 90, 240 // some agents sit done for a while
	}
	a.sleep(min, max)
}

func summary(r *rand.Rand, t Task) string {
	s := []string{
		"Done — " + strings.ToLower(t.Title[:1]) + t.Title[1:] + ". Tests pass.",
		"Finished. " + t.Title + " is in place; I left a note in the PR description.",
		"All set: " + strings.ToLower(t.Title) + ". Nothing else looked risky.",
	}
	return s[r.Intn(len(s))]
}

func uuid(r *rand.Rand) string {
	b := make([]byte, 16)
	r.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
