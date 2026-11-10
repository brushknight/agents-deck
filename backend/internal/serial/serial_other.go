//go:build !darwin && !linux && !windows

package serial

import "os"

func list() ([]Info, error) { return nil, ErrUnsupported }

func open(path string, baud int) (Port, error) { return nil, ErrUnsupported }

// OpenFile is not available on this platform.
func OpenFile(path string, baud int) (*os.File, error) { return nil, ErrUnsupported }
