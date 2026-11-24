package daemon

import (
	"time"

	"github.com/brushknight/agents-deck/backend/internal/tmux"
)

// screenCache keeps the last screen Run read of each agent (capture-pane),
// and reads it again only once tmux says the window printed something
// since. Agents waiting on a question or a menu are checked every second or
// two; most of them sit there unchanged until you answer.
//
// Run's goroutine only. Without look (screens read outside Run), every
// read goes to tmux.
type screenCache struct {
	at   time.Time            // when the current tick looked at tmux
	act  map[string]time.Time // each session's last output then (whole seconds)
	last map[screenKey]screenRead
}

type screenKey struct {
	name string
	back int
}

type screenRead struct {
	at   time.Time // when it was read
	act  time.Time // the session's last output, as known then
	text string
}

// look starts a tick: tmux's session activity as read at now.
func (c *screenCache) look(act map[string]time.Time, now time.Time) {
	c.at, c.act = now, act
	if c.last == nil {
		c.last = map[screenKey]screenRead{}
	}
	for k := range c.last {
		if _, ok := act[k.name]; !ok {
			delete(c.last, k) // the session ended
		}
	}
}

// read is the session's screen plus back lines of history. The last read is
// reused when it was taken in an earlier tick, after the second of the
// window's last output, and that is still the last output: anything the
// window printed after the read would have moved its activity past it.
func (c *screenCache) read(name string, back int) string {
	act := c.act[name]
	k := screenKey{name, back}
	if r, ok := c.last[k]; ok && fresh(r, act, c.at) {
		return r.text
	}
	text := tmux.Capture(name, back)
	if !act.IsZero() && c.last != nil {
		c.last[k] = screenRead{at: time.Now(), act: act, text: text}
	}
	return text
}

// fresh: r still shows the screen, at a tick that looked at tmux at tick
// and saw act as the window's last output.
func fresh(r screenRead, act, tick time.Time) bool {
	return !act.IsZero() && act.Equal(r.act) && r.at.Before(tick) && r.at.Unix() > act.Unix()
}

// paneTTYs are the terminals of our own agents' panes: from Run's last look
// at tmux while it is fresh (Run looks every second), else asked now.
func (d *Daemon) paneTTYs() map[string]bool {
	d.mu.Lock()
	p, at := d.panes, d.panesAt
	d.mu.Unlock()
	if p != nil && time.Since(at) < 3*time.Second {
		return p
	}
	return tmux.PaneTTYs()
}
