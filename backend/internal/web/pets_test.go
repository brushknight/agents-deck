package web_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/web"
)

// The pets' pixel art (pets.js), run in node when it is installed: every
// stage and mood draws, at one scale, with few shapes and no inline styles.
func TestPetArt(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	dir := t.TempDir()
	for _, name := range []string{"deck-core.js", "pets.js"} {
		b, err := fs.ReadFile(web.Static(), name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type": "module"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "check.js"), []byte(petCheck), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, filepath.Join(dir, "check.js")).CombinedOutput(); err != nil {
		t.Errorf("%v\n%s", err, out)
	}
}

const petCheck = `
import { petSVG, STAGES, levelAt, petKind, heartSVG, AURA_COLORS } from './pets.js';
import { HAT_ART, HAT_COLORS } from './deck-core.js';
const fail = (m) => { console.error(m); process.exitCode = 1; };
// The same numbers as the service (internal/pets Threshold).
if (levelAt(0) !== 0 || levelAt(1) !== 50000 || levelAt(5) !== 122070 || levelAt(40) !== 300926554) fail('levels differ from the service');
// Other level settings, worked out from the pet's own numbers.
if (levelAt(5, { level: 3, levelFrom: 40000, nextAt: 80000 }) !== 160000 || levelAt(1, { level: 0, nextAt: 20000 }) !== 20000) fail('levels from the pet');
if (petKind({ stage: 'strong' }) !== 'Strong Builder' || petKind({ stage: 'grown' }) !== 'Builder' || petKind({ stage: 'egg' }) !== 'Egg') fail('pet kinds');
if (heartSVG(true) === heartSVG(false) || /style=/.test(heartSVG(true))) fail('hearts');
if (petSVG(null) !== '') fail('no pet: no drawing');
const hat = { shape: 'top hat', color: 'yellow' };
const box = (s) => /viewBox="([^"]+)"/.exec(s)[1];
let vb = '';
for (const stage of STAGES) for (const mood of ['fed', 'hungry', 'weak', 'asleep']) for (const idle of [false, true]) {
  const what = stage + ' ' + mood + (idle ? ' idle' : '');
  const pet = { stage, mood, ate: 40000, hat: { shape: 'devil horns', color: 'pink' } };
  const s = petSVG(pet, { hat, idle, working: true });
  if (/style=/.test(s)) fail(what + ': inline style');
  vb = vb || box(s);
  if (box(s) !== vb) fail(what + ': drawn at another scale');
  const shapes = (s.match(/<(path|rect)/g) || []).length;
  if (shapes > 40) fail(what + ': ' + shapes + ' shapes');
  // Clean pixels: whole-pixel shapes, nothing see-through around the body
  // (only the ice is).
  if (/[0-9]\.[0-9]/.test(s.replace(/viewBox="[^"]+"/, '').replace(/opacity="[^"]+"/g, ''))) fail(what + ': a shape off the pixel grid');
  if (mood !== 'asleep' && /opacity=/.test(s)) fail(what + ': see-through pixels');
}
// Rows of one colour are one rectangle each, so no seams show between pixels.
if (!/h(1[0-6]|[2-9])v1h-/.test(petSVG({ stage: 'grown', mood: 'fed' }, { hat }))) fail('rows not joined');
// The mystery hat: none before it shows, "?" where asked, the hat from a kid on.
const q = '#8A867C';
if (petSVG({ stage: 'baby', mood: 'fed' }, { hat }).includes(q) || !petSVG({ stage: 'baby', mood: 'fed' }, { hat, mystery: true }).includes(q)) fail('mystery hat on a baby');
if (!petSVG({ stage: 'egg', mood: 'fed', ate: 0 }, { hat, mystery: true }).includes(q)) fail('mystery hat by an egg');
const horns = petSVG({ stage: 'kid', mood: 'fed', hat: { shape: 'devil horns', color: 'teal' } }, { hat });
if (horns === petSVG({ stage: 'kid', mood: 'fed' }, { hat })) fail('devil horns: nothing drawn');
if (petSVG({ stage: 'kid', mood: 'fed' }, { hat }).includes('#E8B94A')) fail('a pet wears the folder hat');
// A ready egg wobbles; hatching shakes; a new pet pops in.
if (!petSVG({ stage: 'egg', mood: 'fed', ate: 60000, ready: true }, { hat }).includes('prdy')) fail('ready egg: no wobble');
if (!petSVG({ stage: 'egg', mood: 'fed', ate: 60000, ready: true }, { hat, hatching: true }).includes('phx')) fail('hatching: no burst');
if (!petSVG({ stage: 'baby', mood: 'fed' }, { hat, born: true }).includes('pborn')) fail('born: no pop');
// Hats inside auras: no flame pixel touches a hat pixel (8 neighbours), and a
// hat in the aura's colour family (contrast under 1.6, hue within 40 degrees,
// worked out here on its own) is drawn in its shade.
const rgb = (x) => [1, 3, 5].map((i) => parseInt(x.slice(i, i + 2), 16) / 255);
const lum = (x) => { const [r, g, b] = rgb(x).map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4)); return 0.2126 * r + 0.7152 * g + 0.0722 * b; };
const ratio = (p, q) => { const [x, y] = [lum(p), lum(q)].sort((m, n) => n - m); return (x + 0.05) / (y + 0.05); };
const hueOf = (x) => { const [r, g, b] = rgb(x); const mx = Math.max(r, g, b), d = mx - Math.min(r, g, b); if (!d) return 0;
  return ((mx === r ? ((g - b) / d) % 6 : mx === g ? (b - r) / d + 2 : (r - g) / d + 4) * 60 + 360) % 360; };
const family = (p, q) => { const dh = Math.abs(hueOf(p) - hueOf(q)); return ratio(p, q) < 1.6 && Math.min(dh, 360 - dh) <= 40; };
// cell -> fill for the paths that pass test(fill, the rest of the tag)
const cellsOf = (svg, test) => {
  const out = new Map();
  for (const m of svg.matchAll(/<path d="([^"]+)" fill="(#[0-9A-Fa-f]{6})"([^>]*)\/>/g)) {
    if (!test(m[2].toUpperCase(), m[3])) continue;
    for (const r of m[1].matchAll(/M(-?\d+) (-?\d+)h(\d+)v1h-\d+z/g)) for (let i = 0; i < +r[3]; i++) out.set((+r[1] + i) + ',' + r[2], m[2].toUpperCase());
  }
  return out;
};
const touches = (a, b) => [...a].some((k) => { const [x, y] = k.split(',').map(Number);
  for (let dx = -1; dx <= 1; dx++) for (let dy = -1; dy <= 1; dy++) if (b.has((x + dx) + ',' + (y + dy))) return true; return false; });
const clashSeen = [];
for (const [stage, flame] of Object.entries(AURA_COLORS)) for (const shape of [...Object.keys(HAT_ART), 'devil horns']) for (const [color, [main, shade]] of Object.entries(HAT_COLORS)) {
  const svg = petSVG({ stage, mood: 'fed', ate: 0, hat: { shape, color } }, { hat });
  const what = stage + ' with a ' + color + ' ' + shape;
  const flames = cellsOf(svg, (f, rest) => f === flame.toUpperCase() && !/prs|ptw/.test(rest));
  // The hat: what differs from the same pet without one (the pencil may share a hat's colour).
  const bare = cellsOf(petSVG({ stage, mood: 'fed', ate: 0 }, { hat }), (f, rest) => !rest);
  const hatCells = new Map([...cellsOf(svg, (f, rest) => !rest)].filter(([k, f]) => bare.get(k) !== f && !flames.has(k)));
  if (!flames.size) fail(what + ': no flames');
  if (!hatCells.size) fail(what + ': no hat');
  if (touches(new Set(flames.keys()), new Set(hatCells.keys()))) fail(what + ': a flame touches the hat');
  const clash = family(main, flame);
  if (clash && !svg.includes('fill="' + shade + '"')) fail(what + ': clashes, but the hat is not in its shade');
  if (clash && [...hatCells.values()].includes(main.toUpperCase())) fail(what + ': clashes, but its main colour is still drawn');
  if (clash && shape === 'cap') clashSeen.push(stage + ' ' + color);
}
// The pairs measured by eye stay in the list.
for (const pair of ['strong yellow', 'strong ink', 'legend yellow', 'mighty teal', 'mighty blue', 'mighty sky']) if (!clashSeen.includes(pair)) fail('expected a clash: ' + pair + ' (found ' + clashSeen.join(', ') + ')');
if (!petSVG({ stage: 'grown', mood: 'hungry' }, { hat }).includes('#BDB8AD')) fail('hungry: no light grey mouth');
if (!petSVG({ stage: 'legend', mood: 'fed' }, { hat }).includes('class="ptw pt0"')) fail('legend: no stars');
if (petSVG({ stage: 'legend', mood: 'weak' }, { hat }).includes('prs')) fail('weak: no aura');
if (!petSVG({ stage: 'kid', mood: 'asleep' }, { hat }).includes('pfall')) fail('asleep: no ice');
if (!petSVG({ stage: 'kid', mood: 'fed' }, { hat, waking: true }).includes('pws ps4')) fail('waking: no shards');
const sil = petSVG({ stage: 'mighty', mood: 'fed' }, { hat, sil: '#3B3A36' });
if (new Set(sil.match(/fill="#[0-9A-Fa-f]{6}"/g)).size !== 1 || sil.includes('prs')) fail('silhouette: one colour, no aura');
`
