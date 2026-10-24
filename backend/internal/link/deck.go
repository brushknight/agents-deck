package link

import (
	"encoding/hex"
	"log"
	"net/http"
	"strings"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
)

// ServerDeck is the daemon behind a session: its store, its device registry
// and its web API, through Server.Dispatch (same scopes, same audit log).
type ServerDeck struct{ S *server.Server }

func (d ServerDeck) Snapshot() model.State { return d.S.Store.Snapshot() }

func (d ServerDeck) Changes() (<-chan struct{}, func()) {
	ch, unsub := d.S.Store.Subscribe()
	return ch, unsub
}

func (d ServerDeck) Board(id, typ, fw string) (devices.Device, error) {
	return d.S.Devices.Board(id, typ, fw)
}

func (d ServerDeck) Device(name string) (devices.Device, bool) { return d.S.Devices.Get(name) }

func (d ServerDeck) Do(dev devices.Device, method, path string, body []byte) (int, []byte) {
	return d.S.Dispatch(dev, method, path, body)
}

// AutoApprove approves a new board plugged into a registered USB port, when
// the registry's setting allows it, and audits it.
func (d ServerDeck) AutoApprove(dev devices.Device, port string) (devices.Device, bool) {
	nd, ok, err := d.S.Devices.AutoApproveUSB(dev.Name)
	if err != nil {
		log.Printf("link %s: approving %s automatically: %v", port, dev.Name, err)
		return dev, false
	}
	if !ok {
		return nd, false
	}
	d.S.Audit.Log(devices.AuditEntry{Device: nd.Name, Action: "approve", Status: http.StatusOK,
		Detail: "approved automatically over USB on " + port + ", scope " + string(nd.Scope)})
	log.Printf("link %s: %s approved automatically over USB (scope %s)", port, nd.Name, nd.Scope)
	if d.S.Store != nil {
		d.S.Store.Touch() // the devices list on the web dashboard
	}
	return nd, true
}

// Pair issues the board's Wi-Fi certificate (server.PairBoard) and says
// where to use it: the network listener's names and addresses, its port and
// its certificate's fingerprint, and the deck's CA.
func (d ServerDeck) Pair(dev devices.Device, csr []byte) (*linkpb.CertIssued, int, string) {
	cert, serial, code, err := d.S.PairBoard(dev, csr)
	if err != nil {
		return nil, code, err.Error()
	}
	hosts := d.S.DeviceHosts
	if len(hosts) > limits["CertIssued.hosts"].count {
		hosts = hosts[:limits["CertIssued.hosts"].count]
	}
	return &linkpb.CertIssued{Cert: cert, Ca: d.S.CA.Cert.Raw, Hosts: hosts, Port: uint32(d.S.DevicePort),
		ServerFingerprint: fingerprintBytes(d.S.ServerFingerprint), DeviceName: dev.Name, CertSerial: serial}, http.StatusOK, ""
}

// fingerprintBytes turns "sha256:AB:CD:…" into its 32 bytes (nil if malformed).
func fingerprintBytes(fp string) []byte {
	hexs := strings.ReplaceAll(strings.TrimPrefix(fp, "sha256:"), ":", "")
	b, err := hex.DecodeString(hexs)
	if err != nil || len(b) != 32 {
		return nil
	}
	return b
}
