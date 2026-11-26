package telegram

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Config is one bridge's settings and progress. It holds no secret: the bot
// token is in the keystore.
type Config struct {
	ID              string    `json:"id"`                 // a short name: "default", "design"…
	UserID          int64     `json:"userId,omitempty"`   // the one Telegram account this bridge listens to
	UserName        string    `json:"userName,omitempty"` // its name, for the prompt's first line
	Agent           string    `json:"agent,omitempty"`    // the linked agent: every batch goes to it
	DebounceSeconds int       `json:"debounceSeconds,omitempty"`
	Offset          int64     `json:"offset,omitempty"` // the next Telegram update to read
	PairCode        string    `json:"pairCode,omitempty"`
	PairUntil       time.Time `json:"pairUntil,omitempty"`
}

// Result is how the deck took a prompt.
type Result struct {
	Queued   bool // false: typed in now
	Position int  // queued: its place in the agent's queue (1 = next)
}

// AgentInfo is what the bridge knows about its linked agent.
type AgentInfo struct {
	Title  string
	Status string
	Queued int
	Ended  bool // exited: prompts can't go in
}

// Timings, variables so tests can shorten them.
var (
	DefaultDebounce = 8 * time.Second
	maxWait         = 60 * time.Second // a batch closes at the latest this long after its first message
	retryDeliver    = time.Minute      // a delivery that failed (not held) is tried again after this
	pollTimeout     = 50 * time.Second
)

const (
	maxBatch = 30 // a batch closes at once with this many messages
	PairTTL  = 10 * time.Minute
)

// Bridge is one bot bound to one Telegram account and linked to one agent.
// Set its fields, then Run it.
type Bridge struct {
	API     *API
	Inbox   string // <data>/telegram/<id>/inbox
	Deliver func(agent, text string) (Result, error)
	Agent   func(id string) (AgentInfo, bool)
	Save    func(Config) error // persists the config (offset, pairing, link)
	Limit   int                // the longest prompt the deck takes, in bytes
	Logf    func(format string, args ...any)
	Now     func() time.Time

	mu      sync.Mutex
	cfg     Config
	open    *Batch
	queue   []*Batch // ready and held, oldest first
	timer   *time.Timer
	retry   *time.Timer
	ctx     context.Context
	lastErr string
	ignored int
	loaded  bool

	work sync.Mutex // closing and delivering batches, one at a time
}

// NewBridge is a bridge for cfg; set its fields, then Load or Run it.
func NewBridge(cfg Config) *Bridge { return &Bridge{cfg: cfg} }

