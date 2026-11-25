package limits

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// The Book survives a restart: what was saved comes back the same, and a
// week past its reset still shows as reset.
func TestKeeperRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	b := &Book{Now: func() time.Time { return now }}
	var sl StatusLine
	if err := json.Unmarshal([]byte(`{"rate_limits":{"seven_day":{"used_percentage":42,"resets_at":`+strconv.FormatInt(now.Add(48*time.Hour).Unix(), 10)+`}}}`), &sl); err != nil {
		t.Fatal(err)
	}
	b.Claude("/home/a/.claude", sl, now)
	b.Hit("claude", "/home/a/.claude", Hit{ID: "model_week:fable", Label: "Fable week", Model: "fable", ResetsAt: now.Add(24 * time.Hour)}, now)
	k := &Keeper{Book: b, Path: filepath.Join(dir, "limits.json")}
	if err := k.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(k.Path)
	if err != nil {
		t.Fatal(err)
	}
	// Saving again with nothing new leaves the file alone.
	if err := k.Save(); err != nil {
		t.Fatal(err)
	}
	if info2, _ := os.Stat(k.Path); !info2.ModTime().Equal(info.ModTime()) {
		t.Error("an unchanged Book was written again")
	}

	b2 := &Book{Now: func() time.Time { return now }}
	k2 := &Keeper{Book: b2, Path: k.Path}
	if err := k2.Load(); err != nil {
		t.Fatal(err)
	}
	got, want := b2.State(nil), b.State(nil)
	if len(got) != 1 || len(got[0].Windows) != len(want[0].Windows) {
		t.Fatalf("loaded %+v, want %+v", got, want)
	}
	for i := range want[0].Windows {
		g, w := got[0].Windows[i], want[0].Windows[i]
		if g.ID != w.ID || g.Used != w.Used || !g.ResetsAt.Equal(w.ResetsAt) || g.Hit != w.Hit || g.Level != w.Level {
			t.Errorf("window %d: %+v, want %+v", i, g, w)
		}
	}
	if !got[0].AsOf.Equal(now) {
		t.Errorf("asOf %v, want %v", got[0].AsOf, now)
	}

	// Three days on: the week shows as reset, the Fable hit is over.
	later := now.Add(72 * time.Hour)
	b2.Now = func() time.Time { return later }
	ls := b2.State(nil)
	if len(ls) != 1 || len(ls[0].Windows) != 1 || !ls[0].Windows[0].Reset || ls[0].Windows[0].Used != 0 {
		t.Errorf("after the reset: %+v", ls)
	}
}

// A missing file is an empty Book; a broken one is reported and changes
// nothing.
func TestKeeperBadFiles(t *testing.T) {
	dir := t.TempDir()
	k := &Keeper{Book: &Book{}, Path: filepath.Join(dir, "none.json")}
	if err := k.Load(); err != nil {
		t.Errorf("missing file: %v", err)
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o600)
	k = &Keeper{Book: &Book{}, Path: bad}
	if err := k.Load(); err == nil {
		t.Error("a broken file loaded")
	}
	if len(k.Book.State(nil)) != 0 {
		t.Error("a broken file left something behind")
	}
	other := filepath.Join(dir, "v9.json")
	os.WriteFile(other, []byte(`{"version":9,"sets":[{"tool":"claude","place":"x","windows":[{"id":"seven_day","used":5}]}]}`), 0o600)
	k = &Keeper{Book: &Book{}, Path: other}
	if err := k.Load(); err != nil || len(k.Book.State(nil)) != 0 {
		t.Errorf("another version: err %v, state %+v", err, k.Book.State(nil))
	}
}
