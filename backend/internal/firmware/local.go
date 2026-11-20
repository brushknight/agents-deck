package firmware

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/update"
)

// Firmware built on this computer: a second catalog, next to the releases.
//
// The folder LocalDir in the service's data folder holds builds in the
// format the release script writes (.github/scripts/firmware_release.py),
// one set of files per board and version:
//
//	agents-deck-<board>-<version>.bin            the full image, written at 0x0 over USB
//	agents-deck-<board>-<version>.app.bin        the app image, sent over Wi-Fi (boards with Wi-Fi)
//	agents-deck-<board>-<version>.manifest.json  what the release script says about them
//
// A firmware-manifest.json may sit next to them; it isn't read. Nothing here
// comes from the network: `agentctl firmware add` copies a build in, after
// the same checks as a firmware file picked in Settings, and every install
// checks the files again (size, SHA-256 against the manifest, the image
// headers, the chip). The version is the manifest's, the one the firmware
// reports (AGENTS_DECK_VERSION). Board and version always come from a manifest that
// passed CheckNames, never from a path someone typed.

// LocalDir is the folder, in the service's data folder, of firmware built
// on this computer.
const LocalDir = "firmware-builds"

// maxLocalManifest caps a board's manifest (they are about a kilobyte).
const maxLocalManifest = 1 << 20

