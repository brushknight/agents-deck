package proc

import "os/exec"

// Windows: no process groups to kill; the helper alone is killed on timeout.
func tidy(cmd *exec.Cmd) { NoWindow(cmd) }
