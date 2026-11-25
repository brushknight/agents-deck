// Prints the expected looks for test/host/test_pet.c from the web deck's
// pets.js and pet-poses.js, so the kit's pets (src/deck_pet.c) stay the
// same pixels:
//
//   node tools/pet_golden.mjs [folder with pets.js] > test/host/pet_golden.h
//
// Each line is one pet in one pose: its inputs and an FNV-1a hash of its
// cells ("x,y,key;" in rows, top to bottom, left to right) at each of the
// times in MS, one after another. The motion classes become whole-cell moves
// as docs/pet-poses.md times them.
import { pathToFileURL } from 'node:url';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const dir = resolve(process.argv[2] || resolve(here, '../../../backend/internal/web/static'));
const { petLook } = await import(pathToFileURL(`${dir}/pets.js`).href);
const { posePet } = await import(pathToFileURL(`${dir}/pet-poses.js`).href);
const { HAT_ART, WORKING_POSES } = await import(pathToFileURL(`${dir}/deck-core.js`).href);

const STAGES = ['egg', 'baby', 'kid', 'grown', 'strong', 'mighty', 'legend'];
const MOODS = ['fed', 'hungry', 'weak', 'asleep'];
const SHAPES = ['hard hat', 'cap', 'beanie', 'helmet', 'wizard', 'beret', 'chef', 'crown', 'headphones',
  'propeller', 'top hat', 'cowboy', 'bandana', 'devil horns'];
// In the order of deck_pet_pose_t (after NONE).
const POSES = ['think', 'read', 'write', 'bash', 'web', 'plan', 'delegate', 'compact', 'run', 'wait', 'err',
  'idle', 'start', 'exit', 'celebrate', 'hungry'];
const MS = [0, 130, 260, 410, 610, 890, 1210, 1730];

const key = (x, y) => `${x},${y}`;
const at = (k) => k.split(',').map(Number);
const rnd = (v) => { const f = Math.floor(v); const d = v - f; return d > 0.5 ? f + 1 : d < 0.5 ? f : (f % 2 ? f + 1 : f); };

// One motion class string at ms: [visible, dx, dy]. zz2: the second "z".
function move(cls, ms, zz2) {
  const c = new Set(cls.split(' ').filter(Boolean));
  const per = c.has('fast') ? 200 : c.has('slow') ? 800 : 500;
  let dx = 0, dy = 0, vis = true;
  const q = (p) => [0, -1, 0, 1][Math.floor((ms % p) * 4 / p)];
  if (c.has('la')) vis = ms % per < per / 2;
  if (c.has('lb')) vis = ms % per >= per / 2;
  if (c.has('bob')) dy = ms % per >= per / 2 ? -1 : 0;
  if (c.has('pjog')) dy = ms % (c.has('pjf') ? 400 : 500) >= (c.has('pjf') ? 200 : 250) ? -1 : 0;
  if (c.has('hop')) dy = ms % 400 >= 200 ? -2 : 0;
  if (c.has('prock')) dx = q(c.has('prk2') ? 600 : 1200);
  if (c.has('scan')) dx = q(1200);
  if (c.has('glance')) dx = ms % 1200 < 600 ? -1 : 1;
  if (c.has('t2')) vis = ms % 1200 >= 300;
  if (c.has('t3')) vis = ms % 1200 >= 600;
  if (c.has('cursor')) vis = ms % 1000 < 500;
  for (const i of [0, 1, 2]) if (c.has(`tick${i}`)) vis = ms % 2000 < (i + 1) * 500;
  if (c.has('dots')) vis = ms % 1200 >= 400 && ms % 1200 < 800;
  if (c.has('press2')) vis = ms % 1200 < 400;
  if (c.has('press1')) vis = ms % 1200 < 800;
  if (c.has('bobc')) dy = ms % 600 >= 300 ? 1 : 0;
  if (c.has('blink')) vis = ms % 1000 < 600;
  if (c.has('zz')) dy = (ms + (zz2 ? 1200 : 0)) % 2400 >= 1200 ? -1 : 0;
  return [vis, dx, dy];
}

// The hat's working frames on an art (pets.js hatFrames), as cell maps.
const HAT_AT = { kid: [5, 7.5, true], builder: [3, 7.5, false], weak: [5, 8, false] };
const HAT_KEY = { a: 'h', b: 'H', c: 'c', d: 'K' };
function hatFrames(name, hat) {
  const art = HAT_ART[hat?.shape];
  if (!art || !HAT_AT[name] || HAT_AT[name][2]) return null;
  const [top, cx] = HAT_AT[name];
  const ox = 1 + rnd(cx - 7.5);
  return { ms: art.ms, frames: art.critter.map((f) => {
    const m = new Map();
    f.rows.forEach((r, i) => [...r].forEach((ch, j) => { if (HAT_KEY[ch]) m.set(key(f.x + ox + j, f.top + top + i), HAT_KEY[ch]); }));
    for (const [x, y, ch] of f.extras) m.set(key(x + ox, y + top), HAT_KEY[ch]);
    if (hat.shape === 'headphones') for (const dy of [0, 1]) { m.set(key(ox - 1, top + dy), 'H'); m.set(key(ox + 14, top + dy), 'H'); }
    return m;
  }) };
}

