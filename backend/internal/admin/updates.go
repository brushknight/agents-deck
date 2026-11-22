package admin

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/update"
)

const catalogTTL = 15 * time.Minute

// cachedCatalog is the last catalog read, without asking the network.
func (a *Admin) cachedCatalog() (*firmware.Catalog, string) {
	a.catMu.Lock()
	defer a.catMu.Unlock()
	return a.catalog, a.catErr
}

// refreshCatalog reads the latest release's catalog in the background when
// the one in hand is old (lists never wait for the network).
func (a *Admin) refreshCatalog() {
	if a.Firmware == nil {
		return
	}
	a.catMu.Lock()
	if a.catBusy || (time.Since(a.catAt) < catalogTTL && a.catAt != time.Time{}) {
		a.catMu.Unlock()
		return
	}
	a.catBusy = true
	a.catMu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_, _ = a.loadCatalog(ctx)
	}()
}

// loadCatalog reads the latest release's catalog now.
func (a *Admin) loadCatalog(ctx context.Context) (firmware.Catalog, error) {
	if a.Firmware == nil {
		return firmware.Catalog{}, errors.New("this service has no firmware catalog")
	}
	cat, err := a.Firmware.Latest(ctx)
	a.catMu.Lock()
	defer a.catMu.Unlock()
	a.catBusy, a.catAt = false, time.Now()
	if err != nil {
		a.catErr = err.Error()
		if a.catalog != nil {
			return *a.catalog, nil // keep showing the last one
		}
		return firmware.Catalog{}, err
	}
	a.catalog, a.catErr = &cat, ""
	return cat, nil
}

// releaseKey: whether this build can check downloads (and so install them).
func (a *Admin) releaseKey() bool {
	switch c := a.Firmware.(type) {
	case *firmware.Client:
		return c.Releases != nil && c.Releases.Key != nil
	case interface{ HasReleaseKey() bool }: // the demo's
		return c.HasReleaseKey()
	}
	return a.Firmware != nil
}

// firmwareCatalog: GET /admin/v1/firmware.
func (a *Admin) firmwareCatalog(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	cat, err := a.loadCatalog(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "couldn't read the firmware list of the latest release: "+err.Error())
		return
	}
	boards := cat.Boards
	if boards == nil {
		boards = []firmware.Board{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": cat.Version, "boards": boards, "releaseKey": a.releaseKey()})
}

// ---- the app's own updates ---------------------------------------------------------

func (a *Admin) updateMode() string {
	m := str(a.readConfig(), "updates", UpdatesNotify)
	if !oneOf(m, UpdatesNotify, UpdatesAuto, UpdatesOff) {
		return UpdatesNotify
	}
	return m
}

// UpdateView is GET /admin/v1/update: the updater's state, the mode and when
// it last looked.
type UpdateView struct {
	update.State
	Mode       string    `json:"mode"`
	Checked    time.Time `json:"checked,omitzero"`
	ReleaseKey bool      `json:"releaseKey"`
	Job        string    `json:"job,omitempty"` // an install that runs
}

func (a *Admin) updateView() UpdateView {
	a.updMu.Lock()
	checked := a.checked
	a.updMu.Unlock()
	v := UpdateView{State: a.updaterState(), Mode: a.updateMode(), Checked: checked, ReleaseKey: a.releaseKey()}
	if j, ok := a.jobs.running(func(j Job) bool { return j.Kind == "app-update" }); ok {
		v.Job = j.ID
	}
	return v
}

func (a *Admin) updateState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.updateView())
}

// check looks for a new release; one that is out is downloaded and checked
// so it is ready to install, and with auto installed.
func (a *Admin) check(ctx context.Context, install bool) error {
	if a.Updater == nil {
		return errors.New("this service can't update itself")
	}
	st, err := a.Updater.Check(ctx)
	a.updMu.Lock()
	a.checked = time.Now().UTC()
	a.updMu.Unlock()
	if a.Firmware != nil {
		_, _ = a.loadCatalog(ctx)
	}
	if err != nil {
		return err
	}
	if st.State == update.StateAvailable && !update.NotifyOnly(st.Install) && st.Message == "" {
		if st, err = a.Updater.Download(ctx); err != nil {
			return err
		}
	}
	if install && st.State == update.StateReady {
		log.Printf("update: installing %s by itself (updates: auto)", st.Version)
		return a.Updater.Apply(ctx)
	}
	return nil
}

// RunUpdates checks for updates shortly after start and then daily, as the
// update mode says ("off" never checks), until ctx ends.
func (a *Admin) RunUpdates(ctx context.Context) {
	first, every := a.FirstCheck, a.CheckEvery
	if first <= 0 {
		first = 2 * time.Minute
	}
	if every <= 0 {
		every = 24 * time.Hour
	}
	t := time.NewTimer(first)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if mode := a.updateMode(); mode != UpdatesOff {
			cctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			if err := a.check(cctx, mode == UpdatesAuto); err != nil {
				log.Printf("update check: %v", err)
			}
			cancel()
		}
		t.Reset(every)
	}
}

// updateCheck: POST /admin/v1/update/check. It answers the state once the
// release is read; a download that follows runs on.
func (a *Admin) updateCheck(w http.ResponseWriter, r *http.Request) {
	if a.Updater == nil {
		writeErr(w, http.StatusNotImplemented, "this service can't update itself")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	_, err := a.Updater.Check(ctx)
	a.updMu.Lock()
	a.checked = time.Now().UTC()
	a.updMu.Unlock()
	if err != nil {
		writeErr(w, http.StatusBadGateway, "couldn't reach the releases: "+err.Error())
		return
	}
	go func() { // fetch it now, so Install doesn't wait
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if err := a.check(ctx, false); err != nil {
			log.Printf("update: %v", err)
		}
	}()
	writeJSON(w, http.StatusOK, a.updateView())
}

// updateApply: POST /admin/v1/update/apply downloads (if needed) and
// installs the latest release as a job. The service restarts at the end.
func (a *Admin) updateApply(w http.ResponseWriter, r *http.Request) {
	if a.Updater == nil {
		writeErr(w, http.StatusNotImplemented, "this service can't update itself")
		return
	}
	st := a.Updater.State()
	switch {
	case st.State == update.StateDisabled:
		msg := st.Message
		if msg == "" {
			msg = update.ErrNoReleaseKey.Error()
		}
		writeErr(w, http.StatusConflict, msg)
		return
	case update.NotifyOnly(st.Install):
		writeErr(w, http.StatusConflict, st.Message)
		return
	case st.State == update.StateNone && st.Version != "":
		writeErr(w, http.StatusConflict, "agents deck is up to date")
		return
	}
	if j, ok := a.jobs.running(func(j Job) bool { return j.Kind == "app-update" }); ok {
		writeJSON(w, http.StatusAccepted, j)
		return
	}
	job := a.jobs.start(Job{Kind: "app-update", Version: st.Version, Message: "Downloading the update"},
		func(ctx context.Context, up func(func(*Job))) error {
			up(func(j *Job) { j.Stage, j.Percent = "download", 10 })
			s, err := a.Updater.Download(ctx)
			if err != nil {
				return err
			}
			up(func(j *Job) {
				j.Stage, j.Percent, j.Version = "install", 70, s.Version
				j.Message = "Installing " + s.Version + ". The service and the app restart in a moment; agents keep running."
				j.Steps = append(j.Steps, "Downloaded and checked "+s.Version)
			})
			log.Printf("update to %s asked from Settings: installing, the service restarts", s.Version)
			return a.Updater.Apply(ctx)
		})
	writeJSON(w, http.StatusAccepted, job)
}
