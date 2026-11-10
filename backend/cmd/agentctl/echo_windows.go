package main

import (
	"os"
	"syscall"
)

const enableEchoInput = 0x0004

var procSetConsoleMode = syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")

// echoOff stops the console on f from showing what is typed; restore turns
// it back on.
func echoOff(f *os.File) (restore func(), err error) {
	h := syscall.Handle(f.Fd())
	var mode uint32
	if err := syscall.GetConsoleMode(h, &mode); err != nil {
		return nil, err
	}
	if r, _, err := procSetConsoleMode.Call(uintptr(h), uintptr(mode&^enableEchoInput)); r == 0 {
		return nil, err
	}
	return func() { _, _, _ = procSetConsoleMode.Call(uintptr(h), uintptr(mode)) }, nil
}
