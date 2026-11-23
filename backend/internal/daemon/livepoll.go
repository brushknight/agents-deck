package daemon

import (
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/claude"
)

// How often each account's Claude Code is asked for its sessions
// (`claude agents --json`).
const (
	// agentsMinGap: never more often than this; it was the fixed period
	// before, so nothing shows later than it did.
	agentsMinGap = 10 * time.Second
	// agentsMaxAge: at least this often, even when no session file changed
	// (a safety net, should Claude Code ever report something it doesn't
	// write down).
	agentsMaxAge = 5 * time.Minute
)

// agentsPoller keeps each account's last `claude agents --json` answer and
// asks again only when the account's session files changed since (a session
// started, ended, was renamed or started or ended a turn; a background job
// needs you), see claude.StateWatch. While nothing changes it asks every
// agentsMaxAge instead of every agentsMinGap.
type agentsPoller struct {
	mu    sync.Mutex
	polls map[string]*agentsPoll // account folder -> its last answer
	state claude.StateWatch
	now   func() time.Time // nil: time.Now
	key   func(dir string, ignore func(string) bool) (string, bool)
	runs  int // times it asked, for tests
}

// agentsPoll is one account's last answer, and its session files then.
type agentsPoll struct {
	at    time.Time
	key   string
	keyOK bool
	list  []claude.Live
	ok    bool
}

// due reports whether to ask the account again at now, its session files
// summing up to key (keyOK false: they can't tell). Never within
// agentsMinGap of the last time; after that when the files changed, can't
// tell, or the last answer failed, and in any case after agentsMaxAge.
func (p *agentsPoll) due(now time.Time, key string, keyOK bool) bool {
	switch {
	case p.at.IsZero():
		return true
	case now.Sub(p.at) < agentsMinGap:
		return false
	case !keyOK || !p.keyOK || !p.ok || key != p.key:
		return true
	}
	return now.Sub(p.at) >= agentsMaxAge
}

// sessions is every account's sessions, as Claude Code reported them, asking
// an account again (ask) only when it is due. ignore tells sessions whose
// changes don't matter (the deck's own agents: their hooks tell). ok is
// false when no account's Claude Code answered.
func (p *agentsPoller) sessions(accs []accounts.Account, ask func(accounts.Account) ([]claude.Live, bool), ignore func(string) bool) (all []claude.Live, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.polls == nil {
		p.polls = map[string]*agentsPoll{}
	}
	now, key := time.Now, p.state.Key
	if p.now != nil {
		now = p.now
	}
	if p.key != nil {
		key = p.key
	}
	seen := map[string]bool{}
	dirs := map[string]bool{}
	for _, a := range accs {
		dir := a.Path()
		dirs[dir] = true
		k, kOK := key(dir, ignore) // before asking: a change while it runs counts next time
		poll := p.polls[dir]
		if poll == nil {
			poll = &agentsPoll{}
			p.polls[dir] = poll
		}
		if t := now(); poll.due(t, k, kOK) {
			poll.list, poll.ok = ask(a)
			poll.at, poll.key, poll.keyOK = t, k, kOK
			p.runs++
		}
		if !poll.ok {
			continue
		}
		ok = true
		for _, l := range poll.list {
			if !seen[l.SessionID] {
				seen[l.SessionID] = true
				all = append(all, l)
			}
		}
	}
	for dir := range p.polls {
		if !dirs[dir] {
			delete(p.polls, dir) // the account was removed
		}
	}
	return all, ok
}
