package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "123456:TEST-token-made-up"

// fakeAPI is a Bot API stand-in: it hands out queued updates, records what
// the bot sends, and serves every file as a few bytes.
type fakeAPI struct {
	mu      sync.Mutex
	updates []Update
	sent    []string
	srv     *httptest.Server
}

func newFake(t *testing.T) *fakeAPI {
	f := &fakeAPI{}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/file/bot"+testToken+"/") {
		w.Write([]byte("made-up file bytes"))
		return
	}
	method, ok := strings.CutPrefix(r.URL.Path, "/bot"+testToken+"/")
	if !ok {
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 401, "description": "Unauthorized"})
		return
	}
	r.ParseForm()
	var result any = true
	switch method {
	case "getMe":
		result = User{ID: 1, IsBot: true, FirstName: "Deck", Username: "test_deck_bot"}
	case "getUpdates":
		f.mu.Lock()
		result, f.updates = f.updates, nil
		f.mu.Unlock()
		if result.([]Update) == nil {
			result = []Update{}
		}
	case "sendMessage":
		f.mu.Lock()
		f.sent = append(f.sent, r.Form.Get("text"))
		f.mu.Unlock()
	case "getFile":
		id := r.Form.Get("file_id")
		p := "documents/" + id
		if strings.HasPrefix(id, "photo") {
			p = "photos/" + id + ".jpg"
		}
		result = map[string]any{"file_id": id, "file_path": p}
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

func (f *fakeAPI) replies() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

type delivered struct{ agent, text string }

func newBridge(t *testing.T, f *fakeAPI, st State) (*Bridge, *[]delivered) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "telegram.json")
	if err := SaveState(statePath, st); err != nil {
		t.Fatal(err)
	}
	var got []delivered
	b := &Bridge{
		API:       &API{Base: f.srv.URL, Token: testToken},
		StatePath: statePath,
		Inbox:     filepath.Join(dir, "inbox"),
		Deliver: func(agent, text string) (Result, error) {
			got = append(got, delivered{agent, text})
			return Result{Queued: true, Position: 2}, nil
		},
		Agent: func(id string) (AgentInfo, bool) {
			if id == "q601r3" || id == "k3f9a2" {
				return AgentInfo{Title: "Sorting desk", Status: "running", Queued: 1}, true
			}
			return AgentInfo{}, false
		},
		Logf: t.Logf,
	}
	b.mu.Lock()
	if err := b.loadLocked(); err != nil {
		t.Fatal(err)
	}
	b.mu.Unlock()
	return b, &got
}

func from(id int64) *User { return &User{ID: id, FirstName: "Test", LastName: "User"} }

func msg(id int64, user int64, text string) *Message {
	return &Message{ID: id, From: from(user), Chat: Chat{ID: user, Type: "private"}, Date: 1795000000, Text: text}
}

func TestPairing(t *testing.T) {
	f := newFake(t)
	b, got := newBridge(t, f, State{PairCode: "ABCD2345", PairUntil: time.Now().Add(time.Minute), Target: "q601r3"})
	ctx := context.Background()

	b.handle(ctx, msg(1, 42, "/start WRONG000"))
	b.handle(ctx, msg(2, 42, "hello there"))
	if r := f.replies(); len(r) != 0 {
		t.Fatalf("an unpaired account got answers: %q", r)
	}
	b.handle(ctx, msg(3, 42, "/start abcd2345")) // any case
	st, _, _ := b.Status()
	if st.UserID != 42 || st.UserName != "Test User" || st.PairCode != "" {
		t.Fatalf("not paired: %+v", st)
	}
	if r := f.replies(); len(r) != 1 || !strings.Contains(r[0], "Paired") || !strings.Contains(r[0], "q601r3") {
		t.Fatalf("pair answer %q", r)
	}
	saved, _ := LoadState(b.StatePath)
	if saved.UserID != 42 || saved.Target != "q601r3" {
		t.Fatalf("saved %+v", saved)
	}
	// Someone else, even with the old code: silence, nothing queued.
	b.handle(ctx, msg(4, 77, "/start ABCD2345"))
	b.handle(ctx, msg(5, 77, "a message from a stranger"))
	b.Flush()
	if len(f.replies()) != 1 || len(*got) != 0 {
		t.Fatalf("a stranger was answered or delivered: %q %v", f.replies(), *got)
	}
	// A group message from the paired user is ignored too.
	g := msg(6, 42, "in a group")
	g.Chat = Chat{ID: -100, Type: "group"}
	b.handle(ctx, g)
	b.Flush()
	if len(*got) != 0 {
		t.Fatal("a group message was delivered")
	}
}

