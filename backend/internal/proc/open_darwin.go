package proc

import "os/exec"

// Open opens a link (https:, file:, an app's own scheme) with the program
// the user chose for it.
func Open(target string) error { return exec.Command("/usr/bin/open", target).Run() }
