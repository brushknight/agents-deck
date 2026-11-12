package ble

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link"
)

// Uplinks runs a Deck Link session over Bluetooth for every board you added
// there (devices.Device.BLE), when its uplink preference lets Bluetooth be
// the one in use. A board seen over Bluetooth is never connected to, let
// alone approved, on sight: Add is the only way in, and it needs the passkey
// on the board's own screen.
type Uplinks struct {
	Central *Central
	Deck    link.Deck
	Hub     *link.Hub
	Devices *devices.Registry
	Audit   func(devices.AuditEntry) // optional
	Every   time.Duration            // how often it looks at the registry (2 s)
	// Tune adjusts each session before it runs (tests shorten its timings).
	Tune func(*link.Session)

	mu    sync.Mutex
	runs  map[string]*run // by Bluetooth identifier
	retry map[string]time.Time
	fails map[string]int
	known map[string]bool   // identifiers bound to a device last time
	why   map[string]string // the last attempt's problem, by identifier, until a Hello
}

type run struct {
	id     string
	device string // "" while it is being added
	adding bool
	cancel context.CancelFunc
	done   chan struct{}
	state  string // connecting | connected
	since  time.Time
	hello  bool  // the board said Hello in this run
	err    error // why the session ended
}

func (u *Uplinks) init() {
	if u.runs == nil {
		u.runs, u.retry, u.fails, u.known, u.why = map[string]*run{}, map[string]time.Time{}, map[string]int{}, map[string]bool{}, map[string]string{}
	}
}

func (u *Uplinks) audit(e devices.AuditEntry) {
	if u.Audit != nil {
		u.Audit(e)
	}
}

// Run follows the registry until ctx ends.
func (u *Uplinks) Run(ctx context.Context) {
	every := u.Every
	if every <= 0 {
		every = 2 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		u.reconcile(ctx)
		select {
		case <-ctx.Done():
			u.mu.Lock()
			for _, r := range u.runs {
				r.cancel()
			}
			u.mu.Unlock()
			return
		case <-t.C:
		}
	}
}

// reconcile starts and stops links to match the registry and the boards'
// other uplinks.
func (u *Uplinks) reconcile(ctx context.Context) {
	if u.Central == nil || u.Devices == nil {
		return
	}
	bound := map[string]devices.Device{}
	for _, d := range u.Devices.List() {
		if d.Kind == devices.Serial && d.BLE != "" {
			bound[d.BLE] = d
		}
	}
	var forget []string
	u.mu.Lock()
	u.init()
	for id, r := range u.runs {
		select {
		case <-r.done:
			delete(u.runs, id)
			if r.hello {
				u.fails[id] = 0
			} else {
				u.fails[id]++
			}
			u.retry[id] = time.Now().Add(backoff(u.fails[id]))
			continue
		default:
		}
		if r.adding {
			continue
		}
		d, ok := bound[id]
		switch {
		case !ok || d.Name != r.device: // revoked, removed, or bound to another board
			log.Printf("bluetooth: %s is no longer one of your boards here: disconnecting", r.device)
			r.cancel()
			if !ok {
				forget = append(forget, id)
				delete(u.known, id)
			}
		case !d.Active():
			r.cancel()
		case !link.UplinkAllowed(d, link.OverBLE) || u.Hub.Preferred(d.Name, link.OverBLE, d.Uplink) != "":
			r.cancel() // its preference leaves Bluetooth out, or a better uplink is in use
		}
	}
	for id := range u.known {
		if _, ok := bound[id]; !ok {
			forget = append(forget, id)
			delete(u.known, id)
		}
	}
	for id := range bound {
		u.known[id] = true
	}
	var start []devices.Device
	for id, d := range bound {
		if u.runs[id] != nil || !d.Active() || time.Now().Before(u.retry[id]) {
			continue
		}
		if !link.UplinkAllowed(d, link.OverBLE) || u.Hub.Preferred(d.Name, link.OverBLE, d.Uplink) != "" {
			continue
		}
		start = append(start, d)
	}
	u.mu.Unlock()
	for _, id := range forget {
		u.Central.Forget(id)
	}
	if !u.Central.Status().Available {
		return
	}
	for _, d := range start {
		u.connect(ctx, d)
	}
}

func backoff(fails int) time.Duration {
	d := 2 * time.Second
	for i := 0; i < fails && d < time.Minute; i++ {
		d *= 2
	}
	return min(d, time.Minute)
}

