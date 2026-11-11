package ble

import (
	"io"
	"sync"
)

// maxWriteLine is the most a Write puts in one "write" line; the helper cuts
// it further to the link's MTU.
const maxWriteLine = 2048

// Stream is a connected board as a byte stream: Deck Link frames go through
// it as they would through a serial port. Writes wait while the helper has a
// full window of bytes it hasn't sent yet ("sent" frees them).
type Stream struct {
	c  *Central
	ID string // the board's identifier on this computer

	// From "connected": the board's Info and the largest write.
	Board, BoardID, Firmware string
	MTU                      int

	passkey   func(msg string)
	connected chan struct{}
	done      chan struct{}

	mu       sync.Mutex
	cond     *sync.Cond
	buf      []byte
	inflight int
	err      error
	conOnce  sync.Once
	doneOnce sync.Once
}

func (s *Stream) onPasskey(msg string) {
	if s.passkey != nil {
		s.passkey(msg)
	}
}

func (s *Stream) onConnected(m msg) {
	s.mu.Lock()
	s.Board, s.BoardID, s.Firmware, s.MTU = m.Board, m.BoardID, m.Firmware, m.MTU
	s.mu.Unlock()
	s.conOnce.Do(func() { close(s.connected) })
}

func (s *Stream) feed(p []byte) {
	if len(p) == 0 {
		return
	}
	s.mu.Lock()
	if s.err == nil {
		s.buf = append(s.buf, p...)
		s.cond.Broadcast()
	}
	s.mu.Unlock()
}

func (s *Stream) credit(n int) {
	s.mu.Lock()
	s.inflight = max(0, s.inflight-n)
	s.cond.Broadcast()
	s.mu.Unlock()
}

// fail ends the stream with err: Read returns what is left, then err.
func (s *Stream) fail(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.cond.Broadcast()
	s.mu.Unlock()
	s.doneOnce.Do(func() { close(s.done) })
}

// Err is why the stream ended (nil while it runs).
func (s *Stream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Done is closed when the stream ends.
func (s *Stream) Done() <-chan struct{} { return s.done }

func (s *Stream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.buf) == 0 && s.err == nil {
		s.cond.Wait()
	}
	if len(s.buf) > 0 {
		n := copy(p, s.buf)
		s.buf = s.buf[n:]
		if len(s.buf) == 0 {
			s.buf = nil
		}
		return n, nil
	}
	return 0, s.err
}

func (s *Stream) Write(p []byte) (int, error) {
	window := s.c.windowSize()
	written := 0
	for len(p) > 0 {
		n := min(len(p), maxWriteLine, window)
		s.mu.Lock()
		for s.err == nil && s.inflight+n > window {
			s.cond.Wait()
		}
		if s.err != nil {
			err := s.err
			s.mu.Unlock()
			return written, err
		}
		s.inflight += n
		s.mu.Unlock()
		if err := s.c.send(map[string]any{"op": "write", "id": s.ID, "data": p[:n]}); err != nil {
			s.fail(err)
			return written, err
		}
		written += n
		p = p[n:]
	}
	return written, nil
}

// Close disconnects the board.
func (s *Stream) Close() error {
	select {
	case <-s.done:
		return nil
	default:
	}
	s.c.drop(s.ID, s)
	s.fail(io.EOF)
	_ = s.c.send(map[string]any{"op": "disconnect", "id": s.ID})
	return nil
}
