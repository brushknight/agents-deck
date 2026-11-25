package queue

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func texts(l []Item) []string {
	out := []string{}
	for _, it := range l {
		out = append(out, it.Text)
	}
	return out
}

func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestOrderFrontAndCancel(t *testing.T) {
	q := Open(filepath.Join(t.TempDir(), "prompt-queue.json"))
	a, pos, _ := q.Add("k3f9a2", "one", "mcp:boss", false)
	if pos != 1 || len(a.ID) != 7 || a.ID[0] != 'q' {
		t.Fatalf("first: %+v %d", a, pos)
	}
	_, pos, _ = q.Add("k3f9a2", "two", "web", false)
	q.Add("h0m3l4", "other", "web", false)
	c, pos2, _ := q.Add("k3f9a2", "urgent", "web", true)
	if pos != 2 || pos2 != 1 {
		t.Fatalf("positions %d %d", pos, pos2)
	}
	if got := texts(q.List("k3f9a2")); !same(got, []string{"urgent", "one", "two"}) {
		t.Fatalf("order: %v", got)
	}
	if q.Count("k3f9a2") != 3 || q.Agents()["h0m3l4"] != 1 {
		t.Fatalf("counts: %v", q.Agents())
	}
	if _, ok, _ := q.Cancel("h0m3l4", c.ID); ok {
		t.Fatal("cancelled another agent's prompt")
	}
	if _, ok, _ := q.Cancel("k3f9a2", c.ID); !ok {
		t.Fatal("cancel")
	}
	if _, ok, _ := q.Cancel("k3f9a2", c.ID); ok {
		t.Fatal("cancelled twice")
	}
	next, _ := q.Next("k3f9a2")
	if next.Text != "one" || !q.Take(next.ID) || q.Take(next.ID) {
		t.Fatalf("next/take: %+v", next)
	}
	q.PutBack(next)
	if got := texts(q.List("k3f9a2")); !same(got, []string{"one", "two"}) {
		t.Fatalf("put back: %v", got)
	}
	if gone := q.Drop("k3f9a2"); len(gone) != 2 || q.Count("k3f9a2") != 0 || q.Count("h0m3l4") != 1 {
		t.Fatalf("drop: %v", gone)
	}
}

// A new Queue on the same file (a restarted service) has every prompt, in order.
func TestSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt-queue.json")
	q := Open(path)
	first, _, _ := q.Add("k3f9a2", "one", "mcp:boss", false)
	q.Add("k3f9a2", "two\nlines", "mcp:boss", false)
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", fi, err)
	}
	again := Open(path)
	l := again.List("k3f9a2")
	if got := texts(l); !same(got, []string{"one", "two\nlines"}) || l[0].ID != first.ID || l[0].By != "mcp:boss" || l[0].Queued.IsZero() {
		t.Fatalf("after restart: %+v", l)
	}
	// In memory only: nothing is written.
	m := Open("")
	m.Add("a", "x", "web", false)
	if m.Count("a") != 1 {
		t.Fatal("memory queue")
	}
}

func TestFull(t *testing.T) {
	q := Open("")
	for i := range MaxPerAgent {
		if _, _, err := q.Add("a", "x", "web", i%2 == 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := q.Add("a", "x", "web", false); !errors.Is(err, ErrFull) {
		t.Fatalf("21st: %v", err)
	}
	if _, _, err := q.Add("b", "x", "web", false); err != nil {
		t.Fatalf("another agent: %v", err)
	}
}
