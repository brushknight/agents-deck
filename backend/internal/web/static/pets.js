// Agent pets: the pixel art. Pure functions, no DOM: petSVG returns the SVG
// markup for an agent's pet (the /v1 Agent "pet"), drawn on a 16-pixel grid
// with its mystery hat once that shows (a kid, level 5). Every colour layer
// is one path of whole pixels, opaque, and every moving part one CSS class
// (settings.css, "agent pets"), so a tile holds a handful of shapes and no
// inline styles (the page's CSP allows none). Draw it at a whole multiple of
// its 20 x 17 viewBox (40, 60, 200 px wide) so every pixel stays crisp.

import { HAT_ART, HAT_COLORS, WORKING_POSES } from './deck-core.js';
import { posePet } from './pet-poses.js';

// . empty · b body · d body shade · e eye · w shell / white · v shell shade
// s spot (hat colour) · k crack / dark line · g ground · p prop (the pencil)
const ART = {
  egg: [
    '......wwww......', '....wwwwwwww....', '...wwwwwswwwv...', '..wwwwwsssswvv..', '..wwwwwwswwwwv..',
    '.wwwwwwwwwwwwvv.', '.wswwwwwwwwwsvv.', '.ssswwwwwwwsssv.', '.wswwwwwwwwwsvv.', '.wwwwwwwwwwwwvv.',
    '..wwwwwwwwwwwv..', '..wwwwwswwwwvv..', '...wwwwwwwwvv...', '.....vvvvvv.....',
  ],
  crack: [
    '......wwww......', '....wwwwwwww....', '...wwwwwswwwv...', '..wwwwwsssswvv..', '..wwwwwwswwwwv..',
    '.wwkwwwkwwwkwvv.', '.wskkwkwkwkkkvv.', '.ssswkwwwkwsssv.', '.wswwwwwwwwwsvv.', '.wwwwwwwwwwwwvv.',
    '..wwwwwwwwwwwv..', '..wwwwwswwwwvv..', '...wwwwwwwwvv...', '.....vvvvvv.....',
  ],
  baby: [
    '................', '................', '................', '................', '.....bbbbbb.....',
    '....bbbbbbbb....', '...bbbbbbbbbb...', '...bbebbbbebb...', '...bbebbbbebb...', '...bbbbddbbbb...',
    '...bbbbbbbbbb...', '....bbbbbbbb....', '....b.b..b.b....', '................',
  ],
  kid: [
    '................', '................', '................', '................', '................',
    '...bbbbbbbbbb...', '...bbbbbbbbbb...', '..bbbebbbbebbb..', '..bbbebbbbebbb..', '...bbbbbbbbbb...',
    '...bbbbbbbbbb...', '....b.b..b.b....', '....b.b..b.b....', '................',
  ],
  builder: [
    '................', '..............p.', '.............pp.', '.bbbbbbbbbbbbpb.', '.bbbbbbbbbbbpbb.',
    '.bbbebbbbbebbbb.', 'bbbbebbbbbebbbbb', 'bbbbbbbbbbbbbbbb', 'bbbbbbbbbbbbbbbb', '.bbbbbbbbbbbbbb.',
    '..b.b......b.b..', '..b.b......b.b..', '................', '................',
  ],
  weak: [
    '................', '................', '................', '................', '..p.............',
    '.ddddddddddddd..', '.ddddddddddddd..', '.ddkkddddkkdddd.', 'dddddddddddddddd', 'ddddddkkkddddddd',
    '.dddddddddddddd.', '..d.d......d.d..', 'gggggggggggggggg', '................',
  ],
};

