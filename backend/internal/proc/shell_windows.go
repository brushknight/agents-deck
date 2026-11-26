package proc

// LoginShell: agents run in tmux, which Windows doesn't have; the deck there
// watches agents and never starts one through a shell.
func LoginShell() string { return "sh" }
