package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// TokenName is a bridge's bot token's name in the keystore.
func TokenName(id string) string { return "telegram-bot-token-" + id }

// Keystore keeps the bot tokens (pki.DefaultKeystore: the macOS Keychain, or
// 0600 files elsewhere).
type Keystore interface {
	Load(name string) ([]byte, error)
	Save(name string, secret []byte) error
	Delete(name string) error
}

var validID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ValidID: a bridge id is lowercase letters, digits and dashes, at most 32.
func ValidID(id string) error {
	if !validID.MatchString(id) {
		return fmt.Errorf("bridge id %q: use lowercase letters, digits and dashes (at most 32)", id)
	}
	return nil
}

// Service runs every Telegram bridge inside agentctl serve, one poller each,
// and answers agentctl telegram on the unix socket (/local/telegram). The
// bridges are in telegram.json; each one's token is in the keystore. A token
// only ever arrives with setup; it is never logged or sent back.
type Service struct {
	Keys    Keystore
	Path    string // telegram.json
	Dir     string // <data>/telegram: each bridge's inbox is <Dir>/<id>/inbox
	Base    string // DefaultBase, or a test server
	HTTP    *http.Client
	Deliver func(agent, text string) (Result, error)
	Agent   func(id string) (AgentInfo, bool)
	Limit   int
	Logf    func(format string, args ...any)

	mu     sync.Mutex // runs and parent
	parent context.Context
	runs   map[string]*run

	fmu  sync.Mutex // telegram.json and cfgs; never held while calling into a bridge
	cfgs []Config
}

// run is one bridge's poller.
type run struct {
	bridge *Bridge
	cancel context.CancelFunc
	done   chan struct{} // closed when its poller has returned
	bot    string        // @username, once the token is checked
	botID  int64
	err    string
}

type file struct {
	Bridges []Config `json:"bridges"`
}

func (s *Service) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

func (s *Service) inbox(id string) string { return filepath.Join(s.Dir, id, "inbox") }

// load reads telegram.json; a missing file is no bridges.
func (s *Service) load() error {
	s.fmu.Lock()
	defer s.fmu.Unlock()
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		s.cfgs = nil
		return nil
	}
	if err != nil {
		return err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(s.Path), err)
	}
	s.cfgs = f.Bridges
	return nil
}