// Where the hat sits on each art: the body's top row, its centre column, and
// whether it takes the small cut (the kid).
const HAT_AT = { kid: [5, 7.5, true], builder: [3, 7.5, false], weak: [5, 8, false] };
// The mystery hat before it shows: a "?" where the hat will be (x, y of its
// top-left corner); the egg's floats beside it.
const MYSTERY = ['.qq.', 'q..q', '..q.', '....', '..q.'];
const MYSTERY_AT = { egg: [13, -3], crack: [13, -3], baby: [6, -1] };
// Smaller cuts of the deck's hats, for the kid. a = hat colour, b = shade,
// c = highlight, d = near-black.
const SMALL_HATS = {
  propeller: ['cc.aa', '..d..', 'ababa'],
  cap: ['..aaaa.', '.aaaaca', 'bbbbbbbb'],
  wizard: ['...a..', '..aca.', '.aaaa.', 'bbbbbb'],
  'top hat': ['.aaaa.', '.bbbb.', 'aaaaaa'],
  crown: ['a.aa.a', 'aaaaaa', 'acaaca'],
  beret: ['..b...', '.aaaaa.', 'aaaaaaa', '.bbbbb.'],
  beanie: ['..cc..', '.aaaa.', 'bababa'],
  'hard hat': ['.aaaa.', 'aacaaa', 'bbbbbbb'],
  cowboy: ['..aa..', 'b.aa.b', 'bbbbbb'],
  helmet: ['.aaaa.', 'aacaaa', 'bbbbbb'],
  chef: ['.cccc.', 'cccccc', '.cccc.', '.aaaa.'],
  headphones: ['.aaaa.', 'a....a', 'b....b'],
  bandana: ['.aaaaaa.', 'acacacac'],
};

const COL = {
  b: '#FF4D00', d: '#C23A00', e: '#141412', w: '#F4F0E6', v: '#B9B4A8', k: '#0A0A0A', g: '#55524B', p: '#E8E4DC',
  m: '#BDB8AD', // the open mouth of a hungry pet: light grey, so it reads on the dark tile
  c: '#F7F4EC', K: '#0A0A0A', // K: a hat's near-black
  q: '#8A867C', // the mystery hat's "?"
};
const WEAK_BODY = '#B0673F';
// The stroke: a black outline one pixel wide (up, down, left, right) around
// every layer of the pet, so it reads on any background and inside any
// aura. Props and bubbles are white.
const STROKE = '#000000';
const PROP = '#FFFFFF';

// Power-up auras: flame tongues around the body, a pixel clear of it (the
// first frame holds still, a second layer glows in and out), sparks drifting
// up; Legend adds twinkling stars. Opaque pixels only: nothing half see-
// through sits next to the body, so no grey edge.
const AURA = {
  strong: { outer: '#FFE08A', spark: '#FFFFFF', h: [1, 3, 1, 2, 2], n: 5 },
  mighty: { outer: '#3FA9F5', spark: '#FFFFFF', h: [2, 4, 2, 3, 5, 2], n: 7 },
  legend: { outer: '#E8A317', spark: '#FFF6C8', h: [3, 5, 2, 6, 3, 4, 5], n: 10 },
};
export const AURA_COLORS = Object.fromEntries(Object.entries(AURA).map(([k, v]) => [k, v.outer]));

const rgb = (hex) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255);
const darker = (hex, f = 0.72) => `#${rgb(hex).map((v) => Math.round(v * f * 255).toString(16).padStart(2, '0')).join('')}`.toUpperCase();

export const STAGES = ['egg', 'baby', 'kid', 'grown', 'strong', 'mighty', 'legend'];
export const STAGE_NAMES = { egg: 'Egg', baby: 'Baby', kid: 'Kid', grown: 'Grown-up', strong: 'Strong', mighty: 'Mighty', legend: 'Legend' };
export const STAGE_LEVEL = { egg: 0, baby: 1, kid: 5, grown: 10, strong: 20, mighty: 30, legend: 40 };
const CRACK_AT = 30000;
export const MAX_HEARTS = 4;
export const MOOD_NAMES = { fed: 'fed', hungry: 'hungry', weak: 'weak', asleep: 'hibernating' };

// The food that reaches level n, as the service counts it (internal/pets).
// The levels can be changed in config.json, so they are worked out from the
// pet's own numbers: a hatched pet's levelFrom and nextAt give the step, an
// egg's nextAt the first level (its step is the default, 1.25).
export function levelAt(n, pet) {
  if (n < 1) return 0;
  let first = 50000, step = 1.25;
  const lv = pet?.level || 0;
  if (lv >= 1 && pet.levelFrom > 0 && pet.nextAt > pet.levelFrom) {
    step = pet.nextAt / pet.levelFrom;
    first = pet.levelFrom / step ** (lv - 1);
  } else if (pet?.nextAt > 0 && !lv) first = pet.nextAt;
  return Math.round(first * step ** (n - 1));
}
// What it is: "Egg", "Kid", "Builder", "Strong Builder". Builder is the only
// grown-up shape so far.
export function petKind(pet) {
  const s = pet?.stage || 'egg';
  if (s === 'egg' || s === 'baby' || s === 'kid') return STAGE_NAMES[s];
  return s === 'grown' ? 'Builder' : `${STAGE_NAMES[s]} Builder`;
}

