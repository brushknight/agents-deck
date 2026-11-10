package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleasePub(t *testing.T) {
	// Empty (no key) or one line of base64 holding 32 bytes: anything else
	// would silently turn updates off.
	if strings.TrimSpace(string(releasePub)) == "" {
		return
	}
	if _, err := ReleaseKey(); err != nil {
		t.Fatalf("release.pub: %v", err)
	}
}

func TestSignVerify(t *testing.T) {
	priv, pub, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	k, err := ParsePrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	if PublicOf(k) != pub {
		t.Fatal("public key doesn't belong to the private one")
	}
	full, err := ParsePrivateKey(b64(k))
	if err != nil || !bytes.Equal(full, k) {
		t.Fatalf("64-byte private key: %v", err)
	}
	pk, err := ParsePublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("abc  file\n")
	sig := Sign(k, data)
	if bytes.Count(sig, []byte("\n")) != 1 || !bytes.HasSuffix(sig, []byte("\n")) {
		t.Fatalf("signature isn't one line: %q", sig)
	}
	if err := Verify(pk, data, sig); err != nil {
		t.Fatal(err)
	}
	if err := Verify(pk, append(data, ' '), sig); err == nil {
		t.Fatal("changed data verified")
	}
	if err := Verify(pk, data, []byte("bm90IGEgc2lnbmF0dXJl\n")); err == nil {
		t.Fatal("garbage verified")
	}
	if err := Verify(nil, data, sig); err != ErrNoReleaseKey {
		t.Fatalf("no key: %v", err)
	}
	if _, err := ParsePublicKey("AAAA"); err == nil {
		t.Fatal("short key accepted")
	}
}

func b64(k ed25519.PrivateKey) string { return base64.StdEncoding.EncodeToString(k) }

func TestParseSums(t *testing.T) {
	h := strings.Repeat("ab", 32)
	got, err := ParseSums([]byte(h + "  agentctl_0.2.0_darwin_arm64.tar.gz\n" + strings.ToUpper(h) + " *firmware-manifest.json\r\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got["agentctl_0.2.0_darwin_arm64.tar.gz"] != h || got["firmware-manifest.json"] != h {
		t.Fatalf("%v", got)
	}
	for _, bad := range []string{
		"", h + "  ../etc/passwd\n", h + "  a/b\n", "xyz  a\n", h + "  a\n" + h + "  a\n", h + "  .hidden\n",
	} {
		if _, err := ParseSums([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestVersions(t *testing.T) {
	order := []string{
		"0.1.0", "0.1.1", "0.2.0-alpha", "0.2.0-alpha.1", "0.2.0-alpha.beta", "0.2.0-beta",
		"0.2.0-beta.2", "0.2.0-beta.11", "0.2.0-rc.1", "0.2.0", "v0.2.1", "1.0.0",
	}
	for i := range order {
		for j := range order {
			a, err := ParseVersion(order[i])
			if err != nil {
				t.Fatal(err)
			}
			b, _ := ParseVersion(order[j])
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := a.Compare(b); got != want {
				t.Errorf("%s vs %s = %d, want %d", order[i], order[j], got, want)
			}
		}
	}
	if v, _ := ParseVersion("1.2.3+build.5"); v.Compare(Version{Major: 1, Minor: 2, Patch: 3}) != 0 {
		t.Error("build metadata counts")
	}
	for _, bad := range []string{"", "dev", "1.2", "1.2.3.4", "01.2.3", "1.2.3-", "1.2.3-01", "1.2.x"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("parsed %q", bad)
		}
	}
	if !Newer("0.2.0", "0.2.0-rc.1") || Newer("0.2.0-rc.1", "0.2.0") || Newer("0.1.9", "0.2.0") || Newer("0.2.0", "dev") {
		t.Error("Newer")
	}
	for v, dev := range map[string]bool{
		"0.2.0": false, "0.2.0-rc.1": false, "dev": true, "": true, "v0.2.0": true,
		"0.2.0-3-gabc1234": true, "0.2.0-dirty": true, "abc1234": true,
	} {
		if IsDevBuild(v) != dev {
			t.Errorf("IsDevBuild(%q) = %v", v, !dev)
		}
	}
}

func TestDetectKind(t *testing.T) {
	cases := []struct{ goos, exe, version, want string }{
		{"darwin", "/Applications/agents deck.app/Contents/MacOS/agentctl", "0.2.0", KindApp},
		{"darwin", "/Users/someone/.local/bin/agentctl", "0.2.0", KindArchive},
		{"darwin", "/Applications/agents deck.app/Contents/MacOS/agentctl", "dev", KindSource},
		{"linux", "/usr/bin/agentctl", "0.2.0", KindDeb},
		{"linux", "/usr/local/bin/agentctl", "0.2.0", KindArchive},
		{"linux", "/home/someone/.local/bin/agentctl", "0.2.0", KindArchive},
		{"linux", "/home/someone/.local/bin/agentctl", "v0.2.0-1-gabcdef0", KindSource},
		{"windows", `C:\Users\someone\AppData\Local\Programs\agents deck\agentctl.exe`, "0.2.0", KindWindows},
	}
	for _, c := range cases {
		if got := detectKind(c.goos, c.exe, c.version); got != c.want {
			t.Errorf("%s %s %s: %s, want %s", c.goos, c.exe, c.version, got, c.want)
		}
	}
}

func TestParseCodesign(t *testing.T) {
	devID := "Executable=/x\nAuthority=Developer ID Application: Someone (ABCDE12345)\nAuthority=Developer ID Certification Authority\nTeamIdentifier=ABCDE12345\n"
	if team, ok := parseCodesign(devID); team != "ABCDE12345" || !ok {
		t.Errorf("developer id: %q %v", team, ok)
	}
	adhoc := "Executable=/x\nSignature=adhoc\nTeamIdentifier=not set\n"
	if team, ok := parseCodesign(adhoc); team != "" || ok {
		t.Errorf("ad hoc: %q %v", team, ok)
	}
}

func TestUntarRefusesEscapes(t *testing.T) {
	for _, h := range []*tar.Header{
		{Name: "../evil", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1},
		{Name: "/abs", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1},
		{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../../outside"},
		{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc"},
	} {
		dir := t.TempDir()
		p := filepath.Join(dir, "a.tar.gz")
		writeTarGz(t, p, h, []byte("x"))
		if err := untarGz(p, filepath.Join(dir, "out"), 1<<20); err == nil {
			t.Errorf("%s accepted", h.Name)
		}
	}
	// and the size budget
	dir := t.TempDir()
	p := filepath.Join(dir, "a.tar.gz")
	writeTarGz(t, p, &tar.Header{Name: "big", Typeflag: tar.TypeReg, Mode: 0o644, Size: 100}, bytes.Repeat([]byte("x"), 100))
	if err := untarGz(p, filepath.Join(dir, "out"), 10); err == nil {
		t.Error("over budget accepted")
	}
}

func writeTarGz(t *testing.T, p string, h *tar.Header, body []byte) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(h); err != nil {
		t.Fatal(err)
	}
	if h.Typeflag == tar.TypeReg {
		tw.Write(body[:h.Size])
	}
	tw.Close()
	gz.Close()
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
