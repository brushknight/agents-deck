//go:build !windows

package server

import "os"

// restrictSocket lets only this user connect to the daemon's socket.
func restrictSocket(path string) error { return os.Chmod(path, 0o600) }
