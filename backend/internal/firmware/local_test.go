package firmware

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// releaseBuild writes a board's build into dir as the release script does
// (firmware_release.py build): the full image, the app image when withApp,
// and the manifest with its agents_deck part. It answers the manifest.
func releaseBuild(t *testing.T, dir, board, version string, withApp bool) []byte {
	t.Helper()
	base := BaseName(board, version)
	app := appImage(0x0009, "v0.1.1-108-gd574bf3-dirty", "agents-deck", 8192)
	full := fullImage(0x0009, app)
	full[3] = 4 << 4 // 16 MB of flash, as the manifest says
	if err := os.WriteFile(filepath.Join(dir, base+".bin"), full, 0o600); err != nil {
		t.Fatal(err)
	}
	var ota any
	if withApp {
		if err := os.WriteFile(filepath.Join(dir, base+".app.bin"), app, 0o600); err != nil {
			t.Fatal(err)
		}
		ota = map[string]any{"file": base + ".app.bin", "size": len(app), "sha256": sum(app)}
	}
	m, _ := json.MarshalIndent(map[string]any{
		"name": "A made-up board", "version": version,
		"builds": []any{map[string]any{"chipFamily": "ESP32-S3", "parts": []any{map[string]any{"path": base + ".bin", "offset": 0}}}},
		"agents_deck": map[string]any{
			"board": board, "version": version, "chip": "esp32s3", "file": base + ".bin", "offset": "0x0",
			"size": len(full), "sha256": sum(full), "env": "idf5", "name": "A made-up board",
			"types": []string{board + "-panel"}, "usb": []any{map[string]string{"vid": "303a", "pid": "1001"}},
			"flash_size": 16 << 20, "wifi": withApp, "ota": ota,
		},
	}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, base+".manifest.json"), m, 0o600); err != nil {
		t.Fatal(err)
	}
	return m
}

// add copies a build from src into the folder as agentctl firmware add does.
func add(t *testing.T, b Builds, src, board, version string) (LocalBuild, error) {
	t.Helper()
	base := filepath.Join(src, BaseName(board, version))
	m, err := os.ReadFile(base + ".manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	cp := func(p string) string {
		data, err := os.ReadFile(p)
		if err != nil {
			return ""
		}
		f, _ := os.CreateTemp(t.TempDir(), "up-*")
		f.Write(data)
		f.Close()
		return f.Name()
	}
	return b.Add(m, cp(base+".bin"), cp(base+".app.bin"))
}

func TestBuildsAddListOpenRemove(t *testing.T) {
	src, dir := t.TempDir(), filepath.Join(t.TempDir(), LocalDir)
	b := Builds{Dir: dir}
	releaseBuild(t, src, "made-up-board", "0.2.0-dev.7", true)
	releaseBuild(t, src, "made-up-board", "0.2.0-dev.10", true)
	releaseBuild(t, src, "usb-only-board", "0.1.0", false)
	for _, v := range [][2]string{{"made-up-board", "0.2.0-dev.7"}, {"made-up-board", "0.2.0-dev.10"}, {"usb-only-board", "0.1.0"}} {
		if _, err := add(t, b, src, v[0], v[1]); err != nil {
			t.Fatalf("add %v: %v", v, err)
		}
	}
	list, skipped := b.List()
	if len(list) != 3 || len(skipped) != 0 {
		t.Fatalf("list: %+v, skipped %v", list, skipped)
	}
	// Newest first, by semver: dev.10 is newer than dev.7.
	if list[0].Version != "0.2.0-dev.10" || list[0].Chip != "ESP32-S3" || list[0].OTA == nil || !list[0].Wifi {
		t.Errorf("first: %+v", list[0])
	}
	cat := b.Latest()
	if got, _ := cat.Board("made-up-board"); got.Version != "0.2.0-dev.10" {
		t.Errorf("latest: %+v", got)
	}
	if got, ok := cat.BoardForType("made-up-board-panel"); !ok || got.Board != "made-up-board" {
		t.Errorf("by Hello type: %+v %v", got, ok)
	}
	v, err := b.Open("made-up-board", "0.2.0-dev.7")
	if err != nil || v.Full.Kind != ImageFull || v.App.Kind != ImageApp || filepath.Dir(v.FullPath) != dir || filepath.Dir(v.AppPath) != dir {
		t.Fatalf("open: %+v %v", v, err)
	}
	if v, err := b.Open("usb-only-board", "0.1.0"); err != nil || v.AppPath != "" {
		t.Errorf("a build without an app image: %+v %v", v, err)
	}
	// The files are checked again when opened: a changed image is refused.
	p := filepath.Join(dir, BaseName("made-up-board", "0.2.0-dev.7")+".bin")
	data, _ := os.ReadFile(p)
	data[0x10000+100] ^= 0xff
	os.WriteFile(p, data, 0o600)
	if _, err := b.Open("made-up-board", "0.2.0-dev.7"); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Errorf("a changed image: %v", err)
	}
	if removed, err := b.Remove("made-up-board", ""); err != nil || len(removed) != 2 {
		t.Errorf("remove: %v %v", removed, err)
	}
	if list, _ := b.List(); len(list) != 1 {
		t.Errorf("after remove: %+v", list)
	}
	if _, err := b.Remove("made-up-board", "0.2.0-dev.7"); err == nil {
		t.Error("removed a build that isn't there")
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), BaseName("made-up-board", "")) {
			t.Errorf("left behind: %s", e.Name())
		}
	}
}

