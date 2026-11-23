package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
)

// A found session's transcript is parsed again only when it changed: syncLive
// asks for every session every 3 seconds.
func TestSessionMetaCached(t *testing.T) {
	const id = "5f0c7c1e-3a7b-4c3e-9d2a-1b2c3d4e5f60"
	path := filepath.Join(t.TempDir(), id+".jsonl")
	write := func(title string, at time.Time) {
		body := `{"type":"user","cwd":"/work/demo","sessionId":"` + id + `"}` + "\n" + `{"type":"ai-title","aiTitle":"` + title + `"}` + "\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	t0 := time.Now().Add(-time.Hour)
	write("first", t0)
	d := &Daemon{}
	l := claude.Live{SessionID: id, Transcript: path}
	if got := d.sessionMeta(l).AITitle; got != "first" {
		t.Fatalf("title %q", got)
	}
	// Same size and time: the cached answer, even though the text differs.
	write("other", t0)
	if got := d.sessionMeta(l).AITitle; got != "first" {
		t.Errorf("re-read an unchanged transcript: %q", got)
	}
	write("second", t0.Add(time.Minute))
	if got := d.sessionMeta(l).AITitle; got != "second" {
		t.Errorf("a changed transcript: %q", got)
	}
}