var (
	// boardRe: a folder under firmware/devices.
	boardRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	// versionRe: a version as Hello carries it (23 bytes) and as file names
	// hold it (build_info.py's rule).
	versionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,22}$`)
)

// CheckNames: a board and a version that can name a build's files.
func CheckNames(board, version string) error {
	if !boardRe.MatchString(board) {
		return fmt.Errorf("%q isn't a board name (a folder under firmware/devices: lower-case letters, digits and dashes)", board)
	}
	if !versionRe.MatchString(version) || strings.Contains(version, "..") {
		return fmt.Errorf("%q isn't a firmware version (at most 23 letters, digits, dots, dashes, plus signs or underscores, like 0.2.0-dev.7)", version)
	}
	if _, err := update.ParseVersion(version); err != nil {
		return fmt.Errorf("version %q doesn't compare with others (it needs MAJOR.MINOR.PATCH, like 0.2.0-dev.7)", version)
	}
	return nil
}

// BaseName is the start of a build's file names.
func BaseName(board, version string) string { return "agents-deck-" + board + "-" + version }

// LocalBuild is one build in the folder: the board as a catalog has it
// (File and OTA.File are file names in the folder).
type LocalBuild struct {
	Board
	Added time.Time `json:"added"`
}

// Verified is a build whose files were just checked, with where they are
// and what their headers say.
type Verified struct {
	LocalBuild
	FullPath string
	AppPath  string // "" when the build has no app image
	Full     ImageInfo
	App      ImageInfo
}

// Builds is the folder of firmware built on this computer.
type Builds struct{ Dir string }

// List reads every build in the folder, by board and newest version first,
// and says in words which files it skipped and why.
func (b Builds) List() ([]LocalBuild, []string) {
	if b.Dir == "" {
		return nil, nil
	}
	names, _ := filepath.Glob(filepath.Join(b.Dir, "agents-deck-*.manifest.json"))
	var out []LocalBuild
	var skipped []string
	for _, p := range names {
		lb, err := b.read(p)
		if err != nil {
			skipped = append(skipped, filepath.Base(p)+": "+err.Error())
			continue
		}
		out = append(out, lb)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Board.Board != out[j].Board.Board {
			return out[i].Board.Board < out[j].Board.Board
		}
		return update.Newer(out[i].Version, out[j].Version)
	})
	return out, skipped
}

// read reads one manifest in the folder and checks that its files are
// there with the sizes it says (Open checks their contents).
func (b Builds) read(path string) (LocalBuild, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return LocalBuild{}, err
	}
	if !fi.Mode().IsRegular() {
		return LocalBuild{}, errors.New("not a plain file")
	}
	data, err := readCapped(path, maxLocalManifest)
	if err != nil {
		return LocalBuild{}, err
	}
	board, _, err := ParseBuildManifest(data)
	if err != nil {
		return LocalBuild{}, err
	}
	if filepath.Base(path) != BaseName(board.Board, board.Version)+".manifest.json" {
		return LocalBuild{}, fmt.Errorf("its name doesn't match the board and version inside (%s %s)", board.Board, board.Version)
	}
	if err := sizeIs(filepath.Join(b.Dir, board.File), board.Size); err != nil {
		return LocalBuild{}, err
	}
	if board.OTA != nil {
		if err := sizeIs(filepath.Join(b.Dir, board.OTA.File), board.OTA.Size); err != nil {
			return LocalBuild{}, err
		}
	}
	return LocalBuild{Board: board, Added: fi.ModTime().UTC()}, nil
}

func sizeIs(path string, size int64) error {
	fi, err := os.Lstat(path)
	switch {
	case err != nil:
		return fmt.Errorf("%s is missing", filepath.Base(path))
	case !fi.Mode().IsRegular():
		return fmt.Errorf("%s isn't a plain file", filepath.Base(path))
	case size > 0 && fi.Size() != size:
		return fmt.Errorf("%s is %d bytes, its manifest says %d", filepath.Base(path), fi.Size(), size)
	}
	return nil
}

// Latest is the newest build of each board, as a catalog.
func (b Builds) Latest() Catalog {
	list, _ := b.List()
	var cat Catalog
	seen := map[string]bool{}
	for _, lb := range list {
		if !seen[lb.Board.Board] {
			seen[lb.Board.Board] = true
			cat.Boards = append(cat.Boards, lb.Board)
		}
	}
	return cat
}

// Open finds board's build of version and checks its files again: their
// sizes and SHA-256 against the manifest, and their headers (a full image
// and an app image for the manifest's chip, of the manifest's version).
func (b Builds) Open(board, version string) (Verified, error) {
	if err := CheckNames(board, version); err != nil {
		return Verified{}, err
	}
	list, _ := b.List()
	for _, lb := range list {
		if lb.Board.Board != board || lb.Version != version {
			continue
		}
		v := Verified{LocalBuild: lb, FullPath: filepath.Join(b.Dir, lb.File)}
		if lb.OTA != nil {
			v.AppPath = filepath.Join(b.Dir, lb.OTA.File)
		}
		var err error
		v.Full, v.App, err = verifyBuild(lb.Board, v.FullPath, v.AppPath)
		if err != nil {
			return Verified{}, fmt.Errorf("the build of %s %s on this computer: %w", board, version, err)
		}
		return v, nil
	}
	return Verified{}, fmt.Errorf("no build of %s %s on this computer (agentctl firmware ls lists them)", board, version)
}

// Add files a build: manifest is the board's .manifest.json as the release
// script writes it (or its "agents_deck" part, or the board's entry in
// firmware-manifest.json); full and app are files with the images (app ""
// for a board without one). They are checked as Open checks them, then
// moved into the folder; a build of the same board and version is replaced.
func (b Builds) Add(manifest []byte, full, app string) (LocalBuild, error) {
	if b.Dir == "" {
		return LocalBuild{}, errors.New("this service keeps no firmware builds")
	}
	board, stored, err := ParseBuildManifest(manifest)
	if err != nil {
		return LocalBuild{}, err
	}
	switch {
	case board.OTA != nil && app == "":
		return LocalBuild{}, fmt.Errorf("the manifest lists %s, the app image for Wi-Fi: add it too", board.OTA.File)
	case board.OTA == nil && app != "":
		return LocalBuild{}, errors.New("the manifest lists no app image, so the app image can't go with it")
	}
	if _, _, err := verifyBuild(board, full, app); err != nil {
		return LocalBuild{}, err
	}
	if err := os.MkdirAll(b.Dir, 0o700); err != nil {
		return LocalBuild{}, err
	}
	base := BaseName(board.Board, board.Version)
	if err := moveInto(full, filepath.Join(b.Dir, base+".bin")); err != nil {
		return LocalBuild{}, err
	}
	if app != "" {
		if err := moveInto(app, filepath.Join(b.Dir, base+".app.bin")); err != nil {
			return LocalBuild{}, err
		}
	} else {
		os.Remove(filepath.Join(b.Dir, base+".app.bin")) // an older build's
	}
	mf := filepath.Join(b.Dir, base+".manifest.json")
	tmp := mf + ".tmp"
	if err := os.WriteFile(tmp, stored, 0o600); err != nil {
		return LocalBuild{}, err
	}
	if err := os.Rename(tmp, mf); err != nil {
		os.Remove(tmp)
		return LocalBuild{}, err
	}
	return b.read(mf)
}

// moveInto renames src to dst, or copies it when they are on different disks.
func moveInto(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// Remove deletes board's build of version, or every build of board when
// version is "". It answers the versions it removed.
func (b Builds) Remove(board, version string) ([]string, error) {
	if !boardRe.MatchString(board) {
		return nil, CheckNames(board, "0.0.0")
	}
	if version != "" {
		if err := CheckNames(board, version); err != nil {
			return nil, err
		}
	}
	list, skipped := b.List()
	var removed []string
	for _, lb := range list {
		if lb.Board.Board != board || (version != "" && lb.Version != version) {
			continue
		}
		base := filepath.Join(b.Dir, BaseName(lb.Board.Board, lb.Version))
		for _, suffix := range []string{".manifest.json", ".bin", ".app.bin"} {
			if err := os.Remove(base + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
				return removed, err
			}
		}
		removed = append(removed, lb.Version)
	}
	if len(removed) == 0 {
		// A build whose manifest was skipped (its files are broken) goes too.
		if version != "" {
			prefix := BaseName(board, version) + ".manifest.json: "
			for _, s := range skipped {
				if strings.HasPrefix(s, prefix) {
					base := filepath.Join(b.Dir, BaseName(board, version))
					for _, suffix := range []string{".manifest.json", ".bin", ".app.bin"} {
						os.Remove(base + suffix)
					}
					return []string{version}, nil
				}
			}
			return nil, fmt.Errorf("no build of %s %s on this computer", board, version)
		}
		return nil, fmt.Errorf("no build of %s on this computer", board)
	}
	return removed, nil
}

// ParseBuildManifest reads a board's manifest as the release script writes
// it (agents-deck-<board>-<version>.manifest.json, whose "agents_deck" part
// is what the deck reads), that part alone, or the board's entry in
// firmware-manifest.json. It answers the board and the bytes to keep in the
// folder, and refuses names that can't name the build's files.
func ParseBuildManifest(data []byte) (Board, []byte, error) {
	var wrap struct {
		AgentsDeck json.RawMessage `json:"agents_deck"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		return Board{}, nil, fmt.Errorf("the manifest isn't JSON: %w", err)
	}
	block, stored := []byte(wrap.AgentsDeck), data
	if len(block) == 0 || string(block) == "null" {
		block = data
		var err error
		if stored, err = json.MarshalIndent(map[string]json.RawMessage{"agents_deck": data}, "", "  "); err != nil {
			return Board{}, nil, err
		}
	}
	var head struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(block, &head); err != nil {
		return Board{}, nil, fmt.Errorf("the manifest's agents_deck part: %w", err)
	}
	index, err := json.Marshal(struct {
		Version string            `json:"version"`
		Boards  []json.RawMessage `json:"boards"`
	}{head.Version, []json.RawMessage{block}})
	if err != nil {
		return Board{}, nil, err
	}
	cat, err := ParseManifest(index)
	if err != nil {
		return Board{}, nil, err
	}
	if len(cat.Boards) != 1 {
		return Board{}, nil, errors.New("the manifest names no board")
	}
	bd := cat.Boards[0]
	if err := CheckNames(bd.Board, bd.Version); err != nil {
		return Board{}, nil, err
	}
	base := BaseName(bd.Board, bd.Version)
	switch {
	case bd.Chip == "":
		return Board{}, nil, errors.New("the manifest doesn't say the chip")
	case bd.File != base+".bin":
		return Board{}, nil, fmt.Errorf("the manifest's full image is %q, and a build of %s %s is %s.bin", bd.File, bd.Board, bd.Version, base)
	case bd.Offset != 0:
		return Board{}, nil, fmt.Errorf("the manifest writes the full image at %#x: a build here is a full image written at 0x0", bd.Offset)
	case bd.OTA != nil && bd.OTA.File != base+".app.bin":
		return Board{}, nil, fmt.Errorf("the manifest's app image is %q, and a build of %s %s is %s.app.bin", bd.OTA.File, bd.Board, bd.Version, base)
	}
	return bd, stored, nil
}

