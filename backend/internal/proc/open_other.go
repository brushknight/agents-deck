//go:build !darwin && !windows

package proc

import "os/exec"

// Open opens a link (https:, file:, an app's own scheme) with the program
// the user chose for it (xdg-open: needs a desktop session).
func Open(target string) error { return exec.Command("xdg-open", target).Run() }
