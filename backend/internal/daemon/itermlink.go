package daemon

import (
	"errors"
	"sync"

	"github.com/brushknight/agents-deck/backend/internal/iterm2"
)

// itermLink keeps one authenticated connection to iTerm's API and uses it to
// select the tab and pane showing a given tty.
type itermLink struct {
	mu   sync.Mutex
	conn *iterm2.Conn
}

var errNoAPI = errors.New("iTerm API not available")

func (l *itermLink) dial() error {
	if !iterm2.Available() {
		return errNoAPI
	}
	cookie, key, err := iterm2.RequestCookie("agentctl")
	if err != nil {
		return err
	}
	c, err := iterm2.Dial(cookie, key, "agentctl")
	if err != nil {
		return err
	}
	l.conn = c
	return nil
}

// selectTTY activates the iTerm session whose tty is tty (selects its tab and
// pane, orders its window front). It reconnects once on a stale connection.
func (l *itermLink) selectTTY(tty string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		if l.conn == nil {
			if err := l.dial(); err != nil {
				return err
			}
		}
		err := l.activate(tty)
		if err == nil || errors.Is(err, errNotShown) {
			return err
		}
		l.conn.Close()
		l.conn = nil
	}
	return errors.New("iTerm API: connection lost")
}

var errNotShown = errors.New("no iTerm session shows that terminal")

func (l *itermLink) activate(tty string) error {
	ss, err := l.conn.Sessions()
	if err != nil {
		return err
	}
	for _, s := range ss {
		t, err := l.conn.Variable(s.ID, "tty")
		if err != nil {
			return err
		}
		if t == tty {
			return l.conn.Activate(s.ID)
		}
	}
	return errNotShown
}
