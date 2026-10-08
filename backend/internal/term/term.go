// Package term talks to the terminal emulator through the terminal itself.
//
// Focus uses iTerm2's proprietary "StealFocus" escape (OSC 1337): written to
// a tab's tty, iTerm brings that window forward — including a hidden hotkey
// window — with the tab selected. No AppleScript, no macOS permissions.
package term

import (
	"errors"
	"os"
	"regexp"
	"syscall"
)

var ttyRe = regexp.MustCompile(`^/dev/ttys[0-9]{3,4}$`)

const (
	stealFocus  = "\x1b]1337;StealFocus\x07"
	reportFocus = "\x1b[?1004h" // DEC focus reporting: terminal sends ESC[I / ESC[O
)

// StealFocus asks the terminal showing tty to come to the front.
func StealFocus(tty string) error { return write(tty, stealFocus) }

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
