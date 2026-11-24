// Agent pets do the critter's poses (deck-core.js spriteSVG): the same
// bubbles, props and motion classes (settings.css, the deck's critters), on
// whole pixels of the pet's 16-pixel grid. RIG says where each pose anchors
// on each art; docs/pet-poses.md is the spec the panel and board ports
// follow. Pure functions, no DOM.

import { GLYPH } from './deck-core.js';

// Per art, in grid cells (x to the right, y down; the art's rows are 0-13):
// eyes [x, y, w, h] (left, right); mouth: the 2 x 2 hungry mouth's top-left;
// arms: the top row of a 1 x 2 arm hanging, half up and up (its column is
// one left of the body's left edge on those two rows, one right of the
// right edge); feet: their rows, columns, and columns mid-step; bubble: the top-left of the
// 5 x 5 bubble box; tow: the top-left of the 5 x 3 mini pet in tow;
// squeeze: how far it drops while compacting (2 unless its face sits too
// low to drop 2 without covering the eyes: the kid's).
export const RIG = {
  egg: { bubble: [14, -4], tow: [16, 11] },
  crack: { bubble: [14, -4], tow: [16, 11] },
  baby: { eyes: [[5, 7, 1, 2], [10, 7, 1, 2]], mouth: [7, 9], arms: [9, 7, 3], feet: [[12], [4, 6, 9, 11], [5, 7, 9, 11]], bubble: [13, -1], tow: [14, 10] },
  kid: { eyes: [[5, 7, 1, 2], [10, 7, 1, 2]], mouth: [7, 9], arms: [9, 7, 4], feet: [[11, 12], [4, 6, 9, 11], [3, 5, 10, 12]], bubble: [14, 0], tow: [14, 10], squeeze: 1 },
  builder: { eyes: [[4, 5, 1, 2], [10, 5, 1, 2]], mouth: [7, 7], arms: [7, 5, 2], feet: [[10, 11], [2, 4, 11, 13], [3, 5, 10, 12]], bubble: [16, -2], tow: [16, 9] },
  weak: { eyes: [[3, 7, 2, 1], [9, 7, 2, 1]], mouth: [6, 10], arms: [9, 7, 4], feet: [[11], [2, 4, 11, 13], [3, 5, 10, 12]], bubble: [15, 0], tow: [17, 9] },
};

// Each pose: eyes (how they change), arms (left, right: down, mid, up;
// "swing" alternates them), walk (legs step; "fast" for bash), grp (the
// motion of the whole pet: pjog lifts it a whole cell, feet and all, pjf
// at the slower beat of bash), bubble, and its extras.
export const POSES = {
  think: { eyes: 'glance', arms: ['down', 'mid'], bubble: 'think' },
  read: { eyes: 'scan', bubble: 'read' },
  bash: { walk: 'fast', grp: 'pjog pjf', bubble: 'bash' },
  write: { walk: 'fast', grp: 'pjog pjf', bubble: 'bash' },
  web: { eyes: 'up', bubble: 'web' },
  plan: { bubble: 'plan' },
  delegate: { walk: 'yes', grp: 'pjog', arms: 'swing', tow: true },
  run: { walk: 'yes', grp: 'pjog', arms: 'swing', bubble: 'dots' },
  compact: { eyes: 'strain', squash: true, bubble: 'press' },
  celebrate: { eyes: 'happy', grp: 'hop', arms: ['up', 'up'], flag: true, bubble: 'sparks' },
  hungry: { eyes: 'up', chomp: true, bubble: 'cookie' },
  wait: { eyes: 'up', bubble: 'ask' },
  err: { eyes: 'x', arms: ['up', 'up'], bubble: 'bang' },
  start: { bubble: 'dots' },
  exit: { hollow: true },
  idle: { eyes: 'shut', bubble: 'zz' },
};
// Poses of a working agent: an egg rocks through them, faster for bash.
const ROCK = new Set(['run', 'think', 'read', 'write', 'bash', 'web', 'plan', 'delegate']);

