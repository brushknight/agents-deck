package header

import (
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

func TestRender(t *testing.T) {
	now := time.Now()
	a := model.Agent{Title: "issue #42", Folder: "deck", Status: model.Waiting, StatusSince: now.Add(-5 * time.Minute),
		Waiting: &model.Prompt{Title: "Bash", Detail: "rm -rf build"}, Context: model.Context{Used: 50, Window: 100},
		Hat: &model.Hat{Shape: "propeller", Color: "teal"}}
	lines := Render(a, now)
	if !strings.Contains(lines[0], "ISSUE ##42") || !strings.Contains(lines[0], "needs you 5m") {
		t.Errorf("title line: %q", lines[0])
	}
	if !strings.Contains(lines[2], "50% context") || !strings.Contains(lines[3], "rm -rf build") {
		t.Errorf("stats/prompt: %q / %q", lines[2], lines[3])
	}
	if strings.Contains(lines[0], "■") {
		t.Errorf("one account: no account on the title line: %q", lines[0])
	}
	a.Account = &model.Account{Name: "work", Color: "yellow"}
	if l := Render(a, now)[0]; !strings.Contains(l, "#[fg="+hatColors["yellow"][0]+"]■") || !strings.Contains(l, "work") {
		t.Errorf("account on the title line: %q", l)
	}
	a.Account = nil
	// The propeller's colour shows up in the sprite; the body is orange while it needs you.
	g := Sprite(a)
	var hat, body bool
	for _, row := range g {
		for _, c := range row {
			hat = hat || c == hatColors["teal"][0]
			body = body || c == accent
		}
	}
	if !hat || !body {
		t.Errorf("sprite: hat %v, body %v", hat, body)
	}
	// Seconds don't change the header (it would redraw every second).
	a.StatusSince = now.Add(-10 * time.Second)
	if Render(a, now) != Render(a, now.Add(20*time.Second)) {
		t.Error("header changes within the first minute")
	}
}

func TestEveryHatFits(t *testing.T) {
	for shape := range hats {
		for _, tool := range []string{"claude", "codex"} {
			a := model.Agent{Tool: tool, Status: model.Running, Hat: &model.Hat{Shape: shape, Color: "pink"}}
			g := Sprite(a)
			n := 0
			for _, row := range g {
				for _, c := range row {
					if c == hatColors["pink"][0] || c == hatColors["pink"][1] {
						n++
					}
				}
			}
			if n == 0 {
				t.Errorf("%s on %s: no hat pixels in the header", shape, tool)
			}
			// A character cell shows two colours at most (fore- and background).
			for y := 0; y < gridH; y += 2 {
				for x := 0; x < gridW; x += 2 {
					cols := map[string]bool{}
					for _, c := range []string{g[y][x], g[y][x+1], g[y+1][x], g[y+1][x+1]} {
						if c == "" {
							c = Bg
						}
						cols[c] = true
					}
					if len(cols) > 2 {
						t.Errorf("%s on %s: cell (%d,%d) needs %d colours", shape, tool, x, y+gridY0, len(cols))
					}
				}
			}
		}
	}
}
