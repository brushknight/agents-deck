package devices

import (
	"path/filepath"
	"testing"
)

func TestRegistry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	r := &Registry{Path: path}
	if err := r.AddNetwork("Desk Panel", Act, "ab", "sha256:00"); err == nil {
		t.Error("bad name accepted")
	}
	if err := r.AddNetwork("desk-panel", Act, "ab", "sha256:00"); err != nil {
		t.Fatal(err)
	}
	if d, ok := r.ByCertSerial("ab"); !ok || d.Name != "desk-panel" || d.Scope != Act {
		t.Fatalf("by serial: %+v %v", d, ok)
	}
	// Persisted.
	r2 := &Registry{Path: path}
	if _, ok := r2.ByCertSerial("ab"); !ok {
		t.Fatal("not saved")
	}
	if err := r2.Revoke("desk-panel"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r2.ByCertSerial("ab"); ok {
		t.Error("revoked device still active")
	}
	// Re-pairing the name replaces the certificate and un-revokes.
	_ = r2.AddNetwork("desk-panel", Act, "cd", "sha256:01")
	if _, ok := r2.ByCertSerial("ab"); ok {
		t.Error("old certificate still works after re-pairing")
	}
	if _, ok := r2.ByCertSerial("cd"); !ok {
		t.Error("new certificate rejected")
	}
}

func TestBoards(t *testing.T) {
	r := &Registry{Path: filepath.Join(t.TempDir(), "devices.json")}
	d, err := r.Board("24:6F:28:3F:A9:C1", "OLED Tile", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "oled-tile-3fa9c1" || d.Active() || d.Scope != View {
		t.Fatalf("new board: %+v", d)
	}
	if err := r.Approve(d.Name); err != nil {
		t.Fatal(err)
	}
	again, _ := r.Board("24:6F:28:3F:A9:C1", "OLED Tile", "1.1.0")
	if !again.Active() || again.Firmware != "1.1.0" || again.Name != d.Name {
		t.Fatalf("known board: %+v", again)
	}
	other, _ := r.Board("xx3fa9c1", "oled tile", "1")
	if other.Name == d.Name {
		t.Error("two boards share a name")
	}
	// Long types keep their model: two round boards don't both become "waveshare-touch-lcd--…".
	for typ, want := range map[string]string{
		"waveshare-touch-lcd-28c":   "waveshare-touch-lcd-28c-00280c",
		"waveshare-touch-amoled-18": "waveshare-touch-amoled-18-00280c",
		"-dash-":                    "dash-00280c",
	} {
		if got := boardName(typ, "AA:BB:CC:00:28:0C"); got != want {
			t.Errorf("boardName(%q) = %q, want %q", typ, got, want)
		}
	}
}

func TestPairingCodes(t *testing.T) {
	r := &Registry{Path: filepath.Join(t.TempDir(), "devices.json")}
	eq := func(a, b string) bool { return a == b }
	if err := r.NewPairingCode("phone", Act, "c0de"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := r.RedeemPairingCode("nope", eq); ok {
		t.Error("wrong code accepted")
	}
	name, scope, ok := r.RedeemPairingCode("c0de", eq)
	if !ok || name != "phone" || scope != Act {
		t.Fatalf("redeem: %s %s %v", name, scope, ok)
	}
	if _, _, ok := r.RedeemPairingCode("c0de", eq); ok {
		t.Error("code used twice")
	}
}

func TestScopes(t *testing.T) {
	if !Admin.Allows(Act) || !Act.Allows(View) || View.Allows(Act) || Act.Allows(Admin) || Scope("").Allows(View) {
		t.Error("scope order")
	}
}

// A board paired for Wi-Fi is the same entry: its certificate works while it
// is approved, and revoking it drops the certificate for good.
func TestBoardCert(t *testing.T) {
	r := &Registry{Path: filepath.Join(t.TempDir(), "devices.json")}
	d, _ := r.Board("AA:BB:CC:00:00:01", "round", "1")
	if err := r.SetBoardCert(d.Name, "c1", "sha256:01"); err == nil {
		t.Fatal("an unapproved board got a certificate")
	}
	_ = r.Approve(d.Name)
	if err := r.SetBoardCert(d.Name, "c1", "sha256:01"); err != nil {
		t.Fatal(err)
	}
	if got, ok := r.ByCertSerial("c1"); !ok || got.Name != d.Name || got.Kind != Serial || got.BoardID != d.BoardID {
		t.Fatalf("by serial: %+v %v", got, ok)
	}
	// Pairing again replaces it.
	_ = r.SetBoardCert(d.Name, "c2", "sha256:02")
	if _, ok := r.ByCertSerial("c1"); ok {
		t.Error("the old certificate still works")
	}
	if err := r.Revoke(d.Name); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.ByCertSerial("c2"); ok {
		t.Error("a revoked board's certificate works")
	}
	_ = r.Approve(d.Name)
	if _, ok := r.ByCertSerial("c2"); ok {
		t.Error("approving again brought the old certificate back")
	}
	if _, ok := r.ByCertSerial(""); ok {
		t.Error("an empty serial matched")
	}
	if err := r.AddNetwork("panel", Act, "n1", "sha256:03"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetBoardCert("panel", "x", "y"); err == nil {
		t.Error("a network device paired as a board")
	}
}
