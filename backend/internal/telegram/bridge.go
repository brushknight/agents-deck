package telegram

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// State is telegram.json in the data folder (0600). It holds no secret: the
// bot token is in the keystore.
type State struct {
	UserID          int64     `json:"userId,omitempty"`   // the one Telegram account the bot listens to
	UserName        string    `json:"userName,omitempty"` // its name, for the prompt's first line
	Target          string    `json:"target,omitempty"`   // the agent the messages go to
	DebounceSeconds int       `json:"debounceSeconds,omitempty"`
	Offset          int64     `json:"offset,omitempty"` // the next update to read
	PairCode        string    `json:"pairCode,omitempty"`
	PairUntil       time.Time `json:"pairUntil,omitempty"`
}

// LoadState reads path; a missing file is an empty state.
func LoadState(p string) (State, error) {
	var s State
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

// SaveState writes path atomically, 0600.
func SaveState(p string, s State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Result is how the deck took a prompt.
type Result struct {
	Queued   bool // false: typed in now
	Position int  // queued: its place in the agent's queue (1 = next)
}

// AgentInfo is what /status and /to show about an agent.
type AgentInfo struct {
	Title  string
	Status string
	Queued int
}

// Defaults.
const (
	DefaultDebounce = 8 * time.Second
	maxWait         = 60 * time.Second // a batch goes at the latest this long after its first message
	maxBatch        = 30               // ...or once it has this many
	pollTimeout     = 50 * time.Second
	PairTTL         = 10 * time.Minute
)

// Bridge reads the bot's messages and hands them on. Build it with its
// fields set, then Run it.
type Bridge struct {
	API       *API
	StatePath string
	Inbox     string                                   // where files go: <Inbox>/<date>/<batch>/
	Deliver   func(agent, text string) (Result, error) // a prompt, queued when the agent is busy
	Agent     func(id string) (AgentInfo, bool)
	Limit     int // the longest prompt the deck takes, in bytes
	Logf      func(format string, args ...any)
	Now       func() time.Time

	mu      sync.Mutex
	state   State
	loaded  bool // state read from StatePath
	pending []*Message
	first   time.Time // when the pending batch's first message came
	timer   *time.Timer
	flushMu sync.Mutex // one batch at a time
	ctx     context.Context
	lastErr string
	ignored int
}

func (b *Bridge) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *Bridge) logf(format string, args ...any) {
	if b.Logf != nil {
		b.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

func (b *Bridge) debounce() time.Duration {
	if b.state.DebounceSeconds > 0 {
		return time.Duration(b.state.DebounceSeconds) * time.Second
	}
	return DefaultDebounce
}

// loadLocked reads telegram.json once. Call with b.mu held.
func (b *Bridge) loadLocked() error {
	if b.loaded {
		return nil
	}
	st, err := LoadState(b.StatePath)
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(b.StatePath), err)
	}
	b.state, b.loaded = st, true
	return nil
}

// Status is the bridge's state for `agentctl telegram status`.
func (b *Bridge) Status() (State, int, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_ = b.loadLocked()
	return b.state, len(b.pending), b.lastErr
}

// Update changes the state (from the CLI), keeping the read offset.
func (b *Bridge) Update(f func(*State)) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.loadLocked(); err != nil {
		return err
	}
	f(&b.state)
	return SaveState(b.StatePath, b.state)
}

