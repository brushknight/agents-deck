//go:build !windows

package main

// platformSupports: every command works on macOS and Linux.
func platformSupports(cmd string) error { return nil }
