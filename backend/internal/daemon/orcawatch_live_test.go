//go:build !windows

package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/orca"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// liveOrca is the real Orca, read-only: screens are read once per terminal
// and kept, so two decks fed from it see the same text; anything that would
// act in Orca fails the test.
type liveOrca struct {
	t      *testing.T
	c      orca.Client
	mu     sync.Mutex
	screen map[string]string
}

func (o *liveOrca) Read() (orca.Snapshot, error) { return o.c.Read() }
func (o *liveOrca) Screen(h string) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if s, ok := o.screen[h]; ok {
		return s, nil
	}
	s, err := o.c.Screen(h)
	o.screen[h] = s
	return s, err
}
func (o *liveOrca) Send(string, string, bool) error { return o.refuse("Send") }
func (o *liveOrca) Switch(string) error             { return o.refuse("Switch") }
func (o *liveOrca) Front() error                    { return o.refuse("Front") }
func (o *liveOrca) refuse(what string) error {
	o.t.Errorf("the live test called %s on the real Orca", what)
	return errors.New("read-only")
}

// deckAgents is what the deck shows for a snapshot: syncOrca on a fresh
// in-memory store, without the fields set from the clock or by chance.
func deckAgents(o *liveOrca, snap orca.Snapshot) []model.Agent {
	d := &Daemon{Store: store.New("", "test", "0")}
	d.orca.client = o
	d.syncOrca(snap)
	var out []model.Agent
	for _, e := range d.Store.Entries() {
		a := e.A
		a.Slot, a.AttentionSlot = 0, 0
		a.StartedAt, a.UpdatedAt, a.StatusSince = time.Time{}, time.Time{}, time.Time{}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// The deck's Orca agents from the socket equal those from the CLI, which
// here is the real fallback: the socket's runtime file is missing, so Read
// goes to the CLI (pinned to the real runtime file, as the CLI honours
// ORCA_USER_DATA_PATH too). Read-only; changes nothing in Orca.
//
//	cd backend && ORCA_LIVE=1 go test -run Live -v ./internal/orca ./internal/daemon
func TestLiveOrcaDeck(t *testing.T) {
	if os.Getenv("ORCA_LIVE") == "" {
		t.Skip("set ORCA_LIVE=1 to read the running Orca")
	}
	bin := orca.Bin()
	if bin == "" || !orca.Running() {
		t.Skip("Orca isn't installed or running")
	}
	realDir := filepath.Dir(orca.RuntimeFile())
	t.Setenv("ORCA_USER_DATA_PATH", realDir) // the same file; switched to noRuntime for the CLI read
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	pinned := filepath.Join(t.TempDir(), "orca")
	script := "#!/bin/sh\nORCA_USER_DATA_PATH=" + q(realDir) + " exec " + q(bin) + " \"$@\"\n"
	if err := os.WriteFile(pinned, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	noRuntime := t.TempDir()
	o := &liveOrca{t: t, c: orca.Client{Bin: bin}, screen: map[string]string{}}
	cli := &liveOrca{t: t, c: orca.Client{Bin: pinned}, screen: o.screen}

	var diff string
	for try := 1; try <= 5; try++ {
		start := time.Now()
		snap, err := o.Read()
		if err != nil {
			t.Fatal(err)
		}
		sockTime := time.Since(start)
		os.Setenv("ORCA_USER_DATA_PATH", noRuntime)
		start = time.Now()
		cliSnap, err := cli.Read()
		cliTime := time.Since(start)
		os.Setenv("ORCA_USER_DATA_PATH", realDir)
		if err != nil {
			t.Fatal(err)
		}
		a, b := deckAgents(o, snap), deckAgents(cli, cliSnap)
		if reflect.DeepEqual(a, b) {
			t.Logf("try %d: %d Orca agents on the deck, the same both ways (socket %v, CLI fallback %v)", try, len(a), sockTime, cliTime)
			for _, x := range a {
				t.Logf("  %s %s %s %q branch %q", x.ID, x.Tool, x.Status, x.Title, x.Branch)
			}
			connected := 0
			for _, w := range snap.Worktrees {
				for _, ag := range w.Agents {
					for _, term := range snap.Terminals {
						if term.PaneKey() == ag.PaneKey && term.Connected {
							connected++
						}
					}
				}
			}
			if len(a) != connected {
				t.Errorf("%d agents on the deck, %d agents in a connected terminal", len(a), connected)
			}
			for _, x := range a {
				if x.App != "orca" || !x.External {
					t.Errorf("agent %s: app %q external %v", x.ID, x.App, x.External)
				}
			}
			return
		}
		ja, _ := json.MarshalIndent(a, "", " ")
		jb, _ := json.MarshalIndent(b, "", " ")
		diff = "socket: " + string(ja) + "\nCLI: " + string(jb)
		time.Sleep(300 * time.Millisecond)
	}
	t.Errorf("deck agents still differ after 5 tries:\n%s", diff)
}
