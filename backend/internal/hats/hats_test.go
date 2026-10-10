package hats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAutoIsStableAndVaried(t *testing.T) {
	a, b := Auto("/Users/sam/dev/checkout-api"), Auto("/Users/sam/dev/checkout-api/")
	if a != b || !a.Auto {
		t.Fatalf("same folder, same hat: %+v %+v", a, b)
	}
	shapes, colors := map[string]bool{}, map[string]bool{}
	for _, f := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p"} {
		h := Auto("/dev/" + f)
		shapes[h.Shape], colors[h.Color] = true, true
	}
	if len(shapes) < 6 || len(colors) < 4 {
		t.Fatalf("hash should vary both: %d shapes, %d colours", len(shapes), len(colors))
	}
	for _, c := range Colors {
		if c == "orange" {
			t.Fatal("orange is the status colour")
		}
	}
}

func TestOverridesInConfig(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfg, []byte(`{"term":"tmux"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	b := &Book{Path: cfg}
	dir := "/Users/sam/dev/app"
	if h := b.For(dir); h == nil || !h.Auto {
		t.Fatalf("automatic first: %+v", h)
	}
	if err := b.Set(dir, "cowboy", "teal", false); err != nil {
		t.Fatal(err)
	}
	if h := b.For(dir); h.Shape != "cowboy" || h.Color != "teal" || h.Auto {
		t.Fatalf("override: %+v", h)
	}
	if err := b.Set(dir, "beret", "", false); err != nil { // keep the automatic colour
		t.Fatal(err)
	}
	if h := b.For(dir); h.Shape != "beret" || h.Color != Auto(dir).Color {
		t.Fatalf("shape only: %+v", h)
	}
	if err := b.Set(dir, None, "", false); err != nil {
		t.Fatal(err)
	}
	if h := b.For(dir); h != nil {
		t.Fatalf("none: %+v", h)
	}
	var c map[string]any
	raw, _ := os.ReadFile(cfg)
	_ = json.Unmarshal(raw, &c)
	if c["term"] != "tmux" || c["hats"] == nil {
		t.Fatalf("other keys kept: %s", raw)
	}
	if err := b.Set(dir, "", "", true); err != nil {
		t.Fatal(err)
	}
	if h := b.For(dir); !h.Auto {
		t.Fatalf("back to auto: %+v", h)
	}
	raw, _ = os.ReadFile(cfg)
	c = nil
	if json.Unmarshal(raw, &c); c["hats"] != nil {
		t.Fatalf("no empty hats key: %s", raw)
	}
	if b.Set(dir, "sombrero", "", false) == nil || b.Set(dir, "cap", "orange", false) == nil {
		t.Fatal("unknown shapes and colours are refused")
	}
}
