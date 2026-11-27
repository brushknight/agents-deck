package daemon

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/opencode"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// opencodeWatch mirrors recent OpenCode sessions and answers their
// permissions or interrupts through OpenCode's local service.
type opencodeWatch struct {
	mu     sync.Mutex
	syncMu sync.Mutex           // serialises permission assignments with an answer
	hidden map[string]time.Time // session id -> when hidden
	done   map[string]bool      // permission request ids answered here
	svc    *opencode.Service
	svcAt  time.Time
}

func (d *Daemon) opencodeWindow() time.Duration { return 6 * time.Hour }

// WatchOpenCode reads OpenCode's store and local service every 2 seconds
// (no-op with "opencode": false in config.json).
func (d *Daemon) WatchOpenCode() {
	if d.Cfg.OpenCode != nil && !*d.Cfg.OpenCode {
		return
	}
	logged := ""
	for {
		svc := d.opencodeService()
		var active map[string]bool
		activeOK := false
		if svc != nil {
			var err error
			active, err = svc.Active()
			activeOK = err == nil
			if err != nil {
				if err.Error() != logged {
					log.Printf("opencode: status: %v", err)
					logged = err.Error()
				}
			} else {
				logged = ""
			}
		}
		ids := make([]string, 0, len(active))
		for id := range active {
			ids = append(ids, id)
		}
		list, err := opencode.RecentSessions(d.opencodeWindow(), ids)
		if err != nil {
			if err.Error() != logged {
				log.Printf("opencode: %v", err)
				logged = err.Error()
			}
			time.Sleep(2 * time.Second)
			continue
		}
		perms := map[string][]opencode.Permission{}
		permOK := map[string]bool{}
		if activeOK {
			for _, s := range list {
				p, err := svc.PermissionsFor(s.ID)
				if err != nil {
					permOK[s.ID] = false
					continue
				}
				perms[s.ID] = p
				permOK[s.ID] = true
			}
		}
		d.syncOpenCode(list, active, activeOK, perms, permOK)
		time.Sleep(2 * time.Second)
	}
}

// opencodeService is the local API, reloaded every 30 seconds.
func (d *Daemon) opencodeService() *opencode.Service {
	d.opencode.mu.Lock()
	defer d.opencode.mu.Unlock()
	if d.opencode.svc != nil && time.Since(d.opencode.svcAt) < 30*time.Second {
		return d.opencode.svc
	}
	svc, err := opencode.LoadService()
	if err != nil {
		d.opencode.svc, d.opencode.svcAt = nil, time.Now()
		return nil
	}
	d.opencode.svc, d.opencode.svcAt = svc, time.Now()
	return svc
}

func opencodePrompt(p opencode.Permission) *model.Prompt {
	title := p.Message
	if title == "" {
		title = "allow " + p.Action + "?"
	}
	options := []model.Option{
		{Key: "1", Label: "allow once", Primary: true},
	}
	if len(p.Save) > 0 {
		options = append(options, model.Option{Key: "2", Label: "always allow"})
	}
	options = append(options, model.Option{Key: "3", Label: "reject"})
	return &model.Prompt{
		ID: p.ID, Kind: "permission", Title: title,
		Detail: strings.Join(p.Resources, "\n"), Context: p.Action,
		Options: options,
	}
}

