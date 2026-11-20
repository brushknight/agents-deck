package main

// Firmware built on this computer (the service's firmware-builds folder):
//
//	agentctl firmware add <folder or files>   copy builds there (the release script's output)
//	agentctl firmware rm <board> [<version>]  remove them
//	agentctl firmware ls                      the release's firmware and these builds
//
// add goes through the service (POST /admin/v1/firmware/builds), which
// checks every build as it checks a file picked in Settings before it keeps
// it; from then on a board that runs an older version installs it from its
// row in Settings, Boards, with one click.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/admin"
	"github.com/brushknight/agents-deck/backend/internal/firmware"
)

// buildFiles is one build to send: its manifest and its images on disk.
type buildFiles struct {
	manifest  []byte
	full, app string
	name      string // board and version, for messages
}

// findBuilds turns folders and files into builds. A folder gives every
// agents-deck-<board>-<version>.manifest.json in it (or, without those, the
// boards in its firmware-manifest.json); a .bin, .app.bin or .manifest.json
// gives its build, found by the manifest next to it.
func findBuilds(args []string) ([]buildFiles, error) {
	var out []buildFiles
	seen := map[string]bool{}
	addFrom := func(dir string, manifest []byte) error {
		bd, _, err := firmware.ParseBuildManifest(manifest)
		if err != nil {
			return err
		}
		key := bd.Board + " " + bd.Version
		if seen[key] {
			return nil
		}
		seen[key] = true
		b := buildFiles{manifest: manifest, full: filepath.Join(dir, bd.File), name: key}
		if bd.OTA != nil {
			b.app = filepath.Join(dir, bd.OTA.File)
		}
		for _, p := range []string{b.full, b.app} {
			if p == "" {
				continue
			}
			if _, err := os.Stat(p); err != nil {
				return fmt.Errorf("%s: %s isn't next to its manifest", key, filepath.Base(p))
			}
		}
		out = append(out, b)
		return nil
	}
	// fromIndex reads the boards of a firmware-manifest.json, keeping those
	// that want(file) says are asked for.
	fromIndex := func(dir string, want func(bd firmware.Board) bool) (int, error) {
		data, err := os.ReadFile(filepath.Join(dir, firmware.ManifestFile))
		if err != nil {
			return 0, err
		}
		var idx struct {
			Boards []json.RawMessage `json:"boards"`
		}
		if err := json.Unmarshal(data, &idx); err != nil {
			return 0, fmt.Errorf("%s: %w", firmware.ManifestFile, err)
		}
		n := 0
		for _, raw := range idx.Boards {
			bd, _, err := firmware.ParseBuildManifest(raw)
			if err != nil {
				return n, fmt.Errorf("%s: %w", firmware.ManifestFile, err)
			}
			if want(bd) {
				if err := addFrom(dir, raw); err != nil {
					return n, err
				}
				n++
			}
		}
		return n, nil
	}
	for _, arg := range args {
		fi, err := os.Stat(arg)
		if err != nil {
			return nil, err
		}
		if fi.IsDir() {
			ms, _ := filepath.Glob(filepath.Join(arg, "agents-deck-*.manifest.json"))
			sort.Strings(ms)
			for _, m := range ms {
				data, err := readSmall(m)
				if err != nil {
					return nil, err
				}
				if err := addFrom(arg, data); err != nil {
					return nil, fmt.Errorf("%s: %w", filepath.Base(m), err)
				}
			}
			if len(ms) == 0 {
				n, err := fromIndex(arg, func(firmware.Board) bool { return true })
				if errors.Is(err, os.ErrNotExist) || (err == nil && n == 0) {
					return nil, fmt.Errorf("%s holds no build: no agents-deck-<board>-<version>.manifest.json and no %s", arg, firmware.ManifestFile)
				}
				if err != nil {
					return nil, err
				}
			}
			continue
		}
		dir, name := filepath.Dir(arg), filepath.Base(arg)
		var base string
		switch {
		case strings.HasSuffix(name, ".manifest.json"):
			base = strings.TrimSuffix(name, ".manifest.json")
		case strings.HasSuffix(name, ".app.bin"):
			base = strings.TrimSuffix(name, ".app.bin")
		case strings.HasSuffix(name, ".bin"):
			base = strings.TrimSuffix(name, ".bin")
		default:
			return nil, fmt.Errorf("%s: give a build's folder, or its .bin, .app.bin or .manifest.json", arg)
		}
		if data, err := readSmall(filepath.Join(dir, base+".manifest.json")); err == nil {
			if err := addFrom(dir, data); err != nil {
				return nil, fmt.Errorf("%s: %w", base+".manifest.json", err)
			}
			continue
		}
		n, err := fromIndex(dir, func(bd firmware.Board) bool {
			return bd.File == name || (bd.OTA != nil && bd.OTA.File == name)
		})
		if err != nil || n == 0 {
			return nil, fmt.Errorf("%s: no %s.manifest.json next to it (the release script writes one with each build)", arg, base)
		}
	}
	return out, nil
}

