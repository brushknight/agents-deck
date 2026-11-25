package admin

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// Diagnostics is GET /admin/v1/diagnostics: the report behind Copy a report,
// and the tail of the log. Both leave out project names, folders, prompts,
// agent and device names, user and host names, networks and addresses:
// only versions, the platform, counts and board models stay.
type Diagnostics struct {
	Report  string    `json:"report"`
	Log     []string  `json:"log"`
	Started time.Time `json:"started"`
	Uptime  string    `json:"uptime"`
	Agents  int       `json:"agents"`
	Online  int       `json:"boardsOnline"`
	Boards  int       `json:"boards"`
	// Where the service keeps its files: shown on this computer only, never
	// part of the report.
	DataDir string `json:"dataDir"`
	LogFile string `json:"logFile"`
}

const logLines = 40

func (a *Admin) diagnostics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.Diagnose())
}

// cpu: GET /admin/v1/cpu[?top=N], the service's CPU use and its helpers',
// the whole machine's, and with top its N biggest users.
func (a *Admin) cpu(w http.ResponseWriter, r *http.Request) {
	if a.CPU == nil {
		writeErr(w, http.StatusNotFound, "this service doesn't count its CPU use")
		return
	}
	var n int
	if q := r.URL.Query().Get("top"); q != "" {
		var err error
		if n, err = strconv.Atoi(q); err != nil || n < 1 {
			writeErr(w, http.StatusBadRequest, "top: how many users to list, 1 or more")
			return
		}
	}
	rep := a.CPU()
	if n > 0 {
		top := proc.CPUTop{Why: "this service doesn't list who uses the CPU", Users: []proc.CPUUser{}}
		if a.CPUTop != nil {
			top = a.CPUTop(min(n, 50))
		}
		rep.Top = &top
	}
	writeJSON(w, http.StatusOK, rep)
}

