package firmware

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// appImage is a made-up app image: the header for chip, one segment whose
// first bytes are esp_app_desc_t with version and project.
func appImage(chip uint16, version, project string, size int) []byte {
	b := make([]byte, size)
	b[0], b[1] = imageMagic, 1
	binary.LittleEndian.PutUint16(b[12:14], chip)
	binary.LittleEndian.PutUint32(b[appDescAt:], appDescMagic)
	copy(b[appDescAt+16:appDescAt+48], version)
	copy(b[appDescAt+48:appDescAt+80], project)
	return b
}

// fullImage is a bootloader for chip, a partition table with nvs and an app
// partition at 0x10000, and app at that offset.
func fullImage(chip uint16, app []byte) []byte {
	b := make([]byte, 0x10000+len(app))
	b[0], b[1] = imageMagic, 3
	binary.LittleEndian.PutUint16(b[12:14], chip)
	b[appDescAt] = 0x50 // a bootloader description, not an app's
	entry := func(i int, typ byte, off uint32) {
		e := b[partTableAt+i*partEntryLen:]
		binary.LittleEndian.PutUint16(e[0:2], partMagic)
		e[2] = typ
		binary.LittleEndian.PutUint32(e[4:8], off)
	}
	entry(0, 0x01, 0x9000) // data: nvs
	entry(1, partTypeApp, 0x10000)
	copy(b[0x10000:], app)
	return b
}

func TestInspectImage(t *testing.T) {
	app := appImage(0x0009, "0.2.0-dev.4", "agents-deck", 4096)
	info, err := InspectImage(bytes.NewReader(app), int64(len(app)))
	if err != nil || info.Kind != ImageApp || info.Chip != "ESP32-S3" || info.Version != "0.2.0-dev.4" || info.Project != "agents-deck" {
		t.Errorf("app: %+v %v", info, err)
	}
	// Arduino builds stamp something that isn't a version: no version then.
	odd := appImage(0x0009, "esp-idf: v5.3.1 1a2b3c", "arduino-lib-builder", 4096)
	if info, err := InspectImage(bytes.NewReader(odd), int64(len(odd))); err != nil || info.Version != "" || info.Kind != ImageApp {
		t.Errorf("no version: %+v %v", info, err)
	}
	full := fullImage(0x0009, app)
	if info, err := InspectImage(bytes.NewReader(full), int64(len(full))); err != nil || info.Kind != ImageFull || info.Chip != "ESP32-S3" || info.Version != "0.2.0-dev.4" {
		t.Errorf("full: %+v %v", info, err)
	}
	c3 := appImage(0x0005, "0.2.0", "x", 4096)
	if info, _ := InspectImage(bytes.NewReader(c3), int64(len(c3))); info.Chip != "ESP32-C3" {
		t.Errorf("C3: %+v", info)
	}
}

func TestInspectImageRefuses(t *testing.T) {
	app := appImage(0x0009, "0.2.0", "agents-deck", 4096)
	for name, tc := range map[string]struct {
		b    []byte
		size int64
		want string
	}{
		"text":            {[]byte(strings.Repeat("hello ", 1000)), 6000, "isn't an ESP32 firmware image"},
		"empty":           {nil, 0, "isn't an ESP32 firmware image"},
		"too small":       {app[:100], 100, "isn't an ESP32 firmware image"},
		"zero segments":   {func() []byte { b := bytes.Clone(app); b[1] = 0; return b }(), 4096, "isn't an ESP32 firmware image"},
		"unknown chip":    {appImage(0x0077, "0.2.0", "x", 4096), 4096, "chip id 119"},
		"too big":         {app, MaxFileImage + 1, "bigger than"},
		"no app desc":     {func() []byte { b := bytes.Clone(app); b[appDescAt] = 0; return b }(), 4096, "neither an app nor a full image"},
		"no part table":   {func() []byte { b := fullImage(0x0009, app); b[partTableAt] = 0; return b }(), int64(0x10000 + len(app)), "no partition table"},
		"mixed chips":     {fullImage(0x0009, appImage(0x0005, "0.2.0", "x", 4096)), int64(0x10000 + 4096), "its app is for an ESP32-C3"},
		"app off the end": {func() []byte { b := fullImage(0x0009, app); return b[:0x10010] }(), 0x10010, "outside the file"},
	} {
		_, err := InspectImage(bytes.NewReader(tc.b), tc.size)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
	if _, err := InspectImage(bytes.NewReader(nil), 10); !errors.Is(err, ErrNotESPImage) {
		t.Errorf("errors.Is: %v", err)
	}
}

func TestSameChip(t *testing.T) {
	if !SameChip("ESP32-S3", "esp32s3") || SameChip("ESP32-S3", "ESP32-C3") {
		t.Error("SameChip")
	}
}

// The ESP32 and ESP32-S2 start their bootloader at 0x1000: their full image
// is blank before it. A bootloader elsewhere would never start.
func TestInspectImageBootloaderOffset(t *testing.T) {
	app := appImage(0x0000, "0.2.0", "agents-deck", 4096)
	full := fullImage(0x0000, app)
	shifted := append(bytes.Repeat([]byte{0xFF}, 0x1000), full[:0x1000]...) // the bootloader at 0x1000
	shifted = append(shifted, full[0x2000:]...)                             // the table still at 0x8000, the app at 0x10000
	shifted[0x1000+3] = 0x20                                                // 4 MB
	info, err := InspectImage(bytes.NewReader(shifted), int64(len(shifted)))
	if err != nil || info.Kind != ImageFull || info.Chip != "ESP32" || info.FlashSize != 4<<20 || info.Version != "0.2.0" {
		t.Errorf("ESP32 full image: %+v %v", info, err)
	}
	if _, err := InspectImage(bytes.NewReader(full), int64(len(full))); err == nil || !strings.Contains(err.Error(), "starts it from 0x1000") {
		t.Errorf("an ESP32 bootloader at 0x0: %v", err)
	}
	// An S3 starts it from 0x0: blank flash before a bootloader is refused.
	s3 := fullImage(0x0009, appImage(0x0009, "0.2.0", "x", 4096))
	moved := append(bytes.Repeat([]byte{0xFF}, 0x1000), s3[:0x1000]...)
	moved = append(moved, s3[0x2000:]...)
	if _, err := InspectImage(bytes.NewReader(moved), int64(len(moved))); !errors.Is(err, ErrNotESPImage) {
		t.Errorf("an S3 bootloader at 0x1000: %v", err)
	}
	// The header's flash size is read from the bootloader.
	s3[3] = 0x40
	if info, _ := InspectImage(bytes.NewReader(s3), int64(len(s3))); info.FlashSize != 16<<20 {
		t.Errorf("flash size: %d", info.FlashSize)
	}
}