// saveConfig puts c in the list (by id) and writes the file, 0600.
func (s *Service) saveConfig(c Config) error {
	s.fmu.Lock()
	defer s.fmu.Unlock()
	found := false
	for i := range s.cfgs {
		if s.cfgs[i].ID == c.ID {
			s.cfgs[i], found = c, true
		}
	}
	if !found {
		s.cfgs = append(s.cfgs, c)
	}
	sort.Slice(s.cfgs, func(i, j int) bool { return s.cfgs[i].ID < s.cfgs[j].ID })
	data, err := json.MarshalIndent(file{Bridges: s.cfgs}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

func (s *Service) config(id string) (Config, bool) {
	s.fmu.Lock()
	defer s.fmu.Unlock()
	for _, c := range s.cfgs {
		if c.ID == id {
			return c, true
		}
	}
	return Config{}, false
}

func (s *Service) ids() []string {
	s.fmu.Lock()
	defer s.fmu.Unlock()
	var out []string
	for _, c := range s.cfgs {
		out = append(out, c.ID)
	}
	return out
}

// Start runs every bridge that has a token, until ctx ends.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	s.parent = ctx
	s.mu.Unlock()
	err := s.load()
	for _, id := range s.ids() {
		s.restart(id)
	}
	return err
}

func (s *Service) runOf(id string) *run {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[id]
}

// stop ends one bridge's poller, if it runs, and waits until it has
// returned (so it can't save its config over a change made next).
func (s *Service) stop(id string) {
	s.mu.Lock()
	r := s.runs[id]
	delete(s.runs, id)
	s.mu.Unlock()
	if r != nil {
		r.cancel()
		select {
		case <-r.done:
		case <-time.After(10 * time.Second):
		}
	}
}

// restart stops one bridge and starts it again with its stored token, if
// it has one. Other bridges are left alone.
func (s *Service) restart(id string) {
	s.stop(id)
	cfg, ok := s.config(id)
	if !ok {
		return
	}
	tok, err := s.Keys.Load(TokenName(id))
	if err != nil || len(tok) == 0 {
		return
	}
	b := NewBridge(cfg)
	b.API = &API{Base: s.Base, Token: strings.TrimSpace(string(tok)), HTTP: s.HTTP}
	b.Inbox, b.Deliver, b.Agent, b.Limit, b.Logf = s.inbox(id), s.Deliver, s.Agent, s.Limit, s.Logf
	b.Save = s.saveConfig
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parent == nil {
		return
	}
	if s.runs == nil {
		s.runs = map[string]*run{}
	}
	ctx, cancel := context.WithCancel(s.parent)
	r := &run{bridge: b, cancel: cancel, done: make(chan struct{})}
	s.runs[id] = r
	go func() {
		defer close(r.done)
		s.poll(ctx, id, r)
	}()
}

// poll checks the token (waiting for the network if needed) and that no
// other bridge uses the same bot, then runs the bridge.
func (s *Service) poll(ctx context.Context, id string, r *run) {
	wait := time.Second
	for ctx.Err() == nil {
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		me, err := r.bridge.API.GetMe(c)
		cancel()
		if err == nil {
			if other := s.botTaken(id, me.ID); other != "" {
				s.setErr(r, fmt.Sprintf("bridge %s already uses @%s: one bot can't serve two bridges", other, me.Username))
				s.logf("telegram %s: @%s is already bridge %s's bot; not started", id, me.Username, other)
				return
			}
			s.mu.Lock()
			r.bot, r.botID, r.err = "@"+me.Username, me.ID, ""
			s.mu.Unlock()
			s.logf("telegram %s: listening as @%s", id, me.Username)
			break
		}
		s.setErr(r, err.Error())
		if errors.Is(err, ErrUnauthorized) {
			s.logf("telegram %s: %v", id, err)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, time.Minute)
	}
	if err := r.bridge.Run(ctx); err != nil {
		s.setErr(r, err.Error())
	}
}

// botTaken is the id of another running bridge with the same bot, or "".
func (s *Service) botTaken(id string, bot int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for other, r := range s.runs {
		if other != id && r.botID == bot && bot != 0 {
			return other
		}
	}
	return ""
}

func (s *Service) setErr(r *run, msg string) {
	s.mu.Lock()
	r.err = msg
	s.mu.Unlock()
}

// BridgeStatus is one bridge in GET /local/telegram.
type BridgeStatus struct {
	ID        string `json:"id"`
	On        bool   `json:"on"` // a token is stored and its poller runs
	Bot       string `json:"bot,omitempty"`
	UserID    int64  `json:"userId,omitempty"`
	UserName  string `json:"userName,omitempty"`
	Agent     string `json:"agent,omitempty"`
	Pending   int    `json:"pending,omitempty"`
	Held      int    `json:"held,omitempty"`
	Waiting   int    `json:"waiting,omitempty"`
	Error     string `json:"error,omitempty"`
	Inbox     string `json:"inbox"`
	PairCode  string `json:"pairCode,omitempty"`  // only in the answer to setup or pair
	Delivered int    `json:"delivered,omitempty"` // only in the answer to link: held batches that went
}

func (s *Service) status(id string) BridgeStatus {
	cfg, _ := s.config(id)
	out := BridgeStatus{ID: id, UserID: cfg.UserID, UserName: cfg.UserName, Agent: cfg.Agent, Inbox: s.inbox(id)}
	s.mu.Lock()
	r := s.runs[id]
	if r != nil {
		out.On, out.Bot, out.Error = true, r.bot, r.err
	}
	s.mu.Unlock()
	if r != nil {
		st := r.bridge.Status()
		out.UserID, out.UserName, out.Agent = st.UserID, st.UserName, st.Agent
		out.Pending, out.Held, out.Waiting = st.Pending, st.Held, st.Waiting
		if st.Error != "" {
			out.Error = st.Error
		}
	} else if all, err := loadBatches(s.inbox(id)); err == nil {
		for _, bt := range all {
			if bt.State == stateHeld {
				out.Held++
			} else if bt.State != stateOpen {
				out.Waiting++
			}
		}
	}
	return out
}

// Request is POST /local/telegram.
type Request struct {
	Op     string `json:"op"`               // setup, pair, link, user, off
	Bridge string `json:"bridge,omitempty"` // "" = the only bridge ("default" for the first setup)
	Token  string `json:"token,omitempty"`  // setup
	Agent  string `json:"agent,omitempty"`  // link
	UserID int64  `json:"userId,omitempty"` // user
}

// pick resolves a request's bridge id.
func (s *Service) pick(id, op string) (string, error) {
	ids := s.ids()
	if id != "" {
		if err := ValidID(id); err != nil {
			return "", err
		}
		for _, x := range ids {
			if x == id {
				return id, nil
			}
		}
		if op == "setup" {
			return id, nil
		}
		return "", fmt.Errorf("there's no bridge %q (agentctl telegram lists them)", id)
	}
	switch len(ids) {
	case 0:
		if op == "setup" {
			return "default", nil
		}
		return "", errors.New("no Telegram bridge yet: agentctl telegram setup")
	case 1:
		return ids[0], nil
	}
	return "", fmt.Errorf("there are %d bridges (%s): say which with --bridge <id>", len(ids), strings.Join(ids, ", "))
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		var out []BridgeStatus
		for _, id := range s.ids() {
			if q := r.URL.Query().Get("bridge"); q == "" || q == id {
				out = append(out, s.status(id))
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"bridges": out})
		return
	}
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "GET or POST")
		return
	}
	var req Request
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, `body must be {"op": "setup"|"pair"|"link"|"user"|"off", "bridge": …}`)
		return
	}
	id, err := s.pick(req.Bridge, req.Op)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	code, msg, extra := s.do(r.Context(), id, req)
	if code != http.StatusOK {
		writeErr(w, code, msg)
		return
	}
	out := s.status(id)
	out.PairCode, out.Delivered = extra.code, extra.delivered
	writeJSON(w, http.StatusOK, out)
}