// A pixel heart, full or empty.
// onOrange: near-black hearts, the empty ones faint, as red won't read on
// orange (the sidebar).
export function heartSVG(full, onOrange = false) {
  const d = ['.rr.rr.', 'rrrrrrr', 'rrrrrrr', '.rrrrr.', '..rrr..', '...r...']
    .flatMap((row, y) => [...row].map((c, x) => (c === 'r' ? `M${x} ${y}h1v1h-1z` : ''))).join('');
  const fill = onOrange ? `#0A0A0A"${full ? '' : ' opacity="0.25"'}` : `${full ? '#E8452B' : '#3B3A36'}"`;
  return `<svg viewBox="0 0 7 6" shape-rendering="crispEdges" aria-hidden="true" focusable="false"><path d="${d}" fill="${fill}/></svg>`;
}

const key = (x, y) => `${x},${y}`;
const at = (k) => k.split(',').map(Number);
// Python's round (halves to even), as the design was drawn with it.
const rnd = (v) => { const f = Math.floor(v); const d = v - f; return d > 0.5 ? f + 1 : d < 0.5 ? f : (f % 2 ? f + 1 : f); };

function gridOf(name) {
  const pix = new Map();
  ART[name].forEach((row, y) => [...row].forEach((ch, x) => { if (ch !== '.') pix.set(key(x, y), ch); }));
  return pix;
}

// The deck's hat on an art: its still frame, moved from the critter's spot to
// the pet's. a/b/c/d become h (hat colour), H (shade), c, K.
const HAT_KEY = { a: 'h', b: 'H', c: 'c', d: 'K' };
// Devil horns, the hat only pets find: two small horns on the head.
const HORNS = { kid: [[3, 3], [3, 4], [4, 4], [12, 3], [12, 4], [11, 4]], builder: [[2, 1], [2, 2], [3, 2], [13, 1], [13, 2], [12, 2]], weak: [[2, 3], [2, 4], [3, 4], [13, 3], [13, 4], [12, 4]] };
// frame: one of the hat's working frames (critter[i]); its still one if none.
function putHat(pix, name, hat, frame) {
  if (hat?.shape === 'devil horns' && HORNS[name]) {
    HORNS[name].forEach(([x, y], i) => pix.set(key(x, y), i % 3 === 0 ? 'H' : 'h'));
    return;
  }
  const art = HAT_ART[hat?.shape];
  const place = HAT_AT[name];
  if (!art || !place) return;
  const [top, cx, small] = place;
  if (small && SMALL_HATS[hat.shape]) {
    const rows = SMALL_HATS[hat.shape];
    const x0 = rnd(cx - rows[0].length / 2 + 0.5);
    const y0 = top - rows.length;
    rows.forEach((r, i) => [...r].forEach((ch, j) => { if (HAT_KEY[ch]) pix.set(key(x0 + j, y0 + i), HAT_KEY[ch]); }));
    return;
  }
  const f = frame || art.critterStill;
  const ox = 1 + rnd(cx - 7.5);
  f.rows.forEach((r, i) => [...r].forEach((ch, j) => { if (HAT_KEY[ch]) pix.set(key(f.x + ox + j, f.top + top + i), HAT_KEY[ch]); }));
  for (const [x, y, ch] of f.extras) pix.set(key(x + ox, y + top), HAT_KEY[ch]);
  if (hat.shape === 'headphones') { // the ear cups, beside the head
    for (const dy of [0, 1]) { pix.set(key(ox - 1, top + dy), 'H'); pix.set(key(ox + 14, top + dy), 'H'); }
  }
}

// The hat's working frames on an art, as cell maps; null when it has none
// (the kid's small cuts and the devil horns hold still).
function hatFrames(name, hat) {
  const art = HAT_ART[hat?.shape];
  if (!art || !HAT_AT[name] || (HAT_AT[name][2] && SMALL_HATS[hat.shape])) return null;
  return { ms: art.ms, frames: art.critter.map((f) => { const m = new Map(); putHat(m, name, hat, f); return m; }) };
}

