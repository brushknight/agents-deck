//go:build !darwin

package cpuguard

import "errors"

// CanSlow says whether background mode exists here: macOS only.
const CanSlow = false

type osPriority struct{}

func (osPriority) Background(int, bool) error { return errors.ErrUnsupported }
