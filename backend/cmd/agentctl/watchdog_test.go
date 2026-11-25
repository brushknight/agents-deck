package main

import (
	"bytes"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuf is a log writer the test can read while the watchdog writes.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// A service that stops answering gets its goroutine stacks in the log, and
// a line once it answers again.
func TestWatchStalls(t *testing.T) {
	stallEvery, stallAfter = 20*time.Millisecond, 100*time.Millisecond
	sock := filepath.Join(t.TempDir(), "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skip(err)
	}
	var mu sync.Mutex
	stuck := true
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		s := stuck
		mu.Unlock()
		if s {
			time.Sleep(300 * time.Millisecond)
		}
	}))
	var buf syncBuf
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	go watchStalls(sock)
	wait := func(what string) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if strings.Contains(buf.String(), what) {
				return
			}
		}
		t.Fatalf("no %q in the log:\n%s", what, buf.String())
	}
	wait("goroutine ")
	wait("end of the goroutine stacks")
	mu.Lock()
	stuck = false
	mu.Unlock()
	wait("answering again after")
}
