//go:build !windows

package term

import (
	"errors"
	"os"
	"syscall"
)

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
