package service

// New is a systemd user unit (what install.sh wrote before agentctl did).
func New(c Config) Manager { return &Systemd{Exe: c.Exe, Home: c.Home, Run: Exec} }
