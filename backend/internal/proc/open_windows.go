package proc

import "os/exec"

// Open opens a link (https:, file:, an app's own scheme) with the program
// the user chose for it. rundll32 hands it to the shell without going
// through cmd.exe, so "&" and other characters in a URL stay as they are.
func Open(target string) error {
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", target).Run()
}