// Eyes shut: each eye becomes a short line at its lower row, turned outwards.
function shutEyes(pix) {
  const eyes = [...pix].filter(([, ch]) => ch === 'e').map(([k]) => at(k));
  if (!eyes.length) return;
  const xs = [...new Set(eyes.map(([x]) => x))].sort((p, q) => p - q);
  const low = Math.max(...eyes.map(([, y]) => y));
  for (const [x, y] of eyes) pix.set(key(x, y), 'b');
  const l = xs[0], r = xs[xs.length - 1];
  for (const x of [l - 1, l, r, r + 1]) pix.set(key(x, low), 'e');
}

const MOUTH = { builder: [7, 7], kid: [7, 9], baby: [7, 9] };

// What to draw for a pet: the art, its pixels, and how it looks.
export function petLook(pet, opts = {}) {
  const stage = pet?.stage || 'egg';
  const mood = pet?.mood || 'fed';
  let name = stage === 'egg' ? (pet?.ate >= CRACK_AT || pet?.ready ? 'crack' : 'egg') : stage === 'baby' ? 'baby' : stage === 'kid' ? 'kid' : 'builder';
  const egg = stage === 'egg';
  const weak = mood === 'weak' && !egg;
  const asleep = mood === 'asleep';
  if (weak && name === 'builder') name = 'weak';
  const pix = gridOf(name);
  if (!egg && name !== 'weak') {
    // Hungry shows its open mouth even while its agent is idle.
    if (mood === 'hungry' && !asleep) {
      const [mx, my] = MOUTH[name];
      for (const [dx, dy] of [[0, 0], [1, 0], [0, 1], [1, 1]]) pix.set(key(mx + dx, my + dy), 'm');
    } else if (asleep || (!opts.pose && (weak || opts.idle))) shutEyes(pix); // a pose sets its own eyes
  }
  const base = new Map(pix);
  // Its own mystery hat, once it shows; before that, "?" where asked.
  const hatPix = new Map();
  if (name !== 'baby' && !egg && pet?.hat) putHat(hatPix, name, pet.hat);
  for (const [k, ch] of hatPix) pix.set(k, ch);
  if (!hatPix.size && opts.mystery && MYSTERY_AT[name]) {
    const [x0, y0] = MYSTERY_AT[name];
    MYSTERY.forEach((r, y) => [...r].forEach((ch, x) => { if (ch === 'q') pix.set(key(x0 + x, y0 + y), 'q'); }));
  }
  const aura = !weak && !asleep && AURA[stage] ? stage : '';
  return { name, pix, base, weak, asleep, aura, egg, hungry: mood === 'hungry' && !asleep && !egg, hatKeys: new Set(hatPix.keys()) };
}

// cells → one <path> per (fill, opacity, class), each row's neighbouring
// pixels joined into one rectangle, so no seams show between them.
function paths(cells) {
  const groups = new Map();
  for (const [x, y, fill, op = 1, cls = ''] of cells) {
    const g = `${fill}|${op}|${cls}`;
    if (!groups.has(g)) groups.set(g, new Set());
    groups.get(g).add(key(x, y));
  }
  let out = '';
  for (const [g, set] of groups) {
    const [fill, op, cls] = g.split('|');
    const pts = [...set].map(at).sort((p, q) => p[1] - q[1] || p[0] - q[0]);
    let d = '';
    for (let i = 0; i < pts.length;) {
      const [x, y] = pts[i];
      let n = 1;
      while (i + n < pts.length && pts[i + n][1] === y && pts[i + n][0] === x + n) n++;
      d += `M${x} ${y}h${n}v1h-${n}z`;
      i += n;
    }
    out += `<path d="${d}" fill="${fill}"${op !== '1' ? ` opacity="${op}"` : ''}${cls ? ` class="${cls}"` : ''}/>`;
  }
  return out;
}

function colours(hat, spots, weak, sil) {
  const [ha, hb] = HAT_COLORS[hat?.color] || HAT_COLORS.teal;
  const c = { ...COL, s: (HAT_COLORS[spots?.color] || HAT_COLORS[hat?.color] || HAT_COLORS.teal)[0], h: ha, H: hb };
  if (weak) { c.b = WEAK_BODY; c.d = WEAK_BODY; }
  if (sil) for (const k of Object.keys(c)) c[k] = sil;
  return c;
}

