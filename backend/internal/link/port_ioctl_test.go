//go:build darwin || linux

package link

import "syscall"

func ioctl(fd int, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, arg); e != 0 {
		return e
	}
	return nil
}
