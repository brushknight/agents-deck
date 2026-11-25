package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
)

// LookPathLogin finds a command on Windows: on PATH (as the service sees it,
// with its .exe or .cmd), else where the usual installers put it: Claude
// Code's native installer in %USERPROFILE%\.local\bin, npm in %APPDATA%\npm.
func LookPathLogin(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
		return p
	}
	home, _ := os.UserHomeDir()
	var dirs []string
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"))
	}
	if d := os.Getenv("APPDATA"); d != "" {
		dirs = append(dirs, filepath.Join(d, "npm"))
	}
	for _, dir := range dirs {
		for _, ext := range []string{".exe", ".cmd"} {
			p := filepath.Join(dir, name+ext)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return ""
}