type extra struct {
	code      string
	delivered int
}

// update changes a bridge's config, through its running bridge if any.
func (s *Service) update(id string, f func(*Config)) error {
	if r := s.runOf(id); r != nil {
		return r.bridge.Update(f)
	}
	c, ok := s.config(id)
	if !ok {
		c = Config{ID: id}
	}
	f(&c)
	return s.saveConfig(c)
}

func (s *Service) do(ctx context.Context, id string, req Request) (int, string, extra) {
	var x extra
	switch req.Op {
	case "setup":
		tok := strings.TrimSpace(req.Token)
		if tok == "" {
			return http.StatusBadRequest, "no token", x
		}
		c, cancel := context.WithTimeout(ctx, 20*time.Second)
		me, err := (&API{Base: s.Base, Token: tok, HTTP: s.HTTP}).GetMe(c)
		cancel()
		if err != nil {
			return http.StatusBadRequest, err.Error(), x
		}
		for _, other := range s.ids() {
			if other == id {
				continue
			}
			if t, err := s.Keys.Load(TokenName(other)); err == nil && strings.TrimSpace(string(t)) == tok {
				return http.StatusConflict, "bridge " + other + " already uses this bot: one bot can't serve two bridges (make another with @BotFather)", x
			}
		}
		if other := s.botTaken(id, me.ID); other != "" {
			return http.StatusConflict, "bridge " + other + " already uses @" + me.Username + ": one bot can't serve two bridges", x
		}
		if err := s.Keys.Save(TokenName(id), []byte(tok)); err != nil {
			return http.StatusInternalServerError, "saving the token: " + err.Error(), x
		}
		s.logf("telegram %s: bot token saved (@%s)", id, me.Username)
		fallthrough
	case "pair":
		if t, err := s.Keys.Load(TokenName(id)); err != nil || len(t) == 0 {
			return http.StatusConflict, "bridge " + id + " has no bot token yet: agentctl telegram setup", x
		}
		x.code = NewPairCode()
		s.stop(id)
		if err := s.update(id, func(c *Config) {
			c.UserID, c.UserName, c.PairCode, c.PairUntil = 0, "", x.code, time.Now().Add(PairTTL)
		}); err != nil {
			return http.StatusInternalServerError, err.Error(), x
		}
		s.restart(id)
		s.waitBot(ctx, id)
	case "link":
		if req.Agent == "" {
			return http.StatusBadRequest, "which agent? agentctl telegram link <agent id>", x
		}
		if s.Agent != nil {
			info, ok := s.Agent(req.Agent)
			if !ok {
				return http.StatusNotFound, "there's no agent " + req.Agent + " on the deck", x
			}
			if info.Ended {
				return http.StatusConflict, req.Agent + " has ended: link an agent that runs", x
			}
		}
		if r := s.runOf(id); r != nil {
			n, err := r.bridge.Link(req.Agent)
			if err != nil {
				return http.StatusInternalServerError, err.Error(), x
			}
			x.delivered = n
		} else if err := s.update(id, func(c *Config) { c.Agent = req.Agent }); err != nil {
			return http.StatusInternalServerError, err.Error(), x
		}
		s.logf("telegram %s: linked to %s", id, req.Agent)
	case "user":
		if req.UserID <= 0 {
			return http.StatusBadRequest, "a Telegram user id is a positive number", x
		}
		if err := s.update(id, func(c *Config) {
			c.UserID, c.UserName, c.PairCode, c.PairUntil = req.UserID, "", "", time.Time{}
		}); err != nil {
			return http.StatusInternalServerError, err.Error(), x
		}
	case "off":
		s.stop(id)
		if err := s.Keys.Delete(TokenName(id)); err != nil {
			return http.StatusInternalServerError, err.Error(), x
		}
		s.logf("telegram %s: bot token removed, bridge off (its inbox and settings stay)", id)
	default:
		return http.StatusBadRequest, `"op" is setup, pair, link, user or off`, x
	}
	return http.StatusOK, "", x
}

// waitBot gives a restarted bridge a moment to learn its bot's name.
func (s *Service) waitBot(ctx context.Context, id string) {
	for range 40 {
		s.mu.Lock()
		r := s.runs[id]
		done := r == nil || r.bot != "" || r.err != ""
		s.mu.Unlock()
		if done {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