// auraSVG: flame tongues a pixel out from the body (the stroke fills that
// pixel), sparks and, for a legend, stars.
function auraSVG(filled, kind) {
  const a = AURA[kind];
  const pts = [...filled].map(at);
  const xs = [...new Set(pts.map(([x]) => x))].sort((p, q) => p - q);
  const ys = [...new Set(pts.map(([, y]) => y))].sort((p, q) => p - q);
  const top = new Map(), span = new Map();
  for (const [x, y] of pts) {
    top.set(x, Math.min(top.get(x) ?? Infinity, y));
    const s = span.get(y) || [Infinity, -Infinity];
    span.set(y, [Math.min(s[0], x), Math.max(s[1], x)]);
  }
  const mid = (ys[0] + ys[ys.length - 1]) / 2;
  const inner = new Set();
  for (const [y, [l, r]] of span) { inner.add(key(l - 1, y)); inner.add(key(r + 1, y)); }
  for (let x = xs[0]; x <= xs[xs.length - 1]; x++) if (top.has(x)) inner.add(key(x, top.get(x) - 1));
  let out = '';
  for (const f of [0, 1]) {
    const fl = new Set();
    for (const [y, [l, r]] of span) {
      const w = y <= mid ? 2 : 1;
      for (let k = 2; k < 2 + w; k++) if ((y + f) % 4 !== 0 || k === 2) { fl.add(key(l - k, y)); fl.add(key(r + k, y)); }
    }
    let i = 0;
    for (let x = xs[0] - 2; x <= xs[xs.length - 1] + 2; x++, i++) {
      const h = a.h[(i + f * 3) % a.h.length];
      const t = (top.get(x) ?? top.get(x - 1) ?? top.get(x + 1) ?? ys[0]) - 2;
      for (let k = 0; k < h; k++) fl.add(key(x, t - k));
    }
    const cells = [...fl].filter((k) => !filled.has(k) && !inner.has(k)).map((k) => [...at(k), a.outer, 1, f ? 'pfc' : '']);
    out += paths(cells);
  }
  const x0 = xs[0], x1 = xs[xs.length - 1], cxm = Math.floor((x0 + x1) / 2);
  const sx = [x0 - 2, x1 + 2, x0 + 2, x1 - 2, cxm, x0 - 1, x1 + 1, x0 + 4, x1 - 4, cxm + 2];
  let sparks = '';
  for (let i = 0; i < a.n; i++) sparks += paths([[sx[i % 10], ys[ys.length - 1] - (i % 3), a.spark, 1, `prs pd${i}`]]);
  let stars = '';
  if (kind === 'legend') {
    [[-2, ys[0] - 2], [17, ys[0]], [-3, 7], [18, 8], [8, ys[0] - 7]].forEach(([x, y], i) => {
      // two frames: a dot, then a cross
      stars += paths([[x, y, '#FFF6C8', 1, `ptc pt${i}`]]) + paths([[-1, 0], [1, 0], [0, -1], [0, 1]].map(([dx, dy]) => [x + dx, y + dy, '#FFF6C8', 1, `ptw pt${i}`]));
    });
  }
  return { behind: out, front: sparks + stars };
}

