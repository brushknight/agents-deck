package service

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// fakeRun records commands and answers from a table (default: success).
type fakeRun struct {
	calls []string
	fail  map[string]bool // "launchctl print" → fails
}

func (f *fakeRun) run(name string, args ...string) ([]byte, error) {
	c := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, c)
	for prefix := range f.fail {
		if strings.HasPrefix(c, prefix) {
			return []byte("refused"), errors.New("exit status 1")
		}
	}
	return nil, nil
}

func TestLaunchdPlistUnchanged(t *testing.T) {
	l := &Launchd{Exe: "/Users/sam/.local/bin/agentctl", Home: "/Users/sam", Log: "/Users/sam/.local/share/agents-terminal/agentd.log"}
	want := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>dev.agents-deck.agentctl</string>
	<key>ProgramArguments</key>
	<array><string>/Users/sam/.local/bin/agentctl</string><string>serve</string></array>
	<key>EnvironmentVariables</key>
	<dict><key>PATH</key><string>/Users/sam/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string></dict>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>ProcessType</key><string>Interactive</string>
	<key>StandardOutPath</key><string>/Users/sam/.local/share/agents-terminal/agentd.log</string>
	<key>StandardErrorPath</key><string>/Users/sam/.local/share/agents-terminal/agentd.log</string>
</dict>
</plist>
`
	if got := l.Plist(); got != want {
		t.Fatalf("the LaunchAgent changed (macOS would announce it again):\n%s", got)
	}
}

func TestLaunchdInstallUninstall(t *testing.T) {
	home := t.TempDir()
	f := &fakeRun{fail: map[string]bool{"launchctl print": true}}
	l := &Launchd{Exe: "/opt/agentctl", Home: home, Log: "/tmp/x.log", UID: 501, Run: f.run}
	var out bytes.Buffer
	if err := l.Install(&out); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"launchctl bootout gui/501/dev.agents-deck.agentctl",
		"launchctl bootstrap gui/501 " + l.PlistPath(),
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("first install ran %q", f.calls)
	}
	if b, err := os.ReadFile(l.PlistPath()); err != nil || string(b) != l.Plist() {
		t.Fatalf("plist: %v", err)
	}
	if r := l.Registration(); !r.Registered || r.Note == "" { // written, but print fails: not loaded
		t.Errorf("registration %+v", r)
	}

	// Same plist, loaded: only a restart (no new background item).
	f.calls, f.fail = nil, nil
	out.Reset()
	if err := l.Install(&out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls, []string{"launchctl print gui/501/dev.agents-deck.agentctl", "launchctl kickstart -k gui/501/dev.agents-deck.agentctl"}) {
		t.Fatalf("reinstall ran %q", f.calls)
	}
	if r := l.Registration(); !r.Registered || r.Note != "" {
		t.Errorf("registration %+v", r)
	}

	f.calls = nil
	out.Reset()
	if err := l.Uninstall(&out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls, []string{"launchctl print gui/501/dev.agents-deck.agentctl", "launchctl bootout gui/501/dev.agents-deck.agentctl"}) {
		t.Fatalf("uninstall ran %q", f.calls)
	}
	if _, err := os.Stat(l.PlistPath()); !os.IsNotExist(err) {
		t.Fatal("plist left behind")
	}
	f.fail = map[string]bool{"launchctl print": true}
	out.Reset()
	if err := l.Uninstall(&out); err != nil || !strings.Contains(out.String(), "not installed") {
		t.Fatalf("second uninstall: %v %q", err, out.String())
	}
}

func TestSystemdUnit(t *testing.T) {
	s := &Systemd{Exe: "/home/sam/.local/bin/agentctl", Home: "/home/sam"}
	want := `[Unit]
Description=agents deck daemon
After=default.target

[Service]
ExecStart=/home/sam/.local/bin/agentctl serve
Restart=on-failure
Environment=PATH=/home/sam/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin

[Install]
WantedBy=default.target
`
	if got := s.UnitFile(); got != want {
		t.Fatalf("unit:\n%s", got)
	}
	for in, want := range map[string]string{
		"/opt/agents deck/agentctl": `"/opt/agents deck/agentctl"`,
		"/opt/100%/agentctl":        "/opt/100%%/agentctl",
	} {
		if got := systemdArg(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

func TestSystemdInstallUninstall(t *testing.T) {
	home := t.TempDir()
	f := &fakeRun{}
	s := &Systemd{Exe: "/opt/agentctl", Home: home, Run: f.run, LookPath: func(string) (string, error) { return "/usr/bin/systemctl", nil }}
	var out bytes.Buffer
	if err := s.Install(&out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls, []string{
		"systemctl --user daemon-reload", "systemctl --user enable agents-deck.service", "systemctl --user restart agents-deck.service",
	}) {
		t.Fatalf("install ran %q", f.calls)
	}
	if !strings.Contains(out.String(), "running") {
		t.Errorf("out %q", out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".config", "systemd", "user", "agents-deck.service")); string(b) != s.UnitFile() {
		t.Fatal("unit not written")
	}
	if r := s.Registration(); !r.Registered || r.Note != "" {
		t.Errorf("registration %+v", r)
	}

	// systemd refuses (no user session): the unit stays, with how to start it.
	f.calls, f.fail = nil, map[string]bool{"systemctl --user enable": true}
	out.Reset()
	if err := s.Install(&out); err != nil || !strings.Contains(out.String(), "systemctl --user enable --now agents-deck.service") {
		t.Fatalf("fallback: %v %q", err, out.String())
	}

	f.calls, f.fail = nil, nil
	if err := s.Uninstall(&out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls, []string{"systemctl --user disable --now agents-deck.service", "systemctl --user daemon-reload"}) {
		t.Fatalf("uninstall ran %q", f.calls)
	}
	if r := s.Registration(); r.Registered {
		t.Error("still registered")
	}
}

func TestSystemdMissing(t *testing.T) {
	f := &fakeRun{}
	s := &Systemd{Exe: "/opt/agentctl", Home: t.TempDir(), Run: f.run, LookPath: func(string) (string, error) { return "", errors.New("not found") }}
	var out bytes.Buffer
	if err := s.Install(&out); err != nil || !strings.Contains(out.String(), "agentctl serve") || len(f.calls) != 0 {
		t.Fatalf("%v %q %q", err, out.String(), f.calls)
	}
	if _, err := os.Stat(s.UnitPath()); err == nil {
		t.Error("unit written without systemd")
	}
}

type fakeReg map[string]string

func (r fakeReg) Get(name string) (string, bool, error) { v, ok := r[name]; return v, ok, nil }
func (r fakeReg) Set(name, value string) error          { r[name] = value; return nil }
func (r fakeReg) Delete(name string) error              { delete(r, name); return nil }

func TestStartupEntry(t *testing.T) {
	reg := fakeReg{}
	var started [][]string
	running := false
	s := &Startup{Exe: `C:\Users\Sam Doe\AppData\Local\Programs\agents deck\agentctl.exe`,
		Log: `C:\Users\Sam Doe\AppData\Local\agents-deck\agentd.log`, SystemRoot: `C:\Windows`, Reg: reg,
		Start: func(argv []string) error { started = append(started, argv); running = true; return nil },
		Stop:  func() (bool, error) { was := running; running = false; return was, nil }}
	cmd := `C:\Windows\System32\conhost.exe --headless "C:\Users\Sam Doe\AppData\Local\Programs\agents deck\agentctl.exe" serve --log "C:\Users\Sam Doe\AppData\Local\agents-deck\agentd.log"`
	if got := s.Command(); got != cmd {
		t.Fatalf("command:\n%s", got)
	}
	if got := proc.SplitCommandLine(cmd); !reflect.DeepEqual(got[2:], s.ServeArgv()) {
		t.Fatalf("the command doesn't split back into serve's argv: %q", got)
	}
	var out bytes.Buffer
	if err := s.Install(&out); err != nil {
		t.Fatal(err)
	}
	if reg[RunValue] != cmd || len(started) != 1 || !reflect.DeepEqual(started[0], s.ServeArgv()) || !strings.Contains(out.String(), "installed") {
		t.Fatalf("install: %q %q %q", reg, started, out.String())
	}
	if r := s.Registration(); !r.Registered || r.Note != "" || r.Where != RunKey+`\`+RunValue {
		t.Errorf("registration %+v", r)
	}
	out.Reset()
	if err := s.Install(&out); err != nil || !strings.Contains(out.String(), "restarted") || len(started) != 2 {
		t.Fatalf("reinstall: %v %q", err, out.String())
	}
	reg[RunValue] = `C:\old\agentctl.exe serve`
	if r := s.Registration(); !r.Registered || !strings.Contains(r.Note, "another agentctl") {
		t.Errorf("stale entry: %+v", r)
	}
	out.Reset()
	if err := s.Uninstall(&out); err != nil || running || len(reg) != 0 || !strings.Contains(out.String(), "removed") {
		t.Fatalf("uninstall: %v running=%v %q %q", err, running, reg, out.String())
	}
	out.Reset()
	if err := s.Uninstall(&out); err != nil || !strings.Contains(out.String(), "not installed") {
		t.Fatalf("second uninstall: %v %q", err, out.String())
	}
}

func TestWinQuoteRoundTrip(t *testing.T) {
	argv := []string{`C:\a b\x.exe`, "", `tail\`, `sp ace\`, `q"uote`, `back\\"slash`, "plain"}
	if got := proc.SplitCommandLine(winJoin(argv)); !reflect.DeepEqual(got, argv) {
		t.Fatalf("%s → %q", winJoin(argv), got)
	}
}

func TestPIDFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentd.pid")
	remove, err := WritePID(path)
	if err != nil {
		t.Fatal(err)
	}
	if pid, ok := ReadPID(path); !ok || pid != os.Getpid() {
		t.Fatalf("read %d %v", pid, ok)
	}
	remove()
	if _, ok := ReadPID(path); ok {
		t.Fatal("not removed")
	}
	// A stale file (no such process) is replaced.
	if err := os.WriteFile(path, []byte("999999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePID(path); err != nil {
		t.Fatal(err)
	}
}
