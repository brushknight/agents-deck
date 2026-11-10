package tmux

import "errors"

// ErrUnsupported: agents started by agentctl live in tmux, which Windows
// doesn't have. Everything here then reports "no sessions", and starting
// or driving one fails with this message.
var ErrUnsupported = errors.New("starting agents with agentctl needs tmux, which is not available on Windows yet")

// Bin names tmux for messages only: it is never run on Windows.
var Bin = "tmux"

func run(args ...string) (string, error) { return "", ErrUnsupported }