// The ice cube of a hibernating pet: still, with a glint now and then and
// snow falling past. waking: it cracks and flies apart once.
function iceSVG(filled, waking) {
  const pts = [...filled].map(at);
  const x0 = Math.min(...pts.map(([x]) => x)) - 1, x1 = Math.max(...pts.map(([x]) => x)) + 1;
  const y0 = Math.min(...pts.map(([, y]) => y)) - 1, y1 = Math.max(...pts.map(([, y]) => y)) + 1;
  const edge = [], box = new Set();
  for (let y = y0; y <= y1; y++) for (let x = x0; x <= x1; x++) {
    box.add(key(x, y));
    if (x === x0 || x === x1 || y === y0 || y === y1) edge.push([x, y, '#DFF4FF', 0.9]);
  }
  const shine = [0, 1, 2].map((i) => [x0 + 1 + i, y0 + 1 + i, '#FFFFFF', 0.8]);
  const glint = [];
  for (let i = 0; i < Math.min(y1 - y0 - 1, x1 - x0 - 4); i++) glint.push([x0 + 3 + i, y0 + 1 + i, '#FFFFFF', 1, 'pgl']);
  const floor = [];
  for (let x = x0 - 1; x <= x1 + 1; x++) floor.push([x, y1 + 1, '#DFF4FF', 0.45]);
  const snow = [[x0 - 2, 0], [x1 + 2, 1], [x0 + 3, 2], [x1 - 3, 3]].map(([x, i]) => [x, y0 - 3, '#E8F6FF', 1, `pfall pf${i}`]);
  const cube = `<rect x="${x0}" y="${y0}" width="${x1 - x0 + 1}" height="${y1 - y0 + 1}" fill="#8FD0F2" fill-opacity="0.4"/>${paths([...edge, ...shine])}`;
  let out = { under: paths(floor), over: '' };
  if (!waking) {
    out.over = `<g>${cube}${paths(glint)}</g>${paths(snow)}`;
    return out;
  }
  const mid = Math.floor((y0 + y1) / 2);
  const zig = [];
  for (let x = x0; x <= x1; x++) zig.push([x, mid + ((x - x0) % 3 === 1 ? 1 : 0) - ((x - x0) % 3 === 2 ? 1 : 0), '#0A0A0A']);
  const shards = [[x0, y0], [x1 - 1, y0], [x0, y1 - 1], [x1 - 1, y1 - 1], [Math.floor((x0 + x1) / 2), y0]]
    .map(([sx, sy], i) => paths([[sx, sy], [sx + 1, sy], [sx, sy + 1], [sx + 1, sy + 1]].map(([x, y]) => [x, y, '#DFF4FF', 0.9, `pws ps${i}`])));
  out.over = `<g class="pwg">${cube}${paths(zig.map((z) => [...z, 1, 'pwc']))}</g>${shards.join('')}`;
  return out;
}

