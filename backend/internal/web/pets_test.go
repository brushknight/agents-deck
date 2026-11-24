package web_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/web"
)

// The pets' pixel art (pets.js, pet-poses.js), run in node when it is
// installed: every stage, mood and pose draws, at one scale, with few shapes
// and no inline styles.
func TestPetArt(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	dir := t.TempDir()
	for _, name := range []string{"deck-core.js", "pets.js", "pet-poses.js"} {
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
import { POSES, RIG } from './pet-poses.js';
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
  if (shapes > 48) fail(what + ': ' + shapes + ' shapes');
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
// Hats inside auras: with the stroke every hat keeps its own colours.
// cell -> fill for the paths that pass test(fill, the rest of the tag)
const cellsOf = (svg, test) => {
  const out = new Map();
  for (const m of svg.matchAll(/<path d="([^"]+)" fill="(#[0-9A-Fa-f]{6})"([^>]*)\/>/g)) {
    if (!test(m[2].toUpperCase(), m[3])) continue;
    for (const r of m[1].matchAll(/M(-?\d+) (-?\d+)h(\d+)v1h-\d+z/g)) for (let i = 0; i < +r[3]; i++) out.set((+r[1] + i) + ',' + r[2], m[2].toUpperCase());
  }
  return out;
};
for (const [stage, flame] of Object.entries(AURA_COLORS)) for (const shape of Object.keys(HAT_ART)) for (const [color, [main]] of Object.entries(HAT_COLORS)) {
  const svg = petSVG({ stage, mood: 'fed', ate: 0, hat: { shape, color } }, { hat });
  const what = stage + ' with a ' + color + ' ' + shape;
  if (!cellsOf(svg, (f, rest) => f === flame.toUpperCase() && !/prs|ptw/.test(rest)).size) fail(what + ': no flames');
  if (shape !== 'chef' && !svg.includes('fill="' + main + '"')) fail(what + ': the hat lost its colour');
}
if (!petSVG({ stage: 'grown', mood: 'hungry' }, { hat }).includes('#BDB8AD')) fail('hungry: no light grey mouth');
if (!petSVG({ stage: 'legend', mood: 'fed' }, { hat }).includes('class="ptw pt0"')) fail('legend: no stars');
if (petSVG({ stage: 'legend', mood: 'weak' }, { hat }).includes('prs')) fail('weak: no aura');
if (!petSVG({ stage: 'kid', mood: 'asleep' }, { hat }).includes('pfall')) fail('asleep: no ice');
if (!petSVG({ stage: 'kid', mood: 'fed' }, { hat, waking: true }).includes('pws ps4')) fail('waking: no shards');
const sil = petSVG({ stage: 'mighty', mood: 'fed' }, { hat, sil: '#3B3A36' });
if (new Set(sil.match(/fill="#[0-9A-Fa-f]{6}"/g)).size !== 1 || sil.includes('prs')) fail('silhouette: one colour, no aura');

// The critter's poses on every pet: whole pixels, one scale, few shapes; the
// pose's bubble and motion show; the ice and the pet's hunger win where the
// spec (docs/pet-poses.md) says.
const SIGN = { run: 'dots', think: 't3', read: 'pp"', bash: 'cursor', write: 'cursor', web: 'lb slow', plan: 'tick2', delegate: 'pp lb',
  compact: 'press2', celebrate: 'spark s1', hungry: 'bobc', wait: 'blink', err: 'bang', start: 'dots', idle: 'zz' };
const MOTION = { run: 'class="pjog"', bash: 'class="pjog pjf"', delegate: 'class="pjog"', celebrate: 'class="hop"', compact: 'class="psq' };
const pp = (s) => (s.match(/class="pp[ "]/g) || []).length;
for (const stage of STAGES) for (const mood of ['fed', 'hungry', 'weak', 'asleep']) for (const pose of [...Object.keys(POSES)]) for (const bubble of [true, false]) {
  const what = stage + ' ' + mood + ' ' + pose + (bubble ? '' : ' without its bubble');
  const pet = { stage, mood, ate: 40000, hat: { shape: 'propeller', color: 'pink' } };
  const s = petSVG(pet, { hat, pose, bubble });
  if (/style=|NaN|undefined|Infinity/.test(s)) fail(what + ': bad markup');
  if (box(s) !== vb) fail(what + ': drawn at another scale');
  const shapes = (s.match(/<(path|rect)/g) || []).length;
  if (shapes > 96) fail(what + ': ' + shapes + ' shapes');
  if (/[0-9]\.[0-9]/.test(s.replace(/viewBox="[^"]+"/, '').replace(/opacity="[^"]+"/g, ''))) fail(what + ': a shape off the pixel grid');
  if (mood !== 'asleep' && /opacity=/.test(s)) fail(what + ': see-through pixels');
  const sign = SIGN[pose];
  const awake = pose === 'idle' && mood === 'hungry' && stage !== 'egg'; // hungry for food: no sleep
  if (mood === 'asleep') {
    // in the ice only "?" and "!" show
    if ((pose === 'wait' || pose === 'err') && bubble ? !s.includes(sign) : pp(s)) fail(what + ': the ice should win');
    continue;
  }
  if (sign && bubble && !awake && sign !== 'pp lb' && !s.includes(sign)) fail(what + ': no ' + sign);
  if (sign && !bubble && sign !== 'pp lb' && sign !== 'pp"' && s.includes(sign) && !['t3'].includes(sign)) fail(what + ': a bubble was asked away');
  if (stage === 'egg') {
    if (['run', 'think', 'read', 'bash', 'web', 'plan', 'delegate'].includes(pose) && !s.includes('prock')) fail(what + ': the egg should rock');
    if (pose === 'bash' && !s.includes('prock prk2')) fail(what + ': the egg should rock fast');
    continue;
  }
  if (MOTION[pose] && !s.includes(MOTION[pose])) fail(what + ': no ' + MOTION[pose]);
  if (awake && (s.includes('zz') || !s.includes('#BDB8AD'))) fail(what + ': a pet hungry for food stays awake, mouth open');
  if (mood === 'hungry' && pose === 'run' && !s.includes('#BDB8AD')) fail(what + ': its hunger should stay visible');
  if (pose === 'exit' && s.includes('prs')) fail(what + ': no aura on an exited agent\'s pet');
}
// A working hat runs through its frames (not the kid's small cuts); other poses hold it still.
const ph = { stage: 'grown', mood: 'fed', hat: { shape: 'propeller', color: 'teal' } };
if (!petSVG(ph, { hat, pose: 'run' }).includes('hf-4-120-3 hfo')) fail('working hat: no frames');
if (petSVG(ph, { hat, pose: 'wait' }).includes('hf-')) fail('waiting: the hat should hold still');
if (petSVG({ ...ph, stage: 'kid' }, { hat, pose: 'run' }).includes('hf-')) fail('kid: its small hat holds still');
// The mystery "?" keeps the bubble away.
if (petSVG({ stage: 'egg', mood: 'fed', ate: 0 }, { hat, mystery: true, pose: 'wait' }).includes('blink')) fail('egg: the bubble over the mystery hat');
// The stroke: every layer of the pet (body, hat frames, props, bubbles, the
// mini pet) sits on a black outline one pixel wider (up, down, left, right)
// in the same motion class, drawn after the flames and before the pet; props
// are white; sparks and stars come after the pet, the ice last. A silhouette
// has none.
const STROKE = '#000000';
const SKIP = /prs|ptw|ptc|pfc|pfall|pgl|pws|pwc|phw|opacity/;
for (const stage of STAGES) for (const mood of ['fed', 'hungry', 'weak', 'asleep']) for (const pose of Object.keys(POSES)) {
  const pet = { stage, mood, ate: 40000, hat: { shape: 'propeller', color: 'teal' } };
  const what = stage + ' ' + mood + ' ' + pose + ': stroke';
  const svg = petSVG(pet, { hat, pose });
  const byCls = new Map();
  for (const m of svg.matchAll(/<path d="([^"]+)" fill="(#[0-9A-Fa-f]{6})"([^>]*)\/>/g)) {
    if (SKIP.test(m[3]) || Object.values(AURA_COLORS).includes(m[2])) continue; // the flames have no stroke
    const cls = (/class="([^"]*)"/.exec(m[3]) || [])[1] || '';
    const isStroke = m[2] === STROKE;
    if (!byCls.has(cls)) byCls.set(cls, { stroke: new Set(), cells: new Set() });
    const g = byCls.get(cls);
    for (const r of m[1].matchAll(/M(-?\d+) (-?\d+)h(\d+)v1h-\d+z/g)) for (let i = 0; i < +r[3]; i++) (isStroke ? g.stroke : g.cells).add((+r[1] + i) + ',' + r[2]);
  }
  for (const [cls, g] of byCls) for (const k of g.cells) {
    const [x, y] = k.split(',').map(Number);
    for (const [dx, dy] of [[0, 0], [1, 0], [-1, 0], [0, 1], [0, -1]]) if (!g.stroke.has((x + dx) + ',' + (y + dy))) { fail(what + ': no stroke at ' + (x + dx) + ',' + (y + dy) + ' (' + (cls || 'body') + ')'); break; }
  }
  const first = svg.indexOf('fill="' + STROKE + '"'), body = svg.search(/fill="#(FF4D00|F4F0E6|B0673F)"/);
  const lastFlame = Math.max(...Object.values(AURA_COLORS).map((f) => svg.lastIndexOf('fill="' + f + '" class="pfc"')));
  if (first < 0 || first > body || first < lastFlame) fail(what + ': the stroke should sit between the flames and the pet');
  if (svg.includes('prs') && svg.indexOf('prs') < body) fail(what + ': sparks should be in front');
  if (svg.includes('pfall') && svg.indexOf('pfall') < svg.lastIndexOf('fill="' + STROKE + '"')) fail(what + ': the ice should be over everything');
  for (const m of svg.matchAll(/fill="(#[0-9A-F]{6})" class="pp (blink|bang|dots|cursor|zz|bobc|t3|spark s1)/g)) if (m[1] !== STROKE && m[1] !== '#FFFFFF') fail(what + ': a bubble not white');
}
if (petSVG({ stage: 'mighty', mood: 'fed' }, { hat, sil: '#3B3A36' }).includes(STROKE)) fail('silhouette: no stroke');
// A stage not reached yet is a still dark shape: no hat, no aura, nothing
// that moves (as the growth path draws it, glow asked or not).
for (const stage of STAGES) for (const glow of [false, true]) {
  const svg = petSVG({ stage, mood: 'fed', ate: 0, ready: stage === 'egg', hat: { shape: 'crown', color: 'yellow' } }, { hat, still: true, glow, sil: '#3B3A36' });
  const classes = [...svg.matchAll(/class="([^"]*)"/g)].map((m) => m[1]).filter((c) => c !== 'pet' && c !== 'pall');
  if (classes.length) fail(stage + ' silhouette: moves (' + classes.join(', ') + ')');
  if (new Set(svg.match(/fill="#[0-9A-Fa-f]{6}"/g)).size !== 1) fail(stage + ' silhouette: more than one colour');
}
// Hearts on orange: near-black, the empty ones faint.
if (!heartSVG(true, true).includes('#0A0A0A') || !heartSVG(false, true).includes('opacity="0.25"') || heartSVG(true, true).includes('opacity')) fail('hearts on orange');
// Every pose of every hatched pet shows both eyes in every frame: the SVG
// drawn as the browser paints it (document order, each path moved and faded
// by its motion classes and those of its groups, as settings.css times them).
const motionOf = (cls, ms) => {
  const c = new Set(cls.split(' ').filter(Boolean));
  const f = (period, delay = 0) => (((ms - delay) % period) + period) % period / period;
  const pace = c.has('fast') ? 200 : c.has('slow') ? 800 : 500;
  let a = 1, dx = 0, dy = 0;
  for (const k of c) {
    if (k === 'la' && f(pace) >= 0.5) a = 0;
    if (k === 'lb' && f(pace) < 0.5) a = 0;
    if (k === 'hfo') a = 0; // hat frames: only frame 0 is checked
    if (k === 'bob' && f(pace) >= 0.5) dy -= 0.5;
    if (k === 'pjog' && f(c.has('pjf') ? 400 : 500) >= 0.5) dy -= 1;
    if (k === 'pbob' && f(1000) >= 0.5) dy -= 1;
    if (k === 'hop' && f(400) >= 0.5) dy -= 2;
    if (k === 'glance') dx += f(1200) < 0.5 ? -1 : 1;
    if (k === 'scan' || k === 'prock') dx += [0, -1, 0, 1][Math.floor(f(k === 'prock' && c.has('prk2') ? 600 : 1200) * 4)];
    if (k === 'bobc' && f(600) >= 0.5) dy += 1;
    if (k === 'psq') { const t = f(1200); dy += t < 0.33 ? 0 : t < 0.66 || c.has('psq1') ? 1 : 2; }
  }
  return [a, dx, dy];
};
const paint = (svg, ms) => {
  const out = new Map();
  const stack = [[1, 0, 0]];
  for (const m of svg.matchAll(/<g(?: class="([^"]*)")?>|<\/g>|<path d="([^"]+)" fill="(#[0-9A-Fa-f]{6})"( opacity="[^"]*")?(?: class="([^"]*)")?\/>/g)) {
    if (m[0] === '</g>') { stack.pop(); continue; }
    const [pa, px, py] = stack[stack.length - 1];
    if (m[0].startsWith('<g')) { const [a, dx, dy] = motionOf(m[1] || '', ms); stack.push([pa * a, px + dx, py + dy]); continue; }
    if (m[4] || /prs|ptw|ptc/.test(m[5] || '')) continue; // see-through, and the aura's sparks and stars passing in front
    const [a, dx, dy] = motionOf(m[5] || '', ms);
    if (!(pa * a)) continue;
    for (const r of m[2].matchAll(/M(-?\d+) (-?\d+)h(\d+)v1h-\d+z/g)) for (let i = 0; i < +r[3]; i++) out.set((+r[1] + i + px + dx) + ',' + (+r[2] + py + dy), m[3].toUpperCase());
  }
  return out;
};
const EYES = { baby: [[5, 7, 1, 2], [10, 7, 1, 2]], kid: [[5, 7, 1, 2], [10, 7, 1, 2]], grown: [[4, 5, 1, 2], [10, 5, 1, 2]], weak: [[3, 7, 2, 1], [9, 7, 2, 1]] };
for (const stage of ['baby', 'kid', 'grown', 'strong', 'mighty', 'legend']) for (const mood of ['fed', 'hungry', 'weak']) for (const pose of Object.keys(POSES)) {
  if (pose === 'exit') continue; // its eyes are dots in the body colour
  const weak = mood === 'weak' && !['baby', 'kid'].includes(stage);
  const eyes = EYES[weak ? 'weak' : ['baby', 'kid'].includes(stage) ? stage : 'grown'];
  const ink = weak ? '#0A0A0A' : '#141412';
  const svg = petSVG({ stage, mood, ate: 0, hat: { shape: 'bandana', color: 'teal' } }, { hat, pose });
  // the face moves with the body (and drops with the squeeze): follow it
  const grp = (/<g(?: class="([^"]*)")?>/.exec(svg.replace('<g class="pall">', '')) || [])[1] || '';
  const at = (ms) => { const [, gx, gy] = motionOf(grp, ms); const sq = (/<g class="(psq[^"]*)">/.exec(svg) || [])[1]; const [, qx, qy] = sq ? motionOf(sq, ms) : [1, 0, 0]; return [gx + qx, gy + qy]; };
  const count = (cells, [x, y, w, h], [ox, oy]) => [...cells].filter(([k, f]) => {
    const [cx, cy] = k.split(',').map(Number);
    const fx = cx - ox, fy = cy - oy;
    return f === ink && fx >= x - 2 && fx <= x + w + 1 && fy >= y - 2 && fy <= (weak ? 8 : y + h + 1);
  }).length;
  const first = paint(svg, 0);
  const want = eyes.map((e) => count(first, e, at(0)));
  if (want.some((n) => !n)) fail(stage + ' ' + mood + ' ' + pose + ': an eye is missing');
  for (let ms = 50; ms < 2400; ms += 50) {
    const cells = paint(svg, ms);
    if (eyes.some((e, i) => count(cells, e, at(ms)) !== want[i])) { fail(stage + ' ' + mood + ' ' + pose + ' at ' + ms + ' ms: an eye is covered'); break; }
  }
}

// Each z of "z z" has its own class, so its own outline moves with it.
{
  const z = petSVG({ stage: 'grown', mood: 'fed' }, { hat, pose: 'idle' });
  for (const c of ['pp zz z0', 'pp zz z1']) if (!z.includes('fill="#000000" class="' + c + '"') || !z.includes('fill="#FFFFFF" class="' + c + '"')) fail('idle: no outline of its own for ' + c);
}
// Walking, the feet never come apart from the body: in every frame each foot
// cell has the body (or the foot's upper row) right above it.
for (const stage of ['baby', 'kid', 'grown', 'legend']) for (const mood of ['fed', 'weak']) for (const pose of ['run', 'bash', 'write', 'delegate']) {
  const weak = mood === 'weak' && !['baby', 'kid'].includes(stage);
  const rig = RIG[weak ? 'weak' : stage === 'baby' || stage === 'kid' ? stage : 'builder'];
  const body = weak ? '#B0673F' : '#FF4D00';
  const svg = petSVG({ stage, mood, ate: 0 }, { hat, pose });
  const grp = (/<g(?: class="([^"]*)")?>/.exec(svg.replace('<g class="pall">', '')) || [])[1] || '';
  for (let ms = 0; ms < 1200; ms += 50) {
    const cells = paint(svg, ms);
    const [, , gy] = motionOf(grp, ms);
    let bad = '';
    for (const [k, f] of cells) {
      const [x, y] = k.split(',').map(Number);
      if (f !== body || x > 13 || !rig.feet[0].includes(y - gy)) continue;
      if (cells.get(x + ',' + (y - 1)) !== body) bad = k;
    }
    if (bad) { fail(stage + ' ' + mood + ' ' + pose + ' at ' + ms + ' ms: a foot apart from the body at ' + bad); break; }
  }
}
`

// Pet animations move whole pet pixels: no fractional translation, no scale
// or brightness in any pet keyframes; the critter's own bob is as it was.
func TestPetKeyframes(t *testing.T) {
	b, err := fs.ReadFile(web.Static(), "settings.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	kf := regexp.MustCompile(`@keyframes (p[a-z0-9]+) \{(.*)\}`)
	n := 0
	for _, line := range strings.Split(strings.ReplaceAll(css, "\n  ", " "), "\n") {
		m := kf.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n++
		if strings.Contains(m[2], "scale(") || strings.Contains(m[2], "brightness(") {
			t.Errorf("%s scales or flashes: %s", m[1], m[2])
		}
		for _, v := range regexp.MustCompile(`translate[XY]?\(([^)]*)\)`).FindAllStringSubmatch(m[2], -1) {
			for _, num := range regexp.MustCompile(`-?[0-9.]+`).FindAllString(v[1], -1) {
				if strings.Contains(num, ".") {
					t.Errorf("%s moves by part of a pixel: %s", m[1], v[0])
				}
			}
		}
	}
	if n < 10 {
		t.Errorf("found only %d pet keyframes", n)
	}
	if !strings.Contains(css, "@keyframes bob { 0%, 49% { transform: translateY(0) } 50%, 100% { transform: translateY(-0.5px) } }") {
		t.Error("the critter's bob changed")
	}
	for _, line := range strings.Split(css, "\n") {
		if strings.Contains(line, ".pet") && strings.Contains(line, " + .") {
			t.Errorf("a pet rule picks a sibling (its outline would not follow): %s", line)
		}
	}
}

// With the sidebar open the deck narrows instead of scaling: a fractional
// transform would blur the pets' pixels.
func TestDeckNotScaled(t *testing.T) {
	css, _ := fs.ReadFile(web.Static(), "settings.css")
	js, _ := fs.ReadFile(web.Static(), "settings.js")
	if regexp.MustCompile(`\.deckmain \{[^}]*scale\(`).Match(css) || strings.Contains(string(js), "--dz") {
		t.Error("the deck is scaled while the sidebar is open")
	}
}

// Panel view's grid: tiles in whole-pixel tracks set by fitPanel, rows a
// little shorter than wide, so square tiles can't spill into the next row
// and overlap its border (as they did with aspect-ratio tiles in 1fr rows).
// Checked by hand in a headless browser at 1024x768, 1280x800, 1440x900,
// 1728x1117 and 1920x1080, on a page and on Needs you: no tile rects
// intersect, 6 px gaps, every tile and pet at whole pixels.
func TestPanelGridWholePixels(t *testing.T) {
	css, _ := fs.ReadFile(web.Static(), "settings.css")
	js, _ := fs.ReadFile(web.Static(), "settings.js")
	for _, want := range []string{"grid-template-rows: repeat(4, var(--pr", ".pgrid > .tile { aspect-ratio: auto;"} {
		if !strings.Contains(string(css), want) {
			t.Errorf("settings.css lost %q", want)
		}
	}
	if !strings.Contains(string(js), "function fitPanel()") || !strings.Contains(string(js), "& ~1") {
		t.Error("fitPanel no longer snaps the panel's tiles to whole, even pixels")
	}
}
