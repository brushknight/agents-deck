package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRelease writes what the release script leaves in a folder for one
// board: the images (contents don't matter here, the service checks them)
// and its manifest; withIndex adds firmware-manifest.json, perBoard the
// board's own manifest.
func writeRelease(t *testing.T, dir, board, version string, withApp, perBoard, withIndex bool) {
	t.Helper()
	base := "agents-deck-" + board + "-" + version
	os.WriteFile(filepath.Join(dir, base+".bin"), []byte("full"), 0o600)
	ad := map[string]any{"board": board, "version": version, "chip": "esp32s3", "file": base + ".bin", "offset": "0x0",
		"size": 4, "sha256": strings.Repeat("a", 64), "ota": nil}
	if withApp {
		os.WriteFile(filepath.Join(dir, base+".app.bin"), []byte("app"), 0o600)
		ad["ota"] = map[string]any{"file": base + ".app.bin", "size": 3, "sha256": strings.Repeat("b", 64)}
	}
	if perBoard {
		m, _ := json.Marshal(map[string]any{"name": board, "version": version, "agents_deck": ad})
		os.WriteFile(filepath.Join(dir, base+".manifest.json"), m, 0o600)
	}
	if withIndex {
		m, _ := json.Marshal(map[string]any{"version": version, "boards": []any{ad}})
		os.WriteFile(filepath.Join(dir, "firmware-manifest.json"), m, 0o600)
	}
}

func TestFindBuilds(t *testing.T) {
	dir := t.TempDir()
	writeRelease(t, dir, "made-up-board", "0.2.0-dev.7", true, true, false)
	writeRelease(t, dir, "usb-only-board", "0.2.0-dev.7", false, true, false)
	got, err := findBuilds([]string{dir})
	if err != nil || len(got) != 2 {
		t.Fatalf("a folder: %+v %v", got, err)
	}
	if got[0].name != "made-up-board 0.2.0-dev.7" || filepath.Base(got[0].app) != "agents-deck-made-up-board-0.2.0-dev.7.app.bin" || got[1].app != "" {
		t.Errorf("builds: %+v", got)
	}
	// A file names its build; the same build twice is sent once.
	got, err = findBuilds([]string{filepath.Join(dir, "agents-deck-made-up-board-0.2.0-dev.7.app.bin"), filepath.Join(dir, "agents-deck-made-up-board-0.2.0-dev.7.bin")})
	if err != nil || len(got) != 1 || got[0].full == "" {
		t.Errorf("files: %+v %v", got, err)
	}
	// Only firmware-manifest.json: its boards.
	idx := t.TempDir()
	writeRelease(t, idx, "made-up-board", "0.3.0", true, false, true)
	if got, err := findBuilds([]string{idx}); err != nil || len(got) != 1 || got[0].name != "made-up-board 0.3.0" {
		t.Errorf("index: %+v %v", got, err)
	}
	if got, err := findBuilds([]string{filepath.Join(idx, "agents-deck-made-up-board-0.3.0.bin")}); err != nil || len(got) != 1 {
		t.Errorf("a file listed in the index: %+v %v", got, err)
	}
	// A missing image, and a folder with nothing in it, say so.
	os.Remove(filepath.Join(dir, "agents-deck-made-up-board-0.2.0-dev.7.app.bin"))
	if _, err := findBuilds([]string{dir}); err == nil || !strings.Contains(err.Error(), "isn't next to its manifest") {
		t.Errorf("a missing image: %v", err)
	}
	if _, err := findBuilds([]string{t.TempDir()}); err == nil || !strings.Contains(err.Error(), "holds no build") {
		t.Errorf("an empty folder: %v", err)
	}
	if _, err := findBuilds([]string{filepath.Join(dir, "notes.txt")}); err == nil {
		t.Error("a file that isn't there")
	}
}
