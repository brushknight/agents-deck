package claude

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// "Report usage from every Claude session" (off by default): the deck's
// status line goes into a Claude account's own settings.json, so every
// session in that account hands its usage limits to the deck, not only the
// agents the deck starts. The status line that was there is kept in the
// deck's data folder; `agentctl statusline` still runs it, and turning the
// setting off puts it back.

// IsDeckStatusLine: a status line command that runs agentctl statusline.
func IsDeckStatusLine(c string) bool {
	return strings.Contains(c, "agentctl") && strings.Contains(c, " statusline")
}

// savedLine is what an account's settings.json had before the deck's.
type savedLine struct {
	Dir        string          `json:"dir"`
	Had        bool            `json:"had"`
	StatusLine json.RawMessage `json:"statusLine,omitempty"`
}

func savedPath(saveDir, configDir string) string {
	h := sha256.Sum256([]byte(filepath.Clean(configDir)))
	return filepath.Join(saveDir, hex.EncodeToString(h[:6])+".json")
}

// SavedStatusLine is the command of the status line the deck's replaced in
// that account ("" for none).
func SavedStatusLine(saveDir, configDir string) string {
	b, err := os.ReadFile(savedPath(saveDir, configDir))
	if err != nil {
		return ""
	}
	var s savedLine
	if json.Unmarshal(b, &s) != nil || !s.Had {
		return ""
	}
	return commandOf(s.StatusLine)
}

func commandOf(raw json.RawMessage) string {
	var sl struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	if json.Unmarshal(raw, &sl) != nil || sl.Type != "command" {
		return ""
	}
	return strings.TrimSpace(sl.Command)
}

// AddDeckStatusLine puts the deck's status line (running agentctl) into the
// account's settings.json, keeping everything else in the file as it is. A
// missing config folder is skipped; a missing settings.json is created.
func AddDeckStatusLine(configDir, agentctl, saveDir string) error {
	if fi, err := os.Stat(configDir); err != nil || !fi.IsDir() {
		return nil
	}
	file := filepath.Join(configDir, "settings.json")
	doc, err := readSettings(file)
	if err != nil {
		return err
	}
	mine, _ := json.Marshal(map[string]string{"type": "command", "command": fmt.Sprintf("%q statusline", agentctl)})
	cur, had := doc.get("statusLine")
	if had && bytes.Equal(compact(cur), compact(mine)) {
		return nil
	}
	if !had || !IsDeckStatusLine(commandOf(cur)) {
		// Theirs (or none): keep it to chain to, and to put back.
		s, _ := json.Marshal(savedLine{Dir: filepath.Clean(configDir), Had: had, StatusLine: cur})
		if err := writeAtomic(savedPath(saveDir, configDir), s, 0o600); err != nil {
			return err
		}
	}
	doc.set("statusLine", mine)
	return writeSettings(file, doc)
}

// RemoveDeckStatusLine puts back the status line the account had before.
// One the user changed since is left alone.
func RemoveDeckStatusLine(configDir, saveDir string) error {
	sp := savedPath(saveDir, configDir)
	var s savedLine
	b, err := os.ReadFile(sp)
	if err == nil && json.Unmarshal(b, &s) != nil {
		return fmt.Errorf("%s: unreadable", sp)
	}
	file := filepath.Join(configDir, "settings.json")
	if _, err := os.Stat(file); err != nil {
		_ = os.Remove(sp)
		return nil
	}
	doc, err := readSettings(file)
	if err != nil {
		return err
	}
	if cur, ok := doc.get("statusLine"); ok && IsDeckStatusLine(commandOf(cur)) {
		if s.Had {
			doc.set("statusLine", s.StatusLine)
		} else {
			doc.del("statusLine")
		}
		if err := writeSettings(file, doc); err != nil {
			return err
		}
	}
	if err := os.Remove(sp); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// settingsDoc is a JSON object with its keys in the file's order.
type settingsDoc struct {
	keys []string
	vals map[string]json.RawMessage
}

func (d *settingsDoc) get(k string) (json.RawMessage, bool) { v, ok := d.vals[k]; return v, ok }

func (d *settingsDoc) set(k string, v json.RawMessage) {
	if _, ok := d.vals[k]; !ok {
		d.keys = append(d.keys, k)
	}
	d.vals[k] = v
}

func (d *settingsDoc) del(k string) {
	delete(d.vals, k)
	for i, x := range d.keys {
		if x == k {
			d.keys = append(d.keys[:i], d.keys[i+1:]...)
			break
		}
	}
}

func readSettings(file string) (*settingsDoc, error) {
	d := &settingsDoc{vals: map[string]json.RawMessage{}}
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return d, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return d, nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, fmt.Errorf("%s: not a JSON object; left alone", file)
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%s: %v; left alone", file, err)
		}
		k, _ := t.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("%s: %v; left alone", file, err)
		}
		d.set(k, v)
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("%s: %v; left alone", file, err)
	}
	return d, nil
}

// writeSettings backs the file up (settings.json.agents-deck.bak), then
// writes the new one in its place in one step, with the old file's mode.
func writeSettings(file string, d *settingsDoc) error {
	var buf bytes.Buffer
	buf.WriteString("{")
	for i, k := range d.keys {
		if i > 0 {
			buf.WriteString(",")
		}
		kb, _ := json.Marshal(k)
		buf.WriteString("\n  ")
		buf.Write(kb)
		buf.WriteString(": ")
		var v bytes.Buffer
		if err := json.Indent(&v, d.vals[k], "  ", "  "); err != nil {
			return err
		}
		buf.Write(v.Bytes())
	}
	if len(d.keys) > 0 {
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	mode := os.FileMode(0o600)
	if old, err := os.ReadFile(file); err == nil {
		if fi, err := os.Stat(file); err == nil {
			mode = fi.Mode().Perm()
		}
		if err := writeAtomic(file+".agents-deck.bak", old, mode); err != nil {
			return err
		}
	}
	return writeAtomic(file, buf.Bytes(), mode)
}

func compact(b []byte) []byte {
	var buf bytes.Buffer
	if json.Compact(&buf, b) != nil {
		return b
	}
	return buf.Bytes()
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(name, mode)); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
