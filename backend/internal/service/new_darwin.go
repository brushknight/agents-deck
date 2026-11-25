package service

import (
	"os"
	"time"
)

// New is launchd, as `agentctl install` has always done it on macOS.
func New(c Config) Manager {
	return &Launchd{Exe: c.Exe, Home: c.Home, Log: c.Log, UID: os.Getuid(), Run: Exec, Sleep: time.Sleep}
}
