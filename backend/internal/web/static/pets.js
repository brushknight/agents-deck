// Agent pets: the pixel art. Pure functions, no DOM: petSVG returns the SVG
// markup for an agent's pet (the /v1 Agent "pet"), drawn on a 16-pixel grid
// with its working folder's hat. Every colour layer is one path and every
// moving part one CSS class (settings.css, "agent pets"), so a tile holds a
// handful of shapes and no inline styles (the page's CSP allows none).

import { HAT_ART, HAT_COLORS } from './deck-core.js';

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
  c: '#F7F4EC',
};
const WEAK_BODY = '#B0673F';
const OUTLINE = '#0A0A0A';

// Power-up auras: flame tongues around the body (the first frame holds
// still, a second layer glows in and out), a light edge hugging the
// outline, sparks drifting up; Legend adds twinkling stars.
const AURA = {
  strong: { outer: '#FFE08A', inner: '#FFF8E6', spark: '#FFFFFF', h: [1, 3, 1, 2, 2], n: 5, op: 0.75 },
  mighty: { outer: '#3FA9F5', inner: '#BDE7FF', spark: '#FFFFFF', h: [2, 4, 2, 3, 5, 2], n: 7, op: 0.7 },
  legend: { outer: '#E8A317', inner: '#FFE58A', spark: '#FFF6C8', h: [3, 5, 2, 6, 3, 4, 5], n: 10, op: 0.85 },
};

export const STAGES = ['egg', 'baby', 'kid', 'grown', 'strong', 'mighty', 'legend'];
export const STAGE_NAMES = { egg: 'Egg', baby: 'Baby', kid: 'Kid', grown: 'Grown-up', strong: 'Strong', mighty: 'Mighty', legend: 'Legend' };
export const STAGE_LEVEL = { egg: 0, baby: 1, kid: 5, grown: 10, strong: 20, mighty: 30, legend: 40 };
const CRACK_AT = 30000;

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
// the pet's. a/b/c/d become h (hat colour), H (shade), c, k.
const HAT_KEY = { a: 'h', b: 'H', c: 'c', d: 'k' };
function putHat(pix, name, hat) {
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
  const f = art.critterStill;
  const ox = 1 + rnd(cx - 7.5);
  f.rows.forEach((r, i) => [...r].forEach((ch, j) => { if (HAT_KEY[ch]) pix.set(key(f.x + ox + j, f.top + top + i), HAT_KEY[ch]); }));
  for (const [x, y, ch] of f.extras) pix.set(key(x + ox, y + top), HAT_KEY[ch]);
  if (hat.shape === 'headphones') { // the ear cups, beside the head
    for (const dy of [0, 1]) { pix.set(key(ox - 1, top + dy), 'H'); pix.set(key(ox + 14, top + dy), 'H'); }
  }
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
  let name = stage === 'egg' ? (pet?.ate >= CRACK_AT ? 'crack' : 'egg') : stage === 'baby' ? 'baby' : stage === 'kid' ? 'kid' : 'builder';
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
    } else if (asleep || weak || opts.idle) shutEyes(pix);
  }
  if (name !== 'baby' && !egg) putHat(pix, name, opts.hat);
  const aura = !weak && !asleep && AURA[stage] ? stage : '';
  return { name, pix, weak, asleep, aura, egg };
}

// cells → one <path> per (fill, opacity, class).
function paths(cells) {
  const groups = new Map();
  for (const [x, y, fill, op = 1, cls = ''] of cells) {
    const g = `${fill}|${op}|${cls}`;
    if (!groups.has(g)) groups.set(g, []);
    groups.get(g).push(`M${x} ${y}h1v1h-1z`);
  }
  let out = '';
  for (const [g, d] of groups) {
    const [fill, op, cls] = g.split('|');
    out += `<path d="${d.join('')}" fill="${fill}"${op !== '1' ? ` opacity="${op}"` : ''}${cls ? ` class="${cls}"` : ''}/>`;
  }
  return out;
}