func readSmall(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 1<<20))
}

// sendBuild posts a build to the service as multipart/form-data.
func sendBuild(b buildFiles) (firmware.LocalBuild, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		err := func() error {
			w, err := mw.CreateFormFile("manifest", "manifest.json")
			if err != nil {
				return err
			}
			if _, err := w.Write(b.manifest); err != nil {
				return err
			}
			for _, p := range [][2]string{{"full", b.full}, {"app", b.app}} {
				if p[1] == "" {
					continue
				}
				f, err := os.Open(p[1])
				if err != nil {
					return err
				}
				w, err := mw.CreateFormFile(p[0], filepath.Base(p[1]))
				if err == nil {
					_, err = io.Copy(w, f)
				}
				f.Close()
				if err != nil {
					return err
				}
			}
			return mw.Close()
		}()
		pw.CloseWithError(err)
	}()
	req, _ := http.NewRequest("POST", "http://agentd/admin/v1/firmware/builds", pr)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	timeout := 2 * time.Minute
	resp, err := unixClient(timeout).Do(req)
	if err != nil {
		return firmware.LocalBuild{}, callErr(err, timeout)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		_ = json.Unmarshal(body, &e)
		if e.Error == "" && resp.StatusCode == http.StatusNotFound {
			return firmware.LocalBuild{}, errOld
		}
		return firmware.LocalBuild{}, errors.New(e.Error)
	}
	var lb firmware.LocalBuild
	return lb, json.Unmarshal(body, &lb)
}

// firmwareAdd: agentctl firmware add <folder or files>.
func firmwareAdd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: agentctl firmware add <folder or files>   (the folder the release script wrote: agents-deck-<board>-<version>.bin, .app.bin, .manifest.json)")
	}
	builds, err := findBuilds(args)
	if err != nil {
		return err
	}
	for _, b := range builds {
		lb, err := sendBuild(b)
		if err != nil {
			return fmt.Errorf("%s: %w", b.name, err)
		}
		how := "USB (full image)"
		if lb.OTA != nil {
			how = "USB (full image) or Wi-Fi (app image)"
		}
		fmt.Printf("added %s %s (%s, %s): installs over %s\n", lb.Board.Board, lb.Version, lb.Name, lb.Chip, how)
	}
	fmt.Println("boards that run an older version show it in Settings, Boards, with an Install button; or: agentctl devices update <name>")
	return nil
}

// firmwareRm: agentctl firmware rm <board> [<version>].
func firmwareRm(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: agentctl firmware rm <board> [<version>]   (every version of the board without one; agentctl firmware ls lists them)")
	}
	version := "all"
	if len(args) == 2 {
		version = args[1]
	}
	var res struct {
		Removed []string `json:"removed"`
	}
	if err := adminCall("DELETE", "/admin/v1/firmware/builds/"+url.PathEscape(args[0])+"/"+url.PathEscape(version), nil, &res); err != nil {
		return err
	}
	fmt.Printf("removed %s %s\n", args[0], strings.Join(res.Removed, ", "))
	return nil
}

// printLocalBuilds lists the builds made on this computer, as the running
// service has them. It says nothing when the service doesn't run.
func printLocalBuilds() {
	var res admin.BuildList
	if err := adminCall("GET", "/admin/v1/firmware/builds", nil, &res); err != nil {
		return
	}
	if len(res.Builds) == 0 && len(res.Skipped) == 0 {
		fmt.Println("\nno firmware built on this computer: agentctl firmware add <folder> puts a build there")
		return
	}
	fmt.Println("\nbuilt on this computer")
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "BOARD\tVERSION\tNAME\tCHIP\tOVER THE AIR\tSIZE\tADDED")
	for _, b := range res.Builds {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d KB\t%s\n", b.Board.Board, b.Version, b.Name, b.Chip, yesNo(b.OTA != nil), (b.Size+1023)/1024, b.Added.Local().Format("2006-01-02 15:04"))
	}
	tw.Flush()
	for _, s := range res.Skipped {
		fmt.Println("skipped " + s)
	}
	fmt.Println("in " + res.Dir)
}

// localBody: the flags that pick a build made on this computer.
func localBody(local bool, version string) map[string]string {
	body := map[string]string{"version": version}
	if local {
		body["source"] = admin.SourceLocal
	}
	return body
}
