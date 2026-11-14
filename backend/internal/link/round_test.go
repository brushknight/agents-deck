package link

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/server"
)

// buildRoundSim compiles the round boards' host program
// (firmware/panel-core/sim/round_sim.cpp: the kit, the adapter, the round
// state machine and both layouts) into dir.
func buildRoundSim(t *testing.T, dir string) string {
	t.Helper()
	return buildSim(t, dir, "round_sim", "-DPANEL_MAX_AGENTS=64", "-DPANEL_OPTION_LABEL=96")
}

// buildSim compiles firmware/panel-core/sim/<name>.cpp with the kit into dir.
// Skips the test without C and C++ compilers.
func buildSim(t *testing.T, dir, name string, defines ...string) string {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler")
	}
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("no C++ compiler")
	}
	fw, _ := filepath.Abs(repo + "/firmware")
	kit := filepath.Join(fw, "deck-link", "src")
	srcs, _ := filepath.Glob(filepath.Join(kit, "proto", "*.c"))
	srcs = append(srcs, filepath.Join(kit, "deck_link.c"), filepath.Join(kit, "cobs.c"), filepath.Join(kit, "crc16.c"))
	var objs []string
	for _, src := range srcs {
		obj := filepath.Join(dir, strings.TrimSuffix(filepath.Base(src), ".c")+".o")
		if out, err := exec.Command(cc, "-std=c99", "-O1", "-I"+kit, "-c", src, "-o", obj).CombinedOutput(); err != nil {
			t.Skipf("can't build the kit: %v\n%s", err, out)
		}
		objs = append(objs, obj)
	}
	bin := filepath.Join(dir, name)
	args := append([]string{"-std=c++17", "-O1"}, defines...)
	args = append(args, "-I"+filepath.Join(fw, "panel-core", "src"), "-I"+kit, "-I"+filepath.Join(kit, "proto"),
		filepath.Join(fw, "panel-core", "sim", name+".cpp"))
	args = append(append(args, objs...), "-o", bin)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s doesn't build: %v\n%s", name, err, out)
	}
	return bin
}

// runSimAgainstDeck runs a board's host program (cmd) against the real
// session on srv: its stdin and stdout are the serial line, its stderr the
// log. When the board shows up unapproved as name, the deck approves it with
// scope act. Returns the board's log once it exits.
func runSimAgainstDeck(t *testing.T, srv *server.Server, name string, cmd *exec.Cmd) string {
	t.Helper()
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	errp, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess := NewSession(ServerDeck{srv}, struct {
		io.Reader
		io.Writer
	}{out, in}, filepath.Base(cmd.Path))
	sess.RecheckEvery, sess.Debounce = 50*time.Millisecond, 20*time.Millisecond
	go func() { _ = sess.Run(ctx) }()
	var log []string
	approved := false
	sc := bufio.NewScanner(errp)
	for sc.Scan() {
		l := sc.Text()
		log = append(log, l)
		if !approved && strings.HasPrefix(l, "link state=unapproved device="+name) {
			approved = true
			go func() {
				_ = srv.Devices.Approve(name)
				_ = srv.Devices.SetScope(name, devices.Act)
			}()
		}
	}
	_ = cmd.Wait()
	return strings.Join(log, "\n")
}

// A round board end to end against the real session: it says Hello as a
// 2.8" board with touch, is approved with scope act, features the waiting
// agent in its middle, and a person taps the middle (the prompt opens) and
// then the first pill. The deck's controller must get that Answer. Every
// layout.
func TestRoundBoardAnswersThroughTheDeck(t *testing.T) {
	bin := buildRoundSim(t, t.TempDir())
	for _, tc := range []struct{ layout, typ, id, name string }{
		{"orbit", "waveshare-touch-lcd-28c", "AA:BB:CC:00:28:0C", "waveshare-touch-lcd-28c-00280c"},
		{"dial", "waveshare-touch-lcd-21", "AA:BB:CC:00:21:00", "waveshare-touch-lcd-21-002100"},
		{"spring", "waveshare-touch-lcd-21", "AA:BB:CC:00:21:01", "waveshare-touch-lcd-21-002101"},
		{"spring28", "waveshare-touch-lcd-28c", "AA:BB:CC:00:28:0D", "waveshare-touch-lcd-28c-00280d"},
	} {
		t.Run(tc.layout, func(t *testing.T) {
			srv, ctl := testDeck(t)
			all := runSimAgainstDeck(t, srv, tc.name, exec.Command(bin, "--layout", tc.layout, "--type", tc.typ,
				"--board-id", tc.id, "--answer", "--seconds", "6"))
			for _, want := range []string{
				"link state=unapproved device=" + tc.name,
				"link state=live device=" + tc.name + " scope=act",
				"featured id=a1 status=waiting",
				"tap x=240 y=240",
				"screen=prompt agent=a1",
				"command answer agent=a1 prompt=p1 key=1 seq=",
				"code=204",
			} {
				if !strings.Contains(all, want) {
					t.Errorf("round_sim log lacks %q:\n%s", want, all)
				}
			}
			if len(ctl.calls) == 0 || ctl.calls[0] != "answer a1 p1 1" {
				t.Errorf("the deck got %v, want the primary option of a1's prompt", ctl.calls)
			}
			// It registered as its type.
			if d, ok := srv.Devices.Get(tc.name); !ok || d.Type != tc.typ {
				t.Errorf("device %s: %+v, want type %s", tc.name, d, tc.typ)
			}
		})
	}
}
