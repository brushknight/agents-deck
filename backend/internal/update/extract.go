package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// entryPath is where an archive entry goes under dir, or an error when its
// name could leave dir (absolute, "..").
func entryPath(dir, name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := path.Clean("/" + name)[1:]
	if clean == "" || clean == "." {
		return "", nil
	}
	if path.IsAbs(name) || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") || name == ".." {
		return "", fmt.Errorf("archive entry %q leaves its folder", name)
	}
	return filepath.Join(dir, filepath.FromSlash(clean)), nil
}

// linkOK: a symlink at p pointing to target stays inside dir.
func linkOK(dir, p, target string) bool {
	if filepath.IsAbs(target) || path.IsAbs(target) {
		return false
	}
	resolved := filepath.Join(filepath.Dir(p), filepath.FromSlash(target))
	rel, err := filepath.Rel(dir, resolved)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

type budget struct{ left int64 }

func (b *budget) copy(dst io.Writer, src io.Reader) error {
	n, err := io.Copy(dst, io.LimitReader(src, b.left+1))
	b.left -= n
	if err != nil {
		return err
	}
	if b.left < 0 {
		return errors.New("archive unpacks to more than allowed")
	}
	return nil
}

func writeEntry(p string, mode fs.FileMode, r io.Reader, b *budget) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm()|0o600)
	if err != nil {
		return err
	}
	err = b.copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// untarGz unpacks a .tar.gz into dir: files, folders and symlinks that stay
// inside dir; at most max bytes.
func untarGz(file, dir string, max int64) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(file), err)
	}
	tr := tar.NewReader(gz)
	b := &budget{left: max}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(file), err)
		}
		p, err := entryPath(dir, h.Name)
		if err != nil {
			return err
		}
		if p == "" {
			continue
		}
		switch h.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(p, 0o755)
		case tar.TypeReg:
			err = writeEntry(p, fs.FileMode(h.Mode), tr, b)
		case tar.TypeSymlink:
			if !linkOK(dir, p, h.Linkname) {
				return fmt.Errorf("archive link %q points outside", h.Name)
			}
			if err = os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
				err = os.Symlink(h.Linkname, p)
			}
		default:
			continue // devices, hard links, …: nothing a release has
		}
		if err != nil {
			return err
		}
	}
}

// unzipGo unpacks a zip into dir like untarGz (ditto does this on macOS).
func unzipGo(file, dir string, max int64) error {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(file), err)
	}
	defer zr.Close()
	b := &budget{left: max}
	for _, zf := range zr.File {
		p, err := entryPath(dir, zf.Name)
		if err != nil {
			return err
		}
		if p == "" {
			continue
		}
		mode := zf.Mode()
		switch {
		case mode.IsDir():
			err = os.MkdirAll(p, 0o755)
		case mode&fs.ModeSymlink != 0:
			var target []byte
			if target, err = readZip(zf, 4096); err == nil {
				if !linkOK(dir, p, string(target)) {
					return fmt.Errorf("archive link %q points outside", zf.Name)
				}
				if err = os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
					err = os.Symlink(string(target), p)
				}
			}
		case mode.IsRegular():
			var rc io.ReadCloser
			if rc, err = zf.Open(); err == nil {
				err = writeEntry(p, mode, rc, b)
				rc.Close()
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func readZip(zf *zip.File, max int64) ([]byte, error) {
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, max))
}

// only is the one entry in dir (ignoring dot files and macOS's __MACOSX),
// which must satisfy want.
func only(dir string, want func(fs.DirEntry) bool) (string, error) {
	es, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var found []string
	for _, e := range es {
		if strings.HasPrefix(e.Name(), ".") || e.Name() == "__MACOSX" {
			continue
		}
		if want(e) {
			found = append(found, filepath.Join(dir, e.Name()))
		}
	}
	if len(found) != 1 {
		return "", fmt.Errorf("expected one entry in the package, found %d", len(found))
	}
	return found[0], nil
}