func TestExpiredPairCode(t *testing.T) {
	f := newFake(t)
	b, _ := newBridge(t, f, State{PairCode: "ABCD2345", PairUntil: time.Now().Add(-time.Second)})
	b.handle(context.Background(), msg(1, 42, "/start ABCD2345"))
	if st, _, _ := b.Status(); st.UserID != 0 {
		t.Fatal("paired with an expired code")
	}
}

// An album and a forward become one prompt with every file's path, the
// forward's author and date, and one answer in Telegram.
func TestBatchIsOnePrompt(t *testing.T) {
	f := newFake(t)
	b, got := newBridge(t, f, State{UserID: 42, UserName: "Test User", Target: "q601r3"})
	ctx := context.Background()

	fwd := msg(10, 42, "Made-up feedback: the header feels heavy, try a lighter weight.")
	fwd.Origin = &Origin{Type: "user", Date: 1794990000, SenderUser: &User{ID: 9, FirstName: "Sam", LastName: "Example"}}
	b.handle(ctx, fwd)
	for i, id := range []string{"photoA", "photoB", "photoC"} {
		m := msg(int64(11+i), 42, "")
		m.MediaGroupID = "album1"
		m.Origin = fwd.Origin
		m.Photo = []PhotoSize{{FileID: id + "-small", Width: 90, Height: 90}, {FileID: id, Width: 1280, Height: 960}}
		if i == 0 {
			m.Caption = "Made-up caption: three options for the empty state."
		}
		b.handle(ctx, m)
	}
	doc := msg(14, 42, "")
	doc.Document = &Document{FileID: "doc1", FileName: "../../spec v2 (final).pdf", MimeType: "application/pdf"}
	b.handle(ctx, doc)
	if _, pending, _ := b.Status(); pending != 5 {
		t.Fatalf("pending %d, want 5 (one batch)", pending)
	}
	b.Flush()

	if len(*got) != 1 {
		t.Fatalf("want one prompt, got %d", len(*got))
	}
	p := (*got)[0]
	if p.agent != "q601r3" {
		t.Errorf("agent %q", p.agent)
	}
	first, _, _ := strings.Cut(p.text, "\n")
	if first != "[Telegram] 5 messages from Test User, 3 images, 1 file" {
		t.Errorf("first line %q", first)
	}
	for _, want := range []string{
		"forwarded from Sam Example, " + stamp(1794990000),
		"Made-up feedback: the header feels heavy",
		"Made-up caption: three options",
	} {
		if !strings.Contains(p.text, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p.text)
		}
	}
	var paths []string
	for _, l := range strings.Split(p.text, "\n") {
		l = strings.TrimSpace(l)
		if rest, ok := strings.CutPrefix(l, "image: "); ok {
			paths = append(paths, rest)
		} else if rest, ok := strings.CutPrefix(l, "file: "); ok {
			paths = append(paths, rest)
		}
	}
	if len(paths) != 4 {
		t.Fatalf("want 4 file paths, got %q", paths)
	}
	for _, p := range paths {
		if !filepath.IsAbs(p) || !strings.HasPrefix(p, b.Inbox) {
			t.Errorf("path %q is not absolute in the inbox", p)
		}
		fi, err := os.Stat(p)
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v", p, err, fi)
		}
	}
	if !strings.HasSuffix(paths[0], "-photo.jpg") || !strings.HasSuffix(paths[3], "spec_v2_final.pdf") {
		t.Errorf("names %q", paths)
	}
	if strings.Contains(paths[3], "..") {
		t.Errorf("unsafe name kept: %q", paths[3])
	}
	if r := f.replies(); len(r) != 1 || r[0] != "Queued for q601r3 (2nd in line): 5 messages, 3 images, 1 file" {
		t.Errorf("answer %q", r)
	}
}

func TestLongBatchIsClipped(t *testing.T) {
	f := newFake(t)
	b, got := newBridge(t, f, State{UserID: 42, Target: "q601r3"})
	b.Limit = 4000
	for i := range 6 {
		b.handle(context.Background(), msg(int64(i), 42, strings.Repeat("made-up words ", 80)))
	}
	b.Flush()
	p := (*got)[0].text
	if len(p) > 4000 {
		t.Fatalf("prompt is %d bytes", len(p))
	}
	if !strings.HasPrefix(p, "[Telegram] 6 messages\n") || !strings.Contains(p, "messages.md") {
		t.Fatalf("clipped prompt:\n%s", p[:200])
	}
	full := strings.TrimSpace(p[strings.Index(p, "are in ")+len("are in "):])
	full, _, _ = strings.Cut(full, "\n")
	b2, err := os.ReadFile(full)
	if err != nil || strings.Count(string(b2), "made-up words") != 6*80 {
		t.Fatalf("full text: %v", err)
	}
}