func (d *Daemon) syncOpenCode(list []opencode.Session, active map[string]bool, activeOK bool, perms map[string][]opencode.Permission, permOK map[string]bool) {
	d.opencode.syncMu.Lock()
	defer d.opencode.syncMu.Unlock()
	d.opencode.mu.Lock()
	hidden := make(map[string]time.Time, len(d.opencode.hidden))
	for id, at := range d.opencode.hidden {
		hidden[id] = at
	}
	done := make(map[string]bool, len(d.opencode.done))
	for id := range d.opencode.done {
		done[id] = true
	}
	d.opencode.mu.Unlock()

	want := map[string]opencode.Session{}
	for _, s := range list {
		id := opencode.AgentID(s.ID)
		if h, ok := hidden[s.ID]; ok && !time.UnixMilli(s.TimeUpdated).After(h) {
			continue
		}
		want[id] = s
	}
	for _, e := range d.Store.Entries() {
		if e.A.App == "opencode" {
			if _, ok := want[e.A.ID]; !ok {
				d.Store.Remove(e.A.ID)
			}
		}
	}
	present := map[string]bool{}
	for _, s := range want {
		for _, p := range perms[s.ID] {
			present[p.ID] = true
		}
	}
	d.opencode.mu.Lock()
	for id := range d.opencode.done {
		if !present[id] {
			delete(d.opencode.done, id)
		}
	}
	d.opencode.mu.Unlock()

	for id, s := range want {
		if _, ok := d.Store.Get(id); !ok {
			e := &store.Entry{External: true, SessionID: s.ID}
			e.A = model.Agent{
				ID: id, Tool: "opencode", External: true, App: "opencode", Status: model.Idle,
				StatusSince: s.Since(model.Idle),
				Title:       s.DisplayTitle(), Cwd: s.Directory, Folder: filepath.Base(s.Directory),
				Model: s.ModelName(), ModelLabel: s.ModelLabel(),
			}
			d.Store.Add(e)
			since := s.Since(model.Idle)
			d.Store.Update(id, func(x *store.Entry) bool { x.A.StatusSince = since; return true })
		}
		d.Store.Update(id, func(e *store.Entry) bool {
			changed := false
			e.SessionID = s.ID
			var waiting *model.Prompt
			if permOK[s.ID] {
				for _, p := range perms[s.ID] {
					if !done[p.ID] {
						waiting = opencodePrompt(p)
						break
					}
				}
			}
			st := e.A.Status
			if activeOK && permOK[s.ID] {
				switch {
				case waiting != nil:
					st = model.Waiting
				case active[s.ID]:
					st = model.Running
				default:
					st = model.Idle
				}
			}
			was := e.A.Status
			if st != was {
				e.SetStatus(st)
				if st == model.Waiting {
					e.A.StatusSince = time.Now().UTC()
				} else if since := s.Since(st); !since.IsZero() {
					e.A.StatusSince = since
				}
				if st == model.Running {
					e.A.Unseen = false
				}
				if (was == model.Running || was == model.Waiting) && st == model.Idle {
					e.A.Unseen = true
				}
				changed = true
			} else if e.A.StatusSince.IsZero() {
				e.A.StatusSince = s.Since(st)
			}
			if waiting != nil && (e.A.Waiting == nil || e.A.Waiting.ID != waiting.ID) {
				e.A.Waiting = waiting
				e.A.StatusSince = time.Now().UTC()
				changed = true
			}
			if st == model.Running {
				activity := &model.Activity{Tool: "work", Detail: "opencode"}
				if e.A.Activity == nil || *e.A.Activity != *activity {
					e.A.Activity = activity
					changed = true
				}
			} else if e.A.Activity != nil {
				e.A.Activity = nil
				changed = true
			}
			if s.Directory != "" && s.Directory != e.A.Cwd {
				e.A.Cwd, e.A.Folder, changed = s.Directory, filepath.Base(s.Directory), true
			}
			if title := s.DisplayTitle(); title != e.A.Title {
				e.A.Title, changed = title, true
			}
			if s.ModelName() != "" && s.ModelName() != e.A.Model {
				e.A.Model, e.A.ModelLabel, changed = s.ModelName(), s.ModelLabel(), true
			}
			tokens := model.Tokens{Input: s.TokensInput, Output: s.TokensOutput, CacheRead: s.CacheRead, CacheWrite: s.CacheWrite}
			if e.A.Tokens != tokens {
				e.A.Tokens, changed = tokens, true
			}
			if e.A.CostUSD != s.Cost {
				e.A.CostUSD, changed = s.Cost, true
			}
			if s.Turns > 0 && s.Turns != e.A.Turns {
				e.A.Turns, changed = s.Turns, true
			}
			if s.TimeUpdated > 0 && e.MarkActiveStep(time.UnixMilli(s.TimeUpdated)) {
				changed = true
			}
			return changed
		})
	}
}

func (d *Daemon) opencodeSend(e store.Entry, key string) error {
	svc := d.opencodeService()
	if e.A.Waiting == nil {
		return server.ErrStalePrompt
	}
	if svc == nil {
		return errors.New("opencode: its local service isn't running")
	}
	decision := map[string]string{"1": "once", "2": "always", "3": "reject"}[key]
	if decision == "" {
		return server.ErrBadKey
	}
	id := e.A.Waiting.ID
	d.opencode.syncMu.Lock()
	d.opencode.mu.Lock()
	if d.opencode.done == nil {
		d.opencode.done = map[string]bool{}
	}
	d.opencode.done[id] = true
	d.opencode.mu.Unlock()
	d.opencode.syncMu.Unlock()
	if err := svc.ReplyPermission(e.SessionID, id, decision); err != nil {
		d.opencode.mu.Lock()
		delete(d.opencode.done, id)
		d.opencode.mu.Unlock()
		var api *opencode.APIError
		if errors.As(err, &api) && (api.Status == 404 || api.Status == 409) {
			return server.ErrStalePrompt
		}
		return fmt.Errorf("opencode: %w", err)
	}
	return nil
}

func (d *Daemon) opencodeInterrupt(e store.Entry) error {
	svc := d.opencodeService()
	if svc == nil {
		return errors.New("opencode: its local service isn't running")
	}
	if err := svc.Interrupt(e.SessionID); err != nil {
		return fmt.Errorf("opencode: %w", err)
	}
	return nil
}

func (d *Daemon) opencodePrompt(e store.Entry, text string) error {
	svc := d.opencodeService()
	if svc == nil {
		return errors.New("opencode: its local service isn't running")
	}
	if err := svc.Prompt(e.SessionID, text); err != nil {
		return fmt.Errorf("opencode: %w", err)
	}
	return nil
}

func (d *Daemon) opencodeFocus(e store.Entry) error {
	return fmt.Errorf("%w: open OpenCode to show this session", server.ErrReadOnly)
}

func (d *Daemon) opencodeHide(e store.Entry) {
	d.opencode.syncMu.Lock()
	defer d.opencode.syncMu.Unlock()
	d.opencode.mu.Lock()
	if d.opencode.hidden == nil {
		d.opencode.hidden = map[string]time.Time{}
	}
	if e.SessionID != "" {
		d.opencode.hidden[e.SessionID] = time.Now().UTC()
	}
	d.opencode.mu.Unlock()
	d.Store.Remove(e.A.ID)
}