// claim registers a run for id, or says why there can't be one.
func (u *Uplinks) claim(ctx context.Context, id, device string, adding bool) (*run, context.Context, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.init()
	if r := u.runs[id]; r != nil {
		select {
		case <-r.done:
		default:
			if r.adding {
				return nil, nil, errors.New("that board is being added already")
			}
			return nil, nil, errors.New("that board is connected already")
		}
	}
	rctx, cancel := context.WithCancel(ctx)
	r := &run{id: id, device: device, adding: adding, cancel: cancel, done: make(chan struct{}), state: "connecting", since: time.Now()}
	u.runs[id] = r
	return r, rctx, nil
}

// connect runs a known board's link in the background: connect (waiting for
// it to come in range), then its Deck Link session.
func (u *Uplinks) connect(ctx context.Context, d devices.Device) {
	r, rctx, err := u.claim(ctx, d.BLE, d.Name, false)
	if err != nil {
		return
	}
	go func() {
		defer close(r.done)
		defer r.cancel()
		st, err := u.Central.Connect(rctx, d.BLE, nil)
		if err != nil {
			if rctx.Err() == nil {
				log.Printf("bluetooth: %s: %v", d.Name, err)
				u.mu.Lock()
				u.why[d.BLE] = err.Error() // "the board no longer knows this computer: …" and the like
				u.mu.Unlock()
			}
			return
		}
		u.session(rctx, r, st, bleDeck{Deck: u.Deck, u: u, r: r, boardID: d.BoardID})
	}()
}

// session runs Deck Link on a connected board until it ends.
func (u *Uplinks) session(ctx context.Context, r *run, st *Stream, deck bleDeck) {
	var err error
	defer func() {
		u.mu.Lock()
		r.err = err
		u.mu.Unlock()
	}()
	u.mu.Lock()
	r.state, r.since = "connected", time.Now()
	u.mu.Unlock()
	name := r.device
	if name == "" {
		name = st.ID
	}
	s := link.NewSession(deck, st, name+"@ble")
	s.Hub, s.Transport, s.Where, s.IdleTimeout = u.Hub, link.OverBLE, "bluetooth", link.NetworkIdle
	if u.Hub != nil {
		s.Wifi = u.Hub.Wifi // a board paired with its passkey is in hand: it may get the default network
	}
	if n, ok := u.Central.Seen(st.ID); ok && n.Name != "" {
		s.Where = "bluetooth " + n.Name
	}
	if u.Tune != nil {
		u.Tune(s)
	}
	log.Printf("bluetooth: %s connected", name)
	err = s.Run(ctx)
	_ = st.Close()
	if ctx.Err() == nil {
		log.Printf("bluetooth: %s left (%v)", name, err)
	}
}

// bleDeck is the Deck for a session over Bluetooth: the board must say Hello
// with the board id it was added with; while it is being added, its Hello
// binds it to this Bluetooth identifier and approves it, unless the board
// has a live session on USB or Wi-Fi: then that session must confirm the
// link (Uplinks.Add) and this Hello is turned away.
type bleDeck struct {
	link.Deck
	u       *Uplinks
	r       *run
	boardID string // what it must say in Hello
	add     *adding
}

type adding struct {
	scope devices.Scope
	hello chan devices.Device
}

func (b bleDeck) Board(boardID, typ, firmware string) (devices.Device, error) {
	if b.boardID != "" && boardID != b.boardID {
		return devices.Device{}, fmt.Errorf("this Bluetooth link belongs to board %q, but the board said Hello as %q", b.boardID, boardID)
	}
	if b.add != nil {
		if d, ok := b.u.Devices.ByBoardID(boardID); ok {
			if via, _ := b.u.Hub.BLEConfirm(d.Name, time.Time{}); via != "" {
				return devices.Device{}, fmt.Errorf("%s is connected over %s: the board confirms this Bluetooth link there", d.Name, viaWords(via))
			}
		}
	}
	d, err := b.Deck.Board(boardID, typ, firmware)
	if err != nil {
		return d, err
	}
	b.u.mu.Lock()
	b.r.hello = true
	delete(b.u.why, b.r.id)
	if b.add != nil {
		b.r.device = d.Name
	}
	b.u.mu.Unlock()
	if b.add == nil {
		return d, nil
	}
	nd, err := b.u.bind(d, b.r, b.add.scope, "its Hello over the encrypted link")
	if err != nil {
		return d, err
	}
	select {
	case b.add.hello <- nd:
	default:
	}
	return nd, nil
}

