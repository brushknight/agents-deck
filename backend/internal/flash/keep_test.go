package flash

import (
	"bytes"
	"testing"
)

func tableEntry(typ, sub byte, off, size uint32) []byte {
	e := make([]byte, 32)
	e[0], e[1], e[2], e[3] = 0xAA, 0x50, typ, sub
	copy(e[4:], le32(off, size))
	return e
}

// A merged image must not erase the board's settings partition.
func TestKeepSettingsSkipsBlankNVS(t *testing.T) {
	img := bytes.Repeat([]byte{0xFF}, 0x20000)
	copy(img, []byte("bootloader"))
	copy(img[0x8000:], tableEntry(1, 2, 0x9000, 0x5000))      // nvs
	copy(img[0x8020:], tableEntry(1, 0, 0xE000, 0x2000))      // otadata
	copy(img[0x8040:], tableEntry(0, 0x10, 0x10000, 0x10000)) // app
	copy(img[0xE000:], []byte("boot_app0"))
	copy(img[0x10000:], []byte("app"))
	parts := keepSettings(img, 0)
	if len(parts) != 2 || parts[0].offset != 0 || len(parts[0].data) != 0x9000 || parts[1].offset != 0xE000 || len(parts[1].data) != 0x20000-0xE000 {
		t.Fatalf("parts: %+v", partsShape(parts))
	}
	// settings the image really carries are written
	img[0x9000] = 1
	if parts := keepSettings(img, 0); len(parts) != 1 {
		t.Fatalf("an image with its own settings is written whole: %+v", partsShape(parts))
	}
	img[0x9000] = 0xFF
	// not a whole-flash image: written as it is
	if parts := keepSettings(img, 0x10000); len(parts) != 1 || parts[0].offset != 0x10000 {
		t.Fatalf("at an offset: %+v", partsShape(parts))
	}
	if parts := keepSettings([]byte("app only"), 0); len(parts) != 1 {
		t.Fatalf("a short image: %+v", partsShape(parts))
	}
}

func partsShape(ps []part) [][2]int {
	var out [][2]int
	for _, p := range ps {
		out = append(out, [2]int{int(p.offset), len(p.data)})
	}
	return out
}
