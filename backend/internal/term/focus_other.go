//go:build !linux

package term

// StealFocus asks the terminal showing tty to come to the front (iTerm2).
func StealFocus(tty string) error { return write(tty, stealFocus) }
