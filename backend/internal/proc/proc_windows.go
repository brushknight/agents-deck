package proc

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const (
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
	createNoWindow                 = 0x08000000
)

// Alive reports whether process pid is still running. A process we may not
// open (another user's, a protected one) still counts.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	return code == stillActive
}

// NoWindow keeps a console program the service starts (claude, powershell)
// from opening a console window of its own: the service runs without one.
func NoWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}

// List lists the running processes with one of the given image names
// ("claude.exe"), with their command lines and start times. Windows has no
// ps; this asks PowerShell (Get-CimInstance, part of Windows 10 and 11).
func List(names ...string) ([]Process, error) {
	var filter []string
	for _, n := range names {
		if strings.ContainsAny(n, `'"\`) {
			return nil, errors.New("bad process name: " + n)
		}
		filter = append(filter, "Name='"+n+"'")
	}
	script := `$ErrorActionPreference='Stop'; [Console]::OutputEncoding=[Text.Encoding]::UTF8; ` +
		`$p = @(Get-CimInstance Win32_Process -Filter "` + strings.Join(filter, " OR ") + `" | ForEach-Object { ` +
		`[pscustomobject]@{pid=[int]$_.ProcessId; name=$_.Name; cmd=$_.CommandLine; ` +
		`started=$_.CreationDate.ToUniversalTime().ToString('o')} }); ` +
		`ConvertTo-Json -Compress -InputObject $p`
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	NoWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseList(out)
}
