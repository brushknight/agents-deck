// Package firmware lists the board firmware in a release (firmware-manifest.json)
// and downloads a board's image, checked against the release's signed
// SHA256SUMS. Images are cached under paths.Data()/releases/<version>/.
package firmware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/update"
)

// ManifestFile lists every board's image in a release.
const ManifestFile = "firmware-manifest.json"

const (
	maxManifest = 1 << 20
	maxImage    = 64 << 20 // the biggest flash on a supported board is 32 MB
	latestTTL   = 15 * time.Minute
)

// USBID is a USB vendor and product id, lower-case hex.
type USBID struct {
	VID string `json:"vid"`
	PID string `json:"pid"`
}

// OTA is a board's over-the-air image: the app alone, for its second slot.
type OTA struct {
	File   string `json:"file"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Board is one board's firmware in a release.
type Board struct {
	Board     string   `json:"board"`           // folder under firmware/devices
	Name      string   `json:"name"`            // as people know it
	Chip      string   `json:"chip"`            // "ESP32-S3"
	Types     []string `json:"types,omitempty"` // every Hello.type its firmware reports
	USB       []USBID  `json:"usb,omitempty"`
	File      string   `json:"file"` // the full image
	Offset    uint32   `json:"offset"`
	Size      int64    `json:"size"`
	SHA256    string   `json:"sha256"`
	FlashSize int64    `json:"flash_size,omitempty"` // bytes; 0 when the release doesn't say
	OTA       *OTA     `json:"ota,omitempty"`
	Picture   string   `json:"picture,omitempty"` // a path in the repository
	Wifi      bool     `json:"wifi"`
	Version   string   `json:"version"`
}

// Catalog is a release's boards.
type Catalog struct {
	Version string  `json:"version"`
	Boards  []Board `json:"boards"`
}

// Client reads the catalog and images of releases.
type Client struct {
	Releases *update.Releases

	mu       sync.Mutex
	catalogs map[string]Catalog // by version, from signed releases
	latest   string
	latestAt time.Time
}

// New reads GitHub releases (or AGENTS_DECK_BASE_URL) with the built-in key.
func New() *Client { return &Client{Releases: update.NewReleases()} }

// Latest is the latest release's catalog. Without a release key it is read
// against the unsigned checksums, for showing only: Image and OTAImage refuse.
func (c *Client) Latest(ctx context.Context) (Catalog, error) {
	c.mu.Lock()
	if c.latest != "" && time.Since(c.latestAt) < latestTTL {
		cat, ok := c.catalogs[c.latest]
		c.mu.Unlock()
		if ok {
			return cat, nil
		}
	} else {
		c.mu.Unlock()
	}
	rel, err := c.Releases.Latest(ctx)
	if err != nil {
		return Catalog{}, err
	}
	cat, err := c.catalog(ctx, rel)
	if err == nil && rel.Signed {
		c.mu.Lock()
		c.latest, c.latestAt = rel.Version, time.Now()
		c.mu.Unlock()
	}
	return cat, err
}

// Catalog is one release's catalog; version "" or "latest" is the latest.
func (c *Client) Catalog(ctx context.Context, version string) (Catalog, error) {
	_, cat, err := c.release(ctx, version)
	return cat, err
}

func (c *Client) release(ctx context.Context, version string) (*update.Release, Catalog, error) {
	var rel *update.Release
	var err error
	if version == "" || version == "latest" {
		rel, err = c.Releases.Latest(ctx)
	} else {
		rel, err = c.Releases.Release(ctx, version)
	}
	if err != nil {
		return nil, Catalog{}, err
	}
	cat, err := c.catalog(ctx, rel)
	return rel, cat, err
}

func (c *Client) catalog(ctx context.Context, rel *update.Release) (Catalog, error) {
	if rel.Signed {
		c.mu.Lock()
		cat, ok := c.catalogs[rel.Version]
		c.mu.Unlock()
		if ok {
			return cat, nil
		}
	}
	b, err := c.Releases.Read(ctx, rel, ManifestFile, maxManifest)
	if err != nil {
		return Catalog{}, err
	}
	cat, err := ParseManifest(b)
	if err != nil {
		return Catalog{}, err
	}
	if v, err := update.ParseVersion(cat.Version); err != nil || v.String() != rel.Version {
		return Catalog{}, fmt.Errorf("%s is for version %q, the release is %s", ManifestFile, cat.Version, rel.Version)
	}
	for _, b := range cat.Boards {
		if sum, ok := rel.Sums[b.File]; !ok || sum != b.SHA256 {
			return Catalog{}, fmt.Errorf("%s: %s doesn't match SHA256SUMS", ManifestFile, b.File)
		}
		if b.OTA != nil {
			if sum, ok := rel.Sums[b.OTA.File]; !ok || sum != b.OTA.SHA256 {
				return Catalog{}, fmt.Errorf("%s: %s doesn't match SHA256SUMS", ManifestFile, b.OTA.File)
			}
		}
	}
	if rel.Signed {
		c.mu.Lock()
		if c.catalogs == nil {
			c.catalogs = map[string]Catalog{}
		}
		c.catalogs[rel.Version] = cat
		c.mu.Unlock()
	}
	return cat, nil
}

// Image is the path of a board's full image (flashed at Board.Offset),
// downloaded and checked; version "" is the latest.
func (c *Client) Image(ctx context.Context, board, version string) (string, error) {
	rel, cat, err := c.release(ctx, version)
	if err != nil {
		return "", err
	}
	b, ok := cat.Board(board)
	if !ok {
		return "", fmt.Errorf("release %s has no firmware for %q", cat.Version, board)
	}
	return c.Releases.File(ctx, rel, b.File, limit(b.Size))
}

// OTAImage is a board's over-the-air image, downloaded and checked, with its
// size and SHA-256 (hex); version "" is the latest.
func (c *Client) OTAImage(ctx context.Context, board, version string) (string, int64, string, error) {
	rel, cat, err := c.release(ctx, version)
	if err != nil {
		return "", 0, "", err
	}
	b, ok := cat.Board(board)
	if !ok {
		return "", 0, "", fmt.Errorf("release %s has no firmware for %q", cat.Version, board)
	}
	if b.OTA == nil {
		return "", 0, "", fmt.Errorf("%s has no over-the-air image in release %s: update it over USB", b.Name, cat.Version)
	}
	p, err := c.Releases.File(ctx, rel, b.OTA.File, limit(b.OTA.Size))
	if err != nil {
		return "", 0, "", err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return "", 0, "", err
	}
	if b.OTA.Size > 0 && fi.Size() != b.OTA.Size {
		return "", 0, "", fmt.Errorf("%s is %d bytes, the manifest says %d", b.OTA.File, fi.Size(), b.OTA.Size)
	}
	return p, fi.Size(), b.OTA.SHA256, nil
}

func limit(size int64) int64 {
	if size > 0 && size < maxImage {
		return size
	}
	return maxImage
}

// Board finds a board by its id.
func (c Catalog) Board(id string) (Board, bool) {
	for _, b := range c.Boards {
		if b.Board == id {
			return b, true
		}
	}
	return Board{}, false
}

// BoardForType is the board whose firmware reports this Hello type. Releases
// from before boards listed their types match only the board id itself.
func (c Catalog) BoardForType(helloType string) (Board, bool) {
	for _, b := range c.Boards {
		for _, t := range b.Types {
			if t == helloType {
				return b, true
			}
		}
	}
	return c.Board(helloType)
}

// Candidates are the boards whose firmware fits a chip (as the bootloader
// names it, "ESP32-S3") with this much flash (0: unknown), the ones made for
// exactly that flash size first.
func (c Catalog) Candidates(chip string, flashSize int64) []Board {
	want := normChip(chip)
	var out []Board
	for _, b := range c.Boards {
		if normChip(b.Chip) != want {
			continue
		}
		if flashSize > 0 {
			need := b.FlashSize
			if need == 0 {
				need = b.Size
			}
			if need > flashSize {
				continue
			}
		}
		out = append(out, b)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ei, ej := out[i].FlashSize == flashSize, out[j].FlashSize == flashSize
		if ei != ej {
			return ei
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ForUSB are the boards that use this USB id (lower-case hex).
func (c Catalog) ForUSB(vid, pid string) []Board {
	var out []Board
	for _, b := range c.Boards {
		for _, u := range b.USB {
			if strings.EqualFold(u.VID, vid) && strings.EqualFold(u.PID, pid) {
				out = append(out, b)
				break
			}
		}
	}
	return out
}

func normChip(s string) string {
	return strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(s))
}

// manifestBoard is a board as firmware-manifest.json writes it: today's fields
// (board, name, chip, chipFamily, file, offset, size, sha256, env, version)
// and the newer ones (types, usb, flash_size, wifi, picture, ota), each optional.
type manifestBoard struct {
	Board      string          `json:"board"`
	Name       string          `json:"name"`
	Chip       string          `json:"chip"`
	ChipFamily string          `json:"chipFamily"`
	File       string          `json:"file"`
	Offset     json.RawMessage `json:"offset"`
	Size       int64           `json:"size"`
	SHA256     string          `json:"sha256"`
	Env        string          `json:"env"`
	Version    string          `json:"version"`
	Types      []string        `json:"types"`
	USB        []USBID         `json:"usb"`
	FlashSize  int64           `json:"flash_size"`
	Wifi       *bool           `json:"wifi"`
	Picture    string          `json:"picture"`
	OTA        *OTA            `json:"ota"`
}

// ParseManifest reads firmware-manifest.json.
func ParseManifest(b []byte) (Catalog, error) {
	var m struct {
		Version string          `json:"version"`
		Boards  []manifestBoard `json:"boards"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return Catalog{}, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	cat := Catalog{Version: m.Version}
	seen := map[string]bool{}
	for _, mb := range m.Boards {
		if mb.Board == "" || mb.File == "" || len(mb.SHA256) != 64 {
			return Catalog{}, fmt.Errorf("%s: a board without board, file or sha256", ManifestFile)
		}
		if seen[mb.Board] {
			return Catalog{}, fmt.Errorf("%s: %s listed twice", ManifestFile, mb.Board)
		}
		seen[mb.Board] = true
		off, err := parseOffset(mb.Offset)
		if err != nil {
			return Catalog{}, fmt.Errorf("%s: %s: %w", ManifestFile, mb.Board, err)
		}
		b := Board{
			Board: mb.Board, Name: mb.Name, Chip: mb.ChipFamily, Types: mb.Types,
			File: mb.File, Offset: off, Size: mb.Size, SHA256: strings.ToLower(mb.SHA256),
			FlashSize: mb.FlashSize, Picture: mb.Picture, Version: mb.Version,
			// Before the manifest said so, the idf5 builds were the Wi-Fi ones.
			Wifi: mb.Env == "idf5",
		}
		if b.Chip == "" {
			b.Chip = chipName(mb.Chip)
		}
		if b.Name == "" {
			b.Name = b.Board
		}
		if b.Version == "" {
			b.Version = m.Version
		}
		if mb.Wifi != nil {
			b.Wifi = *mb.Wifi
		}
		for _, u := range mb.USB {
			b.USB = append(b.USB, USBID{VID: strings.ToLower(u.VID), PID: strings.ToLower(u.PID)})
		}
		// The release script writes an empty ota block for boards without one.
		if mb.OTA != nil && mb.OTA.File != "" {
			if len(mb.OTA.SHA256) != 64 {
				return Catalog{}, fmt.Errorf("%s: %s: ota without sha256", ManifestFile, mb.Board)
			}
			o := *mb.OTA
			o.SHA256 = strings.ToLower(o.SHA256)
			b.OTA = &o
		}
		cat.Boards = append(cat.Boards, b)
	}
	return cat, nil
}

// chipName turns esptool's "esp32s3" into the bootloader's "ESP32-S3".
func chipName(s string) string {
	s = strings.ToUpper(s)
	if strings.HasPrefix(s, "ESP32") && len(s) > 5 && s[5] != '-' {
		return "ESP32-" + s[5:]
	}
	return s
}

func parseOffset(raw json.RawMessage) (uint32, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}
	var n uint32
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, errors.New("offset is neither a number nor a string")
	}
	v, err := strconv.ParseUint(s, 0, 32)
	if err != nil {
		return 0, fmt.Errorf("offset %q: %w", s, err)
	}
	return uint32(v), nil
}
