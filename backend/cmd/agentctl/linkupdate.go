package main

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/link"
)

// linkUpdate: `agentctl link update <board> --image FILE --board B --version V`
// offers a board on Wi-Fi an app image from this Mac (a build to try before
// it is released) and follows the update until the board is back.
func linkUpdate(args []string) error {
	fs := flag.NewFlagSet("link update", flag.ExitOnError)
	image := fs.String("image", "", "the app image (.pio/build/<env>/firmware.bin, or a release's .app.bin)")
	board := fs.String("board", "", "the board the image is for: its folder under firmware/devices (e.g. lilygo-t4-s3)")
	version := fs.String("version", "", "the version the image reports in Hello (AGENTS_DECK_VERSION when it was built)")
	wait := fs.Duration("wait", 10*time.Minute, "how long to follow the update")
	var device string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		device, args = args[0], args[1:]
	}
	_ = fs.Parse(args)
	if device == "" || *image == "" || *board == "" || *version == "" {
		return errors.New("usage: agentctl link update <board> --image FILE --board B --version V   (the board must be on Wi-Fi)")
	}
	var res struct{ Message string }
	if err := call("POST", "/local/link/firmware", map[string]string{"device": device, "board": *board, "version": *version,
		"file": *image}, &res); err != nil {
		return err
	}
	fmt.Println(res.Message)
	last := ""
	deadline := time.Now().Add(*wait)
	for time.Now().Before(deadline) {
		time.Sleep(time.Second)
		var all []link.FirmwareInfo
		if err := call("GET", "/local/link/firmware", nil, &all); err != nil {
			continue // the daemon restarting: try again
		}
		for _, fi := range all {
			if fi.Device != device || fi.Job == nil {
				continue
			}
			j := fi.Job
			line := j.State
			if j.State == link.FirmwareDownloading {
				line = fmt.Sprintf("%s %d%%", j.State, j.Percent)
			}
			if j.Error != "" {
				line += ": " + j.Error
			}
			if line != last {
				fmt.Println(line)
				last = line
			}
			switch j.State {
			case link.FirmwareDone:
				fmt.Printf("%s runs %s\n", device, fi.Version)
				return nil
			case link.FirmwareFailed, link.FirmwareRefused, link.FirmwareRolledBack:
				return fmt.Errorf("%s: %s", device, line)
			}
		}
	}
	return fmt.Errorf("%s: no result after %s (agentctl link shows whether it is back)", device, *wait)
}
