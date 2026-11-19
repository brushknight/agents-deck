package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Job states.
const (
	JobRunning   = "running"
	JobDone      = "done"
	JobFailed    = "failed"
	JobCancelled = "cancelled"
)

// Job is a long task: flashing a board, updating one, installing an update.
// Jobs live in memory; the last 50 are kept.
type Job struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"` // flash | board-update | app-update
	State    string    `json:"state"`
	Percent  int       `json:"percent"`
	Stage    string    `json:"stage,omitempty"` // download | connect | erase | write | verify | reset | offer | install
	Message  string    `json:"message"`
	Error    string    `json:"error,omitempty"`
	BootHelp bool      `json:"bootHelp,omitempty"` // the board didn't answer: hold BOOT, press RST
	Port     string    `json:"port,omitempty"`     // the port's id
	Device   string    `json:"device,omitempty"`
	Board    string    `json:"board,omitempty"` // catalog board id
	Version  string    `json:"version,omitempty"`
	File     string    `json:"file,omitempty"`   // a firmware file picked on this computer, by name
	Source   string    `json:"source,omitempty"` // local: a build made on this computer (firmware-builds)
	Done     int64     `json:"done,omitempty"`   // bytes of the current stage
	Total    int64     `json:"total,omitempty"`  // its size
	Chip     string    `json:"chip,omitempty"`   // what answered in install mode
	Steps    []string  `json:"steps,omitempty"`  // finished steps, in words
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitzero"`
}

type jobRun struct {
	job    Job
	cancel context.CancelFunc
}

type jobs struct {
	once sync.Once
	mu   sync.Mutex
	n    int
	all  map[string]*jobRun
	subs map[chan Job]bool
}

func (j *jobs) init() {
	j.once.Do(func() {
		j.all = map[string]*jobRun{}
		j.subs = map[chan Job]bool{}
	})
}

// start makes a job and runs fn in the background with a context that
// cancel ends. fn reports through up and returns the job's outcome.
func (j *jobs) start(base Job, fn func(ctx context.Context, up func(func(*Job))) error) Job {
	j.init()
	ctx, cancel := context.WithCancel(context.Background())
	j.mu.Lock()
	j.n++
	base.ID = strconv.FormatInt(time.Now().Unix()%100000, 36) + "-" + strconv.Itoa(j.n)
	base.State, base.Started = JobRunning, time.Now().UTC()
	run := &jobRun{job: base, cancel: cancel}
	j.all[base.ID] = run
	j.trim()
	j.mu.Unlock()
	j.publish(base)
	up := func(fn func(*Job)) {
		j.mu.Lock()
		fn(&run.job)
		cp := run.job
		j.mu.Unlock()
		j.publish(cp)
	}
	go func() {
		defer cancel()
		err := fn(ctx, up)
		up(func(job *Job) {
			job.Finished = time.Now().UTC()
			switch {
			case err == nil:
				job.State, job.Percent = JobDone, 100
			case ctx.Err() != nil:
				job.State, job.Error = JobCancelled, "stopped"
				job.Message = "Stopped. If the board stays dark, install again."
			default:
				job.State, job.Error = JobFailed, err.Error()
			}
		})
	}()
	return base
}

func (j *jobs) trim() {
	if len(j.all) <= 50 {
		return
	}
	var old []*jobRun
	for _, r := range j.all {
		if r.job.State != JobRunning {
			old = append(old, r)
		}
	}
	sort.Slice(old, func(a, b int) bool { return old[a].job.Started.Before(old[b].job.Started) })
	for _, r := range old[:max(0, len(j.all)-50)] {
		delete(j.all, r.job.ID)
	}
}

func (j *jobs) get(id string) (Job, bool) {
	j.init()
	j.mu.Lock()
	defer j.mu.Unlock()
	r, ok := j.all[id]
	if !ok {
		return Job{}, false
	}
	return r.job, true
}

func (j *jobs) list() []Job {
	j.init()
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]Job, 0, len(j.all))
	for _, r := range j.all {
		out = append(out, r.job)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Started.After(out[b].Started) })
	return out
}

func (j *jobs) cancel(id string) bool {
	j.init()
	j.mu.Lock()
	defer j.mu.Unlock()
	r, ok := j.all[id]
	if !ok || r.job.State != JobRunning {
		return false
	}
	r.cancel()
	return true
}

func (j *jobs) publish(job Job) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for ch := range j.subs {
		select {
		case ch <- job:
		default: // a slow reader misses a step; the next one carries the state
		}
	}
}

func (j *jobs) subscribe() (chan Job, func()) {
	j.init()
	ch := make(chan Job, 32)
	j.mu.Lock()
	j.subs[ch] = true
	j.mu.Unlock()
	return ch, func() {
		j.mu.Lock()
		delete(j.subs, ch)
		j.mu.Unlock()
	}
}

// running is the job of this kind on this port or device, if one runs.
func (j *jobs) running(match func(Job) bool) (Job, bool) {
	j.init()
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, r := range j.all {
		if r.job.State == JobRunning && match(r.job) {
			return r.job, true
		}
	}
	return Job{}, false
}

// ---- HTTP -------------------------------------------------------------------------

func (a *Admin) listJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"jobs": a.jobs.list()})
}

func (a *Admin) getJob(w http.ResponseWriter, r *http.Request) {
	j, ok := a.jobs.get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "no such job (jobs are kept until the service restarts)")
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (a *Admin) cancelJob(w http.ResponseWriter, r *http.Request) {
	if !a.jobs.cancel(r.PathValue("id")) {
		writeErr(w, http.StatusConflict, "that job isn't running")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// events: GET /admin/v1/events, Server-Sent Events. "job" carries a Job
// whenever one changes; "system" carries System every 10 seconds and after a
// job ends. It is a stream of its own, not /v1/events: that one also goes to
// paired devices on the network and over Deck Link, and the management API
// never does.
func (a *Admin) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	ch, unsub := a.jobs.subscribe()
	defer unsub()
	rc := http.NewResponseController(w)
	send := func(event string, v any) bool {
		b, _ := json.Marshal(v)
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	if !send("system", a.SystemNow()) {
		return
	}
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case j := <-ch:
			if !send("job", j) {
				return
			}
			if j.State != JobRunning && !send("system", a.SystemNow()) {
				return
			}
		case <-tick.C:
			if !send("system", a.SystemNow()) {
				return
			}
		}
	}
}