// Diagnose builds the report.
func (a *Admin) Diagnose() Diagnostics {
	red := a.redactor()
	d := Diagnostics{Started: a.Started.UTC(), Uptime: uptime(time.Since(a.Started)), DataDir: a.DataDir, LogFile: a.LogFile}
	tools := map[string]int{}
	if a.Store != nil {
		for _, ag := range a.Store.Snapshot().Agents {
			d.Agents++
			t := ag.Tool
			if ag.App != "" {
				t = ag.App
			}
			tools[t]++
		}
	}
	models := map[string]int{}
	pending := 0
	for _, row := range a.deviceRows() {
		if row.State == "revoked" {
			continue
		}
		d.Boards++
		if row.Online {
			d.Online++
		}
		if row.State == "waiting" {
			pending++
		}
		m := "phone or panel"
		if row.Kind == "serial" {
			m = row.Model
			if m == "" {
				m = row.Type
			}
			if m == "" {
				m = "board"
			}
			if row.Firmware != "" {
				m += " firmware " + row.Firmware
			}
			if row.Connection != "" {
				m += " (" + row.Connection + ")"
			}
		}
		models[m]++
	}
	portCount := map[string]int{}
	for _, p := range a.portRows() {
		portCount[p.State]++
	}
	u := a.updaterState()

	var b strings.Builder
	fmt.Fprintf(&b, "agents deck diagnostics, %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "version:   %s (installed as %s)\n", a.Version, a.Install)
	fmt.Fprintf(&b, "platform:  %s, %s\n", a.Platform, runtime.Version())
	fmt.Fprintf(&b, "up for:    %s\n", d.Uptime)
	fmt.Fprintf(&b, "agents:    %d%s\n", d.Agents, counts(tools))
	fmt.Fprintf(&b, "boards:    %d online of %d, %d waiting for approval\n", d.Online, d.Boards, pending)
	for _, m := range sortedKeys(models) {
		fmt.Fprintf(&b, "           %d × %s\n", models[m], m)
	}
	ports := strings.Trim(counts(portCount), " ()")
	if ports == "" {
		ports = "none"
	}
	fmt.Fprintf(&b, "ports:     %s\n", ports)
	net := "off"
	if a.NetworkOn {
		net = "on"
	}
	fmt.Fprintf(&b, "network:   %s\n", net)
	upd := u.State
	if u.Version != "" {
		upd += ", latest " + u.Version
	}
	if u.Error != "" {
		upd += ", last check failed: " + red.clean(u.Error)
	}
	fmt.Fprintf(&b, "updates:   %s (mode %s, release key %s)\n", upd, a.updateMode(), yesNo(a.releaseKey()))
	if cat, _ := a.cachedCatalog(); cat != nil {
		fmt.Fprintf(&b, "firmware:  release %s, %d boards\n", cat.Version, len(cat.Boards))
	}
	// Everything above is versions, counts and board models; only the log
	// (and the error above) can hold names, so only they are redacted.
	d.Log = a.logTail(red)
	if len(d.Log) > 0 {
		fmt.Fprintf(&b, "\nlast %d lines of the log:\n", len(d.Log))
		for _, l := range d.Log {
			b.WriteString(l + "\n")
		}
	}
	d.Report = b.String()
	return d
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func counts(m map[string]int) string {
	if len(m) == 0 {
		return ""
	}
	var parts []string
	for _, k := range sortedKeys(m) {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func uptime(d time.Duration) string {
	d = d.Round(time.Minute)
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// logTail is the last lines of the log, redacted.
func (a *Admin) logTail(red redactor) []string {
	out := []string{}
	if a.LogFile == "" {
		return out
	}
	f, err := os.Open(a.LogFile)
	if err != nil {
		return out
	}
	defer f.Close()
	const window = 64 << 10
	if fi, err := f.Stat(); err == nil && fi.Size() > window {
		_, _ = f.Seek(fi.Size()-window, io.SeekStart)
	}
	b, _ := io.ReadAll(io.LimitReader(f, window))
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(b) == window && len(lines) > 1 {
		lines = lines[1:] // the first one is cut
	}
	if len(lines) > logLines {
		lines = lines[len(lines)-logLines:]
	}
	for _, l := range lines {
		if l = strings.TrimSpace(red.clean(l)); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// ---- redaction ---------------------------------------------------------------------

type redactor struct {
	words []string // replaced wherever they appear, longest first
}

var (
	quotedRe = regexp.MustCompile(`"[^"\n]*"|“[^”\n]*”|«[^»\n]*»`)
	pathRe   = regexp.MustCompile(`(?:~|[A-Za-z]:)?(?:[/\\][^/\\\s:"',;()\[\]]+){2,}[/\\]?`)
	ipv4Re   = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?\b`)
	ipv6Re   = regexp.MustCompile(`\b(?:[0-9a-fA-F]{1,4}:){3,7}[0-9a-fA-F]{1,4}\b|\bfe80::[0-9a-fA-F:]+`)
	macRe    = regexp.MustCompile(`\b(?:[0-9a-fA-F]{2}[:-]){5}[0-9a-fA-F]{2}\b`)
	emailRe  = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`)
	localRe  = regexp.MustCompile(`\b[\w-]+\.local\b`)
)

// keptPaths are API paths and device files, not anyone's folders.
var keptPaths = []string{"/v1/", "/admin/v1/", "/local/", "/dev/", "/login", "/hook", "/settings"}

// redactor knows the names to leave out: the user and the host, every
// agent's title, folder, branch and prompt, every device's name, accounts
// and Wi-Fi networks.
func (a *Admin) redactor() redactor {
	set := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if len([]rune(s)) >= 3 {
			set[s] = true
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(home)
	}
	if u, err := user.Current(); err == nil {
		add(u.Username)
		add(u.Name)
		add(u.HomeDir)
	}
	add(os.Getenv("USER"))
	add(os.Getenv("USERNAME"))
	if h, err := os.Hostname(); err == nil {
		add(h)
		add(strings.TrimSuffix(h, ".local"))
	}
	if a.Store != nil {
		st := a.Store.Snapshot()
		add(st.Server.Name)
		for _, ag := range st.Agents {
			add(ag.Title)
			add(ag.AITitle)
			add(ag.Cwd)
			add(ag.Folder)
			add(ag.Branch)
			add(ag.LastPrompt)
			if ag.Waiting != nil {
				add(ag.Waiting.Title)
				add(ag.Waiting.Detail)
				add(ag.Waiting.Context)
			}
		}
	}
	if a.Devices != nil {
		for _, d := range a.Devices.List() {
			add(d.Name)
			add(d.BoardID)
		}
	}
	for _, n := range a.Labels() {
		add(n)
	}
	if accounts.InUse() {
		for _, ac := range accounts.List() {
			add(ac.Name)
			add(ac.Dir)
		}
	}
	if a.WifiSSID != nil {
		add(a.WifiSSID())
	}
	for _, l := range a.linksNow() {
		if l.Net != nil {
			add(l.Net.SSID)
		}
	}
	r := redactor{}
	for w := range set {
		r.words = append(r.words, w)
	}
	sort.Slice(r.words, func(i, j int) bool { return len(r.words[i]) > len(r.words[j]) })
	return r
}

func (r redactor) clean(s string) string {
	s = quotedRe.ReplaceAllString(s, `"…"`)
	s = emailRe.ReplaceAllString(s, "<email>")
	s = pathRe.ReplaceAllStringFunc(s, func(p string) string {
		if strings.Trim(p, "0123456789/") == "" {
			return p // a date in the log's prefix
		}
		for _, k := range keptPaths {
			if strings.HasPrefix(p, k) || p+"/" == k {
				return p
			}
		}
		return "<path>"
	})
	for _, w := range r.words {
		s = replaceFold(s, w, "<name>")
	}
	s = macRe.ReplaceAllString(s, "<mac>")
	s = ipv6Re.ReplaceAllString(s, "<address>")
	s = ipv4Re.ReplaceAllString(s, "<address>")
	s = localRe.ReplaceAllString(s, "<host>")
	return s
}

// replaceFold replaces every case-insensitive occurrence of old.
func replaceFold(s, old, repl string) string {
	if old == "" {
		return s
	}
	lo, lold := strings.ToLower(s), strings.ToLower(old)
	if len(lo) != len(s) || len(lold) != len(old) {
		return strings.ReplaceAll(s, old, repl) // case folding changed the length: plain match
	}
	var b strings.Builder
	for {
		i := strings.Index(lo, lold)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString(repl)
		s, lo = s[i+len(old):], lo[i+len(old):]
	}
}