// The bubbles, in cells of the box: [rows, x, y, motion class, colour].
// Colour P is the prop colour (light, or the body's when that clashes with
// an aura), B the body's, K near-black.
const Z4 = ['####', '..#.', '.#..', '####'];
const Z3 = ['###', '.#.', '###'];
const PLUS = ['.#.', '###', '.#.'];
const BUBBLES = {
  think: [[['#'], 0, 4, 't1'], [['#'], 1, 2, 't2'], [['##', '##'], 3, 0, 't3']],
  read: [[GLYPH.mag, 0, 0, '']],
  bash: [[GLYPH.prompt, 0, 1, ''], [['##'], 3, 3, 'cursor']],
  web: [[GLYPH.globe, 0, 0, 'la slow'], [GLYPH.globe2, 0, 0, 'lb slow']],
  plan: [0, 1, 2].flatMap((i) => [[['#.###'], 0, i * 2, ''], [['#'], 0, i * 2, `tick${i}`, 'B']]),
  dots: [[['#.#.#'], 0, 4, 'dots']],
  press: [[['####'], 0, 0, 'press2'], [['####'], 0, 2, 'press1'], [['####'], 0, 4, '']],
  cookie: [[GLYPH.cookie, 0, 0, 'bobc']],
  ask: [[GLYPH.ask, 1, 0, 'blink']],
  bang: [[GLYPH.bang, 2, 0, 'bang']],
  zz: [[Z4, 0, 1, 'zz z0'], [Z3, 4, -2, 'zz z1']], // each z its own class, so each has its own outline
  sparks: [[PLUS, 0, 2, 'spark s0'], [PLUS, 2, 0, 'spark s1']],
};

const key = (x, y) => `${x},${y}`;
const at = (k) => k.split(',').map(Number);
const glyph = (rows, x0, y0, ch) => rows.flatMap((r, y) => [...r].flatMap((c, x) => (c === '#' ? [[x0 + x, y0 + y, ch]] : [])));
const BODY_CH = new Set(['b', 'd', 'e', 'k', 'w', 'v', 's', 'm']);

// The left and right edge of the art's body on row y.
function span(pix, y) {
  let l = Infinity, r = -Infinity;
  for (const [k, ch] of pix) {
    const [x, yy] = at(k);
    if (yy === y && BODY_CH.has(ch)) { l = Math.min(l, x); r = Math.max(r, x); }
  }
  return [l, r];
}
// A 1 x 2 arm whose top row is y, beside the body.
export function armCells(pix, y, side) {
  const [a, b] = [span(pix, y), span(pix, y + 1)];
  const x = side ? Math.max(a[1], b[1]) + 1 : Math.min(a[0], b[0]) - 1;
  return [[x, y], [x, y + 1]];
}

// One eye [x, y, w, h] drawn as kind; side 0 is the left eye.
export function eyeCells([x, y, w, h], kind, side) {
  const t = h >= 2 ? y : y - 1; // the top of a 3-row mark
  const row = (yy, x0 = x, n = w) => Array.from({ length: n }, (_, i) => [x0 + i, yy]);
  const rect = (dy) => [0, 1].slice(0, h).flatMap((j) => row(y + j + dy));
  switch (kind) {
    case 'up': case 'glance': return rect(-1);
    case 'shut': return row(y + h - 1, side ? x : x - 1, w + 1);
    case 'happy': return [[x - 1, t + 1], ...row(t), [x + w, t + 1]];
    case 'x': return [[x - 1, t], [x + w, t], ...row(t + 1), [x - 1, t + 2], [x + w, t + 2]];
    case 'strain': return side ? [[x + w, t], ...row(t + 1), [x + w, t + 2]] : [[x - 1, t], ...row(t + 1), [x - 1, t + 2]]; // > <
    case 'dot': return [[side ? x : x + w - 1, y + h - 1]];
    default: return rect(0);
  }
}

