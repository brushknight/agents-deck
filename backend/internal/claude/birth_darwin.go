package claude

import (
	"os"
	"syscall"
	"time"
)

// birthTime is when the file was created (macOS keeps it), else its mtime.
func birthTime(st os.FileInfo) time.Time {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return time.Unix(s.Birthtimespec.Unix())
	}
	return st.ModTime()
}