// verifyBuild checks a build's images against its manifest.
func verifyBuild(bd Board, full, app string) (ImageInfo, ImageInfo, error) {
	fi, err := verifyImage(full, bd.File, bd.Size, bd.SHA256, ImageFull, bd)
	if err != nil {
		return ImageInfo{}, ImageInfo{}, err
	}
	if bd.FlashSize > 0 && fi.FlashSize > 0 && fi.FlashSize != bd.FlashSize {
		return ImageInfo{}, ImageInfo{}, fmt.Errorf("%s was built for %d MB of flash, its manifest says %d MB", bd.File, fi.FlashSize>>20, bd.FlashSize>>20)
	}
	var ai ImageInfo
	if bd.OTA != nil {
		if ai, err = verifyImage(app, bd.OTA.File, bd.OTA.Size, bd.OTA.SHA256, ImageApp, bd); err != nil {
			return ImageInfo{}, ImageInfo{}, err
		}
	}
	return fi, ai, nil
}

func verifyImage(path, name string, size int64, sum, kind string, bd Board) (ImageInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return ImageInfo{}, fmt.Errorf("%s: %w", name, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ImageInfo{}, err
	}
	if !st.Mode().IsRegular() {
		return ImageInfo{}, fmt.Errorf("%s isn't a plain file", name)
	}
	if st.Size() > MaxFileImage {
		return ImageInfo{}, fmt.Errorf("%s: %w", name, ErrTooBig)
	}
	if size > 0 && st.Size() != size {
		return ImageInfo{}, fmt.Errorf("%s is %d bytes, its manifest says %d", name, st.Size(), size)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ImageInfo{}, err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != strings.ToLower(sum) {
		return ImageInfo{}, fmt.Errorf("%s doesn't match the SHA-256 in its manifest", name)
	}
	info, err := InspectImage(f, st.Size())
	if err != nil {
		return ImageInfo{}, fmt.Errorf("%s: %w", name, err)
	}
	if info.Kind != kind {
		what := map[string]string{ImageFull: "a full image", ImageApp: "an app image"}
		return ImageInfo{}, fmt.Errorf("%s is %s, and the manifest lists it as %s", name, what[info.Kind], what[kind])
	}
	if !SameChip(info.Chip, bd.Chip) {
		return ImageInfo{}, fmt.Errorf("%s is for an %s, and its manifest says %s", name, info.Chip, bd.Chip)
	}
	// The app description's version is the build system's (often `git
	// describe`), not the one the firmware reports in Hello, so it isn't
	// compared: the manifest's is the one that counts.
	return info, nil
}

func readCapped(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("too big for a manifest")
	}
	return b, nil
}
