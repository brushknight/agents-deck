//go:build linux

package term

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// On Linux, StealFocus raises the GNOME Terminal tab showing tty. The tab
// gets a unique title for a moment; GNOME Terminal's search provider (the
// one the Activities overview uses) finds the tab by that title, and
// activating the result brings its window up with the tab selected. The
// tab's own title is pushed before and popped after (XTWINOPS 22 and 23,
// which VTE keeps a stack for), so nothing shows. gdbus does the D-Bus
// calls: no dependency for the daemon.
//
// GNOME's focus-stealing prevention honours that activation only some of the
// time (it comes with no user timestamp). On X11, wmctrl then raises the
// window, now titled with the marker, as a pager would, which the window
// manager always honours. Without wmctrl (Wayland) the tab is still selected.
const (
	pushTitle   = "\x1b[22;0t"
	popTitle    = "\x1b[23;0t"
	terminalBus = "org.gnome.Terminal"
	searchPath  = "/org/gnome/Terminal/SearchProvider"
	searchIface = "org.gnome.Shell.SearchProvider2"
)

var screenRe = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func StealFocus(tty string) error {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return err
	}
	marker := "agents-deck-" + hex.EncodeToString(b[:])
	if err := write(tty, pushTitle+"\x1b]0;"+marker+"\x07"); err != nil {
		return err
	}
	defer func() { _ = write(tty, popTitle) }()
	// The terminal takes the title as it reads the tty's output: a moment.
	screen := ""
	for i := 0; i < 10 && screen == ""; i++ {
		time.Sleep(50 * time.Millisecond)
		out, err := gdbus("GetInitialResultSet", "['"+marker+"']")
		if err != nil {
			return errors.New("GNOME Terminal doesn't answer (other terminals can't be raised yet)")
		}
		screen = screenOf(out)
	}
	if screen == "" {
		return errors.New("no GNOME Terminal tab shows " + tty)
	}
	if _, err := gdbus("ActivateResult", "'"+screen+"'", "['"+marker+"']", "uint32 0"); err != nil {
		return err
	}
	raiseTitled(marker)
	return nil
}

// raiseTitled raises the X11 window titled title, if wmctrl can see one.
func raiseTitled(title string) {
	wm, err := exec.LookPath("wmctrl")
	if err != nil {
		return
	}
	for i := 0; i < 10; i++ { // the window takes the tab's title once the tab is selected
		out, _ := exec.Command(wm, "-l").Output()
		if id := windowTitled(string(out), title); id != "" {
			_ = exec.Command(wm, "-i", "-a", id).Run()
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
}

// windowTitled is the id of the window titled title in `wmctrl -l` output
// (id, desktop, host, title), "" when none is.
func windowTitled(list, title string) string {
	for _, l := range strings.Split(list, "\n") {
		f := strings.Fields(l)
		if len(f) >= 4 && strings.Join(f[3:], " ") == title {
			return f[0]
		}
	}
	return ""
}

// gdbus calls a method of GNOME Terminal's search provider; out is the reply
// in GVariant text, e.g. (['5d2c…'],).
func gdbus(method string, args ...string) (string, error) {
	a := append([]string{"call", "--session", "--timeout", "2", "--dest", terminalBus,
		"--object-path", searchPath, "--method", searchIface + "." + method}, args...)
	out, err := exec.Command("gdbus", a...).Output()
	return string(out), err
}

// screenOf is the first tab id in a search reply, "" when there is none.
func screenOf(out string) string { return screenRe.FindString(out) }
