package link

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
)

// The firmware kit's Bluetooth byte stream (deck_ble.c) against the real
// deck: board_wifi --ble is a board whose Bluetooth link is a Unix socket,
// in pieces of MTU - 3 bytes as over the air. The deck side here is a plain
// session on that socket; the Bluetooth helper and its rules (pairing and
// credentials allowed only on an encrypted, bonded link) are the host's.
func TestFirmwareKitOverBluetoothSocket(t *testing.T) {
	bin := boardWifi(t)
	srv, _ := testDeck(t)
	// A short path: Unix socket paths are limited to about 100 bytes.
	dir, err := os.MkdirTemp("", "ble")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "b.sock")
	const name = "board-wifi-000077"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var sess *Session
	log, _ := runBoard(t, bin, nil, func(l string) {
		switch {
		case strings.HasPrefix(l, "ble name="):
			go func() {
				conn, err := net.Dial("unix", sock)
				if err != nil {
					t.Errorf("dial: %v", err)
					return
				}
				sess = NewSession(ServerDeck{srv}, conn, "ble-sim")
				go func() { _ = sess.Run(ctx); conn.Close() }()
			}()
		case strings.HasPrefix(l, "welcome ") && strings.Contains(l, "approved=0"):
			go func() {
				_ = srv.Devices.Approve(name)
				_ = srv.Devices.SetScope(name, devices.Act)
			}()
		}
	}, "--ble", sock, "--store", t.TempDir(), "--mtu", "23", "--seconds", "3")
	for _, want := range []string{"ble name=deck-000077 passkey=", "info=board=board-wifi;id=AA:BB:CC:00:00:77;fw=0.1.0;paired=1",
		"ble connected", "welcome name=" + name + " scope=1 approved=0", "welcome name=" + name + " scope=2 approved=1",
		"snapshot_begin agents=3", "agent id=a1 status=waiting", "snapshot_end"} {
		if !strings.Contains(log, want) {
			t.Fatalf("board log lacks %q:\n%s", want, log)
		}
	}
	if sess == nil {
		t.Fatal("no session")
	}
	deadline := time.Now().Add(2 * time.Second)
	for sess.Net() == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	n := sess.Net()
	if n == nil || n.Transport != linkpb.Transport_TRANSPORT_BLE || n.Ble != linkpb.BleState_BLE_CONNECTED {
		t.Fatalf("NetStatus over the socket: %v", n)
	}
}
