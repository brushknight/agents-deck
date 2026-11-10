package update

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// System is everything Apply does outside plain file operations: unpacking
// the app, checking its code signature, and starting the programs that
// restart the service and the app. Tests replace it.
type System interface {
	// Unzip unpacks a zip into dir, keeping symlinks and permissions.
	Unzip(ctx context.Context, zip, dir string) error
	// CodesignVerify checks an app bundle's signature: codesign --verify --deep --strict.
	CodesignVerify(ctx context.Context, bundle string) error
	// SigningTeam is the Team ID that signed a bundle ("" when none) and
	// whether it is signed with a Developer ID certificate.
	SigningTeam(ctx context.Context, bundle string) (team string, developerID bool, err error)
	// Detach starts a program that keeps running after this process exits
	// (the service is about to be restarted under it).
	Detach(name string, args ...string) error
}

// HostSystem runs the real tools: ditto and codesign on macOS.
type HostSystem struct{}

func (HostSystem) Unzip(ctx context.Context, zip, dir string) error {
	if runtime.GOOS != "darwin" {
		return unzipGo(zip, dir, MaxFile)
	}
	// ditto keeps what a signed bundle needs (symlinks in frameworks, extended attributes).
	return run(ctx, "ditto", "-x", "-k", zip, dir)
}

func (HostSystem) CodesignVerify(ctx context.Context, bundle string) error {
	if err := run(ctx, "codesign", "--verify", "--deep", "--strict", bundle); err != nil {
		return fmt.Errorf("the new app's signature doesn't verify: %w", err)
	}
	return nil
}

func (HostSystem) SigningTeam(ctx context.Context, bundle string) (string, bool, error) {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "codesign", "-d", "--verbose=2", bundle)
	cmd.Stdout, cmd.Stderr = &out, &out // codesign -d writes to stderr
	if err := cmd.Run(); err != nil {
		return "", false, fmt.Errorf("codesign -d %s: %v: %s", bundle, err, strings.TrimSpace(out.String()))
	}
	team, devID := parseCodesign(out.String())
	return team, devID, nil
}

func (HostSystem) Detach(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

var (
	teamRe  = regexp.MustCompile(`(?m)^TeamIdentifier=(.+)$`)
	devIDRe = regexp.MustCompile(`(?m)^Authority=Developer ID Application: `)
)

// parseCodesign reads `codesign -d --verbose=2` output.
func parseCodesign(out string) (team string, developerID bool) {
	if m := teamRe.FindStringSubmatch(out); m != nil {
		team = strings.TrimSpace(m[1])
		if team == "not set" {
			team = ""
		}
	}
	return team, devIDRe.MatchString(out) && team != ""
}

func run(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
