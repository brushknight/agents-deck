//go:build !darwin

package claude

import (
	"os"
	"time"
)

func birthTime(st os.FileInfo) time.Time { return st.ModTime() }
