package service

import "os/user"

// New is a systemd user unit (what install.sh wrote before agentctl did).
func New(c Config) Manager {
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	return &Systemd{Exe: c.Exe, Home: c.Home, User: name, Run: Exec}
}