func TestCommands(t *testing.T) {
	f := newFake(t)
	b, got := newBridge(t, f, State{UserID: 42})
	ctx := context.Background()

	b.handle(ctx, msg(1, 42, "a message before any agent is set"))
	b.Flush()
	if len(*got) != 0 || !strings.Contains(f.replies()[0], "Not sent: no agent set") {
		t.Fatalf("no target: %v %q", *got, f.replies())
	}
	b.handle(ctx, msg(2, 42, "/to nosuch"))
	b.handle(ctx, msg(3, 42, "/to@test_deck_bot k3f9a2"))
	b.handle(ctx, msg(4, 42, "/status"))
	r := f.replies()
	if !strings.Contains(r[1], "no agent nosuch") || !strings.Contains(r[2], "now go to k3f9a2") ||
		!strings.Contains(r[3], "k3f9a2 (Sorting desk): running, 1 queued") {
		t.Fatalf("answers %q", r)
	}
	if st, _ := LoadState(b.StatePath); st.Target != "k3f9a2" {
		t.Fatalf("target not saved: %+v", st)
	}
	// A forwarded "/to" is content, not a command.
	fw := msg(5, 42, "/to q601r3")
	fw.Origin = &Origin{Type: "hidden_user", SenderUserName: "A. Nonymous", Date: 1}
	b.handle(ctx, fw)
	b.Flush()
	if len(*got) != 1 || !strings.Contains((*got)[0].text, "forwarded from A. Nonymous") {
		t.Fatalf("forward: %v", *got)
	}
}

func TestDeliverError(t *testing.T) {
	f := newFake(t)
	b, _ := newBridge(t, f, State{UserID: 42, Target: "q601r3"})
	b.Deliver = func(string, string) (Result, error) { return Result{}, errors.New("the agent has exited") }
	b.handle(context.Background(), msg(1, 42, "made-up note"))
	b.Flush()
	if r := f.replies(); len(r) != 1 || !strings.HasPrefix(r[0], "Not delivered to q601r3: the agent has exited.") {
		t.Fatalf("answer %q", r)
	}
}

// Run reads updates, keeps the offset, and a batch goes after the quiet time.
func TestRunPollsAndDebounces(t *testing.T) {
	f := newFake(t)
	b, got := newBridge(t, f, State{UserID: 42, Target: "q601r3", DebounceSeconds: 1})
	var mu sync.Mutex
	deliver := b.Deliver
	b.Deliver = func(a, txt string) (Result, error) {
		mu.Lock()
		defer mu.Unlock()
		return deliver(a, txt)
	}
	f.updates = []Update{{ID: 100, Message: msg(1, 42, "made-up one")}, {ID: 101, Message: msg(2, 42, "made-up two")}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(*got)
		mu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	<-done
	// No user name in this state: the first line has none.
	if len(*got) != 1 || !strings.HasPrefix((*got)[0].text, "[Telegram] 2 messages\n") {
		t.Fatalf("got %v", *got)
	}
	if st, _ := LoadState(b.StatePath); st.Offset != 102 {
		t.Fatalf("offset %d", st.Offset)
	}
}

func TestErrorsNeverShowTheToken(t *testing.T) {
	a := &API{Base: "http://127.0.0.1:1", Token: testToken}
	_, err := a.GetMe(context.Background())
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatalf("error %v", err)
	}
	f := newFake(t)
	bad := &API{Base: f.srv.URL, Token: "999:wrong"}
	if _, err := bad.GetMe(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong token: %v", err)
	}
}

func TestSafeNameAndClip(t *testing.T) {
	for in, want := range map[string]string{
		"../../etc/passwd": "passwd",
		".hidden":          "file.hidden",
		"brief v2.PDF":     "brief_v2.PDF",
		"":                 "file",
		"résumé.docx":      "r_sum.docx",
	} {
		if got := safeName(filepath.Base(in), "file"); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
	if c := clip("ééééé", 8); c != "éé…" || len(c) > 8 {
		t.Errorf("clip %q", c)
	}
}
