package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testToken  = "123456:TEST-token-made-up"
	testToken2 = "654321:TEST-second-made-up"
)

// bots: the fake API's tokens and the bot each belongs to.
var bots = map[string]User{
	testToken:  {ID: 1, IsBot: true, FirstName: "Deck", Username: "test_deck_bot"},
	testToken2: {ID: 2, IsBot: true, FirstName: "Other", Username: "test_other_bot"},
}

func init() { retryWait = time.Millisecond }

// fakeAPI is a Bot API stand-in: it hands out queued updates, records what
// the bot sends, and serves files. A file id can be made to fail a number of
// times, or to be too big.
type fakeAPI struct {
	mu      sync.Mutex
	updates map[string][]Update // by token
	sent    []string
	sentBy  map[string][]string // by token
	fails   map[string]int      // file id: getFile failures left
	tooBig  map[string]bool
	srv     *httptest.Server
}

func newFake(t *testing.T) *fakeAPI {
	f := &fakeAPI{fails: map[string]int{}, tooBig: map[string]bool{}, updates: map[string][]Update{}, sentBy: map[string][]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	var token, method string
	for tok := range bots {
		if p, ok := strings.CutPrefix(r.URL.Path, "/file/bot"+tok+"/"); ok {
			w.Write([]byte("made-up bytes of " + p))
			return
		}
		if m, ok := strings.CutPrefix(r.URL.Path, "/bot"+tok+"/"); ok {
			token, method = tok, m
		}
	}
	if token == "" {
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 401, "description": "Unauthorized"})
		return
	}
	r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	var result any = true
	switch method {
	case "getMe":
		result = bots[token]
	case "getUpdates": // like Telegram: everything from the offset on, until confirmed
		off, _ := strconv.ParseInt(r.Form.Get("offset"), 10, 64)
		ups := []Update{}
		for _, u := range f.updates[token] {
			if u.ID >= off {
				ups = append(ups, u)
			}
		}
		result = ups
	case "sendMessage":
		f.sent = append(f.sent, r.Form.Get("text"))
		f.sentBy[token] = append(f.sentBy[token], r.Form.Get("text"))
	case "getFile":
		id := r.Form.Get("file_id")
		if f.fails[id] > 0 {
			f.fails[id]--
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 500, "description": "Internal Server Error"})
			return
		}
		if f.tooBig[id] {
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 400, "description": "Bad Request: file is too big"})
			return
		}
		p := "documents/" + id
		if strings.HasPrefix(id, "photo") || strings.HasPrefix(id, "thumb") {
			p = "photos/" + id + ".jpg"
		}
		result = map[string]any{"file_id": id, "file_path": p, "file_size": 100}
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

func (f *fakeAPI) push(ups ...Update) { f.pushTo(testToken, ups...) }

func (f *fakeAPI) pushTo(token string, ups ...Update) {
	f.mu.Lock()
	f.updates[token] = append(f.updates[token], ups...)
	f.mu.Unlock()
}

func (f *fakeAPI) replies() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

type delivered struct{ agent, text string }

// harness is a bridge on a fake API with a fake deck: agents that exist,
// agents that ended, and every prompt it delivered.
type harness struct {
	*Bridge
	f      *fakeAPI
	mu     sync.Mutex
	got    []delivered
	saved  Config
	agents map[string]AgentInfo
	inbox  string
}

func newHarness(t *testing.T, f *fakeAPI, cfg Config) *harness {
	h := &harness{f: f, inbox: filepath.Join(t.TempDir(), "inbox"),
		agents: map[string]AgentInfo{
			"q601r3": {Title: "Sorting desk", Status: "running", Queued: 1},
			"k3f9a2": {Title: "Other desk", Status: "idle"},
		}}
	h.Bridge = h.bridge(t, cfg)
	return h
}

