//go:build !darwin && !linux && !windows

package service

import "io"

type unsupported struct{}

// New: no start-at-login support on this platform; run agentctl serve.
func New(Config) Manager { return unsupported{} }

func (unsupported) Install(io.Writer) error   { return ErrUnsupported }
func (unsupported) Uninstall(io.Writer) error { return ErrUnsupported }
func (unsupported) Registration() Registration {
	return Registration{Kind: "none", Note: ErrUnsupported.Error()}
}
