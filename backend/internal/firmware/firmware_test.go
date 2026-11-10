package firmware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/update"
	"github.com/brushknight/agents-deck/backend/internal/update/updatetest"
)

var ctx = context.Background()

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// oldManifest is shaped like v0.1.1's: no types, usb, flash_size, wifi, picture or ota.
func oldManifest(version string, images map[string][]byte) []byte {
	var boards []map[string]any
	for _, b := range []struct{ board, name, env string }{
		{"lilygo-t4-s3", "LilyGO T4-S3", "idf5"},
		{"waveshare-lcd-147", "Waveshare ESP32-S3-LCD-1.47", "waveshare-s3-147b"},
	} {
		file := "agents-deck-" + b.board + "-" + version + ".bin"
		boards = append(boards, map[string]any{
			"name": b.name, "chipFamily": "ESP32-S3", "manifest": "agents-deck-" + b.board + "-" + version + ".manifest.json",
			"board": b.board, "version": version, "chip": "esp32s3", "file": file, "offset": "0x0",
			"size": len(images[file]), "sha256": sum(images[file]), "env": b.env,
			"project": "firmware/devices/" + b.board + "/idf5",
		})
	}
	out, _ := json.Marshal(map[string]any{"version": version, "boards": boards})
	return out
}

func newManifest(version string, images map[string][]byte) []byte {
	full := "agents-deck-lilygo-t4-s3-" + version + ".bin"
	ota := "agents-deck-lilygo-t4-s3-" + version + ".app.bin"
	small := "agents-deck-small-board-" + version + ".bin"
	out, _ := json.Marshal(map[string]any{"version": version, "boards": []map[string]any{
		{
			"name": "LilyGO T4-S3", "chipFamily": "ESP32-S3", "board": "lilygo-t4-s3", "version": version,
			"chip": "esp32s3", "file": full, "offset": "0x0", "size": len(images[full]), "sha256": sum(images[full]),
			"env": "idf5", "types": []string{"t4s3-panel"}, "usb": []map[string]string{{"vid": "303A", "pid": "1001"}},
			"flash_size": 16 << 20, "wifi": true, "picture": "docs/images/devices/t4s3-home.png",
			"ota": map[string]any{"file": ota, "size": len(images[ota]), "sha256": sum(images[ota])},
		},
		{
			"name": "A small board", "chipFamily": "ESP32-S3", "board": "small-board", "version": version,
			"chip": "esp32s3", "file": small, "offset": 0, "size": len(images[small]), "sha256": sum(images[small]),
			"env": "small", "types": []string{"small"}, "flash_size": 4 << 20, "wifi": false,
		},
	}})
	return out
}

func publish(s *updatetest.Server, version string, old bool) map[string][]byte {
	files := map[string][]byte{
		"agentctl_" + version + "_darwin_arm64.tar.gz": []byte("cli"),
	}
	var m []byte
	if old {
		files["agents-deck-lilygo-t4-s3-"+version+".bin"] = bytes.Repeat([]byte{0xe9}, 3000)
		files["agents-deck-waveshare-lcd-147-"+version+".bin"] = bytes.Repeat([]byte{0xe8}, 2000)
		m = oldManifest(version, files)
	} else {
		files["agents-deck-lilygo-t4-s3-"+version+".bin"] = bytes.Repeat([]byte{0xe9}, 3000)
		files["agents-deck-lilygo-t4-s3-"+version+".app.bin"] = bytes.Repeat([]byte{0xea}, 1000)
		files["agents-deck-small-board-"+version+".bin"] = bytes.Repeat([]byte{0xeb}, 500)
		m = newManifest(version, files)
	}
	files[ManifestFile] = m
	s.Add(version, files)
	return files
}

func TestOldManifest(t *testing.T) {
	s := updatetest.New(t)
	files := publish(s, "0.1.1", true)
	c := &Client{Releases: s.Releases(t.TempDir())}
	cat, err := c.Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cat.Version != "0.1.1" || len(cat.Boards) != 2 {
		t.Fatalf("%+v", cat)
	}
	b, ok := cat.Board("lilygo-t4-s3")
	if !ok || b.Chip != "ESP32-S3" || b.Offset != 0 || !b.Wifi || b.OTA != nil || b.Types != nil || b.Version != "0.1.1" {
		t.Errorf("%+v", b)
	}
	if b, _ := cat.Board("waveshare-lcd-147"); b.Wifi {
		t.Error("USB-only board has Wi-Fi")
	}
	// Without types, a Hello type matches only a board id.
	if _, ok := cat.BoardForType("t4s3-panel"); ok {
		t.Error("type matched without types")
	}
	if b, ok := cat.BoardForType("lilygo-t4-s3"); !ok || b.Board != "lilygo-t4-s3" {
		t.Error("board id as type")
	}
	p, err := c.Image(ctx, "lilygo-t4-s3", "")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, files["agents-deck-lilygo-t4-s3-0.1.1.bin"]) {
		t.Error("image content")
	}
	if _, _, _, err := c.OTAImage(ctx, "lilygo-t4-s3", ""); err == nil {
		t.Error("OTA image from a release without one")
	}
	if _, err := c.Image(ctx, "no-such-board", ""); err == nil {
		t.Error("unknown board")
	}
}

