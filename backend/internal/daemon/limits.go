package daemon

import (
	"encoding/json"
	"errors"
	"log"
	"path/filepath"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/codex"
	"github.com/brushknight/agents-deck/backend/internal/limits"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/paths"
)

// Usage limits, from what Claude Code and Codex report themselves (see
// package limits): the status line command, limit hits in Claude
// transcripts, and Codex rollouts.

func (d *Daemon) startLimits() {
	d.limits = &limits.Book{}
	d.limitsKeep = &limits.Keeper{Book: d.limits, Path: paths.Limits()}
	if err := d.limitsKeep.Load(); err != nil {
		log.Printf("limits: %s ignored: %v", paths.Limits(), err)
	}
	d.Store.Limits = d.LimitsState
	claude.LimitHit = d.limitHit
}

// LimitsState is every account's limits, named as the deck names accounts.
func (d *Daemon) LimitsState() []model.Limits {
	if d.limits == nil {
		return nil
	}
	inUse := accounts.InUse()
	return d.limits.State(func(tool, place string) string {
		if tool != "claude" || !inUse {
			return ""
		}
		for _, a := range accounts.List() {
			if a.Path() == place {
				return a.Name
			}
		}
		return ""
	})
}

// claudePlace is the config folder of a Claude account: CLAUDE_CONFIG_DIR,
// or ~/.claude when it is unset.
func claudePlace(dir string) string {
	if dir == "" {
		return accounts.Home()
	}
	return filepath.Clean(dir)
}

// StatusLine takes one status line report (agentctl statusline): the
// account's config folder and Claude Code's status line input.
func (d *Daemon) StatusLine(dir string, body []byte) error {
	var sl limits.StatusLine
	if err := json.Unmarshal(body, &sl); err != nil {
		return err
	}
	if d.limits != nil && d.limits.Claude(claudePlace(dir), sl, time.Now()) {
		d.Store.Touch()
	}
	return nil
}

// limitHit is claude.LimitHit: a transcript recorded a limit hit. It runs
// while the store is locked, so the redraw is asked for on the side.
func (d *Daemon) limitHit(transcript, text string, at time.Time) {
	h, ok := limits.ParseHit(text, at)
	if !ok || d.limits == nil {
		return
	}
	place := ""
	if a, ok := accounts.ForTranscript(transcript); ok {
		place = a.Path()
	} else { // <config folder>/projects/<project>/<session>.jsonl
		place = filepath.Dir(filepath.Dir(filepath.Dir(transcript)))
	}
	if d.limits.Hit("claude", place, h, at) {
		go d.Store.Touch()
	}
}

// WatchLimits saves the limits to limits.json when they changed, reads Codex's newest rollout for its rate limits, and redraws
// when a window passes its reset, every 15 seconds.
func (d *Daemon) WatchLimits() {
	if d.limits == nil {
		return
	}
	var seenPath string
	var seenMod time.Time
	last := ""
	for {
		if d.Cfg.Codex == nil || *d.Cfg.Codex {
			home := codex.Home()
			if f, mod := codex.Newest(filepath.Join(home, "sessions"), 8, time.Now()); f != "" && (f != seenPath || !mod.Equal(seenMod)) {
				seenPath, seenMod = f, mod
				for _, r := range codex.RateLimits(f) {
					d.limits.Codex(home, r.Limits, r.At)
				}
			}
		}
		if d.limitsKeep != nil {
			if err := d.limitsKeep.Save(); err != nil {
				log.Printf("limits: saving: %v", err)
			}
		}
		b, _ := json.Marshal(d.LimitsState())
		if s := string(b); s != last {
			last = s
			d.Store.Touch()
		}
		time.Sleep(15 * time.Second)
	}
}

// UsageEverySession is "Report usage from every Claude session": with on,
// the deck's status line goes into every Claude account's own settings.json
// (the default ~/.claude too), so sessions started outside the deck report
// their usage as well; with off, the status lines they had come back.
func (d *Daemon) UsageEverySession(on bool) error {
	dirs := []string{accounts.Home()}
	for _, a := range accounts.List() {
		if p := a.Path(); p != "" && filepath.Clean(p) != filepath.Clean(accounts.Home()) {
			dirs = append(dirs, p)
		}
	}
	var errs []error
	for _, dir := range dirs {
		var err error
		if on {
			err = claude.AddDeckStatusLine(dir, d.Agentctl, paths.StatusLineSaved())
		} else {
			err = claude.RemoveDeckStatusLine(dir, paths.StatusLineSaved())
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
