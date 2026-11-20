//go:build !windows

package main

import (
	"os"
	"os/exec"
)

// echoOff stops the terminal on f from showing what is typed (stty -echo);
// restore turns it back on.
func echoOff(f *os.File) (restore func(), err error) {
	off := exec.Command("stty", "-echo")
	off.Stdin = f
	if err := off.Run(); err != nil {
		return nil, err
	}
	return func() {
		on := exec.Command("stty", "echo")
		on.Stdin = f
		_ = on.Run()
	}, nil
}