func viaWords(t string) string {
	switch t {
	case link.OverSerial:
		return "USB"
	case link.OverWifi:
		return "Wi-Fi"
	}
	return t
}

// bind makes d one of your boards over Bluetooth with the identifier of r:
// bound to it, approved with scope (unless it was approved before), noted
// in the audit log.
func (u *Uplinks) bind(d devices.Device, r *run, scope devices.Scope, how string) (devices.Device, error) {
	if err := u.Devices.SetBLE(d.Name, r.id); err != nil {
		return d, err
	}
	nd, approved, err := u.Devices.ApproveAs(d.Name, scope)
	if err != nil {
		return d, err
	}
	if approved {
		u.audit(devices.AuditEntry{Device: nd.Name, Action: "approve", Status: http.StatusOK,
			Detail: "added over Bluetooth with the passkey on its screen, scope " + string(nd.Scope)})
	}
	u.audit(devices.AuditEntry{Device: "local", Action: "bluetooth add " + nd.Name, Status: http.StatusOK,
		Detail: "board id " + d.BoardID + ", confirmed by " + how})
	u.mu.Lock()
	r.adding, r.hello, r.device = false, true, nd.Name
	delete(u.why, r.id)
	u.mu.Unlock()
	return nd, nil
}

// confirmed binds the board added on r when its live USB or Wi-Fi session
// confirms the Bluetooth link: the board reported a secure central there
// since the attempt began. ok is false while it hasn't (or it has no such
// session; via says which).
func (u *Uplinks) confirmed(boardID string, r *run, since time.Time, scope devices.Scope) (d devices.Device, via string, ok bool, err error) {
	d, found := u.Devices.ByBoardID(boardID)
	if !found {
		return d, "", false, nil
	}
	via, yes := u.Hub.BLEConfirm(d.Name, since)
	if !yes {
		return d, via, false, nil
	}
	d, err = u.bind(d, r, scope, "its "+viaWords(via)+" session (NetStatus: Bluetooth connected)")
	return d, via, err == nil, err
}

// Progress is told each step of adding a board.
type Progress func(stage, message string)

// AddTimeout bounds adding a board: connecting, the person typing the code,
// and the board's confirmation.
var AddTimeout = 3 * time.Minute

