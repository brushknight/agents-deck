//go:build !darwin && !linux && !windows

package serial

import "io"

func list() ([]Info, error) { return nil, ErrUnsupported }

func open(path string, baud int) (Port, error) { return nil, ErrUnsupported }

// OpenFile is not available on this platform.
func OpenFile(path string, baud int) (io.ReadWriteCloser, error) { return nil, ErrUnsupported }
