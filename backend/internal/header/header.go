// Package header draws the band at the top of every agent terminal: the
// agent's pixel critter in its folder's hat (half-block characters, two
// pixels per cell), its name, status and the numbers the deck shows. It
// renders tmux status-format lines; the daemon keeps them up to date.
//go:generate node gen_hats.js

package header

import (
	"fmt"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// Lines is how many status lines the header takes (tmux allows at most 5).
const Lines = 5

// Colours of the deck (app.css).
const (
	Bg     = "#1C1B18" // the band: a step lighter than the terminal, so it stands apart
	ink    = "#E8E4DC"
	dim    = "#8A867D"
	accent = "#FF4D00"
	black  = "#0A0A0A"
	white  = "#F7F4EC"
)

// The mascot is Claude Code's own logo (▐▛███▜▌ …) in quarter blocks: each
// character cell is 2×2 pixels, so it is 9 cells wide and 3 lines tall, plus
// a one-line hat. Pixel x runs 0…17, y -2…5 (the hat on rows -2 and -1).
// A cell shows at most two colours (fore- and background), and the hats are
// drawn so that every 2×2 cell keeps to that.
const (
	gridW  = 18
	gridY0 = -2
	gridH  = 8 // 4 lines
	// MascotLines is how many header lines the mascot takes; the line below
	// it stays empty, a gap between the header and the agent.
	MascotLines = gridH / 2
)

// The logo: # body, e eyes (background), n the onigiri's nori.
var claudeLogo = []string{
	"...############...",
	"...##e######e##...",
	".################.",
	"...############...",
	"....#.#....#.#....",
	"..................",
}

var onigiriLogo = []string{
	".......####.......",
	".....########.....",
	"...###e####e###...",
	"..##############..",
	"..##nnnnnnnnnn##..",
	"..................",
}

// Hats, rows -2 and -1: a = the hat's colour, b = its shade, c = white,
// d = near-black; cups are extra pixels beside the head (headphones).
var hats = map[string][2]string{
	"hard hat":   {"....aaaaaaaaaa....", "..bbbbbbbbbbbbbb.."},
	"cap":        {"......aaaaaaaaa...", "bbbbbbbbaaaaaaa..."},
	"beanie":     {"......aaccaa......", "..aabbaabbaabbaa.."},
	"helmet":     {".....aaacaaaa.....", "...aaaaaaaaaaaa..."},
	"wizard":     {".......aaaa.......", "..bbbbaaaaaabbbb.."},
	"beret":      {"........bb........", "...aaaaaaaaaaaaa.."},
	"chef":       {"....cc..cc..cc....", "....aaaaaaaaaa...."},
	"crown":      {"....a...aa...a....", "....aaaacaaaaa...."},
	"headphones": {"...bbbbbbbbbbbb...", "..b............b.."},
	"propeller":  {"..cccc....bbbb....", "....aaaaaaaaaa...."},
	"top hat":    {"......bbbbbb......", "....bbaaaaaabb...."},
	"cowboy":     {"......aa..aa......", "bbbbbbbbbbbbbbbbbb"},
	"bandana":    {"b.................", ".b.aaaccaaccaaa..."},
}

// Sprite returns the mascot as rows of colours ("" = background).
func Sprite(a model.Agent) [gridH][gridW]string {
	var g [gridH][gridW]string
	body := bodyColour(a)
	logo := claudeLogo
	if a.Tool == "codex" {
		logo = onigiriLogo
	}
	for y, row := range logo {
		for x, ch := range row {
			switch ch {
			case '#':
				g[y-gridY0][x] = body
			case 'e', 'n':
				g[y-gridY0][x] = Bg
			}
		}
	}
	if a.Hat == nil {
		return g
	}
	art, ok := hats[a.Hat.Shape]
	if !ok {
		return g
	}
	cs, ok := hatColors[a.Hat.Color]
	if !ok {
		cs = hatColors["ink"]
	}
	fill := map[byte]string{'a': cs[0], 'b': cs[1], 'c': white, 'd': black}
	for r, row := range art {
		for x := 0; x < len(row) && x < gridW; x++ {
			if col, ok := fill[row[x]]; ok {
				g[r][x] = col
			}
		}
	}
	if a.Hat.Shape == "headphones" { // the cups, beside the head
		for _, x := range []int{2, 15} {
			g[0-gridY0][x], g[1-gridY0][x] = cs[0], cs[0]
		}
	}
	return g
}

func asleep(a model.Agent) bool {
	return a.Status == model.Idle && !a.Unseen || a.Status == model.Exited
}

func bodyColour(a model.Agent) string {
	switch {
	case a.Status == model.Running, a.Status == model.Waiting:
		return accent
	case a.Status == model.Error, a.Unseen:
		return ink
	}
	return dim
}

// quadrants: the block character for a 2×2 cell, by which pixels show the
// foreground (1 upper left, 2 upper right, 4 lower left, 8 lower right).
var quadrants = []rune(" ▘▝▀▖▌▞▛▗▚▐▜▄▙▟█")

// cellChar picks a cell's character and colours: the most common colour is
// the foreground, the next (or the band's background) the background.
func cellChar(px [4]string) (ch rune, fg, bg string) {
	count := map[string]int{}
	for _, c := range px {
		if c == "" {
			c = Bg
		}
		count[c]++
	}
	fg, bg = "", Bg
	for _, c := range px { // in pixel order, so ties are stable
		if c == "" || c == Bg {
			continue
		}
		if fg == "" || count[c] > count[fg] {
			fg = c
		}
	}
	if fg == "" {
		return ' ', Bg, Bg
	}
	for _, c := range px {
		if c != "" && c != Bg && c != fg && count[Bg] == 0 {
			bg = c // two colours and no background in this cell
			break
		}
	}
	mask := 0
	for i, c := range px {
		if c == fg {
			mask |= 1 << i
		}
	}
	return quadrants[mask], fg, bg
}

// Render returns the header's tmux status-format lines for an agent.
func Render(a model.Agent, now time.Time) [Lines]string {
	g := Sprite(a)
	text := textLines(a, now)
	var out [Lines]string
	for i := range Lines {
		var b strings.Builder
		b.WriteString("#[bg=" + Bg + "]  ")
		cur := ""
		for k := 0; k < gridW; k += 2 {
			ch, fg, bg := ' ', Bg, Bg
			if i < MascotLines {
				ch, fg, bg = cellChar([4]string{g[2*i][k], g[2*i][k+1], g[2*i+1][k], g[2*i+1][k+1]})
			}
			if st := "#[fg=" + fg + ",bg=" + bg + "]"; st != cur {
				b.WriteString(st)
				cur = st
			}
			b.WriteRune(ch)
		}
		b.WriteString("#[fg=" + ink + ",bg=" + Bg + "]   ")
		b.WriteString(text[i])
		out[i] = b.String()
	}
	return out
}

// esc makes text safe in a tmux format ("#" starts a format).
func esc(s string) string {
	s = strings.ReplaceAll(s, "#", "##")
	return strings.Map(func(r rune) rune {
		if r < ' ' {
			return ' '
		}
		return r
	}, s)
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func textLines(a model.Agent, now time.Time) [Lines]string {
	var t [Lines]string
	name := a.Title
	if name == "" {
		name = a.Folder
	}
	pillBg, pillFg, pill := dim, black, "idle"
	switch {
	case a.Status == model.Waiting:
		pillBg, pillFg, pill = accent, black, "needs you"
	case a.Status == model.Running:
		pillBg, pillFg, pill = accent, black, "working"
	case a.Status == model.Error:
		pillBg, pillFg, pill = ink, black, "error"
	case a.Unseen:
		pillBg, pillFg, pill = ink, black, "done · not reviewed"
	case a.Lost:
		pill = "lost with tmux"
	case a.Status == model.Exited:
		pill = "exited"
	case a.Status == model.Starting:
		pill = "starting"
	}
	if !a.StatusSince.IsZero() && a.Status != model.Exited {
		if d := ago(now.Sub(a.StatusSince)); d != "" {
			pill += " " + d
		}
	}
	t[0] = "#[bold,fg=" + white + "]" + esc(strings.ToUpper(clip(name, 48))) + "#[nobold]  " +
		"#[fg=" + pillFg + ",bg=" + pillBg + ",bold] " + esc(pill) + " #[nobold,fg=" + ink + ",bg=" + Bg + "]"

	where := []string{a.Folder}
	if a.Branch != "" {
		where = append(where, "⎇ "+a.Branch)
	}
	m := a.ModelLabel
	if m == "" {
		m = a.Model
	}
	if m != "" && a.Effort != "" {
		m += " · " + a.Effort + " effort"
	}
	if m != "" {
		where = append(where, m)
	}
	t[1] = "#[fg=" + dim + "]" + esc(clip(strings.Join(where, "  ·  "), 100))
	var stats []string
	if w := a.Context.Window; w > 0 {
		pct := int(100 * a.Context.Used / w)
		const n = 16
		fill := min(n, int((a.Context.Used*n+w/2)/w))
		col := ink
		if pct >= 80 {
			col = accent
		}
		stats = append(stats, "#[fg="+col+"]"+strings.Repeat("█", fill)+"#[fg=#3A3833]"+strings.Repeat("█", n-fill)+
			"#[fg="+ink+"] "+fmt.Sprintf("%d%% context", pct))
	}
	tok := a.Tokens.Input + a.Tokens.Output + a.Tokens.CacheRead + a.Tokens.CacheWrite
	if tok > 0 {
		stats = append(stats, "#[fg="+dim+"]"+count(tok)+" tokens")
	}
	if a.CostUSD > 0 {
		stats = append(stats, "#[fg="+dim+"]"+fmt.Sprintf("$%.2f", a.CostUSD))
	}
	if a.Turns > 0 {
		stats = append(stats, "#[fg="+dim+"]"+fmt.Sprintf("%d turns", a.Turns))
	}
	t[2] = strings.Join(stats, "#[fg="+dim+"]  ·  ")

	switch {
	case a.Status == model.Waiting && a.Waiting != nil:
		t[3] = "#[fg=" + accent + ",bold]▶ " + esc(clip(a.Waiting.Title+" "+a.Waiting.Detail, 100)) + "#[nobold]"
	case a.Status == model.Error && a.Error != nil:
		t[3] = "#[fg=" + accent + "]✕ " + esc(clip(a.Error.Message, 100))
	case a.Status == model.Running && a.Activity != nil:
		t[3] = "#[fg=" + ink + "]▸ " + esc(clip(strings.TrimSpace(a.Activity.Tool+"  "+a.Activity.Detail), 100))
	case a.LastPrompt != "":
		t[3] = "#[fg=" + dim + "]› " + esc(clip(a.LastPrompt, 100))
	}
	if a.Account != nil { // which Claude account: a square in its colour, like the deck's
		col := dim
		if cs, ok := hatColors[a.Account.Color]; ok {
			col = cs[0]
		}
		t[0] += "  #[fg=" + col + "]■ #[fg=" + dim + "]" + esc(clip(a.Account.Name, 16))
	}
	if n := len(a.Subagents); n > 0 { // next to the status: line 4 is the gap
		label := "subagent"
		if n > 1 {
			label += "s"
		}
		t[0] += "  #[fg=" + dim + "]" + fmt.Sprintf("+ %d %s", n, label)
	}
	return t
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "" // seconds would redraw every header every second
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func count(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprint(n)
}