// posePet: what changes on a pet's pixels for a critter pose. look is
// petLook's (its pixels with the hat and mouth on), opts.bubble false leaves
// the bubble out, opts.keepOut: cells the bubble must not touch (the
// mystery "?"). Returns pix (the body, changed), the motion class of the
// body (grp), layers inside it (inner) and outside it (outer: feet that
// step, the bubble, the mini pet), each [cells [x, y, colour key], class],
// top (the cells that drop when it is squeezed), hollow and shade (the hat
// dims).
export function posePet(look, pose, opts = {}) {
  const spec = POSES[pose] || POSES.idle;
  const rig = RIG[look.name];
  const pix = new Map(look.pix);
  const out = { pix, grp: '', inner: [], outer: [], top: null, hollow: false, shade: false };
  if (!rig) return out;
  const egg = look.egg;
  const body = look.name === 'weak' ? 'd' : 'b';
  const eyeCh = look.name === 'weak' ? 'k' : 'e';
  const asleep = look.asleep;
  // In the ice nothing moves: only "?" and "!" show beside the cube.
  const bubbleOnly = asleep && (pose === 'wait' || pose === 'err');
  if (asleep && !bubbleOnly) return out;
  // A pet hungry for food stays awake: an idle one keeps its eyes open and
  // its mouth wide, no "z z".
  const awake = pose === 'idle' && look.hungry;
  const layer = (cells, cls, where = out.inner) => { if (cells.length) where.push([cells, cls]); };

  if (!egg && !bubbleOnly) {
    // eyes
    const kind = awake ? '' : spec.eyes;
    if (kind && rig.eyes) {
      const cells = [];
      rig.eyes.forEach((e, side) => {
        for (const [x, y] of eyeCells(e, 'open', side)) pix.set(key(x, y), body);
        cells.push(...eyeCells(e, kind, side).map(([x, y]) => [x, y, eyeCh]));
      });
      if (kind === 'glance' || kind === 'scan') layer(cells, kind);
      else for (const [x, y, ch] of cells) pix.set(key(x, y), ch);
    }
    // arms
    const arm = (pos, side, cls = '') => {
      const y = rig.arms[{ down: 0, mid: 1, up: 2 }[pos]];
      layer(armCells(look.pix, y, side).map(([x, yy]) => [x, yy, 'b']), `pp${cls ? ` ${cls}` : ''}`);
    };
    if (spec.arms === 'swing') { arm('down', 0, 'la'); arm('mid', 1, 'la'); arm('mid', 0, 'lb'); arm('down', 1, 'lb'); }
    else if (spec.arms) { arm(spec.arms[0], 0); arm(spec.arms[1], 1); }
    // feet that step between two places
    if (spec.walk) {
      const [rows, xs, stepped] = rig.feet;
      for (const y of rows) for (const x of xs) pix.delete(key(x, y));
      const fast = spec.walk === 'fast' ? ' fast' : '';
      // inside the body's motion: the feet never come apart from the body
      layer(rows.flatMap((y) => xs.map((x) => [x, y, body])), `la${fast}`);
      layer(rows.flatMap((y) => stepped.map((x) => [x, y, body])), `lb${fast}`);
    }
    // the mouth chomps (the hungry mouth's grey, or the weak pet's line)
    if (spec.chomp) {
      const [mx, my] = rig.mouth;
      if (look.name === 'weak') layer([[mx, my, 'k'], [mx + 1, my, 'k'], [mx + 2, my, 'k']], 'la slow');
      else {
        for (const [dx, dy] of [[0, 0], [1, 0], [0, 1], [1, 1]]) pix.set(key(mx + dx, my + dy), body);
        layer([[mx, my, 'm'], [mx + 1, my, 'm'], [mx, my + 1, 'm'], [mx + 1, my + 1, 'm']], 'la slow');
        layer([[mx, my + 1, 'm'], [mx + 1, my + 1, 'm']], 'lb slow');
      }
    }
    // the finish flag, in the raised right hand: a pole and a 3 x 2 checker
    // that waves by swapping its squares
    if (spec.flag) {
      const up = rig.arms[2];
      const px = armCells(look.pix, up, 1)[0][0] + 1;
      const checker = (phase) => [0, 1, 2].flatMap((cx) => [0, 1].map((cy) => [px + 1 + cx, up - 4 + cy, (cx + cy + phase) % 2 ? 'k' : 'w']));
      layer([0, 1, 2, 3, 4, 5].map((i) => [px, up - 4 + i, 'P']), 'pp');
      layer(checker(0), 'pp la');
      layer(checker(1), 'pp lb');
      out.flagAt = [px, up];
    }
    if (spec.hollow) {
      // only the outline stays, with two dots for eyes; the hat dims
      for (const k of [...pix.keys()]) {
        const [x, y] = at(k);
        if (look.hatKeys.has(k)) continue;
        if ([[1, 0], [-1, 0], [0, 1], [0, -1]].every(([dx, dy]) => look.pix.has(key(x + dx, y + dy)) && !look.hatKeys.has(key(x + dx, y + dy)))) pix.delete(k);
      }
      rig.eyes?.forEach((e, side) => { for (const [x, y] of eyeCells(e, 'dot', side)) pix.set(key(x, y), body); });
      out.hollow = out.shade = true;
    }
  } else if (egg && !bubbleOnly && spec.hollow) {
    for (const k of [...pix.keys()]) {
      const [x, y] = at(k);
      if ([[1, 0], [-1, 0], [0, 1], [0, -1]].every(([dx, dy]) => look.pix.has(key(x + dx, y + dy)))) pix.delete(k);
    }
    out.hollow = true;
  }
  if (!bubbleOnly) {
    // the whole body moves: an egg rocks while it works
    if (egg) out.grp = ROCK.has(pose) ? `prock${spec.walk === 'fast' ? ' prk2' : ''}` : pose === 'celebrate' ? 'hop' : '';
    else out.grp = spec.grp || '';
    // squeezed: all but the rows it drops over (above the feet) drop, and
    // are drawn over them, face and all
    if (spec.squash) {
      const feetTop = rig.feet ? rig.feet[0][0] : Math.max(...[...pix.keys()].map((k) => at(k)[1])) + 1;
      out.squeeze = rig.squeeze || 2;
      const split = feetTop - out.squeeze;
      out.top = new Set([...pix.keys()].filter((k) => at(k)[1] < split));
    }
  }
  // the mini pet in tow, on stepping feet (it doesn't bob: its feet would
  // come apart from it)
  if (spec.tow && !bubbleOnly) {
    const [tx, ty] = rig.tow;
    layer(glyph(['#####', '#.#.#'], tx, ty, body), 'pp', out.outer);
    layer(glyph(['#...#'], tx, ty + 2, body), 'pp la', out.outer);
    layer(glyph(['.#.#.'], tx, ty + 2, body), 'pp lb', out.outer);
  }
  // the bubble, up and to the right; the hatched pet's sparks go by its flag
  const name = awake ? '' : spec.bubble;
  if (name && opts.bubble !== false) {
    let [bx, by] = rig.bubble;
    let list = BUBBLES[name];
    // beside the flag: one spark right of it, one above
    if (name === 'sparks' && out.flagAt) { [bx, by] = out.flagAt; list = [[PLUS, 5, -4, 'spark s0'], [PLUS, 3, -8, 'spark s1']]; }
    const cells = list.map(([rows, x, y, cls, ch = 'P']) => [glyph(rows, bx + x, by + y, ch), cls]);
    const keep = opts.keepOut || new Set();
    const clash = cells.some(([cs]) => cs.some(([x, y]) => [-1, 0, 1].some((dx) => [-1, 0, 1].some((dy) => keep.has(key(x + dx, y + dy))))));
    // the sparks by the flag hop with it
    if (!clash) for (const [cs, cls] of cells) layer(cs, `pp${cls ? ` ${cls}` : ''}`, out.flagAt && name === 'sparks' ? out.inner : out.outer);
  }
  return out;
}