// petSVG: the pet as SVG markup. opts: pose (the critter's pose,
// deck-core.js poseOf: the pet does it, pet-poses.js), bubble (false: the
// pose without its bubble), hat (the agent's hat: only the egg's spots take
// its colour; the pet wears its own pet.hat), idle (eyes shut, without a
// pose), working (it bobs, without a pose), waking (the ice cracks off),
// mystery (a "?" where the hat will be, until it shows), hatching (the
// ready egg shakes and bursts), born (the new pet pops in), sil (one colour,
// no aura or stroke: a stage not reached yet), still (no aura or motion;
// glow keeps the aura, still: the growth path). A
// ready egg wobbles. The viewBox is the same for every pet, so all of them
// are drawn at one scale; auras, strokes, bubbles and props reach outside
// it. Back to front: flames, the stroke, the pet, its bubble and props,
// sparks and stars, and the ice over everything.
export function petSVG(pet, opts = {}) {
  if (!pet) return '';
  const moving = !opts.still && !opts.sil;
  const pose = moving && !opts.hatching && !opts.waking ? opts.pose || '' : '';
  const look = petLook(pet, opts);
  const c = colours(pet.hat, opts.hat, look.weak, opts.sil);
  const glow = look.aura && !opts.sil && (moving || opts.glow) && pose !== 'exit'; // an exited agent's pet is a hollow outline, no aura
  const keepOut = new Set([...look.pix].filter(([, ch]) => ch === 'q').map(([k]) => k));
  const p = pose ? posePet(look, pose, { bubble: opts.bubble, keepOut }) : null;
  const pix = p ? p.pix : look.pix;
  // A working hat cycles through its frames: what they share is drawn with
  // the body, the rest one layer per frame (the critter's frame classes).
  const hatLayers = [];
  const hf = p && WORKING_POSES.has(pose) && !look.asleep && hatFrames(look.name, pet.hat);
  if (hf) {
    for (const k of look.hatKeys) { if (look.base.has(k)) pix.set(k, look.base.get(k)); else pix.delete(k); }
    const { frames, ms } = hf;
    for (const k of new Set(frames.flatMap((m) => [...m.keys()]))) {
      const ch = frames[0].get(k);
      if (ch && frames.every((m) => m.get(k) === ch)) pix.set(k, ch);
    }
    frames.forEach((m, i) => {
      const cells = [...m].filter(([k, ch]) => !frames.every((o) => o.get(k) === ch)).map(([k, ch]) => [...at(k), ch]);
      hatLayers.push([cells, `hf-${frames.length}-${ms}-${i}${i ? ' hfo' : ''}`]);
    });
  }
  // The hat of a pet whose agent has exited dims to its shade.
  if (p?.shade) { c.h = c.H; c.H = darker(c.H); }
  c.P = opts.sil || PROP;
  const cell = (ch) => c[ch] || c.b;
  const lay = (layers) => layers.map(([cells, cls]) => paths(cells.map(([x, y, ch]) => [x, y, cell(ch), 1, cls]))).join('');
  let bodyG;
  if (p?.top) {
    const top = [...pix].filter(([k]) => p.top.has(k)), low = [...pix].filter(([k]) => !p.top.has(k));
    // the rows below first: the top drops over them, face and all
    bodyG = `${paths(low.map(([k, ch]) => [...at(k), cell(ch)]))}<g class="psq${p.squeeze === 1 ? ' psq1' : ''}">${paths(top.map(([k, ch]) => [...at(k), cell(ch)]))}${lay(p.inner)}${lay(hatLayers)}</g>`;
  } else bodyG = paths([...pix].map(([k, ch]) => [...at(k), cell(ch)])) + (p ? lay(p.inner) + lay(hatLayers) : '');
  let behind = '', front = '', under = '', over = '';
  if ((look.asleep || opts.waking) && !opts.sil) {
    const ice = iceSVG(new Set(look.pix.keys()), opts.waking);
    under = ice.under; over = ice.over;
  }
  if (glow) ({ behind, front } = auraSVG(new Set(look.pix.keys()), look.aura));
  const grp = p ? p.grp : opts.working && moving && !look.asleep ? 'pbob' : '';
  const cls = `pet${opts.waking ? ' pwk' : ''}${moving && opts.hatching ? ' phx' : moving && pet.ready ? ' prdy' : ''}${moving && opts.born ? ' pborn' : ''}`;
  const g0 = `<g${grp ? ` class="${grp}"` : ''}>`;
  const outer = p ? lay(p.outer) : '';
  const stroke = opts.sil ? '' : `${g0}${outline(bodyG)}</g>${outline(outer)}`;
  // Hatching: after the shake the egg flashes white, a frame of whole pixels.
  const flash = moving && opts.hatching ? paths([...look.pix.keys()].map((k) => [...at(k), '#FFFFFF', 1, 'phw'])) : '';
  // The aura moves with the pet (its flames, sparks and stars in the same
  // motion group); a wrapper carries the ready egg's wobble, the hatch and the
  // pop-in, in whole cells.
  const inG = (m) => (m && grp ? `${g0}${m}</g>` : m);
  return `<svg class="${cls}" viewBox="-2 -3 20 17" shape-rendering="crispEdges" aria-hidden="true" focusable="false"><g class="pall">${inG(behind)}${under}${stroke}${g0}${bodyG}${flash}</g>${outer}${inG(front)}${over}</g></svg>`;
}

// outline: the stroke under some markup: the cells of its paths grown by one
// (up, down, left, right) in black, one path per motion class, so each
// outline moves with its own layer. <g> wrappers (the squeeze) stay, so the
// stroke drops with its cells.
function outline(markup) {
  let out = '';
  let byCls = new Map();
  const flush = () => {
    for (const [cls, cells] of byCls) out += paths([...cells].map((k) => [...at(k), STROKE, 1, cls]));
    byCls = new Map();
  };
  for (const m of markup.matchAll(/<path d="([^"]*)" fill="[^"]*"( opacity="[^"]*")?(?: class="([^"]*)")?\/>|<g[^>]*>|<\/g>/g)) {
    if (!m[0].startsWith('<path')) { flush(); out += m[0]; continue; }
    if (m[2]) continue; // nothing see-through gets a stroke
    const cls = m[3] || '';
    if (!byCls.has(cls)) byCls.set(cls, new Set());
    const set = byCls.get(cls);
    for (const [, x, y, n] of m[1].matchAll(/M(-?\d+) (-?\d+)h(\d+)v1h-\d+z/g)) {
      for (let i = 0; i < +n; i++) for (const [dx, dy] of [[0, 0], [1, 0], [-1, 0], [0, 1], [0, -1]]) set.add(key(+x + i + dx, +y + dy));
    }
  }
  flush();
  return out;
}
