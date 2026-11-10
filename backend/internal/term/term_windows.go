package term

import "errors"

// write: Windows terminals have no tty device to write to, so focusing a
// tab this way is not available on Windows yet.
func write(tty, seq string) error {
	return errors.New("focusing a terminal tab is not available on Windows yet")
}