// Run polls until ctx ends, or the token is refused.
func (b *Bridge) Run(ctx context.Context) error {
	b.mu.Lock()
	err := b.loadLocked()
	b.ctx = ctx
	b.mu.Unlock()
	if err != nil {
		return err
	}
	backoff := time.Second
	for ctx.Err() == nil {
		b.mu.Lock()
		offset := b.state.Offset
		b.mu.Unlock()
		ups, err := b.API.GetUpdates(ctx, offset, pollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			b.mu.Lock()
			b.lastErr = err.Error()
			b.mu.Unlock()
			if errors.Is(err, ErrUnauthorized) {
				b.logf("telegram: %v", err)
				return err
			}
			b.logf("telegram: %v (again in %s)", err, backoff)
			select {
			case <-ctx.Done():
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = time.Second
		b.mu.Lock()
		b.lastErr = ""
		b.mu.Unlock()
		for _, u := range ups {
			if u.Message != nil {
				b.handle(ctx, u.Message)
			}
			b.mu.Lock()
			b.state.Offset = u.ID + 1
			b.mu.Unlock()
		}
		if len(ups) > 0 {
			b.mu.Lock()
			err := SaveState(b.StatePath, b.state)
			b.mu.Unlock()
			if err != nil {
				b.logf("telegram: %v", err)
			}
		}
	}
	b.mu.Lock()
	if b.timer != nil {
		b.timer.Stop()
	}
	b.mu.Unlock()
	return nil
}

// handle takes one message: pairing, a command, or part of the next batch.
// Anyone but the paired user is ignored without an answer.
func (b *Bridge) handle(ctx context.Context, m *Message) {
	if m.Chat.Type != "private" || m.From == nil || m.From.IsBot {
		return
	}
	cmd, arg := command(m)
	b.mu.Lock()
	st := b.state
	b.mu.Unlock()

	if st.UserID == 0 || m.From.ID != st.UserID {
		if cmd == "/start" && st.UserID == 0 && st.PairCode != "" && b.now().Before(st.PairUntil) &&
			strings.EqualFold(arg, st.PairCode) {
			b.Update(func(s *State) {
				s.UserID, s.UserName, s.PairCode, s.PairUntil = m.From.ID, m.From.Name(), "", time.Time{}
			})
			b.logf("telegram: paired with a Telegram account")
			b.reply(ctx, m, "Paired. Forward messages here and they go to "+b.targetText(st.Target)+".")
			return
		}
		b.mu.Lock()
		b.ignored++
		n := b.ignored
		b.mu.Unlock()
		if n == 1 || n%50 == 0 {
			b.logf("telegram: ignored %d message(s) from accounts that aren't paired", n)
		}
		return
	}
	switch cmd {
	case "/start", "/help":
		b.reply(ctx, m, "Send or forward messages, photos and files: after a few quiet seconds they go to "+
			b.targetText(st.Target)+" as one prompt.\n/to <agent id> picks the agent\n/status shows where things go")
		return
	case "/to":
		b.cmdTo(ctx, m, arg)
		return
	case "/status":
		b.cmdStatus(ctx, m)
		return
	}
	b.add(m)
}

func (b *Bridge) targetText(target string) string {
	if target == "" {
		return "no agent yet (send /to <agent id>)"
	}
	return target
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

func (b *Bridge) cmdTo(ctx context.Context, m *Message, id string) {
	if id == "" {
		b.reply(ctx, m, "Which agent? /to <agent id> (agentctl ls shows the ids)")
		return
	}
	info, ok := b.Agent(id)
	if !ok {
		b.reply(ctx, m, "There's no agent "+id+" on the deck.")
		return
	}
	if err := b.Update(func(s *State) { s.Target = id }); err != nil {
		b.reply(ctx, m, "Couldn't save that: "+err.Error())
		return
	}
	b.reply(ctx, m, "Messages now go to "+id+" ("+info.Title+", "+info.Status+").")
}

func (b *Bridge) cmdStatus(ctx context.Context, m *Message) {
	st, pending, _ := b.Status()
	if st.Target == "" {
		b.reply(ctx, m, "No agent set: /to <agent id>.")
		return
	}
	info, ok := b.Agent(st.Target)
	if !ok {
		b.reply(ctx, m, "Messages go to "+st.Target+", which isn't on the deck now: /to <agent id>.")
		return
	}
	s := fmt.Sprintf("Messages go to %s (%s): %s", st.Target, info.Title, info.Status)
	if info.Queued > 0 {
		s += fmt.Sprintf(", %d queued", info.Queued)
	}
	if pending > 0 {
		s += fmt.Sprintf(". %d message(s) waiting to go", pending)
	}
	b.reply(ctx, m, s+".")
}

func (b *Bridge) reply(ctx context.Context, m *Message, text string) {
	if err := b.API.SendMessage(ctx, m.Chat.ID, text); err != nil {
		b.logf("telegram: answering: %v", err)
	}
}

// add puts m in the pending batch and (re)sets its timer: the batch goes
// once no message came for the debounce time, at most maxWait after its
// first, or at once when it holds maxBatch messages.
func (b *Bridge) add(m *Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if len(b.pending) == 0 {
		b.first = now
	}
	b.pending = append(b.pending, m)
	wait := b.debounce()
	if left := maxWait - now.Sub(b.first); left < wait {
		wait = max(left, 0)
	}
	if len(b.pending) >= maxBatch {
		wait = 0
	}
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(wait, b.Flush)
}

// Flush sends the pending batch now.
func (b *Bridge) Flush() {
	b.flushMu.Lock()
	defer b.flushMu.Unlock()
	b.mu.Lock()
	batch, st, ctx := b.pending, b.state, b.ctx
	b.pending = nil
	b.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	chat := batch[0]
	if st.Target == "" {
		b.reply(ctx, chat, "Not sent: no agent set. Send /to <agent id>, then forward them again.")
		return
	}
	p := b.build(ctx, batch, st)
	res, err := b.Deliver(st.Target, p.text)
	if err != nil {
		b.logf("telegram: %d message(s) for %s: %v", len(batch), st.Target, err)
		b.reply(ctx, chat, "Not delivered to "+st.Target+": "+err.Error()+". The files are in the inbox: "+p.dir)
		return
	}
	how := "sent to " + st.Target
	if res.Queued {
		how = fmt.Sprintf("queued for %s (%s in line)", st.Target, ordinal(res.Position))
	}
	b.logf("telegram: %d message(s), %s", len(batch), how)
	b.reply(ctx, chat, capitalize(how)+": "+p.summary)
}

// prompt is one built batch.
type prompt struct {
	text    string
	summary string // "3 messages, 2 images, 1 file"
	dir     string
}

// build downloads the batch's files into its own inbox folder and writes
// the prompt. The whole text is also saved there as messages.md; when the
// prompt would be too long, the prompt clips the text and points to it.
func (b *Bridge) build(ctx context.Context, batch []*Message, st State) prompt {
	at := b.now()
	dir := filepath.Join(b.Inbox, at.Format("2006-01-02"), fmt.Sprintf("%s-%d", at.Format("150405"), batch[0].ID))
	var images, files int
	var parts []string
	dirMade := false
	for i, m := range batch {
		var lines []string
		head := stamp(m.Date)
		if from, when, ok := forwarded(m); ok {
			head = "forwarded from " + from
			if when != 0 {
				head += ", " + stamp(when)
			}
		}
		lines = append(lines, fmt.Sprintf("%d. %s:", i+1, head))
		if t := clean(m.Text + m.Caption); t != "" {
			lines = append(lines, t)
		}
		for _, f := range attachments(m) {
			if f.id == "" {
				lines = append(lines, "   ("+f.kind+": not supported yet)")
				continue
			}
			if !dirMade {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					lines = append(lines, "   ("+f.kind+" not saved: "+err.Error()+")")
					continue
				}
				dirMade = true
			}
			name := fmt.Sprintf("%02d-%s", i+1, f.name)
			dst := filepath.Join(dir, name)
			got, err := b.API.Download(ctx, f.id, dst)
			if err != nil {
				lines = append(lines, "   ("+f.kind+" not downloaded: "+err.Error()+")")
				continue
			}
			if filepath.Ext(name) == "" && path.Ext(got) != "" { // a photo: the extension Telegram gave it
				if err := os.Rename(dst, dst+path.Ext(got)); err == nil {
					dst += path.Ext(got)
				}
			}
			if f.kind == "image" {
				images++
			} else {
				files++
			}
			lines = append(lines, "   "+f.kind+": "+dst)
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	var extra string
	if images > 0 {
		extra += ", " + plural(images, "image")
	}
	if files > 0 {
		extra += ", " + plural(files, "file")
	}
	summary := plural(len(batch), "message") + extra
	first := "[Telegram] " + plural(len(batch), "message")
	if st.UserName != "" {
		first += " from " + clean(st.UserName)
	}
	first += extra
	body := strings.Join(parts, "\n\n")
	text := first + "\n\n" + body
	if !dirMade {
		if err := os.MkdirAll(dir, 0o700); err == nil {
			dirMade = true
		}
	}
	full := filepath.Join(dir, "messages.md")
	if dirMade {
		_ = os.WriteFile(full, []byte(text+"\n"), 0o600)
	}
	limit := b.Limit
	if limit <= 0 {
		limit = 4000
	}
	if len(text) > limit {
		note := "\n\nThe messages are long: this is the start. All of them, with every file, are in " + full + "\n\n"
		room := limit - len(first) - len(note) - 8
		text = first + note + clip(body, room)
	}
	return prompt{text: text, summary: summary, dir: dir}
}

// attachment is one file of a message to download.
type attachment struct {
	kind string // image or file, or what isn't supported
	id   string // "" when it isn't downloaded
	name string
}

func attachments(m *Message) []attachment {
	var out []attachment
	if len(m.Photo) > 0 {
		big := m.Photo[len(m.Photo)-1]
		for _, p := range m.Photo { // largest last, but don't count on it
			if p.Width*p.Height > big.Width*big.Height {
				big = p
			}
		}
		out = append(out, attachment{kind: "image", id: big.FileID, name: "photo"})
	}
	if d := m.Document; d != nil {
		kind := "file"
		if strings.HasPrefix(d.MimeType, "image/") {
			kind = "image"
		}
		out = append(out, attachment{kind: kind, id: d.FileID, name: safeName(d.FileName, "file")})
	}
	for _, x := range []struct {
		d    *Document
		kind string
	}{{m.Video, "a video"}, {m.Voice, "a voice message"}, {m.Audio, "an audio file"}} {
		if x.d != nil {
			out = append(out, attachment{kind: x.kind})
		}
	}
	if m.Sticker != nil {
		out = append(out, attachment{kind: "a sticker"})
	}
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

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// safeName is a file name with nothing but letters, digits, dots, dashes and
// underscores, never hidden, at most 80 bytes.
func safeName(name, fallback string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	base = strings.Trim(unsafeName.ReplaceAllString(base, "_"), "._")
	ext = unsafeName.ReplaceAllString(ext, "")
	if ext == "." {
		ext = ""
	}
	if base == "" {
		base = fallback
	}
	if len(base) > 70 {
		base = base[:70]
	}
	if len(ext) > 10 {
		ext = ""
	}
	return base + ext
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

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
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