// The pet's cells at ms, drawn in the order petSVG draws them.
function frame(pet, pose, ms) {
  const look = petLook(pet, { pose });
  const p = posePet(look, pose, { bubble: true });
  const pix = new Map(p.pix);
  let hatLayers = [];
  const hf = WORKING_POSES.has(pose) && !look.asleep && hatFrames(look.name, pet.hat);
  if (hf) {
    for (const k of look.hatKeys) { if (look.base.has(k)) pix.set(k, look.base.get(k)); else pix.delete(k); }
    const keys = new Set(hf.frames.flatMap((m) => [...m.keys()]));
    for (const k of keys) { const ch = hf.frames[0].get(k); if (ch && hf.frames.every((m) => m.get(k) === ch)) pix.set(k, ch); }
    const i = Math.floor(ms / hf.ms) % hf.frames.length;
    hatLayers = [[[...hf.frames[i]].filter(([k, ch]) => !hf.frames.every((o) => o.get(k) === ch)).map(([k, ch]) => [...at(k), ch]), '']];
  }
  const out = new Map();
  const [, gx, gy] = move(p.grp, ms);
  const [, sx, sy] = p.top ? [true, 0, p.squeeze === 1 ? (ms % 1200 >= 400 ? 1 : 0) : Math.floor((ms % 1200) / 400)] : [true, 0, 0];
  const put = (x, y, ch, cls, inTop, inGroup, zz2) => {
    const [vis, dx, dy] = move(cls, ms, zz2);
    if (!vis) return;
    out.set(key(x + dx + (inGroup ? gx : 0) + (inTop ? sx : 0), y + dy + (inGroup ? gy : 0) + (inTop ? sy : 0)), ch);
  };
  const top = (k) => p.top && p.top.has(k);
  // squeezed: the rows it drops over first, then the part that drops, face and hat on top
  if (p.top) for (const [k, ch] of pix) if (!top(k)) put(...at(k), ch, '', false, true);
  for (const [k, ch] of pix) if (!p.top || top(k)) put(...at(k), ch, '', !!p.top, true);
  for (const [cells, cls] of [...p.inner, ...hatLayers]) for (const [x, y, ch] of cells) put(x, y, ch, cls, !!p.top, true);
  let z = 0;
  for (const [cells, cls] of p.outer) {
    const zz2 = cls.split(' ').includes('zz') && z++ > 0;
    for (const [x, y, ch] of cells) put(x, y, ch, cls, false, false, zz2);
  }
  return out;
}

function fnv(h, str) {
  for (let i = 0; i < str.length; i++) {
    h ^= str.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h >>> 0;
}

function hashOf(cells) {
  const list = [...cells].map(([k, ch]) => [...at(k), ch]).sort((a, b) => a[1] - b[1] || a[0] - b[0]);
  return fnv(0x811c9dc5, list.map(([x, y, ch]) => `${x},${y},${ch};`).join(''));
}

// The pets: stage, mood, cracked, ready, the pet's own hat (1 + shape, 0 none).
const PETS = [
  [0, 0, 0, 0, 0], [0, 0, 1, 1, 0], [1, 0, 0, 0, 0], [1, 1, 0, 0, 0], [2, 0, 0, 0, 3], [2, 2, 0, 0, 14],
  [3, 0, 0, 0, 11], [3, 1, 0, 0, 9], [4, 0, 0, 0, 2], [5, 0, 0, 0, 14], [6, 2, 0, 0, 10], [3, 3, 0, 0, 5],
  [0, 3, 0, 0, 0], [6, 0, 0, 0, 13],
];
const lines = [];
for (const [stage, mood, cracked, ready, hat] of PETS) {
  const pet = { stage: STAGES[stage], mood: MOODS[mood], ate: stage === 0 ? (cracked ? 40000 : 100) : 1000000, ready: !!ready,
    hat: hat ? { shape: SHAPES[hat - 1], color: 'blue' } : undefined };
  POSES.forEach((pose, i) => {
    const h = MS.map((ms) => hashOf(frame(pet, pose, ms)));
    lines.push(`\t{${stage}, ${mood}, ${cracked}, ${ready}, ${hat}, ${i + 1}, {${h.map((v) => `0x${v.toString(16).padStart(8, '0')}u`).join(', ')}}},`);
  });
}

console.log(`/* Generated by tools/pet_golden.mjs from pets.js and pet-poses.js: do not edit. */
/* stage, mood, cracked, ready, hat (1 + shape), pose, hash of the cells at each of pet_golden_ms */
static const uint32_t pet_golden_ms[] = {${MS.join(', ')}};
static const struct { int stage, mood, cracked, ready, hat, pose; uint32_t hash[${MS.length}]; } pet_golden[] = {
${lines.join('\n')}
};`);