func (b *Bridge) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *Bridge) logf(format string, args ...any) {
	format = "telegram " + b.cfg.ID + ": " + format
	if b.Logf != nil {
		b.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

func (b *Bridge) debounce() time.Duration {
	if b.cfg.DebounceSeconds > 0 {
		return time.Duration(b.cfg.DebounceSeconds) * time.Second
	}
	return DefaultDebounce
}

func (b *Bridge) context() context.Context {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ctx != nil {
		return b.ctx
	}
	return context.Background()
}

// Load reads the inbox: an open batch resumes, ready and held ones wait in
// order. Safe to call more than once.
func (b *Bridge) Load() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.loaded {
		return nil
	}
	all, err := loadBatches(b.Inbox)
	if err != nil {
		return err
	}
	for _, bt := range all {
		if bt.State == stateOpen {
			if b.open != nil { // two open ones (shouldn't happen): the older goes first, as it is
				b.open.State = stateReady
				b.queue = append(b.queue, b.open)
			}
			b.open = bt
		} else {
			b.queue = append(b.queue, bt)
		}
	}
	b.loaded = true
	if b.open != nil {
		b.timer = time.AfterFunc(b.debounce(), b.closeOpen)
	}
	return nil
}

// Status is a bridge's config plus how many messages are arriving, how many
// batches are held or waiting to go, and its last error.
type Status struct {
	Config
	Pending int    `json:"pending,omitempty"`
	Held    int    `json:"held,omitempty"`
	Waiting int    `json:"waiting,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Status reports the bridge.
func (b *Bridge) Status() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := Status{Config: b.cfg, Error: b.lastErr}
	if b.open != nil {
		s.Pending = len(b.open.Messages)
	}
	for _, bt := range b.queue {
		if bt.State == stateHeld {
			s.Held++
		} else {
			s.Waiting++
		}
	}
	return s
}

// Config is the bridge's current config.
func (b *Bridge) Config() Config {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg
}

// Update changes the config and saves it.
func (b *Bridge) Update(f func(*Config)) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	f(&b.cfg)
	return b.saveLocked()
}

func (b *Bridge) saveLocked() error {
	if b.Save == nil {
		return nil
	}
	return b.Save(b.cfg)
}

// Link points the bridge at an agent and delivers what was held, in order.
// It returns how many batches went.
func (b *Bridge) Link(agent string) (int, error) {
	if err := b.Load(); err != nil {
		return 0, err
	}
	if err := b.Update(func(c *Config) { c.Agent = agent }); err != nil {
		return 0, err
	}
	return b.process(), nil
}

// Run polls Telegram until ctx ends, or the token is refused.
func (b *Bridge) Run(ctx context.Context) error {
	b.mu.Lock()
	b.ctx = ctx
	b.mu.Unlock()
	if err := b.Load(); err != nil {
		return err
	}
	go b.process() // what waited when the service stopped
	backoff := time.Second
	for ctx.Err() == nil {
		b.mu.Lock()
		offset := b.cfg.Offset
		b.mu.Unlock()
		ups, err := b.API.GetUpdates(ctx, offset, pollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			b.setErr(err.Error())
			if errors.Is(err, ErrUnauthorized) {
				b.logf("%v", err)
				return err
			}
			b.logf("%v (again in %s)", err, backoff)
			select {
			case <-ctx.Done():
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = time.Second
		b.setErr("")
		// The offset moves past an update only once it is on disk: after a
		// crash it is read again (a message already in the open batch is
		// skipped).
		moved, stuck := false, false
		for _, u := range ups {
			if u.Message != nil {
				if err := b.handle(ctx, u.Message); err != nil {
					b.setErr("saving a message: " + err.Error())
					b.logf("saving a message: %v (it will be read again)", err)
					stuck = true
					break
				}
			}
			b.mu.Lock()
			b.cfg.Offset, moved = u.ID+1, true
			b.mu.Unlock()
		}
		if moved {
			b.mu.Lock()
			err := b.saveLocked()
			b.mu.Unlock()
			if err != nil {
				b.logf("saving the offset: %v", err)
			}
		}
		if stuck {
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
		}
	}
	b.mu.Lock()
	if b.timer != nil {
		b.timer.Stop()
	}
	if b.retry != nil {
		b.retry.Stop()
	}
	b.mu.Unlock()
	return nil
}

func (b *Bridge) setErr(s string) {
	b.mu.Lock()
	b.lastErr = s
	b.mu.Unlock()
}

// handle takes one message: pairing, a command, or part of the open batch
// (saved before it returns). Anyone but the paired account is ignored
// without an answer.
func (b *Bridge) handle(ctx context.Context, m *Message) error {
	if m.Chat.Type != "private" || m.From == nil || m.From.IsBot {
		return nil
	}
	cmd, arg := command(m)
	cfg := b.Config()
	if cfg.UserID == 0 || m.From.ID != cfg.UserID {
		if cmd == "/start" && cfg.UserID == 0 && cfg.PairCode != "" && b.now().Before(cfg.PairUntil) &&
			strings.EqualFold(arg, cfg.PairCode) {
			if err := b.Update(func(c *Config) {
				c.UserID, c.UserName, c.PairCode, c.PairUntil = m.From.ID, m.From.Name(), "", time.Time{}
			}); err != nil {
				return err
			}
			b.logf("paired with a Telegram account")
			b.reply(ctx, m.Chat.ID, "Paired. Send or forward messages here: they go to "+agentText(cfg.Agent)+".")
			return nil
		}
		b.mu.Lock()
		b.ignored++
		n := b.ignored
		b.mu.Unlock()
		if n == 1 || n%50 == 0 {
			b.logf("ignored %d message(s) from accounts that aren't paired", n)
		}
		return nil
	}
	switch cmd {
	case "/start", "/help":
		b.reply(ctx, m.Chat.ID, "Send or forward messages, photos and files: after a few quiet seconds they go to "+
			agentText(cfg.Agent)+" as one prompt. /status shows where things stand.")
		return nil
	case "/status":
		b.reply(ctx, m.Chat.ID, b.statusText())
		return nil
	}
	return b.add(m)
}

func agentText(agent string) string {
	if agent == "" {
		return "no agent yet (link one: agentctl telegram link <agent id>)"
	}
	return agent
}

func (b *Bridge) statusText() string {
	st := b.Status()
	var s string
	if st.Agent == "" {
		s = "Not linked to an agent: agentctl telegram link <agent id>."
	} else if info, ok := b.Agent(st.Agent); !ok || info.Ended {
		s = "Linked to " + st.Agent + ", which has ended: re-link to deliver (agentctl telegram link <agent id>)."
	} else {
		s = fmt.Sprintf("Linked to %s (%s): %s", st.Agent, info.Title, info.Status)
		if info.Queued > 0 {
			s += fmt.Sprintf(", %d queued", info.Queued)
		}
		s += "."
	}
	if st.UserName != "" {
		s += " Paired with " + st.UserName + "."
	}
	if st.Held > 0 {
		s += " " + plural(st.Held, "batch") + " held."
	}
	if st.Waiting > 0 {
		s += " " + plural(st.Waiting, "batch") + " waiting to go."
	}
	if st.Pending > 0 {
		s += " " + plural(st.Pending, "message") + " arriving."
	}
	return s
}

// command returns a message's /command and its argument; "" for a forward
// or a plain message.
func command(m *Message) (string, string) {
	if m.Origin != nil || m.ForwardDate != 0 || !strings.HasPrefix(m.Text, "/") {
		return "", ""
	}
	cmd, arg, _ := strings.Cut(strings.TrimSpace(m.Text), " ")
	cmd, _, _ = strings.Cut(cmd, "@") // /status@somebot
	return strings.ToLower(cmd), strings.TrimSpace(arg)
}

func (b *Bridge) reply(ctx context.Context, chat int64, text string) {
	if err := b.API.SendMessage(ctx, chat, text); err != nil {
		b.logf("answering: %v", err)
	}
}

// add puts m in the open batch, saves it, and (re)sets its timer: the batch
// closes once no message came for the debounce time, at most maxWait after
// its first, or at once when it holds maxBatch messages.
func (b *Bridge) add(m *Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if b.open == nil {
		b.open = newBatch(b.Inbox, now, m)
	}
	if b.open.has(m.ID) { // read again after a crash
		return nil
	}
	b.open.Messages = append(b.open.Messages, *m)
	if err := b.open.save(); err != nil {
		b.open.Messages = b.open.Messages[:len(b.open.Messages)-1]
		return err
	}
	wait := b.debounce()
	if left := maxWait - now.Sub(b.open.Opened); left < wait {
		wait = max(left, 0)
	}
	if len(b.open.Messages) >= maxBatch {
		wait = 0
	}
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(wait, b.closeOpen)
	return nil
}

// closeOpen closes the open batch: its files are downloaded, it joins the
// queue, and the queue is worked.
func (b *Bridge) closeOpen() {
	b.work.Lock()
	b.mu.Lock()
	bt := b.open
	b.open = nil
	b.mu.Unlock()
	if bt != nil && len(bt.Messages) > 0 {
		b.download(bt)
		bt.State = stateReady
		if err := bt.save(); err != nil {
			b.logf("saving batch %s: %v", bt.ID, err)
		}
		b.mu.Lock()
		b.queue = append(b.queue, bt)
		b.mu.Unlock()
	}
	b.work.Unlock()
	b.process()
}

// CloseNow closes the open batch at once.
func (b *Bridge) CloseNow() {
	b.mu.Lock()
	if b.timer != nil {
		b.timer.Stop()
	}
	b.mu.Unlock()
	b.closeOpen()
}

// download fetches every file of the batch into its folder. A file over the
// Bot API's limit isn't tried; one that still fails after the retries is
// listed as failed. Nothing is left out.
func (b *Bridge) download(bt *Batch) {
	ctx := b.context()
	taken := names{"batch.json": true, "batch.json.tmp": true, "messages.md": true}
	bt.Files = nil
	for i := range bt.Messages {
		for _, a := range assets(&bt.Messages[i]) {
			f := FileResult{Msg: i, Kind: a.kind, Size: a.size, Name: a.name}
			if a.size > MaxDownload {
				f.Status = "too big"
				bt.Files = append(bt.Files, f)
				continue
			}
			name := taken.take(fmt.Sprintf("%02d-%s", i+1, a.name))
			dst := filepath.Join(bt.dir, name)
			got, err := b.API.Download(ctx, a.id, dst)
			switch {
			case errors.Is(err, ErrTooBig):
				f.Status = "too big"
			case err != nil:
				f.Status, f.Error = "failed", err.Error()
				b.logf("batch %s: a %s couldn't be downloaded: %v", bt.ID, a.kind, err)
			default:
				if filepath.Ext(name) == "" { // a photo or thumbnail: the extension Telegram gave it
					if ext := safeName("x"+path.Ext(got), "x"); len(ext) > 1 {
						with := taken.take(name + ext[1:])
						if os.Rename(dst, filepath.Join(bt.dir, with)) == nil {
							name = with
						}
					}
				}
				f.Name, f.Status, f.Path = name, "ok", filepath.Join(bt.dir, name)
			}
			bt.Files = append(bt.Files, f)
		}
	}
}

// process delivers the queue in order. It stops at a batch it can't deliver,
// so a newer batch never overtakes an older one: with the linked agent gone
// the batch is held until a re-link; any other failure is tried again later.
// It returns how many batches went.
func (b *Bridge) process() int {
	b.work.Lock()
	defer b.work.Unlock()
	ctx := b.context()
	sent := 0
	for {
		b.mu.Lock()
		if len(b.queue) == 0 {
			b.mu.Unlock()
			return sent
		}
		bt, cfg := b.queue[0], b.cfg
		b.mu.Unlock()

		wasHeld := bt.State == stateHeld
		var info AgentInfo
		ok := false
		if cfg.Agent != "" {
			info, ok = b.Agent(cfg.Agent)
		}
		if !ok || info.Ended {
			if !wasHeld || bt.Agent != cfg.Agent {
				bt.State, bt.Agent = stateHeld, cfg.Agent
				msg := "Held: no agent linked. Link one to deliver: agentctl telegram link <agent id>."
				bt.Error = "no linked agent"
				if cfg.Agent != "" {
					bt.Error = cfg.Agent + " ended"
					msg = "Held: " + cfg.Agent + " ended, re-link to deliver (agentctl telegram link <agent id>)."
				}
				if err := bt.save(); err != nil {
					b.logf("batch %s: %v", bt.ID, err)
				}
				b.logf("batch %s held: %s", bt.ID, bt.Error)
				b.reply(ctx, bt.Chat, msg+" "+b.summary(bt).short)
			}
			return sent
		}
		p := b.build(bt, cfg)
		res, err := b.Deliver(cfg.Agent, p.text)
		if err != nil {
			first := bt.Error == "" || wasHeld
			bt.State, bt.Error = stateReady, err.Error()
			_ = bt.save()
			b.logf("batch %s for %s: %v (again in %s)", bt.ID, cfg.Agent, err, retryDeliver)
			if first {
				b.reply(ctx, bt.Chat, "Not delivered to "+cfg.Agent+" yet ("+err.Error()+"), trying again: "+p.short)
			}
			b.mu.Lock()
			if b.retry != nil {
				b.retry.Stop()
			}
			b.retry = time.AfterFunc(retryDeliver, func() { b.process() })
			b.mu.Unlock()
			return sent
		}
		bt.State, bt.Agent, bt.Error, bt.Delivered = stateDelivered, cfg.Agent, "", b.now()
		if err := bt.save(); err != nil {
			b.logf("batch %s: %v", bt.ID, err)
		}
		b.mu.Lock()
		b.queue = b.queue[1:]
		b.mu.Unlock()
		sent++
		how := "Sent to " + cfg.Agent
		if res.Queued {
			how = fmt.Sprintf("Queued for %s (%s in line)", cfg.Agent, ordinal(res.Position))
		}
		if wasHeld {
			how += ", held until now"
		}
		b.logf("batch %s: %s, %s", bt.ID, plural(len(bt.Messages), "message"), strings.ToLower(how[:1])+how[1:])
		b.reply(ctx, bt.Chat, how+": "+p.short+p.problems)
	}
}

// built is a batch as a prompt.
type built struct {
	text     string
	short    string // "3 messages, 2 images, 1 file"
	problems string // "; too big for the Bot API (31 MB): clip.mov, open it in Telegram"
}

func (b *Bridge) summary(bt *Batch) built {
	var images, files int
	var problems []string
	for _, f := range bt.Files {
		switch {
		case f.Status == "too big":
			problems = append(problems, "too big for the Bot API ("+megabytes(f.Size)+"): "+f.Name+", open it in Telegram")
		case f.Status == "failed":
			problems = append(problems, "failed to download: "+f.Name+", open it in Telegram")
		case f.Kind == "image":
			images++
		default:
			files++
		}
	}
	s := plural(len(bt.Messages), "message")
	if images > 0 {
		s += ", " + plural(images, "image")
	}
	if files > 0 {
		s += ", " + plural(files, "file")
	}
	out := built{short: s}
	if len(problems) > 0 {
		out.problems = "; " + strings.Join(problems, "; ")
	}
	return out
}

// build writes the batch's prompt: a first line, then each message in order
// with its text or caption and its files by absolute path. The whole text
// is also saved as messages.md in the batch folder; a prompt over the
// deck's limit keeps the start and points to it.
func (b *Bridge) build(bt *Batch, cfg Config) built {
	out := b.summary(bt)
	count := plural(len(bt.Messages), "message")
	first := "[Telegram] " + count
	if cfg.UserName != "" {
		first += " from " + clean(cfg.UserName)
	}
	first += strings.TrimPrefix(out.short, count)

	var parts []string
	for i := range bt.Messages {
		m := &bt.Messages[i]
		head := stamp(m.Date)
		if from, when, ok := forwarded(m); ok {
			head = "forwarded from " + from
			if when != 0 {
				head += ", " + stamp(when)
			}
		}
		lines := []string{fmt.Sprintf("%d. %s:", i+1, head)}
		if t := clean(m.Text + m.Caption); t != "" {
			lines = append(lines, t)
		}
		for _, f := range bt.Files {
			if f.Msg != i {
				continue
			}
			switch f.Status {
			case "ok":
				lines = append(lines, "   "+f.Kind+": "+f.Path)
			case "too big":
				lines = append(lines, "   "+f.Kind+" too big for the Bot API ("+megabytes(f.Size)+"): "+f.Name+", open it in Telegram")
			default:
				lines = append(lines, "   "+f.Kind+" failed to download ("+f.Error+"): "+f.Name+", open it in Telegram")
			}
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	body := strings.Join(parts, "\n\n")
	text := first + "\n\n" + body
	full := filepath.Join(bt.dir, "messages.md")
	if err := os.WriteFile(full, []byte(text+"\n"), 0o600); err != nil {
		b.logf("batch %s: %v", bt.ID, err)
	}
	limit := b.Limit
	if limit <= 0 {
		limit = 4000
	}
	if len(text) > limit {
		note := "\n\nThe messages are long: this is the start. All of them, with every file, are in " + full + "\n\n"
		text = first + note + clip(body, limit-len(first)-len(note))
	}
	out.text = text
	return out
}

// forwarded says who wrote a forwarded message and when.
func forwarded(m *Message) (string, int64, bool) {
	if o := m.Origin; o != nil {
		var from string
		switch {
		case o.SenderUser != nil:
			from = o.SenderUser.Name()
		case o.SenderUserName != "":
			from = o.SenderUserName
		case o.Chat != nil:
			from = o.Chat.Title
		case o.SenderChat != nil:
			from = o.SenderChat.Title
		}
		if o.AuthorSignature != "" {
			from = strings.TrimSpace(from + " (" + o.AuthorSignature + ")")
		}
		if from == "" {
			from = "someone"
		}
		return clean(from), o.Date, true
	}
	if m.ForwardDate != 0 {
		from := m.ForwardSenderName
		if m.ForwardFrom != nil {
			from = m.ForwardFrom.Name()
		} else if m.ForwardFromChat != nil {
			from = m.ForwardFromChat.Title
		}
		if from == "" {
			from = "someone"
		}
		return clean(from), m.ForwardDate, true
	}
	return "", 0, false
}

func stamp(unix int64) string {
	if unix == 0 {
		return "undated"
	}
	return time.Unix(unix, 0).Local().Format("2 Jan 2006 15:04")
}

// clean makes text safe to type into a terminal: newlines and tabs stay,
// other control characters become spaces.
func clean(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// clip cuts s to at most n bytes on a rune boundary, with "…".
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 4 {
		return ""
	}
	s = s[:n-3]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return strings.TrimRight(s, " \n") + "…"
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	if strings.HasSuffix(word, "ch") {
		return fmt.Sprintf("%d %ses", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

// NewPairCode is a one-time code for /start: 8 letters and digits that are
// hard to mix up.
func NewPairCode() string {
	const abc = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = abc[int(b[i])%len(abc)]
	}
	return string(b)
}
