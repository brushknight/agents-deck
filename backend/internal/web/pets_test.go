package web_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
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
import { POSES } from './pet-poses.js';
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
const SIGN = { run: 'dots', think: 't3', read: 'pp"', bash: 'cursor', write: 'cursor', web: 'lb slow', plan: 'tick2', delegate: 'pp bob',
  compact: 'press2', celebrate: 'spark s1', hungry: 'bobc', wait: 'blink', err: 'bang', start: 'dots', idle: 'zz' };
const MOTION = { run: 'class="bob"', bash: 'class="bob fast"', delegate: 'class="bob"', celebrate: 'class="hop"', compact: 'class="psq"' };
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
  if (sign && bubble && !awake && sign !== 'pp bob' && !s.includes(sign)) fail(what + ': no ' + sign);
  if (sign && !bubble && sign !== 'pp bob' && sign !== 'pp"' && s.includes(sign) && !['t3'].includes(sign)) fail(what + ': a bubble was asked away');
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
const SKIP = /prs|ptw|pfc|pfall|pgl|pws|pwc|opacity/;
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
`
