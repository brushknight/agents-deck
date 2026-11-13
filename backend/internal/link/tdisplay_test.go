package link

import (
	"os/exec"
	"strings"
	"testing"
)

// The LilyGO T-Display-S3 end to end against the real session
// (firmware/panel-core/sim/panel_sim.cpp --board tds3, with layout_tds3.h): it
// says Hello as itself with two buttons, is approved with scope act, shows the
// waiting agent as its prompt, and a person taps the primary option. The
// deck's controller must get that Answer.
func TestTDisplayS3AnswersThroughTheDeck(t *testing.T) {
	bin := buildSim(t, t.TempDir(), "panel_sim")
	const typ, id, name = "lilygo-t-display-s3", "AA:BB:CC:00:53:03", "lilygo-t-display-s3-005303"
	srv, ctl := testDeck(t)
	all := runSimAgainstDeck(t, srv, name,
		exec.Command(bin, "--board", "tds3", "--board-id", id, "--answer", "--seconds", "6"))
	for _, want := range []string{
		"link state=unapproved device=" + name,
		"link state=live device=" + name + " scope=act",
		"hero=a1 status=waiting",
		"tap x=",
		"command answer agent=a1 prompt=p1 key=1 seq=",
		"code=204",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("panel_sim log lacks %q:\n%s", want, all)
		}
	}
	if len(ctl.calls) == 0 || ctl.calls[0] != "answer a1 p1 1" {
		t.Errorf("the deck got %v, want the primary option of a1's prompt", ctl.calls)
	}
	if d, ok := srv.Devices.Get(name); !ok || d.Type != typ {
		t.Errorf("device %s: %+v, want type %s", name, d, typ)
	}
}
