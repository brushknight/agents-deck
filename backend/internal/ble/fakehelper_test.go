package ble

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// The fake helper: this test binary, run with DECK_BLE_FAKE=<dir>, speaks the
// helper's line protocol (clients/ble/README.md) with made-up boards and no
// radio. The boards are <dir>/boards.json, read again on every scan and
// connect. A connected board's bytes go to and from the unix socket
// <dir>/<sock>, where the test plays the board. Every command it gets is
// appended to <dir>/commands.log.

const fakeEnv = "DECK_BLE_FAKE"

type fakeBoard struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	RSSI    int    `json:"rssi"`
	Info    string `json:"info"`    // the Info characteristic: board=..;id=..;fw=..;paired=0|1
	Paired  bool   `json:"paired"`  // what paired= says
	Passkey string `json:"passkey"` // ok (default) | fail: what happens when it pairs
	Sock    string `json:"sock"`    // the test's socket for this board
	MTU     int    `json:"mtu"`     // the link's ATT MTU: writes and notifications carry MTU - 3 bytes (default 67)
}

// chunk is the most bytes one write or notification carries on b's link.
func (b fakeBoard) chunk() int {
	if b.MTU <= 3 {
		return 64
	}
	return b.MTU - 3
}

// infoField is one key of an Info value ("board", "id", "fw"), or def.
func infoField(info, key, def string) string {
	for _, part := range splitSemi(info) {
		if len(part) > len(key) && part[:len(key)+1] == key+"=" {
			return part[len(key)+1:]
		}
	}
	return def
}

func fakeHelper(dir string) {
	out := json.NewEncoder(os.Stdout)
	var omu sync.Mutex
	emit := func(v map[string]any) {
		omu.Lock()
		defer omu.Unlock()
		_ = out.Encode(v)
	}
	boards := func() map[string]fakeBoard {
		var list []fakeBoard
		b, _ := os.ReadFile(filepath.Join(dir, "boards.json"))
		_ = json.Unmarshal(b, &list)
		m := map[string]fakeBoard{}
		for _, x := range list {
			m[x.ID] = x
		}
		return m
	}
	logf, _ := os.OpenFile(filepath.Join(dir, "commands.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	emit(map[string]any{"ev": "ready", "protocol": 1, "helper": "fake", "window": 256})
	emit(map[string]any{"ev": "state", "state": "on", "message": "Bluetooth is on"})
	var mu sync.Mutex
	conns := map[string]net.Conn{}
	chunks := map[string]int{} // by board: its largest write
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 64*1024), 1<<20)
	for in.Scan() {
		var c struct {
			Op   string `json:"op"`
			ID   string `json:"id"`
			On   bool   `json:"on"`
			Data []byte `json:"data"`
		}
		if json.Unmarshal(in.Bytes(), &c) != nil {
			emit(map[string]any{"ev": "error", "message": "not a JSON object"})
			continue
		}
		if c.Op != "write" {
			fmt.Fprintf(logf, "%s %s\n", c.Op, c.ID)
		}
		switch c.Op {
		case "state":
			emit(map[string]any{"ev": "state", "state": "on", "message": "Bluetooth is on"})
		case "scan":
			emit(map[string]any{"ev": "scanning", "on": c.On})
			if c.On {
				for _, b := range boards() {
					emit(map[string]any{"ev": "found", "id": b.ID, "name": b.Name, "rssi": b.RSSI})
					emit(map[string]any{"ev": "found", "id": b.ID, "name": b.Name, "rssi": b.RSSI,
						"board": infoField(b.Info, "board", "round"), "boardId": infoID(b.Info), "firmware": infoField(b.Info, "fw", "0.4.0"), "paired": b.Paired})
				}
			}
		case "connect":
			b, ok := boards()[c.ID]
			if !ok {
				emit(map[string]any{"ev": "disconnected", "id": c.ID, "code": "unknown-peripheral", "reason": "this computer doesn't know that board"})
				continue
			}
			emit(map[string]any{"ev": "connecting", "id": c.ID})
			if !b.Paired {
				emit(map[string]any{"ev": "passkey", "id": c.ID, "message": "Type the 6-digit code shown on the board into the window macOS opens."})
			}
			if b.Passkey == "fail" {
				emit(map[string]any{"ev": "disconnected", "id": c.ID, "code": "pairing-failed",
					"reason": "pairing failed: the code typed didn't match the board's, or pairing was cancelled"})
				continue
			}
			conn, err := net.Dial("unix", filepath.Join(dir, b.Sock))
			if err != nil {
				emit(map[string]any{"ev": "disconnected", "id": c.ID, "code": "timeout", "reason": "the board stopped answering"})
				continue
			}
			mu.Lock()
			conns[c.ID] = conn
			mu.Unlock()
			emit(map[string]any{"ev": "connected", "id": c.ID, "mtu": b.chunk(), "board": infoField(b.Info, "board", "round"),
				"boardId": infoID(b.Info), "firmware": infoField(b.Info, "fw", "0.4.0")})
			chunks[c.ID] = b.chunk()
			go func(id string, conn net.Conn, size int) {
				buf := make([]byte, size) // notifications of at most MTU - 3 bytes
				for {
					n, err := conn.Read(buf)
					if n > 0 {
						emit(map[string]any{"ev": "data", "id": id, "data": buf[:n]})
					}
					if err != nil {
						mu.Lock()
						mine := conns[id] == conn
						if mine {
							delete(conns, id)
						}
						mu.Unlock()
						if mine {
							emit(map[string]any{"ev": "disconnected", "id": id, "code": "gone", "reason": "the board disconnected"})
						}
						return
					}
				}
			}(c.ID, conn, b.chunk())
		case "write":
			mu.Lock()
			conn := conns[c.ID]
			mu.Unlock()
			if conn == nil {
				emit(map[string]any{"ev": "error", "id": c.ID, "message": "not connected to that board"})
				continue
			}
			size := chunks[c.ID]
			for i := 0; i < len(c.Data); i += size { // cut to the largest write, as the real helper does
				_, _ = conn.Write(c.Data[i:min(i+size, len(c.Data))])
			}
			emit(map[string]any{"ev": "sent", "id": c.ID, "n": len(c.Data)})
		case "disconnect", "forget":
			mu.Lock()
			conn := conns[c.ID]
			delete(conns, c.ID)
			mu.Unlock()
			if conn != nil {
				conn.Close()
			}
			if c.Op == "forget" {
				emit(map[string]any{"ev": "forgotten", "id": c.ID})
			} else {
				emit(map[string]any{"ev": "disconnected", "id": c.ID, "code": "cancelled", "reason": "disconnected"})
			}
		default:
			emit(map[string]any{"ev": "error", "message": "unknown op " + c.Op})
		}
	}
	os.Exit(0)
}

func infoID(info string) string {
	var id string
	for _, part := range splitSemi(info) {
		if len(part) > 3 && part[:3] == "id=" {
			id = part[3:]
		}
	}
	return id
}

func splitSemi(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ';' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}