func (h *harness) bridge(t *testing.T, cfg Config) *Bridge {
	b := NewBridge(cfg)
	b.API = &API{Base: h.f.srv.URL, Token: testToken}
	b.Inbox = h.inbox
	b.Logf = t.Logf
	b.Save = func(c Config) error { h.mu.Lock(); h.saved = c; h.mu.Unlock(); return nil }
	b.Deliver = func(agent, text string) (Result, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.got = append(h.got, delivered{agent, text})
		return Result{Queued: true, Position: 2}, nil
	}
	b.Agent = func(id string) (AgentInfo, bool) {
		h.mu.Lock()
		defer h.mu.Unlock()
		a, ok := h.agents[id]
		return a, ok
	}
	if err := b.Load(); err != nil {
		t.Fatal(err)
	}
	return b
}

func (h *harness) prompts() []delivered {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]delivered(nil), h.got...)
}

func from(id int64) *User { return &User{ID: id, FirstName: "Test", LastName: "User"} }

func msg(id int64, user int64, text string) *Message {
	return &Message{ID: id, From: from(user), Chat: Chat{ID: user, Type: "private"}, Date: 1795000000, Text: text}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestPairing(t *testing.T) {
	f := newFake(t)
	h := newHarness(t, f, Config{ID: "default", PairCode: "ABCD2345", PairUntil: time.Now().Add(time.Minute), Agent: "q601r3"})
	ctx := context.Background()

	must(t, h.handle(ctx, msg(1, 42, "/start WRONG000")))
	must(t, h.handle(ctx, msg(2, 42, "hello there")))
	if r := f.replies(); len(r) != 0 {
		t.Fatalf("an unpaired account got answers: %q", r)
	}
	must(t, h.handle(ctx, msg(3, 42, "/start abcd2345"))) // any case
	if c := h.Config(); c.UserID != 42 || c.UserName != "Test User" || c.PairCode != "" {
		t.Fatalf("not paired: %+v", c)
	}
	if h.saved.UserID != 42 || h.saved.Agent != "q601r3" {
		t.Fatalf("saved %+v", h.saved)
	}
	if r := f.replies(); len(r) != 1 || !strings.Contains(r[0], "Paired") || !strings.Contains(r[0], "q601r3") {
		t.Fatalf("pair answer %q", r)
	}
	// Someone else, even with the old code, and a group: silence, nothing kept.
	must(t, h.handle(ctx, msg(4, 77, "/start ABCD2345")))
	must(t, h.handle(ctx, msg(5, 77, "a message from a stranger")))
	g := msg(6, 42, "in a group")
	g.Chat = Chat{ID: -100, Type: "group"}
	must(t, h.handle(ctx, g))
	h.CloseNow()
	if len(f.replies()) != 1 || len(h.prompts()) != 0 {
		t.Fatalf("a stranger or a group got through: %q %v", f.replies(), h.prompts())
	}
}

func TestExpiredPairCode(t *testing.T) {
	f := newFake(t)
	h := newHarness(t, f, Config{ID: "default", PairCode: "ABCD2345", PairUntil: time.Now().Add(-time.Second)})
	must(t, h.handle(context.Background(), msg(1, 42, "/start ABCD2345")))
	if h.Config().UserID != 0 {
		t.Fatal("paired with an expired code")
	}
}

// An album, forwards and every media type become one prompt: every file in
// message order next to its text, original names kept and made unique.
func TestBatchEveryMediaType(t *testing.T) {
	f := newFake(t)
	h := newHarness(t, f, Config{ID: "default", UserID: 42, UserName: "Test User", Agent: "q601r3"})
	ctx := context.Background()
	origin := &Origin{Type: "user", Date: 1794990000, SenderUser: &User{ID: 9, FirstName: "Sam", LastName: "Example"}}

	var ms []*Message
	add := func(m *Message) { ms = append(ms, m) }
	fwd := msg(10, 42, "Made-up feedback: the header feels heavy, try a lighter weight.")
	fwd.Origin = origin
	add(fwd)
	for i, id := range []string{"photoA", "photoB"} { // an album, forwarded
		m := msg(int64(11+i), 42, "")
		m.MediaGroupID, m.Origin = "album1", origin
		m.Photo = []PhotoSize{{FileID: id + "-small", Width: 90, Height: 90}, {FileID: id, Width: 1280, Height: 960}}
		if i == 0 {
			m.Caption = "Made-up caption: two options for the empty state."
		}
		add(m)
	}
	d1 := msg(13, 42, "")
	d1.Document = &Document{FileID: "doc1", FileName: "../../spec v2 (final).pdf", MimeType: "application/pdf"}
	add(d1)
	d2 := msg(14, 42, "")
	d2.Document = &Document{FileID: "doc2", FileName: "spec v2 (final).pdf"} // same name: made unique
	add(d2)
	v := msg(15, 42, "")
	v.Video = &Document{FileID: "vid1", FileName: "walkthrough.mp4"}
	add(v)
	vo := msg(16, 42, "")
	vo.Voice = &Document{FileID: "voice1"}
	add(vo)
	au := msg(17, 42, "")
	au.Audio = &Document{FileID: "aud1", FileName: "notes.m4a"}
	add(au)
	vn := msg(18, 42, "")
	vn.VideoNote = &Document{FileID: "vnote1"}
	add(vn)
	an := msg(19, 42, "")
	an.Animation = &Document{FileID: "anim1", FileName: "loop.mp4"}
	an.Document = &Document{FileID: "anim1", FileName: "loop.mp4"} // Telegram sends both
	add(an)
	st := msg(20, 42, "")
	st.Sticker = &Sticker{FileID: "stk1"}
	add(st)
	ast := msg(21, 42, "")
	ast.Sticker = &Sticker{FileID: "stk2", IsAnimated: true, Thumbnail: &PhotoSize{FileID: "thumb2"}}
	add(ast)
	for _, m := range ms {
		must(t, h.handle(ctx, m))
	}
	if s := h.Status(); s.Pending != len(ms) {
		t.Fatalf("pending %d, want %d (one batch)", s.Pending, len(ms))
	}
	h.CloseNow()

	got := h.prompts()
	if len(got) != 1 || got[0].agent != "q601r3" {
		t.Fatalf("want one prompt for q601r3, got %v", got)
	}
	p := got[0].text
	first, _, _ := strings.Cut(p, "\n")
	if first != "[Telegram] 12 messages from Test User, 4 images, 8 files" {
		t.Errorf("first line %q", first)
	}
	if !strings.Contains(p, "forwarded from Sam Example, "+stamp(1794990000)) || !strings.Contains(p, "Made-up caption: two options") {
		t.Errorf("forward or caption missing:\n%s", p)
	}
	// Files in message order, right after their message's text.
	var paths []string
	for _, l := range strings.Split(p, "\n") {
		if _, rest, ok := strings.Cut(strings.TrimSpace(l), ": /"); ok {
			paths = append(paths, "/"+rest)
		}
	}
	wantEnds := []string{"02-photo.jpg", "03-photo.jpg", "04-spec_v2_final.pdf", "05-spec_v2_final.pdf",
		"06-walkthrough.mp4", "07-voice", "08-notes.m4a", "09-video_note", "10-loop.mp4", "11-sticker.webp",
		"12-sticker.tgs", "12-sticker_thumbnail.jpg"}
	if len(paths) != len(wantEnds) {
		t.Fatalf("paths %q", paths)
	}
	for i, p := range paths {
		if !strings.HasSuffix(p, wantEnds[i]) || !strings.HasPrefix(p, h.inbox) {
			t.Errorf("file %d: %q, want …%s in the inbox", i, p, wantEnds[i])
		}
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v", p, err)
		}
	}
	caption := strings.Index(p, "Made-up caption")
	if caption < 0 || caption > strings.Index(p, "02-photo.jpg") || strings.Index(p, "02-photo.jpg") > strings.Index(p, "03-photo.jpg") {
		t.Error("files not next to their text, in order")
	}
	if r := f.replies(); len(r) != 1 || r[0] != "Queued for q601r3 (2nd in line): 12 messages, 4 images, 8 files" {
		t.Errorf("answer %q", r)
	}
}