function colours(hat, weak, sil) {
  const [ha, hb] = HAT_COLORS[hat?.color] || HAT_COLORS.teal;
  const c = { ...COL, s: ha, h: ha, H: hb };
  if (weak) { c.b = WEAK_BODY; c.d = WEAK_BODY; }
  if (sil) for (const k of Object.keys(c)) c[k] = sil;
  return c;
}

function outlineOf(filled) {
  const out = [];
  const seen = new Set();
  for (const k of filled) {
    const [x, y] = at(k);
    for (let dx = -1; dx <= 1; dx++) for (let dy = -1; dy <= 1; dy++) {
      const n = key(x + dx, y + dy);
      if (!filled.has(n) && !seen.has(n)) { seen.add(n); out.push([x + dx, y + dy, OUTLINE]); }
    }
  }
  return out;
}

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
    const cells = [...fl].filter((k) => !filled.has(k) && !inner.has(k)).map((k) => [...at(k), a.outer, a.op, f ? 'pfc' : '']);
    out += paths(cells);
  }
  const glow = paths([...inner].map((k) => [...at(k), a.inner, 0.9]));
  const x0 = xs[0], x1 = xs[xs.length - 1], cxm = Math.floor((x0 + x1) / 2);
  const sx = [x0 - 2, x1 + 2, x0 + 2, x1 - 2, cxm, x0 - 1, x1 + 1, x0 + 4, x1 - 4, cxm + 2];
  let sparks = '';
  for (let i = 0; i < a.n; i++) sparks += paths([[sx[i % 10], ys[ys.length - 1] - (i % 3), a.spark, 1, `prs pd${i}`]]);
  let stars = '';
  if (kind === 'legend') {
    [[-2, ys[0] - 2], [17, ys[0]], [-3, 7], [18, 8], [8, ys[0] - 7]].forEach(([x, y], i) => {
      stars += paths([[0, 0], [-1, 0], [1, 0], [0, -1], [0, 1]].map(([dx, dy]) => [x + dx, y + dy, '#FFF6C8', 1, `ptw pt${i}`]));
    });
  }
  return { behind: out + glow, front: sparks + stars };
}

// The ice cube of a hibernating pet: still, with a glint now and then and
// snow falling past. waking: it cracks and flies apart once.
function iceSVG(filled, waking, outline) {
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
  let out = { under: paths(floor) + (outline ? paths(outlineOf(box)) : ''), over: '' };
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

// petSVG: the pet as SVG markup. opts: hat (the agent's hat), idle (eyes
// shut), working (it bobs), outline (a dark edge, for orange tiles), waking
// (the ice cracks off), sil (one colour, no aura: a stage not reached yet),
// still (no aura or motion). The viewBox is the same for every pet, so all
// of them are drawn at one scale; auras glow outside it.
export function petSVG(pet, opts = {}) {
  if (!pet) return '';
  const look = petLook(pet, opts);
  const c = colours(opts.hat, look.weak, opts.sil);
  const filled = new Set(look.pix.keys());
  const body = paths([...look.pix].map(([k, ch]) => [...at(k), c[ch] || c.b]));
  let behind = '', front = '', under = '', over = '';
  if ((look.asleep || opts.waking) && !opts.sil) {
    const ice = iceSVG(filled, opts.waking, opts.outline);
    under = ice.under; over = ice.over;
  } else if (opts.outline) under = paths(outlineOf(filled));
  if (look.aura && !opts.sil && !opts.still) ({ behind, front } = auraSVG(filled, look.aura));
  const bob = opts.working && !opts.still && !look.asleep ? ' class="pbob"' : '';
  const cls = `pet${opts.waking ? ' pwk' : ''}`;
  return `<svg class="${cls}" viewBox="-2 -3 20 17" shape-rendering="crispEdges" aria-hidden="true" focusable="false">${behind}${under}<g${bob}>${body}</g>${over}${front}</svg>`;
}
