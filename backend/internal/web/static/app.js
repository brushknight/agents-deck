// agents-terminal web dashboard. Vanilla ES module, no dependencies, CSP-safe
// (no inline handlers, no eval, no style attributes — only CSSOM custom properties).
// Contract: docs/protocol.md. Live data: GET /v1/state + SSE /v1/events ("state" snapshots).

const $ = (id) => document.getElementById(id);
const params = new URLSearchParams(location.search);
const FIXTURE = location.protocol === 'file:' || params.has('fixture');

// ---------------------------------------------------------------- model
let state = null;          // last State from the server
let skew = 0;              // server clock minus local clock (ms)
let selectedId = null;     // agent shown in the detail panel
let needsFirst = false;    // "needs you first" sort toggle
let panelMode = false;     // "panel view": the 720×720 panel's 4×4 grid
let panelPage = 0;
let online = false;
let expired = false;

const now = () => Date.now() + skew;

// ---------------------------------------------------------------- format
const fmtN = (n) => {
  n = Number(n) || 0;
  const unit = (v, s) => {
    const r = v >= 100 ? Math.round(v) : Math.round(v * 10) / 10;
    return (r >= 1000 && s === 'k') ? '1m' : `${r}${s}`;
  };
  if (n >= 1e6) return unit(n / 1e6, 'm');
  if (n >= 1e3) return unit(n / 1e3, 'k');
  return String(Math.round(n));
};
const fmtCost = (usd) => `$${(Number(usd) || 0).toFixed(2)}`;
const rel = (iso) => {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return '';
  const s = Math.max(0, Math.round((now() - t) / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h`;
  return `${Math.floor(h / 24)}d`;
};
const ageMin = (iso) => (now() - Date.parse(iso)) / 60000;
const tilde = (p) => (p || '').replace(/^\/(Users|home)\/[^/]+(?=\/|$)/, '~');
const ctxPct = (a) => {
  const w = a.context?.window || 0;
  return w ? Math.min(100, Math.round((a.context.used / w) * 100)) : 0;
};
const VERBS = { todowrite: 'plan', webfetch: 'web', websearch: 'search', multiedit: 'edit', notebookedit: 'edit', task: 'agent' };
const verb = (tool) => { const t = (tool || '').toLowerCase(); return VERBS[t] || t || 'thinking'; };

// status → sprite pose
const POSE = { running: 'run', waiting: 'wait', error: 'err', idle: 'idle', starting: 'start', exited: 'exit' };
// A running agent's pose follows its tool; a just-finished one hops for a bit.
const CELEBRATE_MS = 6000;
const TOOL_POSE = {
  read: 'read', grep: 'read', glob: 'read', ls: 'read',
  edit: 'write', write: 'write', multiedit: 'write', notebookedit: 'write',
  bash: 'bash', bashoutput: 'bash', killshell: 'bash', killbash: 'bash',
  webfetch: 'web', websearch: 'web', todowrite: 'plan', task: 'delegate', agent: 'delegate',
};
const poseOf = (a) => {
  if (a.status === 'running') {
    const tool = (a.activity?.tool || '').toLowerCase();
    return tool ? TOOL_POSE[tool] || 'run' : 'think';
  }
  if (a.status === 'idle' && a.statusSince && now() - Date.parse(a.statusSince) < CELEBRATE_MS) return 'celebrate';
  if (hungry(a)) return 'hungry';
  return POSE[a.status] || 'idle';
};

const needsYou = (a) => a.status === 'waiting' || a.status === 'error';

// Finished a turn nobody has reviewed yet: hungry until its terminal is focused.
const hungry = (a) => a.status === 'idle' && a.unseen;

function tileMeta(a) {
  if (hungry(a)) return 'hungry · feed me';
  switch (a.status) {
    case 'running': return `${verb(a.activity?.tool)} · ${ctxPct(a)}% ctx`;
    case 'waiting': {
      const w = a.waiting || {};
      if (w.kind === 'permission') {
        const what = (w.context || '').split('·')[0].trim().toLowerCase();
        return what ? `allow ${what}?` : 'allow?';
      }
      if (w.kind === 'question') return `pick 1 of ${(w.options || []).length}`;
      return 'needs input';
    }
    case 'idle': return ageMin(a.statusSince) < 120 ? `done · ${rel(a.statusSince)} ago` : `idle · ${rel(a.statusSince)}`;
    case 'error': return a.error?.message || 'error';
    case 'starting': return 'starting…';
    case 'exited': return a.resumable ? `ended · resumable` : `exited · ${rel(a.statusSince)} ago`;
    default: return a.status || '';
  }
}

function pillText(a) {
  const since = rel(a.statusSince);
  switch (a.status) {
    case 'running': return `running · ${since}`;
    case 'waiting': return a.waiting?.kind === 'question' && a.waiting.context ? a.waiting.context : `needs you · ${since}`;
    case 'idle': return a.unseen ? `hungry · ${since}` : `idle · ${since}`;
    case 'error': return `error · ${since}`;
    case 'exited': return `exited · ${since}`;
    default: return a.status;
  }
}

// ---------------------------------------------------------------- sprite
// Pixel critter from the approved panel mockup (viewBox 0 -4 18 13). Colours come from
// CSS: .b = var(--body), .c / .cx = var(--cut) (the background the sprite sits on).
const BODY = 'M2 0h10v1h1v1h1v2h-1v2h-1v1h-10v-1h-1v-2h-1v-2h1v-1h1z';
const LEGS_A = [2, 4, 9, 11];
const LEGS_B = [3, 5, 8, 10];
const critterLegs = (xs, cls = '') => xs.map((x) => `<rect class="b ${cls}" x="${x}" y="7" width="1" height="2"/>`).join('');
// Codex bot: block feet that step in turn (legs B lifts the left foot).
const botFeet = (xs, cls = '') => `<rect class="b ${cls}" x="2.5" y="${xs === LEGS_B ? 6.4 : 7}" width="3" height="1.6"/><rect class="b ${cls}" x="8.5" y="7" width="3" height="1.6"/>`;
const BOT_BODY = 'M1 0h12v1h1v5h-1v1h-12v-1h-1v-5h1z';
const BOT_ANTENNA = '<rect class="b" x="6.6" y="-1.4" width="0.8" height="1.4"/><rect class="b ant" x="6.2" y="-2.4" width="1.6" height="1"/>';
let legs = critterLegs;
const eyesOpen = (y) => `<rect class="c" x="4" y="${y}" width="1" height="2"/><rect class="c" x="9" y="${y}" width="1" height="2"/>`;
const eyesOpenAt = (y, h) => `<rect class="c" x="4" y="${y}" width="1" height="${h}"/><rect class="c" x="9" y="${y}" width="1" height="${h}"/>`;
const eyesShut = '<rect class="c" x="3.5" y="3.2" width="2" height="0.55"/><rect class="c" x="8.5" y="3.2" width="2" height="0.55"/>';
const DOTS = '<g class="dots"><rect class="b" x="14.6" y="-2.6" width="0.9" height="0.9"/><rect class="b" x="15.9" y="-2.6" width="0.9" height="0.9"/><rect class="b" x="17.2" y="-2.6" width="0.8" height="0.9"/></g>';
const pixels = (rows, x0, w, cls, h = 0.9, y0 = -4) => `<g class="${cls}">` + rows.flatMap((row, r) => [...row].map((ch, c) =>
  ch === '#' ? `<rect class="b" x="${x0 + c * w}" y="${y0 + r * h}" width="${w}" height="${h}"/>` : '')).join('') + '</g>';

const MINI = (x, y, s) => `<g transform="translate(${x} ${y}) scale(${s})"><g class="bob"><path class="b" d="${BODY}"/>` +
  `<rect class="c" x="4" y="2" width="1" height="2"/><rect class="c" x="9" y="2" width="1" height="2"/></g>${legs(LEGS_A, 'la')}${legs(LEGS_B, 'lb')}</g>`;

function spriteSVG(pose, species = 'claude') {
  const bot = species === 'codex';
  legs = bot ? botFeet : critterLegs;
  let body = bot ? `<path class="b" d="${BOT_BODY}"/>${BOT_ANTENNA}` : `<path class="b" d="${BODY}"/>`, eyes, feet = legs(LEGS_A), bubble = '', grp = '';
  switch (pose) {
    case 'think':
      eyes = `<g class="glance">${eyesOpenAt(1, 1.5)}</g>`;
      bubble = '<rect class="b t1" x="14.3" y="-0.9" width="0.6" height="0.6"/><rect class="b t2" x="15.3" y="-2.1" width="0.9" height="0.9"/><rect class="b t3" x="16.5" y="-3.8" width="1.3" height="1.3"/>';
      break;
    case 'read':
      eyes = `<g class="scan">${eyesOpen(2)}</g>`;
      bubble = pixels(['.##..', '#..#.', '#..#.', '.##..', '....#'], 14.6, 0.75, '', 0.75);
      break;
    case 'write':
      eyes = '<rect class="c" x="4" y="3.2" width="1" height="1.2"/><rect class="c" x="9" y="3.2" width="1" height="1.2"/>';
      feet = legs(LEGS_A) + '<rect class="b tapA" x="5.9" y="7" width="0.9" height="0.8"/><rect class="b tapB" x="7.3" y="7.6" width="0.9" height="0.8"/>';
      bubble = pixels(['...##', '..##.', '.##..', '##...', '#....'], 14.7, 0.7, '', 0.7);
      break;
    case 'bash':
      eyes = eyesOpen(2); feet = legs(LEGS_A, 'la fast') + legs(LEGS_B, 'lb fast'); grp = 'bob fast';
      bubble = pixels(['#..', '.#.', '#..'], 14.5, 0.7, '', 0.8, -3.7) + '<rect class="b cursor" x="16.5" y="-1.7" width="1.2" height="0.5"/>';
      break;
    case 'web':
      eyes = '<rect class="c" x="4.4" y="1" width="1" height="2"/><rect class="c" x="9.4" y="1" width="1" height="2"/>';
      bubble = pixels(['.###.', '#.#.#', '#####', '#.#.#', '.###.'], 14.4, 0.72, 'la slow', 0.72, -4.2) +
        pixels(['.###.', '##.##', '#####', '##.##', '.###.'], 14.4, 0.72, 'lb slow', 0.72, -4.2);
      break;
    case 'plan':
      eyes = eyesOpen(2);
      bubble = [0, 1, 2].map((i) => { const y = -3.9 + i * 1.25;
        return `<rect class="b" x="14.4" y="${y}" width="0.9" height="0.9"/><rect class="c tick${i}" x="14.6" y="${y + 0.2}" width="0.5" height="0.5"/><rect class="b" x="15.6" y="${y + 0.25}" width="2.2" height="0.45"/>`; }).join('');
      break;
    case 'delegate':
      eyes = eyesOpen(2); feet = legs(LEGS_A, 'la') + legs(LEGS_B, 'lb'); grp = 'bob';
      bubble = MINI(14.4, 3.2, 0.26);
      break;
    case 'hungry':
      // Eyes up at a bobbing cookie; the mouth chomps open and shut.
      eyes = '<rect class="c" x="4.6" y="1" width="1" height="2"/><rect class="c" x="9.6" y="1" width="1" height="2"/>' +
        '<rect class="c la slow" x="6.2" y="4.2" width="2.2" height="1.8"/><rect class="c lb slow" x="6.2" y="5" width="2.2" height="0.5"/>';
      bubble = `<g class="bobc">${pixels(['.###.', '##.##', '#####', '#.###', '.###.'], 14.3, 0.75, '', 0.75, -4.2)}</g>`;
      break;
    case 'celebrate': {
      eyes = [4.5, 9.5].map((x) => `<path class="cx" stroke-width="0.55" d="M${x - 1} 3.3 L${x} 2.2 L${x + 1} 3.3"/>`).join('');
      grp = 'hop'; feet = `<g class="hop">${legs(LEGS_A)}</g>`;
      const plus = (x, y, i) => `<g class="spark s${i}"><rect class="b" x="${x - 0.5}" y="${y}" width="1.5" height="0.5"/><rect class="b" x="${x}" y="${y - 0.5}" width="0.5" height="1.5"/></g>`;
      bubble = plus(15, -3.4, 0) + plus(17.2, -1.9, 1) + plus(14.5, -0.8, 2);
      break;
    }
    case 'run':
      eyes = eyesOpen(2); feet = legs(LEGS_A, 'la') + legs(LEGS_B, 'lb'); bubble = DOTS; grp = 'bob'; break;
    case 'wait':
      eyes = eyesOpen(1); bubble = pixels(['###', '..#', '.#.', '...', '.#.'], 14.5, 1.1, 'blink'); break;
    case 'err':
      eyes = [3.8, 8.8].map((x) => `<path class="cx" stroke-width="0.55" d="M${x - 0.4} 1.8 l1.8 2.2 M${x + 1.4} 1.8 l-1.8 2.2"/>`).join('');
      bubble = pixels(['#', '#', '#', '.', '#'], 15.6, 1.2, 'bang'); break;
    case 'start':
      eyes = eyesOpen(2); bubble = DOTS; break;
    case 'exit':
      body += bot ? '<path class="c" d="M2 1h10v5h-10z"/>' : '<path class="c" d="M3 1h8v1h1v3h-1v1h-8v-1h-1v-3h1z"/>';
      eyes = '<rect class="b" x="4" y="3" width="1" height="1"/><rect class="b" x="9" y="3" width="1" height="1"/>'; break;
    default: // idle: asleep
      eyes = eyesShut;
      bubble = '<text class="zt zz" x="14.8" y="-0.6" font-size="3.2">z</text><text class="zt zz" x="16.6" y="-2.4" font-size="2.3">z</text>';
  }
  return `<svg viewBox="0 -4 18 13" shape-rendering="crispEdges" aria-hidden="true" focusable="false"><g class="${grp}">${body}${eyes}</g>${feet}${bubble}</svg>`;
}

// ---------------------------------------------------------------- dom helpers
function h(tag, attrs = {}, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k === 'text') el.textContent = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const kid of kids.flat()) if (kid != null && kid !== false) el.append(kid);
  return el;
}
const setText = (el, t) => { if (el.textContent !== t) el.textContent = t; };
const setSprite = (el, pose, species = 'claude') => {
  const key = `${pose}|${species}`;
  if (el.dataset.pose !== key) { el.dataset.pose = key; el.innerHTML = spriteSVG(pose, species); }
};

// ---------------------------------------------------------------- tiles (keyed)
const tiles = new Map(); // id -> { el, spr, name, meta, chip, key }

// The tile border doubles as a context gauge: filled clockwise from the top centre.
const RING_D = 'M50 1H99V99H1V1Z';
function makeRing() {
  const ns = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(ns, 'svg');
  svg.setAttribute('class', 'ring'); svg.setAttribute('viewBox', '0 0 100 100');
  svg.setAttribute('aria-hidden', 'true'); // tiles are square: uniform scaling keeps the dash exact
  const path = (cls) => {
    const p = document.createElementNS(ns, 'path');
    p.setAttribute('class', cls); p.setAttribute('d', RING_D); p.setAttribute('pathLength', '100');
    svg.append(p); return p;
  };
  path('rt');
  const fill = path('rf');
  fill.setAttribute('stroke-dasharray', '0 100');
  return { svg, fill };
}

function makeTile(a) {
  const spr = h('span', { class: 'spr' });
  const name = h('span', { class: 'name' });
  const meta = h('span', { class: 'meta' });
  const ring = makeRing();
  const el = h('button', { type: 'button', class: 'tile', 'data-id': a.id },
    h('span', { class: 'chip', 'aria-hidden': 'true' }), spr, name, meta, ring.svg);
  el.addEventListener('click', () => openDetail(a.id));
  wireDrag(el, a.id);
  return { el, spr, name, meta, ring: ring.fill, dash: '', cls: '', label: '' };
}

// ---------------------------------------------------------------- reorder
// Tiles can be dragged (or moved with Alt+arrows) to change the agents' slots;
// the daemon stores the order, so the panel follows. Disabled while the
// "needs you first" view is on, since that sort isn't the real order.
let dragId = null;

const slotOrder = () => [...(state?.agents || [])].sort((x, y) => x.slot - y.slot).map((a) => a.id);

async function saveOrder(ids) {
  const pos = new Map(ids.map((id, i) => [id, i]));
  for (const a of state.agents) if (pos.has(a.id)) a.slot = pos.get(a.id);
  renderGrid();
  const r = await api('/v1/order', { ids });
  if (!r.ok) loadState(); // put the real order back
}

// Dropping a tile on another swaps the two agents' positions.
function swap(a, b) {
  const ids = slotOrder();
  const i = ids.indexOf(a), j = ids.indexOf(b);
  if (i < 0 || j < 0 || i === j) return;
  [ids[i], ids[j]] = [ids[j], ids[i]];
  saveOrder(ids);
}

function moveTo(id, index) {
  const ids = slotOrder().filter((x) => x !== id);
  ids.splice(Math.max(0, Math.min(ids.length, index)), 0, id);
  saveOrder(ids);
}

function wireDrag(el, id) {
  el.draggable = true;
  el.addEventListener('dragstart', (e) => {
    if (needsFirst) { e.preventDefault(); return; }
    dragId = id;
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', id);
    el.dataset.dragging = '';
  });
  el.addEventListener('dragover', (e) => {
    if (!dragId || dragId === id) return;
    e.preventDefault();
    el.dataset.drop = 'swap';
  });
  el.addEventListener('dragleave', () => { delete el.dataset.drop; });
  el.addEventListener('drop', (e) => {
    e.preventDefault();
    delete el.dataset.drop;
    if (dragId && dragId !== id) swap(dragId, id);
  });
  el.addEventListener('dragend', () => {
    dragId = null;
    for (const t of tiles.values()) { delete t.el.dataset.drop; delete t.el.dataset.dragging; }
  });
}

function updateTile(t, a) {
  const cls = `tile st-${a.status} tool-${a.tool}` + (hungry(a) ? ' hungry' : '') + (a.focused ? ' focused' : '') + (a.id === selectedId ? ' selected' : '');
  if (t.cls !== cls) { t.el.className = cls; t.cls = cls; }
  setSprite(t.spr, poseOf(a), a.tool);
  const used = a.context?.window > 0 ? Math.min(100, (a.context.used / a.context.window) * 100) : 0;
  const dash = `${used.toFixed(2)} 100`;
  if (t.dash !== dash) { t.ring.setAttribute('stroke-dasharray', dash); t.dash = dash; }
  const meta = tileMeta(a);
  setText(t.name, a.title);
  setText(t.meta, meta);
  const label = `${a.title}, ${a.tool}, ${a.status === 'waiting' ? 'needs you' : a.status}: ${meta}${a.focused ? ', focused terminal' : ''}`;
  if (t.label !== label) { t.el.setAttribute('aria-label', label); t.label = label; }
  if (a.id === selectedId) t.el.setAttribute('aria-expanded', 'true'); else t.el.removeAttribute('aria-expanded');
}

function orderedAgents() {
  const list = [...(state?.agents || [])].sort((x, y) => x.slot - y.slot);
  if (needsFirst) list.sort((x, y) => (needsYou(y) ? 1 : 0) - (needsYou(x) ? 1 : 0)); // stable
  return list;
}

function renderGrid() {
  const grid = $('grid');
  const list = orderedAgents();
  const seen = new Set();
  for (const a of list) {
    seen.add(a.id);
    let t = tiles.get(a.id);
    if (!t) { t = makeTile(a); tiles.set(a.id, t); }
    updateTile(t, a);
  }
  for (const [id, t] of tiles) if (!seen.has(id)) { t.el.remove(); tiles.delete(id); }
  const want = panelMode ? panelCells(list) : list.map((a) => tiles.get(a.id).el);
  // Reconcile in place so running animations aren't restarted needlessly.
  want.forEach((el, i) => { if (grid.children[i] !== el) grid.insertBefore(el, grid.children[i] || null); });
  while (grid.children.length > want.length) grid.lastElementChild.remove();
  $('empty').hidden = list.length > 0 || !state || panelMode;
  grid.hidden = list.length === 0 && !panelMode;
}

// Panel view: 16 cells like the device — up to 16 agents, else 15 per page
// plus a pager cell; free cells are empty slots you can drop a tile on.
const emptyCells = [];
let pagerCell = null;
function panelCells(list) {
  const paged = list.length > 16;
  const per = paged ? 15 : 16;
  const pages = Math.max(1, Math.ceil(list.length / per));
  if (panelPage >= pages) panelPage = 0;
  const start = panelPage * per;
  const shown = list.slice(start, start + per);
  const cells = shown.map((a) => tiles.get(a.id).el);
  for (let i = 0; cells.length < per; i++) {
    if (!emptyCells[i]) emptyCells[i] = makeEmptyCell();
    emptyCells[i].dataset.index = String(start + shown.length + i);
    cells.push(emptyCells[i]);
  }
  if (paged) {
    if (!pagerCell) {
      pagerCell = h('button', { type: 'button', class: 'tile pager' });
      pagerCell.addEventListener('click', () => { panelPage++; renderGrid(); });
    }
    const elsewhere = list.some((a, i) => (i < start || i >= start + per) && needsYou(a));
    pagerCell.classList.toggle('hot', elsewhere);
    const bar = Array.from({ length: pages }, (_, i) => (i === panelPage ? '■' : '□')).join(' ');
    pagerCell.replaceChildren(h('span', { class: 'name', text: 'next ›' }), h('span', { class: 'meta', text: bar }));
    pagerCell.setAttribute('aria-label', `page ${panelPage + 1} of ${pages}, next page`);
    cells.push(pagerCell);
  }
  return cells;
}

function makeEmptyCell() {
  const el = h('div', { class: 'tile-empty', 'aria-hidden': 'true' });
  el.addEventListener('dragover', (e) => { if (dragId) { e.preventDefault(); el.dataset.drop = 'swap'; } });
  el.addEventListener('dragleave', () => { delete el.dataset.drop; });
  el.addEventListener('drop', (e) => {
    e.preventDefault();
    delete el.dataset.drop;
    if (dragId) moveTo(dragId, Number(el.dataset.index)); // a free slot: move there (the end)
  });
  return el;
}

function renderTop() {
  const c = { running: 0, waiting: 0, idle: 0, error: 0 };
  for (const a of state?.agents || []) {
    if (a.status === 'running' || a.status === 'starting') c.running++;
    else if (a.status === 'exited') c.idle++;
    else if (a.status in c) c[a.status]++;
  }
  for (const k of Object.keys(c)) setText($(`n-${k}`), String(c[k]));
  $('c-waiting').classList.toggle('hot', c.waiting > 0);
  $('c-error').classList.toggle('hot', c.error > 0);
  if (state?.server?.name) setText($('server-name'), state.server.name);
  const attention = c.waiting ? `(${c.waiting}) ` : '';
  const title = `${attention}agents${state?.server?.name ? ' · ' + state.server.name : ''}`;
  if (document.title !== title) document.title = title;
}

function setConn(mode) { // live | reconnecting | connecting
  online = mode === 'live';
  const el = $('conn');
  el.classList.toggle('live', online);
  el.classList.toggle('down', mode === 'reconnecting');
  setText($('conn-label'), mode === 'live' ? (FIXTURE ? 'fixture' : 'live') : mode === 'reconnecting' ? 'reconnecting…' : 'connecting…');
  const down = mode === 'reconnecting' && !!state;
  document.body.classList.toggle('offline', down);
  $('banner').hidden = !down;
}

function tickClock() {
  const d = new Date(now());
  setText($('clock'), `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`);
}

// ---------------------------------------------------------------- detail panel
const panel = $('detail');
let det = null; // { key, id, refs, update(a) }
const sending = new Map(); // promptId -> { key, status: 'sending'|'sent' }

function detailKey(a) { return `${a.id}|${a.status}|${a.waiting?.id || ''}|${a.resumable ? 'r' : ''}`; }

function openDetail(id) {
  selectedId = id;
  det = null;
  render();
  $('d-title')?.focus({ preventScroll: true });
}

function closeDetail() {
  const id = selectedId;
  selectedId = null; det = null;
  panel.hidden = true; panel.replaceChildren();
  render();
  tiles.get(id)?.el.focus({ preventScroll: true });
}

const closeIcon = () => {
  const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  s.setAttribute('viewBox', '0 0 12 12'); s.setAttribute('aria-hidden', 'true');
  s.innerHTML = '<path d="M1 1L11 11M11 1L1 11" stroke="currentColor" stroke-width="2"/>';
  return s;
};

function header(a) {
  const spr = h('span', { class: 'd-spr' });
  const pill = h('span', { class: 'pill' });
  const ai = h('span', { class: 'd-ai' });
  const el = h('div', { class: 'd-top' },
    h('div', { class: 'd-id' }, spr, h('h2', { class: 'd-title', id: 'd-title', tabindex: '-1', text: a.title }), ai),
    h('div', { class: 'd-right' },
      h('button', { type: 'button', class: 'd-close', 'aria-label': 'close panel (esc)', onclick: closeDetail }, closeIcon()),
      h('div', { class: `d-pillrow tool-${a.tool}` }, h('span', { class: 'chip', title: a.tool, 'aria-label': `tool: ${a.tool}`, role: 'img' }), pill)));
  setSprite(spr, poseOf(a), a.tool);
  return { el, update: (x) => { setText(pill, pillText(x)); setText(ai, x.aiTitle || ''); ai.hidden = !x.aiTitle; } };
}

function postAction(a, path, body) {
  return api(`/v1/agents/${encodeURIComponent(a.id)}/${path}`, body);
}

function actionButton(a, label, path, cls, note) {
  const b = h('button', { type: 'button', class: `btn ${cls}` , text: label });
  b.addEventListener('click', async () => {
    b.disabled = true;
    note.className = 'note'; note.textContent = `${label} · sending…`;
    const r = await postAction(a, path);
    b.disabled = false;
    if (r.ok) note.textContent = `${label} · sent`;
    else { note.className = 'note bad'; note.textContent = `${label} failed · ${r.error}`; }
  });
  return b;
}

// Exited agents are removed at once; a live one is stopped only on a second click.
function removeButton(a, note) {
  const exited = a.status === 'exited' || a.external; // hiding a Codex thread needs no confirm
  const idle = a.external ? 'hide' : a.status === 'exited' ? 'remove' : 'stop';
  const b = h('button', { type: 'button', class: 'btn narrow', text: idle });
  let armed = null;
  b.addEventListener('click', async () => {
    if (!exited && !armed) {
      b.textContent = 'click again to stop';
      b.classList.add('armed');
      armed = setTimeout(() => { armed = null; b.textContent = idle; b.classList.remove('armed'); }, 3000);
      return;
    }
    clearTimeout(armed); armed = null;
    b.disabled = true;
    note.className = 'note'; note.textContent = `${idle} · sending…`;
    const r = await postAction(a, 'dismiss');
    b.disabled = false;
    if (!r.ok) { note.className = 'note bad'; note.textContent = `${idle} failed · ${r.error}`; }
  });
  return b;
}

function buildInfo(a) {
  const hd = header(a);
  const doingLbl = h('span', { class: 'lbl' });
  const doing = h('span', { class: 'val' });
  const ctxNums = h('small');
  const ctxPctEl = h('span', { class: 'h' });
  const on = h('span', { class: 'on' });
  const bar = h('div', { class: 'ticks', role: 'meter', 'aria-label': 'context used', 'aria-valuemin': '0', 'aria-valuemax': '100' }, on, h('span', { class: 'off' }));
  const stat = (k) => { const v = h('span', { class: 'v' }); return [h('div', { class: 'stat' }, h('span', { class: 'k', text: k }), v), v]; };
  const S = ['tokens in', 'tokens out', 'cache read', 'cache write', 'cost', 'turns'].map(stat);
  const prompt = h('span', { class: 'val' });
  const foot = h('span');
  const server = h('span');
  const note = h('div', { class: 'note', role: 'status', 'aria-live': 'polite' });
  const actions = h('div', { class: 'actions' });
  if (a.status !== 'exited') actions.append(actionButton(a, a.external ? 'open in codex' : hungry(a) ? 'review in terminal' : 'focus terminal', 'focus', 'solid', note));
  else if (a.resumable) actions.append(actionButton(a, 'resume', 'resume', 'solid', note));
  if (a.status === 'running' && !a.external) actions.append(actionButton(a, 'interrupt', 'interrupt', 'narrow', note));
  else actions.append(removeButton(a, note));

  panel.className = 'detail' + (a.status === 'error' ? ' p-error' : ['idle', 'starting', 'exited'].includes(a.status) ? ' p-dim' : '');
  panel.replaceChildren(hd.el,
    h('div', { class: 'sec' }, doingLbl, doing),
    h('div', { class: 'sec' }, h('div', { class: 'ctx-head' }, h('span', { class: 'h' }, 'context', ctxNums), ctxPctEl), bar),
    h('div', { class: 'stats' }, S.map((s) => s[0])),
    h('div', { class: 'sec grow' }, h('span', { class: 'lbl', text: 'last prompt' }), prompt),
    h('div', { class: 'd-foot' }, foot, server),
    actions, note);

  return (x) => {
    hd.update(x);
    if (x.status === 'running') {
      setText(doingLbl, 'doing now');
      if (x.activity) doing.replaceChildren(verb(x.activity.tool), h('span', { class: 'sep', text: ' · ' }), x.activity.detail || '');
      else setText(doing, 'thinking…');
      doing.className = 'val';
    } else if (x.status === 'error') {
      setText(doingLbl, 'error'); setText(doing, x.error?.message || 'unknown error'); doing.className = 'val err';
    } else {
      setText(doingLbl, 'status');
      setText(doing, x.status === 'idle' ? `finished · ${rel(x.statusSince)} ago` : x.status === 'starting' ? 'starting…' : `exited · ${rel(x.statusSince)} ago`);
      doing.className = 'val';
    }
    const pct = ctxPct(x);
    setText(ctxNums, `${fmtN(x.context?.used)} / ${fmtN(x.context?.window)}`);
    setText(ctxPctEl, `${pct}%`);
    bar.style.setProperty('--pct', String(pct));
    bar.classList.toggle('hot', pct >= 85);
    bar.setAttribute('aria-valuenow', String(pct));
    const t = x.tokens || {};
    [fmtN(t.input), fmtN(t.output), fmtN(t.cacheRead), fmtN(t.cacheWrite)].forEach((v, i) => setText(S[i][1], v));
    const cost = x.external ? '—' : fmtCost(x.costUsd); // no price list for Codex models
    if (S[4][1].dataset.v !== cost) { S[4][1].dataset.v = cost; S[4][1].replaceChildren(cost, ...(x.external ? [] : [h('small', { text: 'est' })])); }
    setText(S[5][1], String(x.turns ?? 0));
    setText(prompt, x.lastPrompt || '—');
    setText(foot, [x.modelLabel || x.model, tilde(x.cwd), x.branch].filter(Boolean).join(' · '));
    setText(server, state?.server?.name || '');
  };
}

function splitLabel(label) {
  const i = label.indexOf(' — ');
  return i > 0 ? [label.slice(0, i), label.slice(i + 3)] : [label, ''];
}

function buildPrompt(a) {
  const w = a.waiting;
  const hd = header(a);
  const note = h('div', { class: 'note', role: 'status', 'aria-live': 'polite' });
  const opts = [];
  let selected = (w.options.find((o) => o.primary) || w.options[0])?.key;
  const isQ = w.kind === 'question';

  const setBusy = (busy, sentKey) => {
    for (const o of opts) { o.el.disabled = busy; o.el.classList.toggle('sent', o.key === sentKey); }
    if (sendBtn) sendBtn.disabled = busy;
  };

  const answer = async (key) => {
    const cur = sending.get(w.id);
    if (cur) return;
    sending.set(w.id, { key, status: 'sending' });
    setBusy(true, null);
    note.className = 'note'; note.textContent = `sending ${key}…`;
    const r = await postAction(a, 'answer', { promptId: w.id, key });
    if (r.ok) {
      sending.set(w.id, { key, status: 'sent' });
      setBusy(true, key);
      note.textContent = `sent ${key} · waiting for the agent…`;
      return;
    }
    sending.delete(w.id);
    setBusy(false, null);
    note.className = 'note bad';
    note.textContent = r.status === 409 ? 'prompt changed · showing the latest' : `not sent · ${r.error}`;
  };

  const optList = h('div', { class: 'opts', role: isQ ? 'radiogroup' : 'group', 'aria-labelledby': 'p-title' });
  for (const o of w.options) {
    const [title, sub] = splitLabel(o.label);
    const el = h('button', { type: 'button', class: 'opt' + (o.primary ? ' primary' : ''), 'aria-keyshortcuts': o.key,
      ...(isQ ? { role: 'radio', 'aria-checked': String(o.key === selected) } : {}) },
      h('span', { class: 'n', text: o.key }),
      h('span', { class: 't' }, h('span', { text: isQ ? title : o.label }), isQ && sub ? h('span', { class: 's', text: sub }) : null));
    el.addEventListener('click', () => {
      if (!isQ) return answer(o.key);
      selected = o.key;
      for (const x of opts) x.el.setAttribute('aria-checked', String(x.key === selected));
      setText(sendBtn, `send ${selected}`);
    });
    opts.push({ key: o.key, el });
    optList.append(el);
  }

  let sendBtn = null;
  const focusBtn = (cls, label) => actionButton(a, label, 'focus', cls, note);
  const kids = [hd.el];
  if (isQ) {
    panel.className = 'detail p-question';
    sendBtn = h('button', { type: 'button', class: 'btn acc', text: `send ${selected}` });
    sendBtn.addEventListener('click', () => selected && answer(selected));
    kids.push(h('h3', { class: 'p-title', id: 'p-title', text: w.title }));
    if (w.detail) kids.push(h('span', { class: 'val', text: w.detail }));
    kids.push(h('div', { class: 'sec grow' }, optList), h('div', { class: 'actions' }, sendBtn, focusBtn('narrow', 'type in terminal')), note);
  } else {
    panel.className = 'detail p-accent';
    kids.push(h('div', { class: 'sec' },
      h('h3', { class: 'p-title', id: 'p-title', text: w.title }),
      w.detail ? h('pre', { class: 'codeblock', tabindex: '0', 'aria-label': 'prompt detail', text: w.detail }) : null,
      w.context ? h('span', { class: 'lbl', text: w.context }) : null));
    if (w.kind === 'input' || !w.options.length) {
      kids.push(h('div', { class: 'sec grow' }, h('span', { class: 'val', text: 'the agent wants free-text input — answer it in its terminal.' })),
        h('div', { class: 'actions' }, focusBtn('solid', 'focus terminal')), note);
    } else {
      const fb = h('button', { type: 'button', class: 'linkbtn', text: 'focus terminal' });
      fb.addEventListener('click', async () => { const r = await postAction(a, 'focus'); note.className = r.ok ? 'note' : 'note bad'; note.textContent = r.ok ? 'focus terminal · sent' : `focus failed · ${r.error}`; });
      kids.push(h('div', { class: 'sec grow' }, optList),
        h('div', { class: 'foot-row' }, h('span', { text: 'answer here or at the terminal' }), fb), note);
    }
  }
  panel.replaceChildren(...kids);

  const s = sending.get(w.id);
  if (s) { setBusy(true, s.status === 'sent' ? s.key : null); note.textContent = s.status === 'sent' ? `sent ${s.key} · waiting for the agent…` : `sending ${s.key}…`; }

  det.answer = (key) => {
    if (!opts.some((o) => o.key === key)) return;
    answer(key);
  };
  return (x) => hd.update(x);
}

function renderDetail() {
  const a = selectedId && state?.agents.find((x) => x.id === selectedId);
  if (!a) {
    if (selectedId && state) { selectedId = null; det = null; panel.hidden = true; panel.replaceChildren(); }
    return;
  }
  panel.hidden = false;
  panel.setAttribute('aria-labelledby', 'd-title');
  const key = detailKey(a);
  if (!det || det.key !== key) {
    const hadFocus = panel.contains(document.activeElement);
    det = { key, id: a.id };
    det.update = a.status === 'waiting' && a.waiting ? buildPrompt(a) : buildInfo(a);
    if (hadFocus) $('d-title')?.focus({ preventScroll: true });
  }
  det.update(a);
}

// ---------------------------------------------------------------- render
function render() {
  renderTop();
  renderGrid();
  renderDetail();
}

function applyState(s) {
  if (!s || !Array.isArray(s.agents)) return;
  const t = Date.parse(s.server?.time);
  if (Number.isFinite(t)) skew = t - Date.now();
  state = s;
  $('boot').hidden = true;
  // forget answers for prompts that are gone
  const live = new Set(s.agents.map((a) => a.waiting?.id).filter(Boolean));
  for (const id of sending.keys()) if (!live.has(id)) sending.delete(id);
  render();
}

// ---------------------------------------------------------------- keyboard
document.addEventListener('keydown', (e) => {
  const t = e.target;
  // Alt+arrow on a tile moves the agent (left/right by one, up/down by a row).
  if (e.altKey && !needsFirst && !expired && t instanceof HTMLElement && t.classList.contains('tile') && e.key.startsWith('Arrow')) {
    e.preventDefault();
    const els = [...$('grid').children];
    const top0 = els[0].offsetTop;
    let cols = els.findIndex((el) => el.offsetTop !== top0);
    if (cols < 0) cols = els.length;
    const ids = slotOrder();
    const i = ids.indexOf(t.dataset.id);
    const step = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -cols, ArrowDown: cols }[e.key] || 0;
    if (i >= 0 && step) moveTo(t.dataset.id, i + step);
    return;
  }
  if (e.metaKey || e.ctrlKey || e.altKey || expired) return;
  if (e.key === 'Escape' && selectedId) { e.preventDefault(); closeDetail(); return; }
  if (/^[1-9]$/.test(e.key) && det?.answer && !(t instanceof HTMLInputElement || t instanceof HTMLTextAreaElement)) {
    e.preventDefault(); det.answer(e.key); return;
  }
  if (t instanceof HTMLElement && t.classList.contains('tile') && e.key.startsWith('Arrow')) {
    e.preventDefault();
    const els = [...$('grid').children];
    const i = els.indexOf(t);
    const top0 = els[0].offsetTop;
    let cols = els.findIndex((el) => el.offsetTop !== top0);
    if (cols < 0) cols = els.length;
    const step = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -cols, ArrowDown: cols }[e.key] || 0;
    els[Math.max(0, Math.min(els.length - 1, i + step))]?.focus();
  }
});

function setPanelMode(on) {
  panelMode = on;
  document.body.classList.toggle('panel-mode', on);
  $('panel-toggle').setAttribute('aria-pressed', String(on));
  try { localStorage.setItem('agents.panelMode', on ? '1' : '0'); } catch { /* storage unavailable */ }
  renderGrid();
}
$('panel-toggle').addEventListener('click', () => setPanelMode(!panelMode));
try { panelMode = localStorage.getItem('agents.panelMode') === '1'; } catch { panelMode = false; }
if (new URLSearchParams(location.search).has('panel')) panelMode = true; // bookmarkable
document.body.classList.toggle('panel-mode', panelMode);
$('panel-toggle').setAttribute('aria-pressed', String(panelMode));

$('sort-toggle').addEventListener('click', (e) => {
  needsFirst = !needsFirst;
  e.currentTarget.setAttribute('aria-pressed', String(needsFirst));
  try { localStorage.setItem('agents.needsFirst', needsFirst ? '1' : '0'); } catch { /* storage unavailable */ }
  renderGrid();
});
try { needsFirst = localStorage.getItem('agents.needsFirst') === '1'; } catch { needsFirst = false; }
$('sort-toggle').setAttribute('aria-pressed', String(needsFirst));

// ---------------------------------------------------------------- network
async function api(path, body) {
  if (FIXTURE) return fixtureAction(path, body);
  try {
    const r = await fetch(path, {
      method: 'POST', credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body ?? {}),
    });
    if (r.status === 401) { showExpired(); return { ok: false, status: 401, error: 'session expired' }; }
    if (r.ok) return { ok: true, status: r.status };
    let msg = `http ${r.status}`;
    try { const j = await r.json(); if (j?.error) msg = j.error; } catch { /* not json */ }
    return { ok: false, status: r.status, error: msg };
  } catch {
    return { ok: false, status: 0, error: 'daemon unreachable' };
  }
}

function showExpired() {
  expired = true;
  es?.close();
  $('expired').hidden = false;
  setSprite($('expired-art'), 'err');
}

async function loadState() {
  const r = await fetch('/v1/state', { credentials: 'same-origin', cache: 'no-store' });
  if (r.status === 401) { showExpired(); return false; }
  if (!r.ok) throw new Error(`http ${r.status}`);
  applyState(await r.json());
  return true;
}

let es = null;
let retry = 1000;
function connect() {
  if (expired) return;
  es = new EventSource('/v1/events');
  es.addEventListener('open', () => { retry = 1000; setConn('live'); });
  es.addEventListener('state', (ev) => {
    try { applyState(JSON.parse(ev.data)); setConn('live'); } catch (err) { console.warn('bad state event', err); }
  });
  es.addEventListener('error', () => {
    if (expired) return;
    setConn('reconnecting');
    if (es.readyState === EventSource.CLOSED) {
      // Non-200 response (e.g. 401) — the browser will not retry by itself. Probe, then back off.
      es.close();
      setTimeout(async () => {
        try { if (!(await loadState())) return; } catch { /* still down */ }
        retry = Math.min(retry * 2, 10000);
        connect();
      }, retry);
    }
  });
}

async function start() {
  setSprite($('empty-art'), 'idle');
  tickClock();
  setInterval(tickClock, 1000);
  setInterval(() => state && render(), 15000); // refresh relative times
  // End "just finished" hops on time (cheap: only re-renders when one is due).
  setInterval(() => {
    if (state?.agents.some((a) => a.status === 'idle' && a.statusSince && now() - Date.parse(a.statusSince) < CELEBRATE_MS + 1500)) renderGrid();
  }, 1000);
  if (FIXTURE) return startFixture();
  setConn('connecting');
  try { if (!(await loadState())) return; } catch { setConn('reconnecting'); }
  connect();
}

// ---------------------------------------------------------------- fixture mode (dev only)
// ?fixture loads ./fixture.json and animates it locally. Extra dev flags:
//   &open=<id>  open that agent's panel   &still  no simulation
//   &empty      no agents                 &offline  disconnected banner   &expired  401 screen
let fx = null;
const AGO = [38, 2, 12, 4, 3, 63, 5, 60, 9, 190, 7]; // minutes since status change, by slot
const ACTS = [
  ['Edit', 'src/auth/session.ts'], ['Bash', 'go test ./auth/...'], ['Read', 'internal/state/store.go'],
  ['Grep', 'refreshToken'], ['TodoWrite', 'planning 6 steps'], ['Write', 'docs/install.md'], ['WebFetch', 'pkg.go.dev/net/http'],
];
const PERM = { kind: 'permission', title: 'run this command?', context: 'bash · ~/dev/scratch',
  detail: 'rm -rf ./build && make release VERSION=0.2.0 \\\n  SIGN_KEY=~/.config/keys/release.pem',
  options: [{ key: '1', label: 'yes', primary: true }, { key: '2', label: "yes, and don't ask again for make" }, { key: '3', label: 'no' }] };

const iso = (ms) => new Date(ms).toISOString();
function fxEmit() {
  fx.server.time = iso(Date.now());
  applyState(structuredClone(fx));
}

async function startFixture() {
  setConn('connecting');
  let data;
  try {
    const r = await fetch('./fixture.json', { cache: 'no-store' });
    data = await r.json();
  } catch {
    setConn('reconnecting');
    setText($('boot'), 'fixture mode: could not load fixture.json — serve this folder over http (python3 -m http.server)');
    return;
  }
  const t0 = Date.now();
  for (const a of data.agents) {
    const ago = (AGO[a.slot] ?? 5) * 60000;
    a.statusSince = iso(t0 - ago);
    a.startedAt = iso(t0 - ago - 40 * 60000);
    a.updatedAt = iso(t0 - 5000);
  }
  if (params.has('empty')) data.agents = [];
  fx = data;
  fxEmit();
  setConn('live');
  if (params.has('offline')) setConn('reconnecting');
  if (params.has('expired')) showExpired();
  if (params.get('open')) openDetail(params.get('open'));
  if (!params.has('still')) setInterval(fxTick, 3500);
}

let fxN = 0;
function fxTick() {
  fxN++;
  const t = Date.now();
  const by = (st) => fx.agents.filter((a) => a.status === st);
  const set = (a, status, extra = {}) => Object.assign(a, { status, statusSince: iso(t), updatedAt: iso(t), activity: null, waiting: null, error: null }, extra);
  // running agents make progress every tick
  for (const a of by('running')) {
    const [tool, detail] = ACTS[(fxN + a.slot) % ACTS.length];
    a.activity = { tool, detail };
    a.context.used = Math.min(a.context.window, a.context.used + Math.round(a.context.window * 0.004));
    a.tokens.output += 600; a.tokens.cacheRead += 9000; a.costUsd = +(a.costUsd + 0.02).toFixed(2);
  }
  const run = by('running'), idle = by('idle'), err = by('error');
  if (fxN % 6 === 0 && run.length > 2 && by('waiting').length < 4) {
    set(run[fxN % run.length], 'waiting', { waiting: { ...structuredClone(PERM), id: `p${100 + fxN}` } });
  } else if (fxN % 5 === 0 && idle.length) {
    const a = idle[fxN % idle.length]; set(a, 'running', { activity: { tool: 'Read', detail: 'README.md' }, turns: a.turns + 1 });
  } else if (fxN % 7 === 0 && run.length) {
    set(run[(fxN * 3) % run.length], 'idle');
  } else if (fxN % 9 === 0 && err.length) {
    set(err[0], 'running', { activity: { tool: 'Bash', detail: 'terraform plan' } });
  } else if (fxN % 11 === 0 && run.length) {
    set(run[0], 'error', { error: { message: 'api 529 overloaded · retrying' } });
  }
  fxEmit();
}

async function fixtureAction(path, body) {
  await new Promise((r) => setTimeout(r, 250));
  if (path === '/v1/order') {
    body.ids.forEach((id, i) => { const a = fx.agents.find((x) => x.id === id); if (a) a.slot = i; });
    fxEmit();
    return { ok: true, status: 204 };
  }
  const [, , , id, action] = path.split('/');
  const a = fx?.agents.find((x) => x.id === decodeURIComponent(id));
  if (!a) return { ok: false, status: 404, error: 'no such agent' };
  const t = iso(Date.now());
  if (action === 'focus') {
    for (const x of fx.agents) { x.focused = x === a; x.attached = x.attached || x === a; }
  } else if (action === 'resume') {
    if (a.status !== 'exited') return { ok: false, status: 500, error: 'agent is still running' };
    Object.assign(a, { status: 'idle', statusSince: t, resumable: false });
  } else if (action === 'dismiss') {
    fx.agents.splice(fx.agents.indexOf(a), 1);
  } else if (action === 'interrupt') {
    if (a.status !== 'running') return { ok: false, status: 409, error: 'not running' };
    Object.assign(a, { status: 'idle', statusSince: t, activity: null });
  } else if (action === 'answer') {
    if (a.waiting?.id !== body?.promptId) return { ok: false, status: 409, error: 'prompt changed' };
    if (!a.waiting.options.some((o) => o.key === body.key)) return { ok: false, status: 400, error: 'bad key' };
    const rejected = a.waiting.kind === 'permission' && body.key === String(a.waiting.options.length);
    setTimeout(() => {
      Object.assign(a, { status: rejected ? 'idle' : 'running', statusSince: iso(Date.now()), waiting: null,
        activity: rejected ? null : { tool: 'Bash', detail: 'running approved command' } });
      fxEmit();
    }, 900);
    return { ok: true, status: 204 };
  }
  fxEmit();
  return { ok: true, status: 204 };
}

start();