// A file over 20 MB isn't tried; one that keeps failing is listed as failed;
// one that fails once is fetched on the retry. Nothing is left out.
func TestTooBigAndFailedDownloads(t *testing.T) {
	f := newFake(t)
	f.fails["flaky"] = 2   // fails twice, the third try works
	f.fails["broken"] = 99 // never works
	f.tooBig["bigger"] = true
	h := newHarness(t, f, Config{ID: "default", UserID: 42, Agent: "q601r3"})
	ctx := context.Background()
	m1 := msg(1, 42, "made-up clip")
	m1.Video = &Document{FileID: "huge", FileName: "screen recording.mov", FileSize: 31 << 20}
	m2 := msg(2, 42, "")
	m2.Document = &Document{FileID: "flaky", FileName: "flaky.png", MimeType: "image/png"}
	m3 := msg(3, 42, "")
	m3.Document = &Document{FileID: "broken", FileName: "broken.zip"}
	m4 := msg(4, 42, "")
	m4.Document = &Document{FileID: "bigger", FileName: "no-size.bin"} // size unknown; getFile says too big
	for _, m := range []*Message{m1, m2, m3, m4} {
		must(t, h.handle(ctx, m))
	}
	h.CloseNow()
	p := h.prompts()[0].text
	for _, want := range []string{
		"video too big for the Bot API (31 MB): screen_recording.mov, open it in Telegram",
		"image: " + h.inbox, // flaky.png made it
		"file failed to download (",
		"broken.zip, open it in Telegram",
		"file too big for the Bot API (0 MB): no-size.bin",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	r := f.replies()
	if len(r) != 1 || !strings.Contains(r[0], "too big for the Bot API (31 MB): screen_recording.mov") ||
		!strings.Contains(r[0], "failed to download: broken.zip") {
		t.Errorf("answer %q", r)
	}
}

// The linked agent ends: batches are held in the inbox, in order, until a
// re-link delivers them.
func TestHeldUntilRelink(t *testing.T) {
	f := newFake(t)
	h := newHarness(t, f, Config{ID: "default", UserID: 42, Agent: "q601r3"})
	ctx := context.Background()
	h.mu.Lock()
	h.agents["q601r3"] = AgentInfo{Title: "Sorting desk", Status: "exited", Ended: true}
	h.mu.Unlock()

	must(t, h.handle(ctx, msg(1, 42, "made-up first batch")))
	h.CloseNow()
	must(t, h.handle(ctx, msg(2, 42, "made-up second batch")))
	h.CloseNow()
	if n := len(h.prompts()); n != 0 {
		t.Fatalf("delivered %d to an ended agent", n)
	}
	if s := h.Status(); s.Held != 1 || s.Waiting != 1 {
		t.Fatalf("held %d waiting %d", s.Held, s.Waiting) // the second waits behind the held first
	}
	if r := f.replies(); len(r) != 1 || !strings.HasPrefix(r[0], "Held: q601r3 ended, re-link to deliver") {
		t.Fatalf("answers %q", r)
	}
	// /status says so, read only.
	must(t, h.handle(ctx, msg(3, 42, "/status")))
	if r := f.replies(); !strings.Contains(r[1], "q601r3, which has ended") || !strings.Contains(r[1], "1 batch held") {
		t.Fatalf("status %q", r[1])
	}
	// A restart in between keeps them.
	b2 := h.bridge(t, h.Config())
	if s := b2.Status(); s.Held+s.Waiting != 2 {
		t.Fatalf("after a restart: %+v", s)
	}
	h.Bridge = b2
	n, err := h.Link("k3f9a2")
	if err != nil || n != 2 {
		t.Fatalf("link delivered %d: %v", n, err)
	}
	got := h.prompts()
	if len(got) != 2 || got[0].agent != "k3f9a2" || !strings.Contains(got[0].text, "first batch") || !strings.Contains(got[1].text, "second batch") {
		t.Fatalf("delivered %v", got)
	}
	r := f.replies()
	if !strings.Contains(r[len(r)-2], "held until now") {
		t.Errorf("answer %q", r[len(r)-2])
	}
	if s := h.Status(); s.Held != 0 || s.Waiting != 0 {
		t.Errorf("left %+v", s)
	}
}

// A delivery that fails for another reason stays and is tried again.
func TestDeliveryRetried(t *testing.T) {
	old := retryDeliver
	retryDeliver = 20 * time.Millisecond
	defer func() { retryDeliver = old }()
	f := newFake(t)
	h := newHarness(t, f, Config{ID: "default", UserID: 42, Agent: "q601r3"})
	var mu sync.Mutex
	fails := 1
	h.Deliver = func(agent, text string) (Result, error) {
		mu.Lock()
		defer mu.Unlock()
		if fails > 0 {
			fails--
			return Result{}, errors.New("the queue for this agent is full")
		}
		h.mu.Lock()
		h.got = append(h.got, delivered{agent, text})
		h.mu.Unlock()
		return Result{}, nil
	}
	must(t, h.handle(context.Background(), msg(1, 42, "made-up note")))
	h.CloseNow()
	waitFor(t, func() bool { return len(h.prompts()) == 1 && len(f.replies()) == 2 })
	r := f.replies()
	if len(r) != 2 || !strings.HasPrefix(r[0], "Not delivered to q601r3 yet (the queue for this agent is full)") || !strings.HasPrefix(r[1], "Sent to q601r3") {
		t.Fatalf("answers %q", r)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for range 200 {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out")
}

// Run reads updates; the offset moves only past what's on disk. A crash with
// an open batch resumes it, and an update read again isn't kept twice.
func TestRunCrashAndResume(t *testing.T) {
	old := DefaultDebounce
	DefaultDebounce = 100 * time.Millisecond
	defer func() { DefaultDebounce = old }()
	f := newFake(t)
	h := newHarness(t, f, Config{ID: "default", UserID: 42, Agent: "q601r3"})
	f.push(Update{ID: 100, Message: msg(1, 42, "made-up one")}, Update{ID: 101, Message: msg(2, 42, "made-up two")})

	// A "crash" while the batch is open: Run stops before the debounce ends.
	DefaultDebounce = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.Run(ctx); close(done) }()
	waitFor(t, func() bool { return h.Status().Pending == 2 })
	cancel()
	<-done
	h.mu.Lock()
	offset := h.saved.Offset
	h.mu.Unlock()
	if offset != 102 {
		t.Fatalf("offset %d", offset)
	}
	// The offset didn't reach the disk before the crash: Telegram hands out
	// 100 and 101 again, plus a new 102.
	f.push(Update{ID: 102, Message: msg(3, 42, "made-up three")})
	cfg := h.saved
	cfg.Offset = 100
	h.Bridge = h.bridge(t, cfg)
	if s := h.Status(); s.Pending != 2 {
		t.Fatalf("the open batch wasn't resumed: %+v", s)
	}
	ctx, cancel = context.WithCancel(context.Background())
	done = make(chan struct{})
	go func() { h.Run(ctx); close(done) }()
	waitFor(t, func() bool { return h.Status().Pending == 3 })
	h.CloseNow()
	cancel()
	<-done
	p := h.prompts()[0].text
	if !strings.HasPrefix(p, "[Telegram] 3 messages\n") || strings.Count(p, "made-up two") != 1 {
		t.Fatalf("resumed batch:\n%s", p)
	}
}

// A message that can't be saved isn't confirmed to Telegram: the offset stays.
func TestOffsetWaitsForDisk(t *testing.T) {
	f := newFake(t)
	h := newHarness(t, f, Config{ID: "default", UserID: 42, Agent: "q601r3"})
	must(t, os.MkdirAll(filepath.Dir(h.inbox), 0o700))
	must(t, os.WriteFile(h.inbox, []byte("not a folder"), 0o600)) // the inbox can't be made
	f.push(Update{ID: 7, Message: msg(1, 42, "made-up note")})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.Run(ctx); close(done) }()
	waitFor(t, func() bool { return strings.HasPrefix(h.Status().Error, "saving a message") })
	cancel()
	<-done
	if h.Config().Offset != 0 {
		t.Fatalf("offset moved to %d past an unsaved message", h.Config().Offset)
	}
}

func TestLongBatchIsClipped(t *testing.T) {
	f := newFake(t)
	h := newHarness(t, f, Config{ID: "default", UserID: 42, Agent: "q601r3"})
	h.Limit = 4000
	for i := range 6 {
		must(t, h.handle(context.Background(), msg(int64(i), 42, strings.Repeat("made-up words ", 80))))
	}
	h.CloseNow()
	p := h.prompts()[0].text
	if len(p) > 4000 || !strings.Contains(p, "messages.md") {
		t.Fatalf("prompt is %d bytes", len(p))
	}
	full := p[strings.Index(p, "are in ")+len("are in "):]
	full, _, _ = strings.Cut(full, "\n")
	b, err := os.ReadFile(full)
	if err != nil || strings.Count(string(b), "made-up words") != 6*80 {
		t.Fatalf("full text: %v", err)
	}
}

func TestNotLinked(t *testing.T) {
	f := newFake(t)
	h := newHarness(t, f, Config{ID: "default", UserID: 42})
	must(t, h.handle(context.Background(), msg(1, 42, "made-up note")))
	h.CloseNow()
	if len(h.prompts()) != 0 || !strings.HasPrefix(f.replies()[0], "Held: no agent linked") {
		t.Fatalf("%v %q", h.prompts(), f.replies())
	}
	// A forwarded "/to" or "/status" is content, not a command.
	fw := msg(2, 42, "/status")
	fw.Origin = &Origin{Type: "hidden_user", SenderUserName: "A. Nonymous", Date: 1}
	must(t, h.handle(context.Background(), fw))
	if h.Status().Pending != 1 {
		t.Fatal("a forwarded command wasn't kept as content")
	}
}

func TestErrorsNeverShowTheToken(t *testing.T) {
	a := &API{Base: "http://127.0.0.1:1", Token: testToken}
	_, err := a.GetMe(context.Background())
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatalf("error %v", err)
	}
	f := newFake(t)
	if _, err := (&API{Base: f.srv.URL, Token: "999:wrong"}).GetMe(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong token: %v", err)
	}
}

