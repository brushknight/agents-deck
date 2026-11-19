package claude

import (
	"bytes"
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Turn is where a session's turn stands, from the end of its transcript.
type Turn struct {
	Over   bool      // Claude's final reply, an API error or an interrupt (Esc) ends it
	InTool bool      // the latest message is a tool call still waiting for its result
	Tool   string    // the tool of the latest call
	At     time.Time // when the latest message was written (the file's time if none was found)
}

const (
	turnChunk = 256 << 10 // read backwards this much at a time
	// turnLimit is how far back from the end the latest message is looked
	// for. An idle session keeps getting notes appended (titles, modes,
	// ledgers: a few kilobytes every so often), so after days the last
	// message can sit hundreds of kilobytes up.
	turnLimit = 32 << 20
	turnTool  = 256 << 10 // how far past the latest message the latest tool call is looked for
)

// turnSeen is what was read of a transcript last time: up to where (a whole
// number of lines), the bytes just before that point (to tell an appended
// file from a replaced one), and the turn as it stood there.
type turnSeen struct {
	size    int64
	edge    []byte
	turn    Turn
	decided bool // a message was found (else the turn is unknown)
}

var turnCache = struct {
	sync.Mutex
	m map[string]turnSeen
}{m: map[string]turnSeen{}}

// ReadTurn reads where the turn stands off the end of a main session
// transcript. Lines that are no message of the conversation are skipped:
// the notes Claude Code keeps appending to an idle session, text it adds
// for itself, a compaction's summary, subagents' messages. Only what was
// appended since the last call is read. ok is false when the file can't be
// read.
func ReadTurn(path string) (t Turn, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return t, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return t, false
	}
	size := st.Size()
	turnCache.Lock()
	prev, hit := turnCache.m[path]
	turnCache.Unlock()
	lo := max(0, size-turnLimit)
	if hit && prev.size <= size && prev.size >= lo && edgeMatches(f, prev) {
		lo = prev.size // appended to since: read only the new lines
	} else {
		hit = false
	}

	var found Turn
	decided, past := false, int64(0)
	committed := scanBack(f, lo, size, func(b []byte) bool {
		if decided {
			past += int64(len(b)) + 1
		}
		var l line
		if len(b) == 0 || json.Unmarshal(b, &l) != nil {
			return !decided || past < turnTool // not JSON, or a line cut off at the lower edge
		}
		d, ends := l.turnState()
		var blocks []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}
		if d && l.Type == "assistant" {
			_ = json.Unmarshal(l.Message.Content, &blocks)
		}
		if d && !decided {
			decided, found.Over = true, ends
			if ts, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil {
				found.At = ts
			}
			for _, b := range blocks {
				found.InTool = found.InTool || b.Type == "tool_use"
			}
		}
		for _, b := range blocks {
			if b.Type == "tool_use" && found.Tool == "" {
				found.Tool = b.Name
			}
		}
		return found.Tool == "" && (!decided || past < turnTool)
	})

	seen := turnSeen{size: committed, turn: found, decided: decided}
	if hit && !decided { // nothing new that decides: the turn stands where it stood
		seen.turn, seen.decided = prev.turn, prev.decided
	}
	if hit && seen.turn.Tool == "" {
		seen.turn.Tool = prev.turn.Tool
	}
	if !seen.decided {
		seen.turn.At = st.ModTime()
	}
	seen.edge = readEdge(f, committed)
	turnCache.Lock()
	turnCache.m[path] = seen // an older read racing this one is still true up to its own size
	turnCache.Unlock()
	return seen.turn, true
}

const edgeLen = 64

func readEdge(f *os.File, at int64) []byte {
	b := make([]byte, min(at, edgeLen))
	n, _ := f.ReadAt(b, at-int64(len(b)))
	return b[:n]
}

// edgeMatches reports whether the file still holds what was read last time
// (Claude Code only appends; a file written anew starts over).
func edgeMatches(f *os.File, prev turnSeen) bool {
	return bytes.Equal(readEdge(f, prev.size), prev.edge)
}

// scanBack calls fn for each whole line of f between lo and hi, newest
// first, until fn returns false. lo must be the start of a line or the
// file's start (or the line there is cut and fails to parse). A last line
// with no newline yet is still being written and is left for the next read.
// It returns the end of the last whole line.
func scanBack(f *os.File, lo, hi int64, fn func([]byte) bool) (committed int64) {
	committed = lo
	var carry []byte // the start of a line whose end the previous chunk held
	top := true      // the end of the last whole line is not known yet
	for end := hi; end > lo; {
		off := max(lo, end-turnChunk)
		buf := make([]byte, end-off)
		n, _ := f.ReadAt(buf, off)
		if n < len(buf) {
			return committed // the file shrank while read
		}
		lines := bytes.Split(append(buf, carry...), []byte("\n"))
		end = off
		if top {
			if len(lines) == 1 && off > lo {
				carry = lines[0] // one long line, unfinished: read on up
				continue
			}
			committed = hi - int64(len(lines[len(lines)-1]))
			lines = lines[:len(lines)-1]
			top = false
		}
		carry = nil
		if off > lo {
			carry, lines = lines[0], lines[1:]
		}
		for i := len(lines) - 1; i >= 0; i-- {
			if !fn(lines[i]) {
				return committed
			}
		}
	}
	return committed
}