// Add connects to a board seen in the scan and lets the computer pair with
// it (the person types the code shown on the board; a board this computer
// is still paired with needs none). Once the link is encrypted the helper
// reads the board's Info again over it; the board id there must be the one
// it advertised. Then the board is bound to this Bluetooth identifier and
// approved with scope, on what the encrypted link proves:
//
//   - a board with a live session on USB or Wi-Fi (its certificate) keeps
//     Bluetooth for an introduction: that session must report the secure
//     central (NetStatus.ble CONNECTED) after the attempt began. No session
//     runs over Bluetooth then; the link closes, and Bluetooth stands by.
//   - any other board says Hello over the encrypted link, and its session
//     carries on over Bluetooth.
//
// A revoked board is refused: approve it first. Stages: connect, passkey (only
// when the board is in setup), connected, done.
func (u *Uplinks) Add(ctx context.Context, id string, scope devices.Scope, prog Progress) (devices.Device, error) {
	if prog == nil {
		prog = func(string, string) {}
	}
	if u == nil || u.Central == nil {
		return devices.Device{}, ErrUnavailable
	}
	if err := u.Central.usable(); err != nil {
		return devices.Device{}, err
	}
	if scope != devices.View && scope != devices.Act {
		return devices.Device{}, errors.New("a board added over Bluetooth gets view or act; admin is given by hand (agentctl devices scope)")
	}
	n, ok := u.Central.Seen(id)
	if !ok {
		return devices.Device{}, errors.New("no such board nearby: scan first (agentctl bluetooth scan)")
	}
	// A scan reads what a board is on a short connection without pairing. A
	// board that is already paired with this computer doesn't answer that
	// (seen on a real board), so its id may be unknown here: then it is taken
	// from the read made over the encrypted link, which is the one that counts.
	boardID := n.BoardID
	known := func(boardID string) (devices.Device, error) {
		if d, ok := u.Devices.ByBoardID(boardID); ok && d.Revoked {
			return devices.Device{}, fmt.Errorf("%s was revoked: agentctl devices approve %s first, then add it again", d.Name, d.Name)
		}
		if d, ok := u.Devices.ByBLE(id); ok && d.BoardID == boardID && !d.Revoked {
			return d, fmt.Errorf("%s is already one of your boards over Bluetooth", d.Name)
		}
		return devices.Device{}, nil
	}
	if boardID != "" {
		if d, err := known(boardID); err != nil {
			return d, err
		}
	}
	name := orText(n.Name, "the board")
	ctx, cancel := context.WithTimeout(ctx, AddTimeout)
	defer cancel()
	r, rctx, err := u.claim(context.Background(), id, "", true)
	if err != nil {
		return devices.Device{}, err
	}
	stop := context.AfterFunc(ctx, r.cancel) // a cancelled job ends the attempt; a finished one leaves the link up
	defer stop()
	since := time.Now()
	prog("connect", "Connecting to "+name)
	var asked atomic.Bool
	// The helper says "passkey" once it is connected, has read Info (the board
	// is in setup) and asks for TX: macOS opens its window for the code now,
	// and the board shows it.
	st, err := u.Central.Connect(rctx, id, func(string) {
		asked.Store(true)
		prog("passkey", "Type the code shown on "+name+" into the window this computer opens.")
	})
	if err != nil {
		r.cancel()
		close(r.done)
		u.Central.Forget(id)
		return devices.Device{}, addFailed(ctx, err, asked.Load(), n.Setup)
	}
	fail := func(err error) (devices.Device, error) {
		r.cancel()
		<-r.done
		u.Central.Forget(id)
		return devices.Device{}, err
	}
	if boardID == "" {
		if st.BoardID == "" {
			go func() { defer close(r.done); _ = st.Close() }()
			return fail(errors.New("couldn't read what " + name + " is, even over the encrypted link: bring it closer and try again"))
		}
		boardID = st.BoardID
		if d, err := known(boardID); err != nil {
			go func() { defer close(r.done); _ = st.Close() }()
			r.cancel()
			<-r.done
			return d, err
		}
	}
	if st.BoardID != "" && st.BoardID != boardID {
		go func() { defer close(r.done); _ = st.Close() }()
		return fail(fmt.Errorf("over the encrypted link the board says it is %q, but it advertised %q", st.BoardID, boardID))
	}
	if asked.Load() {
		prog("connected", "Paired. Checking with the board.")
	} else {
		prog("connected", "Connected with the pairing this computer already has. Checking with the board.")
	}
	add := &adding{scope: scope, hello: make(chan devices.Device, 1)}
	go func() {
		defer close(r.done)
		defer r.cancel()
		u.session(rctx, r, st, bleDeck{Deck: u.Deck, u: u, r: r, boardID: boardID, add: add})
	}()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case d := <-add.hello:
			stop()
			prog("done", d.Name+" is one of your boards now, connected over Bluetooth.")
			return d, nil
		case <-tick.C:
			d, via, ok, err := u.confirmed(boardID, r, since, scope)
			if err != nil {
				return fail(err)
			}
			if ok {
				r.cancel() // the board uses another link: Bluetooth stands by
				<-r.done
				prog("done", d.Name+" is one of your boards now. It uses "+viaWords(via)+
					" now; Bluetooth takes over when its uplink preference says so (agentctl devices uplink "+d.Name+" ble).")
				return d, nil
			}
		case <-r.done:
			// The session ended: maybe the board confirmed elsewhere just before.
			if d, via, ok, err := u.confirmed(boardID, r, since, scope); err == nil && ok {
				prog("done", d.Name+" is one of your boards now. It uses "+viaWords(via)+" now.")
				return d, nil
			}
			u.Central.Forget(id)
			u.mu.Lock()
			err := r.err
			u.mu.Unlock()
			if ctx.Err() != nil {
				return devices.Device{}, u.unconfirmed(ctx, boardID)
			}
			if err != nil {
				return devices.Device{}, fmt.Errorf("not added: %v", err)
			}
			return devices.Device{}, errors.New("the board dropped the Bluetooth link before it confirmed")
		case <-ctx.Done():
			r.cancel()
			<-r.done
			u.Central.Forget(id)
			return devices.Device{}, u.unconfirmed(ctx, boardID)
		}
	}
}