func TestNewManifest(t *testing.T) {
	s := updatetest.New(t)
	publish(s, "0.1.1", true)
	files := publish(s, "0.2.0", false)
	c := &Client{Releases: s.Releases(t.TempDir())}
	cat, err := c.Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := cat.BoardForType("t4s3-panel")
	if !ok || b.Board != "lilygo-t4-s3" || b.FlashSize != 16<<20 || b.Picture == "" || len(b.USB) != 1 || b.USB[0].VID != "303a" {
		t.Fatalf("%+v", b)
	}
	if got := cat.ForUSB("303a", "1001"); len(got) != 1 {
		t.Errorf("ForUSB: %v", got)
	}
	// 16 MB flash: both fit, the 16 MB board first. 4 MB: only the small one.
	if got := cat.Candidates("ESP32-S3", 16<<20); len(got) != 2 || got[0].Board != "lilygo-t4-s3" {
		t.Errorf("16 MB: %v", got)
	}
	if got := cat.Candidates("ESP32-S3", 4<<20); len(got) != 1 || got[0].Board != "small-board" {
		t.Errorf("4 MB: %v", got)
	}
	if got := cat.Candidates("esp32s3", 0); len(got) != 2 {
		t.Errorf("unknown flash: %v", got)
	}
	if got := cat.Candidates("ESP32-C3", 4<<20); len(got) != 0 {
		t.Errorf("other chip: %v", got)
	}

	p, size, sha, err := c.OTAImage(ctx, "lilygo-t4-s3", "0.2.0")
	ota := files["agents-deck-lilygo-t4-s3-0.2.0.app.bin"]
	if err != nil || size != int64(len(ota)) || sha != sum(ota) {
		t.Fatalf("ota: %v %d %s", err, size, sha)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, ota) {
		t.Error("ota content")
	}
	// An older release's image by version.
	if _, err := c.Image(ctx, "waveshare-lcd-147", "0.1.1"); err != nil {
		t.Fatal(err)
	}
	// Cached: a second Latest doesn't read the network.
	before := s.Hits("SHA256SUMS")
	if _, err := c.Latest(ctx); err != nil || s.Hits("SHA256SUMS") != before {
		t.Errorf("Latest not cached: %v", err)
	}
}

func TestTamperedImage(t *testing.T) {
	s := updatetest.New(t)
	publish(s, "0.2.0", false)
	s.Put("0.2.0", "agents-deck-lilygo-t4-s3-0.2.0.bin", bytes.Repeat([]byte{0}, 3000))
	c := &Client{Releases: s.Releases(t.TempDir())}
	if _, err := c.Image(ctx, "lilygo-t4-s3", ""); err == nil {
		t.Fatal("tampered image")
	}
	// A manifest that disagrees with SHA256SUMS.
	s2 := updatetest.New(t)
	files := publish(s2, "0.2.0", false)
	var m map[string]any
	json.Unmarshal(files[ManifestFile], &m)
	m["boards"].([]any)[0].(map[string]any)["sha256"] = sum([]byte("other"))
	files[ManifestFile], _ = json.Marshal(m)
	s2.Add("0.2.0", files)
	c2 := &Client{Releases: s2.Releases(t.TempDir())}
	if _, err := c2.Latest(ctx); err == nil {
		t.Fatal("manifest disagreeing with SHA256SUMS accepted")
	}
}

func TestNoKeyListsButNeverDownloads(t *testing.T) {
	s := updatetest.New(t)
	publish(s, "0.2.0", false)
	r := s.Releases(t.TempDir())
	r.Key = nil
	c := &Client{Releases: r}
	cat, err := c.Latest(ctx)
	if err != nil || len(cat.Boards) != 2 {
		t.Fatalf("listing without a key: %v", err)
	}
	if _, err := c.Image(ctx, "lilygo-t4-s3", ""); !errors.Is(err, update.ErrNoReleaseKey) {
		t.Fatalf("image: %v", err)
	}
	if _, _, _, err := c.OTAImage(ctx, "lilygo-t4-s3", ""); !errors.Is(err, update.ErrNoReleaseKey) {
		t.Fatalf("ota: %v", err)
	}
	if s.Hits(".bin") != 0 {
		t.Error("downloaded an image without a key")
	}
}

func TestParseOffset(t *testing.T) {
	for raw, want := range map[string]uint32{`"0x10000"`: 0x10000, `0`: 0, `65536`: 65536, `null`: 0, `"0"`: 0} {
		got, err := parseOffset(json.RawMessage(raw))
		if err != nil || got != want {
			t.Errorf("%s: %d %v", raw, got, err)
		}
	}
	if _, err := parseOffset(json.RawMessage(`"zz"`)); err == nil {
		t.Error("bad offset")
	}
}
