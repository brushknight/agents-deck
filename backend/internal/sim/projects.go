// Package sim imitates Claude Code agents for demos and recordings. A simulated
// agent runs in tmux like a real one, sends the same hook events and writes a
// Claude-style transcript, so the daemon and every client treat it as real.
//
// It is strictly read-only towards the projects it "works" on: it lists file
// names to make its activity plausible, and never opens, edits or executes
// anything in them. Edits and commands are only displayed.
package sim

import (
	"io/fs"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var projectMarkers = []string{".git", "go.mod", "package.json", "Cargo.toml", "pyproject.toml", "requirements.txt", "Makefile", "CMakeLists.txt", "platformio.ini", "pubspec.yaml"}

// Projects returns the immediate subfolders of root that look like projects.
func Projects(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, e.Name())
		for _, m := range projectMarkers {
			if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
				out = append(out, dir)
				break
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// Project is what the fake agent knows about its folder.
type Project struct {
	Dir    string
	Kind   string   // go, node, rust, python, flutter, firmware, make, generic
	Files  []string // relative paths of source-ish files (names only)
	Dirs   []string
	Branch string
}

var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, "build": true, "dist": true,
	"target": true, ".dart_tool": true, ".venv": true, "venv": true, "__pycache__": true, ".pio": true, ".next": true, "Pods": true}

var sourceExt = map[string]bool{".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".py": true, ".rs": true,
	".dart": true, ".c": true, ".h": true, ".cpp": true, ".hpp": true, ".swift": true, ".kt": true, ".java": true, ".rb": true,
	".md": true, ".yaml": true, ".yml": true, ".toml": true, ".json": true, ".sql": true, ".sh": true, ".css": true, ".tf": true}

// Scan lists file names (never contents) and detects the project kind.
func Scan(dir string) Project {
	p := Project{Dir: dir, Kind: kind(dir), Branch: branch(dir)}
	n := 0
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			if rel, err := filepath.Rel(dir, path); err == nil && rel != "." && strings.Count(rel, "/") < 3 {
				p.Dirs = append(p.Dirs, rel)
			}
			return nil
		}
		if n >= 4000 {
			return filepath.SkipAll
		}
		if sourceExt[filepath.Ext(d.Name())] {
			if rel, err := filepath.Rel(dir, path); err == nil {
				p.Files = append(p.Files, rel)
				n++
			}
		}
		return nil
	})
	if len(p.Files) == 0 {
		p.Files = []string{"README.md"}
	}
	if len(p.Dirs) == 0 {
		p.Dirs = []string{"."}
	}
	return p
}

func kind(dir string) string {
	has := func(f string) bool { _, err := os.Stat(filepath.Join(dir, f)); return err == nil }
	switch {
	case has("go.mod"):
		return "go"
	case has("Cargo.toml"):
		return "rust"
	case has("pubspec.yaml"):
		return "flutter"
	case has("package.json"):
		return "node"
	case has("pyproject.toml") || has("requirements.txt"):
		return "python"
	case has("platformio.ini"):
		return "firmware"
	case has("Makefile") || has("CMakeLists.txt"):
		return "make"
	}
	return "generic"
}

func branch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Commands are what the agent pretends to run, per project kind.
func (p Project) Commands() []string {
	switch p.Kind {
	case "go":
		return []string{"go test ./...", "go vet ./...", "go build ./...", "go test -race ./internal/...", "golangci-lint run"}
	case "node":
		return []string{"npm test", "npm run lint", "npm run build", "npx tsc --noEmit", "npm run test -- --watch=false"}
	case "rust":
		return []string{"cargo test", "cargo clippy", "cargo build --release", "cargo fmt --check"}
	case "python":
		return []string{"pytest -q", "ruff check .", "mypy .", "pytest -q -k smoke"}
	case "flutter":
		return []string{"flutter test", "flutter analyze", "dart format --set-exit-if-changed ."}
	case "firmware":
		return []string{"pio run", "pio test -e native", "pio check"}
	case "make":
		return []string{"make", "make test", "make lint"}
	}
	return []string{"git status", "git diff --stat", "ls -la"}
}

func (p Project) file(r *rand.Rand) string { return p.Files[r.Intn(len(p.Files))] }
func (p Project) dir(r *rand.Rand) string  { return p.Dirs[r.Intn(len(p.Dirs))] }

func base(f string) string { return strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)) }

// Task is one simulated assignment.
type Task struct {
	Prompt string
	Title  string
}

func (p Project) Task(r *rand.Rand) Task {
	f, d := p.file(r), p.dir(r)
	templates := []Task{
		{"fix the flaky test around " + base(f) + " and make it deterministic", "Fix flaky " + base(f) + " test"},
		{"refactor " + f + " — split the big function and add tests", "Refactor " + base(f)},
		{"add error handling to " + f + " for the timeout case", "Timeout handling in " + base(f)},
		{"write docs for everything under " + d, "Document " + d},
		{"find why the build is slow and fix the worst offender", "Speed up the build"},
		{"review the last commit and tighten anything sloppy", "Review last commit"},
		{"add a regression test for the bug in " + base(f), "Regression test for " + base(f)},
		{"rename the config fields in " + f + " to match the new schema", "Rename config fields"},
		{"bump dependencies and fix whatever breaks", "Bump dependencies"},
		{"add structured logging to " + d, "Structured logging in " + d},
	}
	return templates[r.Intn(len(templates))]
}

// Grep patterns that read like real searches.
var patterns = []string{"TODO", "func New", "timeout", "retry", "context.Context", "export default", "deprecated", "panic(", "Config", "error"}
