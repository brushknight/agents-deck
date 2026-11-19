package firmware

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/brushknight/agents-deck/backend/internal/update"
)

// Reading an ESP32 firmware file someone picked on this computer, before
// anything is sent to a board: what kind of image it is, which chip it was
// built for and, when the app says so, its version. The board still checks
// everything it checks today (its own board name, the image's SHA-256, the
// bootloader's checks); this only refuses a wrong file early.

// Image kinds.
const (
	ImageApp  = "app"  // the app alone (.app.bin, .pio/build/<env>/firmware.bin): over the air
	ImageFull = "full" // bootloader, partition table and app, written at 0x0 over USB
)

// MaxFileImage is the biggest file accepted: the biggest flash on a supported board.
const MaxFileImage = 32 << 20

// ImageInfo is what InspectImage read.
type ImageInfo struct {
	Kind    string `json:"kind"`              // ImageApp | ImageFull
	Chip    string `json:"chip"`              // "ESP32-S3", from the image header
	Version string `json:"version,omitempty"` // the app's version, when it reads as one (semver)
	Project string `json:"project,omitempty"` // the app's project name
	Size    int64  `json:"size"`
}

const (
	imageMagic   = 0xE9
	appDescMagic = 0xABCD5432
	headerLen    = 24            // esp_image_header_t
	appDescAt    = headerLen + 8 // after the first segment's header
	partTableAt  = 0x8000        // in a full image
	partMagic    = 0x50AA        // esp_partition_info_t.magic, little-endian AA 50
	partEntryLen = 32            // one partition entry
	partTypeApp  = 0x00          // partition type "app"
	partMaxCount = 95            // a 3 KB table
	minFullImage = partTableAt + 1024
)

// chipIDs are esp_image_header_t.chip_id values (esp_chip_id_t).
var chipIDs = map[uint16]string{
	0x0000: "ESP32", 0x0002: "ESP32-S2", 0x0005: "ESP32-C3", 0x0009: "ESP32-S3",
	0x000C: "ESP32-C2", 0x000D: "ESP32-C6", 0x0010: "ESP32-H2", 0x0012: "ESP32-P4",
}

// Errors from InspectImage.
var (
	ErrNotESPImage = errors.New("this isn't an ESP32 firmware image (it doesn't start with the image header)")
	ErrTooBig      = fmt.Errorf("the file is bigger than %d MB, more than any board's flash", MaxFileImage>>20)
)

// InspectImage reads an image file's headers. It refuses anything that
// isn't an ESP32 app image or a full image with a partition table and an app.
func InspectImage(r io.ReaderAt, size int64) (ImageInfo, error) {
	if size > MaxFileImage {
		return ImageInfo{}, ErrTooBig
	}
	if size < appDescAt+256 {
		return ImageInfo{}, ErrNotESPImage
	}
	chip, err := headerChip(r, 0)
	if err != nil {
		return ImageInfo{}, err
	}
	info := ImageInfo{Chip: chip, Size: size}
	if ver, project, ok := appDesc(r, 0); ok {
		info.Kind, info.Version, info.Project = ImageApp, ver, project
		return info, nil
	}
	// Not an app: a full image starts with the bootloader, has the partition
	// table at 0x8000 and an app for the same chip in its first app partition.
	if size < minFullImage {
		return ImageInfo{}, errors.New("this ESP32 image is neither an app nor a full image (no app description, too small for a partition table)")
	}
	off, err := firstApp(r, size)
	if err != nil {
		return ImageInfo{}, err
	}
	appChip, err := headerChip(r, off)
	if err != nil {
		return ImageInfo{}, fmt.Errorf("the app in this full image: %w", err)
	}
	if appChip != chip {
		return ImageInfo{}, fmt.Errorf("this full image's bootloader is for an %s but its app is for an %s", chip, appChip)
	}
	ver, project, ok := appDesc(r, off)
	if !ok {
		return ImageInfo{}, errors.New("the app in this full image has no app description")
	}
	info.Kind, info.Version, info.Project = ImageFull, ver, project
	return info, nil
}

// headerChip checks the image header at off and names its chip.
func headerChip(r io.ReaderAt, off int64) (string, error) {
	h := make([]byte, headerLen)
	if _, err := r.ReadAt(h, off); err != nil {
		return "", ErrNotESPImage
	}
	if h[0] != imageMagic || h[1] == 0 || h[1] > 16 { // 1..16 segments
		return "", ErrNotESPImage
	}
	id := binary.LittleEndian.Uint16(h[12:14])
	name, ok := chipIDs[id]
	if !ok {
		return "", fmt.Errorf("this image is for a chip agents deck doesn't know (chip id %d)", id)
	}
	return name, nil
}

// appDesc reads esp_app_desc_t after the first segment header of the image
// at off: its version (only when it reads as a version) and project name.
func appDesc(r io.ReaderAt, off int64) (version, project string, ok bool) {
	d := make([]byte, 80)
	if _, err := r.ReadAt(d, off+appDescAt); err != nil {
		return "", "", false
	}
	if binary.LittleEndian.Uint32(d[0:4]) != appDescMagic {
		return "", "", false
	}
	version = cString(d[16:48])
	project = cString(d[48:80])
	if _, err := update.ParseVersion(version); err != nil {
		version = "" // Arduino builds put other things here; the page asks then
	}
	return version, project, true
}

// firstApp is where the first app partition starts in a full image.
func firstApp(r io.ReaderAt, size int64) (int64, error) {
	e := make([]byte, partEntryLen)
	for i := 0; i < partMaxCount; i++ {
		if _, err := r.ReadAt(e, partTableAt+int64(i*partEntryLen)); err != nil {
			break
		}
		if binary.LittleEndian.Uint16(e[0:2]) != partMagic {
			break
		}
		if e[2] == partTypeApp {
			off := int64(binary.LittleEndian.Uint32(e[4:8]))
			if off <= partTableAt || off+headerLen >= size {
				return 0, errors.New("this full image's app partition lies outside the file")
			}
			return off, nil
		}
	}
	return 0, errors.New("this ESP32 image is neither an app nor a full image (no partition table with an app at 0x8000)")
}

func cString(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		b = b[:i]
	}
	s := strings.TrimSpace(string(b))
	for _, c := range s {
		if c < 0x20 || c > 0x7e {
			return ""
		}
	}
	return s
}

// SameChip: two chip names ("ESP32-S3", "esp32s3") are the same chip.
func SameChip(a, b string) bool { return normChip(a) == normChip(b) }