func TestSafeNamesAndClip(t *testing.T) {
	for in, want := range map[string]string{
		"../../etc/passwd": "passwd",
		`..\..\x.exe`:      "x.exe",
		".hidden":          "file.hidden",
		"brief v2.PDF":     "brief_v2.PDF",
		"":                 "file",
		"résumé.docx":      "r_sum.docx",
	} {
		if got := safeName(in, "file"); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
	n := names{}
	if a, b, c := n.take("a.png"), n.take("A.png"), n.take("a.png"); a != "a.png" || b != "A-2.png" || c != "a-3.png" {
		t.Errorf("unique names %q %q %q", a, b, c)
	}
	if c := clip("ééééé", 8); c != "éé…" {
		t.Errorf("clip %q", c)
	}
}

// The stand-in Bot API is only ever on this computer.
func TestStandIn(t *testing.T) {
	for v, want := range map[string]bool{
		"": true, "http://127.0.0.1:17399": true, "http://localhost:8081/": true, "https://[::1]:9000": true, "http://127.0.0.2:80": true,
		"https://api.telegram.org": false, "http://192.0.2.10:8081": false, "http://example.com": false, "junk": false,
		"127.0.0.1:17399": false, "ftp://127.0.0.1": false, "http://user@127.0.0.1:1": false,
	} {
		base, ok := StandIn(v)
		if ok != want || (!ok && base != "") {
			t.Errorf("StandIn(%q) = %q, %v; want ok %v", v, base, ok, want)
		}
	}
}

// The app shows the code as "ABCD-2345": /start takes it with or without
// the dash, or with a space.
func TestPairCodeAsShown(t *testing.T) {
	for _, typed := range []string{"/start ABCD-2345", "/start abcd 2345", "/start ABCD2345"} {
		f := newFake(t)
		h := newHarness(t, f, Config{ID: "default", PairCode: "ABCD2345", PairUntil: time.Now().Add(time.Minute)})
		must(t, h.handle(context.Background(), msg(1, 42, typed)))
		if h.Config().UserID != 42 {
			t.Errorf("%q didn't pair", typed)
		}
	}
}