// unconfirmed: the link came up but the board neither said Hello over it nor
// confirmed it on its other session in time.
func (u *Uplinks) unconfirmed(ctx context.Context, boardID string) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return errors.New("cancelled")
	}
	if d, ok := u.Devices.ByBoardID(boardID); ok {
		if via, _ := u.Hub.BLEConfirm(d.Name, time.Time{}); via != "" {
			return fmt.Errorf("timed out: paired, but %s didn't confirm the Bluetooth link over %s (its firmware may be older than this computer's agents deck: update it and add it again)", d.Name, viaWords(via))
		}
	}
	return errors.New("timed out: paired, but the board didn't start a session over Bluetooth (with its uplink preference wifi it keeps Bluetooth for setup only: plug it in with a USB cable and add it again)")
}

// addFailed says why connecting and pairing failed, from the person's side.
func addFailed(ctx context.Context, err error, asked, setup bool) error {
	var le *LinkError
	if errors.As(err, &le) {
		switch le.Code {
		case "pairing-failed":
			if asked {
				return errors.New("pairing failed: the code typed didn't match the one on the board, or pairing was cancelled")
			}
			if !setup {
				return errors.New("the board refused this computer: it is paired with another computer, or it forgot this one. " +
					"Put it back in setup (hold BOOT for 5 seconds, then press it again), remove it in System Settings, Bluetooth if it is listed there, and add it again")
			}
		case "pairing-timeout":
			return errors.New("pairing timed out: the code shown on the board wasn't typed in time")
		case "gone":
			if asked {
				return errors.New("the board dropped the Bluetooth link before pairing finished")
			}
			return errors.New("the board dropped the Bluetooth link")
		}
		return err
	}
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return errors.New("cancelled")
	case ctx.Err() != nil && asked:
		return errors.New("timed out: the code shown on the board wasn't typed")
	case ctx.Err() != nil:
		return errors.New("timed out connecting: the board isn't advertising or is out of range (is it on and nearby? scan again)")
	}
	return err
}

// LinkState is a board's Bluetooth link, for the device list.
type LinkState struct {
	ID    string    `json:"id"`
	State string    `json:"state"` // connected | connecting (waiting for it to come in range) | standing by (another uplink is in use) | not used (its preference) | off (Bluetooth isn't available)
	Uses  string    `json:"uses,omitempty"`
	Since time.Time `json:"since,omitzero"`
	// Problem: why the last attempt failed, until the board says Hello again
	// (the helper's words, such as "the board no longer knows this computer:
	// … remove it in System Settings, Bluetooth …").
	Problem string `json:"problem,omitempty"`
}

// State is the board's Bluetooth link ("" state when it has none).
func (u *Uplinks) State(d devices.Device) LinkState {
	if u == nil || d.BLE == "" {
		return LinkState{}
	}
	ls := LinkState{ID: d.BLE}
	u.mu.Lock()
	ls.Problem = u.why[d.BLE]
	r := u.runs[d.BLE]
	if r != nil {
		select {
		case <-r.done:
			r = nil
		default:
			ls.State, ls.Since = r.state, r.since
		}
	}
	u.mu.Unlock()
	if r != nil {
		return ls
	}
	if !link.UplinkAllowed(d, link.OverBLE) {
		ls.State = "not used"
		return ls
	}
	if uses := u.Hub.Preferred(d.Name, link.OverBLE, d.Uplink); uses != "" {
		ls.State, ls.Uses = "standing by", uses
		return ls
	}
	if u.Central == nil || !u.Central.Status().Available {
		ls.State = "off"
		return ls
	}
	ls.State = "connecting"
	return ls
}

// Status is Bluetooth on this computer.
func (u *Uplinks) Status() Status {
	if u == nil || u.Central == nil {
		return Status{Helper: "none", Why: NotHere}
	}
	return u.Central.Status()
}

// Scan starts or stops looking for boards (Central.Scan).
func (u *Uplinks) Scan(on bool) error {
	if u == nil || u.Central == nil {
		if !on {
			return nil
		}
		return ErrUnavailable
	}
	return u.Central.Scan(on)
}

// Nearby lists the boards seen since the scan started (Central.Nearby).
func (u *Uplinks) Nearby() []Nearby {
	if u == nil || u.Central == nil {
		return nil
	}
	return u.Central.Nearby()
}

// Seen is a board from the scan, by identifier (Central.Seen).
func (u *Uplinks) Seen(id string) (Nearby, bool) {
	if u == nil || u.Central == nil {
		return Nearby{}, false
	}
	return u.Central.Seen(id)
}
