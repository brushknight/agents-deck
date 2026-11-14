// Package term talks to the terminal emulator through the terminal itself.
//
// Focus (StealFocus) brings the window showing a tty forward with its tab
// selected: on macOS through iTerm2's proprietary "StealFocus" escape (OSC
// 1337), which also raises a hidden hotkey window, with no AppleScript and no
// permissions (focus_other.go); on Linux through GNOME Terminal
// (focus_linux.go).
package term

import (
	"errors"
	"os"
	"regexp"
	"syscall"
)

// A terminal's device: macOS's /dev/ttysNNN, Linux's /dev/pts/N.
var ttyRe = regexp.MustCompile(`^/dev/(ttys[0-9]{3,4}|pts/[0-9]{1,5})$`)

const (
	stealFocus  = "\x1b]1337;StealFocus\x07"
	reportFocus = "\x1b[?1004h" // DEC focus reporting: terminal sends ESC[I / ESC[O
)

// ReportFocus turns on focus reporting for tty, so tmux learns about tab
// switches immediately (clients attached before focus-events was enabled
// never asked for it).
func ReportFocus(tty string) error { return write(tty, reportFocus) }

func write(tty, seq string) error {
	if !ttyRe.MatchString(tty) {
		return errors.New("not a terminal: " + tty)
	}
	st, err := os.Stat(tty)
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeCharDevice == 0 {
		return errors.New("not a terminal: " + tty)
	}
	if s, ok := st.Sys().(*syscall.Stat_t); ok && int(s.Uid) != os.Getuid() {
		return errors.New("terminal belongs to another user")
	}
	f, err := os.OpenFile(tty, os.O_WRONLY|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write([]byte(seq)) // one small write: lands as a unit
	return err
}