func TestBuildsRefuse(t *testing.T) {
	src := t.TempDir()
	b := Builds{Dir: filepath.Join(t.TempDir(), LocalDir)}
	m := releaseBuild(t, src, "made-up-board", "0.2.0", true)
	base := filepath.Join(src, BaseName("made-up-board", "0.2.0"))
	edit := func(f func(ad map[string]any)) []byte {
		var doc map[string]any
		json.Unmarshal(m, &doc)
		f(doc["agents_deck"].(map[string]any))
		out, _ := json.Marshal(doc)
		return out
	}
	for name, tc := range map[string]struct {
		manifest  []byte
		full, app string
		want      string
	}{
		"path in the board":   {edit(func(ad map[string]any) { ad["board"] = "../escape" }), base + ".bin", base + ".app.bin", "isn't a board name"},
		"path in the version": {edit(func(ad map[string]any) { ad["version"] = "../../x" }), base + ".bin", base + ".app.bin", "isn't a firmware version"},
		"no semver":           {edit(func(ad map[string]any) { ad["version"] = "dev" }), base + ".bin", base + ".app.bin", "doesn't compare"},
		"other file name":     {edit(func(ad map[string]any) { ad["file"] = "../../etc/x.bin" }), base + ".bin", base + ".app.bin", "full image is"},
		"other offset":        {edit(func(ad map[string]any) { ad["offset"] = "0x10000" }), base + ".bin", base + ".app.bin", "0x0"},
		"other chip":          {edit(func(ad map[string]any) { ad["chip"] = "esp32c3" }), base + ".bin", base + ".app.bin", "is for an ESP32-S3"},
		"swapped images":      {m, base + ".app.bin", base + ".bin", "bytes"},
		"app missing":         {m, base + ".bin", "", "add it too"},
		"not JSON":            {[]byte("nope"), base + ".bin", base + ".app.bin", "isn't JSON"},
	} {
		if _, err := b.Add(tc.manifest, tc.full, tc.app); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
	if list, _ := b.List(); len(list) != 0 {
		t.Errorf("a refused build was kept: %+v", list)
	}
	// Names from a client never reach a path: Open and Remove check them.
	if _, err := b.Open("../x", "0.1.0"); err == nil {
		t.Error("opened ../x")
	}
	if _, err := b.Remove("..", ""); err == nil {
		t.Error("removed ..")
	}
}

// A build copied into the folder by hand is listed; one whose files don't
// match its manifest is skipped with the reason.
func TestBuildsByHand(t *testing.T) {
	dir := t.TempDir()
	releaseBuild(t, dir, "made-up-board", "0.2.0-dev.7", true)
	releaseBuild(t, dir, "other-board", "0.1.0", true)
	os.Remove(filepath.Join(dir, BaseName("other-board", "0.1.0")+".app.bin"))
	os.WriteFile(filepath.Join(dir, "firmware-manifest.json"), []byte(`{"version":"0.2.0-dev.7","boards":[]}`), 0o600)
	list, skipped := Builds{Dir: dir}.List()
	if len(list) != 1 || list[0].Board.Board != "made-up-board" {
		t.Errorf("list: %+v", list)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "missing") {
		t.Errorf("skipped: %v", skipped)
	}
}

// The board's entry in firmware-manifest.json, and the agents_deck part
// alone, are manifests too.
func TestParseBuildManifestForms(t *testing.T) {
	m := releaseBuild(t, t.TempDir(), "made-up-board", "0.2.0", true)
	var doc map[string]json.RawMessage
	json.Unmarshal(m, &doc)
	for name, data := range map[string][]byte{"board manifest": m, "agents_deck part": doc["agents_deck"]} {
		bd, stored, err := ParseBuildManifest(data)
		if err != nil || bd.Board != "made-up-board" || bd.Version != "0.2.0" || bd.OTA == nil || !strings.Contains(string(stored), "agents_deck") {
			t.Errorf("%s: %+v %v", name, bd, err)
		}
	}
}
