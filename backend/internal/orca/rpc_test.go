//go:build !windows

package orca

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// fakeOrca serves the runtime socket the way the Orca app does: it checks
// the token, sends a keepalive, then the answer. It writes the runtime file
// and points ORCA_USER_DATA_PATH at it.
func fakeOrca(t *testing.T, answer func(req map[string]any) map[string]any) {
	t.Helper()
	dir, err := os.MkdirTemp("", "orca") // short: unix socket paths are limited
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "o.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	meta, _ := json.Marshal(map[string]any{"runtimeId": "rt-1", "authToken": "tok-1",
		"transports": []map[string]string{{"kind": "unix", "endpoint": sock}}})
	if err := os.WriteFile(filepath.Join(dir, "orca-runtime.json"), meta, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORCA_USER_DATA_PATH", dir)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				line, err := bufio.NewReader(c).ReadBytes('\n')
				if err != nil {
					return
				}
				var req map[string]any
				_ = json.Unmarshal(line, &req)
				res := map[string]any{"id": req["id"], "_meta": map[string]any{"runtimeId": "rt-1"}}
				if req["authToken"] != "tok-1" {
					res["ok"], res["error"] = false, map[string]any{"code": "unauthorized", "message": "bad token"}
				} else {
					for k, v := range answer(req) {
						res[k] = v
					}
				}
				b, _ := json.Marshal(res)
				_, _ = c.Write([]byte("{\"_keepalive\":true}\n\n"))
				_, _ = c.Write(append(b, '\n'))
			}()
		}
	}()
}

func TestReadOverSocket(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	fakeOrca(t, func(req map[string]any) map[string]any {
		mu.Lock()
		methods = append(methods, req["method"].(string))
		mu.Unlock()
		switch req["method"] {
		case "worktree.ps":
			return map[string]any{"ok": true, "result": map[string]any{"worktrees": []map[string]any{
				{"worktreeId": "r::/w", "path": "/w", "agents": []map[string]any{{"paneKey": "t:l", "state": "working"}}}}}}
		case "terminal.list":
			if p, _ := req["params"].(map[string]any); p["includeVisualLayouts"] != false {
				return map[string]any{"ok": false, "error": map[string]any{"message": "layouts asked for"}}
			}
			return map[string]any{"ok": true, "result": map[string]any{"terminals": []map[string]any{{"handle": "h1", "tabId": "t", "leafId": "l"}}}}
		}
		return map[string]any{"ok": false, "error": map[string]any{"message": "unknown method"}}
	})
	// No CLI: everything has to come over the socket.
	snap, err := Client{}.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Worktrees) != 1 || snap.Worktrees[0].Agents[0].State != "working" || len(snap.Terminals) != 1 || snap.Terminals[0].PaneKey() != "t:l" {
		t.Fatalf("snapshot %+v", snap)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 2 || methods[0] != "worktree.ps" || methods[1] != "terminal.list" {
		t.Errorf("methods %v", methods)
	}
}

// Orca's own refusal is an error to show; a socket that can't be used means
// the CLI instead.
func TestRPCErrors(t *testing.T) {
	fakeOrca(t, func(map[string]any) map[string]any {
		return map[string]any{"ok": false, "error": map[string]any{"message": "workspace gone"}}
	})
	err := Client{}.rpc(nil, "worktree.ps", nil)
	if err == nil || errors.Is(err, errTransport) || err.Error() != "orca worktree.ps: workspace gone" {
		t.Errorf("an answered error: %v", err)
	}
	// What Orca 1.4 answers for a method it doesn't have and for a wrong token.
	for _, code := range []string{"method_not_found", "unauthorized"} {
		fakeOrca(t, func(map[string]any) map[string]any {
			return map[string]any{"ok": false, "error": map[string]any{"code": code, "message": code}}
		})
		if err := (Client{}).rpc(nil, "worktree.ps", nil); !errors.Is(err, errTransport) {
			t.Errorf("%s: %v", code, err)
		}
	}
	t.Setenv("ORCA_USER_DATA_PATH", t.TempDir()) // no runtime file
	if err := (Client{}).rpc(nil, "worktree.ps", nil); !errors.Is(err, errTransport) {
		t.Errorf("no runtime: %v", err)
	}
}
