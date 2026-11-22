// agents deck Settings. Every control calls the service's management API
// (/admin/v1, docs/protocol.md): the same calls the CLI makes. Plain DOM, no
// innerHTML: names and messages from the service are always text.

import {
  setSkew, now, fmtN, fmtCost, rel, ageMin, lastActive, tilde, ctxPct, verb, TOOL_POSE, poseOf, codexThread, watched, inApp, hungry,
  subsOnly, tileMeta, pillText, HAT_ART, HAT_COLORS, HAT_SHAPES, HAT_COLOR_NAMES, BOT_BODY, spriteSVG,
} from './deck-core.js';

const PAGES = ['home', 'agents', 'boards', 'network', 'general', 'updates', 'troubleshoot', 'about'];
const SCOPES = [
  { value: 'view', text: 'Look only', icon: 'eye', title: 'Sees the deck, nothing more' },
  { value: 'act', text: 'Can answer', icon: 'reply', title: 'Sees the deck and answers agents' },
  { value: 'admin', text: 'Can manage', icon: 'key', title: 'Also starts and stops agents' },
];
const scopeWords = (s) => (SCOPES.find((x) => x.value === s) || { text: s }).text;
const scopeLower = (s) => scopeWords(s).toLowerCase();

// In the agents deck app the page sits in a native window: the shell
// injects window.agentsDeckApp at document start (and marks <html> with
// .in-app and .app-<platform>), so the page has no top bar of its own and
// "Back to the deck" shows the deck. Its user agent carries
// "AgentsDeckApp/<platform>" too, for older injections.
const APP = (() => {
  const a = window.agentsDeckApp;
  if (a && typeof a.platform === 'string') return a.platform;
  const m = /AgentsDeckApp\/(\w+)/.exec(navigator.userAgent);
  return m ? m[1] : '';
})();
if (APP) document.documentElement.classList.add('in-app', `app-${APP}`);
// The macOS app puts the status strip in its title bar row: it drags the
// window only over the sidebar, so tell it how wide the sidebar is.
if (window.agentsDeckApp && window.agentsDeckApp.titlebarRow) {
  document.documentElement.classList.add('app-row');
  const side = () => document.querySelector('.side');
  const tell = () => {
    const el = side();
    // The panel view covers the window: then only the traffic lights' corner drags.
    const left = document.querySelector('.panelview') ? 84 : el ? Math.round(el.getBoundingClientRect().width) : -1;
    if (left < 0 || left === told) return;
    told = left;
    try { window.agentsDeckApp.post({ action: 'titlebar', left }); } catch (e) { /* no app */ }
  };
  let told = -1;
  const watch = () => {
    const el = side();
    if (el && 'ResizeObserver' in window) new ResizeObserver(tell).observe(el);
    new MutationObserver(tell).observe(document.body, { childList: true, subtree: true });
    tell();
  };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', watch); else watch();
}

// appPost asks the app for something only it can do; false outside the app.
function appPost(action) {
  try {
    if (window.agentsDeckApp && typeof window.agentsDeckApp.post === 'function') {
      window.agentsDeckApp.post({ action });
      return true;
    }
  } catch (e) { /* no app */ }
  return false;
}

const S = {
  page: '', arg: '',
  system: null, settings: null, devices: null, ports: null, update: null, agents: null, live: null, diag: null, state: null,
  jobs: {},
  open: { wifi: '', rename: '' },
  nb: null, // the new-board flow
  confirmLoginOff: false,
  pairing: null,
  showReport: false,
  laterUpdate: '',
  rates: {}, // job id -> {t, done} for time left
  errors: {},
  bt: null, btBoards: null, // Bluetooth on this computer, and the boards nearby
  btAdd: {}, // Bluetooth identifier -> the job adding that board
  file: {}, // fileTarget key -> installing a firmware file: {stage, name, size, up, error, version}
  btScope: {}, // Bluetooth identifier -> the scope picked before Add
  btFind: {}, // device id -> looking for that board over Bluetooth, to add it from its page
  mcp: null, // MCP tokens and where clients connect
  mcpNew: null, // the new-token form, then the token made (shown once)
};

// ---- small helpers -----------------------------------------------------------------

const $ = (sel, el = document) => el.querySelector(sel);

function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k === 'text') el.textContent = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (v === true) el.setAttribute(k, '');
    else el.setAttribute(k, String(v));
  }
  for (const c of kids.flat()) {
    if (c === undefined || c === null || c === false) continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}

// put: el's children become kids; null, false and undefined are left out
// (replaceChildren would write them in as text: "null").
const put = (el, ...kids) => el.replaceChildren(...kids.flat().filter((k) => k !== null && k !== undefined && k !== false));

const NS = 'http://www.w3.org/2000/svg';
function sv(tag, attrs, ...kids) {
  const el = document.createElementNS(NS, tag);
  for (const [k, v] of Object.entries(attrs || {})) if (v !== undefined && v !== null && v !== false) el.setAttribute(k, String(v));
  for (const c of kids.flat()) if (c) el.append(c);
  return el;
}

class ApiError extends Error {
  constructor(status, msg, data) { super(msg); this.status = status; this.data = data || {}; }
}

let connOK = true;
function setConn(ok, why) {
  const changed = connOK !== ok;
  connOK = ok;
  const b = $('#banner');
  if (ok) { b.hidden = true; } else { b.hidden = false; b.textContent = why || "The service isn't answering. It may be restarting: this page tries again by itself."; }
  const f = $('#foot');
  f.textContent = ok ? (S.system ? `${S.system.version} · service running` : 'service running') : 'service not answering';
  f.classList.toggle('bad', !ok);
  if (changed) drawTop();
}

async function api(method, path, body) {
  let r;
  try {
    r = await fetch(path, {
      method, credentials: 'same-origin', cache: 'no-store',
      headers: body !== undefined ? { 'Content-Type': 'application/json' } : {},
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch (e) {
    setConn(false);
    throw new ApiError(0, "The service isn't answering. It may be restarting.");
  }
  if (r.status === 401) { showExpired(); throw new ApiError(401, 'Your session ended.'); }
  setConn(true);
  const text = await r.text();
  let data = null;
  if (text) { try { data = JSON.parse(text); } catch (e) { data = null; } }
  if (!r.ok) {
    let msg = (data && data.error) || r.statusText || 'Something went wrong';
    if (r.status === 404 && path.startsWith('/admin/') && !(data && data.error)) msg = 'This service is too old for Settings: update agents deck.';
    throw new ApiError(r.status, msg, data);
  }
  return data;
}

function showExpired() {
  if (APP) $('#expired p').textContent = 'Close this window and open Settings again from the agents deck icon.';
  $('#expired').hidden = false;
}

let toastTimer = 0;
function toast(msg, bad) {
  const t = $('#toast');
  t.textContent = msg;
  t.classList.toggle('bad', !!bad);
  t.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { t.hidden = true; }, bad ? 7000 : 4000);
}

function ago(iso) {
  if (!iso) return '';
  const t = new Date(iso).getTime();
  if (!t) return '';
  const s = Math.max(0, (Date.now() - t) / 1000);
  if (s < 60) return 'just now';
  if (s < 3600) return `${Math.round(s / 60)} min ago`;
  if (s < 2 * 3600) return 'an hour ago';
  if (s < 24 * 3600) return `${Math.round(s / 3600)} h ago`;
  if (s < 48 * 3600) return 'yesterday';
  if (s < 14 * 86400) return `${Math.round(s / 86400)} days ago`;
  return `${Math.round(s / (7 * 86400))} weeks ago`;
}

const plural = (n, one, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;
const WORDS = ['No', 'One', 'Two', 'Three', 'Four', 'Five', 'Six', 'Seven', 'Eight', 'Nine', 'Ten'];
const say = (n) => WORDS[n] || String(n);
const cap = (s) => (s ? s[0].toUpperCase() + s.slice(1) : s);

// mb: megabytes with one decimal; anything started shows as at least 0.1.
const mb = (n) => (n > 0 && n < 104858 ? '0.1' : (n / 1048576).toFixed(1));

// busy runs an action with its button marked busy; errors become a toast.
async function busy(btn, fn, okMsg) {
  if (btn) { btn.disabled = true; btn.setAttribute('aria-busy', 'true'); }
  try {
    const r = await fn();
    if (okMsg) toast(typeof okMsg === 'function' ? okMsg(r) : okMsg);
    return r;
  } catch (e) {
    if (e.status !== 401) toast(e.message, true);
    return undefined;
  } finally {
    if (btn && btn.isConnected) { btn.disabled = false; btn.removeAttribute('aria-busy'); }
  }
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch (e) {
    const ta = h('textarea', { 'aria-hidden': 'true' });
    ta.value = text;
    ta.className = 'sr';
    document.body.append(ta);
    ta.select();
    let ok = false;
    try { ok = document.execCommand('copy'); } catch (x) { ok = false; }
    ta.remove();
    return ok;
  }
}
const copied = async (text, what = 'Copied') => toast(await copyText(text) ? what : 'Couldn\'t copy: select it by hand', false);

// ---- icons (the kit's line icons) ---------------------------------------------------------

const ICONS = {
  usb: [['path', { d: 'M12 3v13M9 6l3-3 3 3M7 10v2.5l5 3 5-3V10' }], ['circle', { cx: 12, cy: 18.5, r: 2 }]],
  wifi: [['path', { d: 'M2.5 9a14 14 0 0 1 19 0M5.5 12.5a9.5 9.5 0 0 1 13 0M8.8 16a4.8 4.8 0 0 1 6.4 0' }], ['circle', { cx: 12, cy: 19.3, r: 0.9 }]],
  bluetooth: [['path', { d: 'M7 7.5l10 9-5 4.5V3l5 4.5-10 9' }]],
  offline: [['path', { d: 'M4 4l16 16' }], ['path', { d: 'M8.8 16a4.8 4.8 0 0 1 6.4 0M5.5 12.5a9.5 9.5 0 0 1 4-2.3' }]],
  plus: [['path', { d: 'M12 5v14M5 12h14' }]],
  x: [['path', { d: 'M6.5 6.5l11 11M17.5 6.5l-11 11' }]],
  check: [['path', { d: 'M5 12.5l4.5 4.5L19 7.5' }]],
  copy: [['rect', { x: 8, y: 8, width: 12, height: 12, rx: 1 }], ['path', { d: 'M16 8V5a1 1 0 0 0-1-1H5a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h3' }]],
  key: [['circle', { cx: 8, cy: 15, r: 4 }], ['path', { d: 'M11 12l8.5-8.5M16 7l3 3' }]],
  magic: [['path', { d: 'M4 20L15 9M14 4v3M19 9h-3M17.5 5.5l-2 2M9 4.5v1.5M20 14h-1.5' }]],
  lock: [['rect', { x: 5, y: 10.5, width: 14, height: 10, rx: 1 }], ['path', { d: 'M8 10.5V7.5a4 4 0 0 1 8 0v3' }]],
  phone: [['rect', { x: 7, y: 2.5, width: 10, height: 19, rx: 2 }], ['path', { d: 'M11 18.5h2' }]],
  panel: [['rect', { x: 3.5, y: 4.5, width: 17, height: 12, rx: 1.5 }], ['path', { d: 'M9 20h6M12 16.5V20' }]],
  board: [['rect', { x: 6, y: 6, width: 12, height: 12, rx: 2 }], ['path', { d: 'M9.5 2.5V6M14.5 2.5V6M9.5 18v3.5M14.5 18v3.5M2.5 9.5H6M2.5 14.5H6M18 9.5h3.5M18 14.5h3.5' }]],
  back: [['path', { d: 'M14.5 5.5L8 12l6.5 6.5' }]],
  restart: [['path', { d: 'M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7' }]],
  out: [['path', { d: 'M7 17L17 7M9 7h8v8' }]],
  eye: [['path', { d: 'M2.5 12s3.5-6.5 9.5-6.5S21.5 12 21.5 12s-3.5 6.5-9.5 6.5S2.5 12 2.5 12z' }], ['circle', { cx: 12, cy: 12, r: 2.6 }]],
  reply: [['path', { d: 'M10 7L4.5 12l5.5 5M5 12h9a5.5 5.5 0 0 1 5.5 5.5V19' }]],
  auto: [['path', { d: 'M4 12a8 8 0 0 1 13.7-5.6M20 12a8 8 0 0 1-13.7 5.6M17.5 3v3.6h-3.6M6.5 21v-3.6h3.6' }]],
  power: [['path', { d: 'M12 3v8M6.3 6.3a8 8 0 1 0 11.4 0' }]],
};
function icon(name, size = 14, width = 2) {
  return sv('svg', { width: size, height: size, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': width, 'stroke-linecap': 'round', 'stroke-linejoin': 'round', 'aria-hidden': 'true' },
    (ICONS[name] || []).map(([t, a]) => sv(t, a)));
}

// ---- the critter (from the deck) ----------------------------------------------------------


function hatCells(f, fill, cls) {
  const g = sv('g', { class: cls });
  f.rows.forEach((row, r) => [...row].forEach((ch, c) => {
    if (fill[ch]) g.append(sv('rect', { x: f.x + c, y: f.top + r, width: 1, height: 1.04, fill: fill[ch] }));
  }));
  for (const [ex, ey, ch] of f.extras || []) g.append(sv('rect', { x: ex, y: ey, width: 1, height: 1, fill: fill[ch] }));
  return g;
}

// critter draws one: pose hi (eyes open), wait (a question mark beside it)
// or idle (asleep); spin turns a propeller hat.
function critter({ pose = 'hi', bot = false, hat = null, spin = false, body = '#FF4D00', cut = '#141412', centre = false } = {}) {
  const r = (x, y, w, hh, f) => sv('rect', { x, y, width: w, height: hh, fill: f });
  const g = sv('svg', { viewBox: '0 -4 18 13', 'shape-rendering': 'crispEdges', 'aria-hidden': 'true', focusable: 'false' });
  const fy = bot ? 0 : -0.5;
  if (bot) g.append(sv('path', { d: BOT_BODY, fill: body }), r(4, 5.4, 6, 1.6, cut), r(1, 1, 1, 2, body), r(12, 1, 1, 2, body));
  else g.append(r(1, 0, 12, 6, body), r(0, 2, 1, 2, body), r(13, 2, 1, 2, body));
  if (pose === 'idle') g.append(r(3.5, 3.2 + fy, 2, 0.55, cut), r(8.5, 3.2 + fy, 2, 0.55, cut));
  else { const y = (pose === 'wait' ? 1 : 2) + fy; g.append(r(4, y, 1, 2, cut), r(9, y, 1, 2, cut)); }
  if (bot) g.append(r(3.5, 7, 2, 1.4, body), r(8.5, 7, 2, 1.4, body));
  else for (const x of [2, 4, 9, 11]) g.append(r(x, 6, 1, 1.6, body));
  const pix = (rows, x0, w, hh, y0) => rows.forEach((row, ri) => [...row].forEach((ch, ci) => { if (ch === '#') g.append(r(x0 + ci * w, y0 + ri * hh, w, hh, body)); }));
  if (pose === 'wait') pix(['###', '..#', '.#.', '...', '.#.'], 14.5, 1.1, 0.9, -4);
  if (pose === 'idle') { pix(['###', '..#', '.#.', '#..', '###'], 14.6, 0.55, 0.55, -2.4); pix(['###', '.#.', '###'], 16.8, 0.45, 0.45, -3.8); }
  const art = hat && HAT_ART[hat.shape];
  if (art) {
    const [a, b] = HAT_COLORS[hat.color] || HAT_COLORS.ink;
    const fill = { a, b, c: '#F7F4EC', d: '#0A0A0A' };
    const frames = bot ? art.onigiri : art.critter;
    if (spin && frames.length === 4) frames.forEach((f, i) => g.append(hatCells(f, fill, `hp hp${i}${i ? ' hpo' : ''}`)));
    else g.append(hatCells(bot ? art.onigiriStill : art.critterStill, fill, ''));
  }
  return h('span', { class: `spr${centre ? ' c' : ''}${spin ? ' spin' : ''}`, 'aria-hidden': 'true' }, g);
}

// ---- building blocks -----------------------------------------------------------------------

const btn = (text, onclick, cls = '', attrs = {}) => h('button', { type: 'button', class: `btn sm ${cls}`.trim(), onclick, ...attrs }, text);
const link = (text, href, cls = '', attrs = {}) => h('a', { class: `btn sm ${cls}`.trim(), href, ...attrs }, text);
// chip: a status in a square; ic is an icon's name, or 'dot' for a square dot.
const chip = (text, cls = '', ic = null, attrs = {}) => h('span', { class: `chip ${cls}`.trim(), ...attrs },
  ic === 'dot' ? h('i', { 'aria-hidden': 'true' }) : ic ? icon(ic, 12) : null, text);

function toggle(label, on, onChange, opts = {}) {
  return h('button', {
    class: 'sw', type: 'button', role: 'switch', 'aria-checked': on ? 'true' : 'false', 'aria-label': label,
    disabled: opts.disabled, 'data-k': opts.key || label,
    onclick: (e) => onChange(!on, e.currentTarget),
  }, h('span', {}));
}

// seg: a group of choices, one picked (a radio group: arrow keys move).
function seg(label, options, current, onChange, opts = {}) {
  const group = h('div', { class: 'seg', role: 'radiogroup', 'aria-label': label });
  options.forEach((o) => {
    const on = o.value === current;
    group.append(h('button', {
      type: 'button', role: 'radio', 'aria-checked': on ? 'true' : 'false', tabindex: on ? '0' : '-1',
      disabled: opts.disabled || o.disabled, title: o.title, 'data-k': `${opts.key || label}:${o.value}`,
      onclick: () => { if (!on) onChange(o.value); },
      onkeydown: (e) => radioKeys(e, group, onChange, options),
    }, o.icon ? icon(o.icon, 13) : null, o.iconOnly ? h('span', { class: 'sr' }, o.text) : o.text));
  });
  if (!options.some((o) => o.value === current) && group.firstChild) group.firstChild.tabIndex = 0;
  return group;
}

function radioKeys(e, group, onChange, options) {
  const keys = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 };
  if (!(e.key in keys)) return;
  e.preventDefault();
  const items = [...group.querySelectorAll('[role="radio"]')];
  const i = items.indexOf(e.currentTarget);
  const n = (i + keys[e.key] + items.length) % items.length;
  items[n].focus();
  if (options) onChange(options[n].value);
  else items[n].click();
}

// head: the page's eyebrow and headline (the headline may end in an
// orange italic word).
function head(eyebrow, title, em) {
  return h('header', { class: 'hd' }, h('span', { class: 'eyebrow acc' }, eyebrow),
    h('h1', {}, title, em ? ' ' : null, em ? h('em', {}, em) : null));
}

// sec: a section with its rule (name, meta on the right, a link) and content.
function sec(title, meta, more, ...content) {
  return h('section', { class: 'sec', 'data-sec': title.toLowerCase() },
    h('div', { class: 'rule' }, h('h2', {}, title), meta ? h('span', { class: 'm' }, meta) : null,
      more ? h('a', { href: more[1] }, `${more[0]} →`) : null),
    ...content);
}

const blk = (...rows) => h('div', { class: 'blk' }, ...rows);

// row: a line in a block: a picture, a title with a line under it, and
// controls on the right. opts.name marks the title as a name (a board's, a
// token's): mono, one line, cut with … and whole in its tooltip.
function row(title, meta, right, opts = {}) {
  const t = opts.name
    ? h('span', { class: `nm${opts.off ? ' off' : ''}`, title: typeof title === 'string' ? title : null }, title)
    : h('span', { class: `ttl${opts.off ? ' txt dim' : ''}` }, title);
  const m = meta ? h('span', { class: opts.name ? 'sub' : 'txt dim' }, meta) : null;
  return h('div', { class: `row${opts.cls ? ` ${opts.cls}` : ''}`, 'data-row': opts.id },
    opts.pic,
    h('div', { class: 'grow' }, t, m, opts.below),
    right && [right].flat().filter(Boolean).length ? h('div', { class: 'r' }, right) : null,
    opts.extra ? [opts.extra].flat().filter(Boolean).map((e) => (e.classList ? (e.classList.add('full'), e) : e)) : null);
}

// labelled: a setting's line in a board's page: a label on the left.
function labelled(label, content, right, opts = {}) {
  return h('div', { class: 'row top', 'data-row': opts.id },
    h('span', { class: 'lbl' }, label),
    h('div', { class: 'grow' }, content),
    right && [right].flat().filter(Boolean).length ? h('div', { class: 'r' }, right) : null,
    opts.extra ? [opts.extra].flat().filter(Boolean).map((e) => (e.classList ? (e.classList.add('full'), e) : e)) : null);
}

const note = (text, cls = '') => h('p', { class: `note ${cls}`.trim() }, text);
const ico = (name, cls = '') => h('span', { class: `ico ${cls}`.trim() }, icon(name, 16, 1.8));

function errorBox(msg, retry) {
  return h('div', { class: 'blk pad err', role: 'alert' },
    h('p', { class: 'txt' }, msg),
    retry ? h('div', { class: 'actions' }, btn('Try again', retry)) : null);
}

// render swaps a page body, keeping focus on the same control when it is
// still there, and leaves it alone while you type in it.
function render(id, build, force) {
  const body = document.getElementById(id);
  const active = document.activeElement;
  if (active && body.contains(active) && (active.tagName === 'INPUT' || active.tagName === 'TEXTAREA' || active.tagName === 'SELECT') && !force) return;
  const key = active && body.contains(active) ? active.getAttribute('data-k') : null;
  const frag = document.createDocumentFragment();
  const out = build();
  frag.append(...[out].flat().filter(Boolean));
  body.replaceChildren(frag);
  body.removeAttribute('aria-busy');
  if (key) {
    const again = body.querySelector(`[data-k="${CSS.escape(key)}"]`);
    if (again) again.focus();
  }
}

// putJob keeps the newest news of a job: the event stream can tell that a
// job ended before the answer that started it arrives.
function putJob(j) {
  const old = S.jobs[j.id];
  if (old && old.state !== 'running' && j.state === 'running') return;
  S.jobs[j.id] = j;
}

// ---- boards: their real screens -------------------------------------------------------------

// The boards the page has a real screen picture for (static/boards/<board>.png).
function boardOf(s) {
  s = (s || '').toLowerCase();
  if (/t4s3|t4-s3/.test(s)) return 'lilygo-t4-s3';
  if (/1\.47|lcd-147|strip/.test(s)) return 'waveshare-lcd-147';
  if (/amoled|1\.8/.test(s)) return 'waveshare-touch-amoled-18';
  if (/2\.1|lcd-21|round-21/.test(s)) return 'waveshare-touch-lcd-21';
  if (/2\.8|lcd-28c|round/.test(s)) return 'waveshare-touch-lcd-28c';
  if (/lcd-4\b|4[″"]|square/.test(s)) return 'waveshare-touch-lcd-4';
  return '';
}
const devBoard = (d) => (d.kind === 'network' ? '' : boardOf(`${d.board || ''} ${d.type || ''} ${d.model || ''}`));

// screen: the board's own screen in its frame; size xs, (64), mid, lg or xl.
function screen(key, size = '', off = false, kind = '') {
  const round = /lcd-21|lcd-28c/.test(key), strip = /lcd-147/.test(key);
  const cls = ['scr', size, round ? 'round' : '', strip ? 'strip' : '', off ? 'off' : ''].filter(Boolean).join(' ');
  if (!key) return h('span', { class: `${cls} none`, 'aria-hidden': 'true' }, icon(kind === 'network' ? 'phone' : 'board', size === 'xs' ? 16 : 26, 1.8));
  return h('span', { class: cls, 'aria-hidden': 'true' }, h('img', { src: `./boards/${key}.png`, alt: '', decoding: 'async' }));
}
const devScreen = (d, size) => screen(devBoard(d), size, !d.online, d.kind);
// thumb: a small screen in a fixed width, so names line up in a list.
const thumb = (el) => h('span', { class: 'thumb' }, el);

const VIA = { usb: 'USB', wifi: 'Wi-Fi', bluetooth: 'Bluetooth', network: 'Network' };
const VIA_ICON = { usb: 'usb', wifi: 'wifi', bluetooth: 'bluetooth', network: 'wifi' };

// connChip: how a device is connected, one fixed size.
function connChip(d) {
  if (d.state === 'revoked') return chip('Cut off', 'off st');
  if (d.state === 'waiting') return chip('Waiting', 'fill st');
  if (d.job) {
    const j = S.jobs[d.job];
    return chip(j ? `Updating ${j.percent}%` : 'Updating', 'acc st');
  }
  if (!d.online) return chip('Offline', 'off st', 'offline');
  return chip(VIA[d.connection] || 'Connected', 'ok st', VIA_ICON[d.connection]);
}

const hasUpdate = (d) => d.status && (d.status.state === 'update' || d.status.state === 'local') && d.status.ready;

// ---- data ------------------------------------------------------------------------------

async function load(what) {
  const paths = {
    system: '/admin/v1/system', settings: '/admin/v1/settings', devices: '/admin/v1/devices',
    ports: '/admin/v1/ports', update: '/admin/v1/update', agents: '/admin/v1/agents', diag: '/admin/v1/diagnostics',
    live: '/v1/live', state: '/v1/state', projects: '/admin/v1/projects', accounts: '/v1/accounts', mcp: '/admin/v1/mcp', bt: '/admin/v1/bluetooth', btBoards: '/admin/v1/bluetooth/boards', jobList: '/admin/v1/jobs',
  };
  try {
    const r = await api('GET', paths[what]);
    S[what] = what === 'accounts' ? (r?.accounts || []) : r;
    if (what === 'state' && r?.server?.time) setSkew(Date.parse(r.server.time) - Date.now());
    delete S.errors[what];
  } catch (e) {
    S.errors[what] = e;
  }
  return S[what];
}

const loadAll = (...names) => Promise.all(names.map(load));

function needs(page) {
  return {
    home: ['update', 'devices', 'ports', 'state'], agent: ['state'], start: ['projects', 'agents', 'state', 'live'], general: ['settings'], agents: ['agents', 'live', 'settings'],
    boards: ['devices', 'ports', 'settings'], board: ['devices', 'ports', 'settings'], network: ['settings', 'devices'],
    updates: ['update', 'devices'], troubleshoot: ['diag', 'system'], about: ['system', 'update', 'state'],
    newboard: ['ports', 'devices'], flash: ['devices', 'ports'],
  }[page] || [];
}

// extras: what a page shows when it can, and does without (an older service
// has no Bluetooth or MCP calls).
function extras(page) {
  return { updates: ['jobList'], home: ['projects', 'live'], agent: ['accounts'], boards: ['bt', 'btBoards', 'jobList'], board: ['bt', 'btBoards', 'jobList'], network: ['bt'], agents: ['mcp', 'projects', 'state'] }[page] || [];
}

function firstError(page) {
  for (const n of needs(page)) if (S.errors[n]) return S.errors[n];
  return null;
}

// ---- the chrome: counts in the sidebar and the bar on top ------------------------------------

const agentsList = () => S.state?.agents || [];
const needsYou = (a) => a.status === 'waiting' || a.status === 'error';
const boardsWaiting = (sys) => (sys?.boards?.pending || 0) + (sys?.ports?.new || 0);
const appUpdate = (u) => !!u && ['ready', 'available'].includes(u.state);
const updatesReady = (sys) => (appUpdate(sys?.update) ? 1 : 0) + (sys?.firmware_updates || 0);

function badge(id, n, label) {
  const el = $(id);
  el.hidden = !n;
  el.textContent = String(n);
  el.setAttribute('aria-label', label);
}

function showSystem(sys) {
  if (!sys) return;
  S.system = sys;
  setConn(connOK);
  const need = agentsList().filter(needsYou).length;
  badge('#n-home', need, `${plural(need, 'agent')} need${need === 1 ? 's' : ''} you`);
  const w = boardsWaiting(sys);
  badge('#n-boards', w, `${plural(w, 'board')} waiting`);
  const u = updatesReady(sys);
  badge('#n-updates', u, plural(u, 'update'));
  drawTop();
}

// drawTop: the bar over every page but Home: who needs you, updates, boards
// waiting, and the service.
function drawTop() {
  const bar = $('#topbar');
  const show = !!S.page && !['home', 'agent', 'start'].includes(S.page);
  bar.hidden = !show;
  if (!show) return;
  const sys = S.system || {};
  const need = agentsList().filter(needsYou).length;
  const cell = (n, words, href, hot) => h('a', { class: hot ? 'hotc' : '', href }, h('b', {}, String(n)), h('span', {}, words));
  bar.replaceChildren(
    cell(need, 'need you', '#home', need > 0),
    cell(updatesReady(sys), updatesReady(sys) === 1 ? 'update' : 'updates', '#updates'),
    cell(boardsWaiting(sys), 'boards waiting', '#boards'),
    h('a', { class: 'st', href: '#troubleshoot', title: 'Troubleshoot' },
      h('span', {}, h('span', { class: 'p' }, '$'), ' agentctl status'),
      connOK ? h('span', { class: 'okc' }, 'running') : h('span', { class: 'badc' }, 'not answering'),
      h('span', {}, sys.version || '')));
}

// The sidebar folds to an icon rail; the choice is remembered here only.
function setFolded(on) {
  document.documentElement.classList.toggle('folded', on);
  const f = $('#fold');
  f.setAttribute('aria-expanded', on ? 'false' : 'true');
  f.setAttribute('aria-label', on ? 'Expand the sidebar' : 'Collapse the sidebar');
  try { localStorage.setItem('settings.sidebar', on ? 'folded' : 'open'); } catch (e) { /* private window */ }
}

// ---- Home: the deck ------------------------------------------------------------------------
// Home is the deck: the agents as tiles in pages of 16 (the wall panel's
// 4 × 4), what needs you and what is ready for you. A tile opens the agent;
// a free spot starts one. The tiles follow the service's live state
// (/v1/events), drawn with the deck's own critters (deck-core.js).

const SPOTS = 16;
const TOOL_COLORS = { claude: '#C98B6B', codex: '#4FB3A6' };
const acctColor = (acc) => (HAT_COLORS[acc?.color] || HAT_COLORS.ink)[0];
// The colour in a tile's corner: its Claude account's, else its tool's.
const chipColor = (a) => (a.account ? acctColor(a.account) : TOOL_COLORS[a.tool] || '#E8E4DC');
const TOOL_NAMES = { claude: 'Claude Code', codex: 'Codex', shell: 'Shell', gemini: 'Gemini CLI', opencode: 'opencode', sim: 'Simulated' };

// spriteNode: the deck's critter as an SVG element (parsed from the
// deck's own drawing; the drawing holds no names or text from agents).
const spriteCache = new Map();
function spriteNode(pose, tool = 'claude', hat = null, opts = {}) {
  const key = `${pose}|${tool}|${hat ? `${hat.shape}/${hat.color}` : ''}|${opts.bubble === false ? 'nb' : ''}`;
  let node = spriteCache.get(key);
  if (!node) {
    const doc = new DOMParser().parseFromString(spriteSVG(pose, tool, hat, opts).replace('<svg ', `<svg xmlns="${NS}" `), 'image/svg+xml');
    node = document.importNode(doc.documentElement, true);
    spriteCache.set(key, node);
  }
  return node.cloneNode(true);
}
// setSprite fills el with a critter, redrawing only when its pose changes.
function setSprite(el, pose, tool, hat, opts = {}) {
  const key = `${pose}|${tool}|${hat ? `${hat.shape}/${hat.color}` : ''}|${opts.bubble === false ? 'nb' : ''}`;
  if (el.dataset.pose === key) return;
  el.dataset.pose = key;
  el.replaceChildren(spriteNode(pose, tool, hat, opts));
}

const bySlot = () => [...agentsList()].sort((x, y) => (x.slot ?? 999) - (y.slot ?? 999));
const pageCount = () => {
  const list = agentsList();
  const last = list.length ? Math.max(...list.map((a) => a.slot ?? 0)) : 0;
  return Math.floor(last / SPOTS) + 1;
};
const agentName = (a) => a.title || a.folder || 'agent';

// The question an agent asks, short: "Allow bash?", "Pick 1 of 3".
function askOf(a) {
  if (a.status === 'error') return { q: 'Stopped with an error', dt: a.error?.message || '' };
  const w = a.waiting || {};
  if (w.kind === 'permission') {
    const what = (w.context || '').split('·')[0].trim();
    return { q: what ? `Allow ${what.toLowerCase()}?` : 'Allow this?', dt: w.detail ? `$ ${w.detail}` : (w.title || '') };
  }
  if (w.kind === 'question') return { q: `Pick 1 of ${(w.options || []).length}`, dt: w.title || w.context || '' };
  return { q: 'Needs input', dt: w.title || w.detail || '' };
}

// The label over a question: "Question · 1 of 1", "Question · size · pick
// any". A context that already says "question" ("question · 1 of 1", "size ·
// question 2 of 3", "review · 2 questions") goes in as it is.
function askLabel(w) {
  if (w.kind === 'permission') return 'Permission';
  if (w.kind !== 'question') return 'Input';
  const c = (w.context || '').trim();
  if (!c) return `Question · pick 1 of ${(w.options || []).length}`;
  return /\bquestions?\b/i.test(c) ? c : `Question · ${c}`;
}

function tileClass(a) {
  if (a.status === 'waiting') return 'wait';
  if (a.status === 'error') return 'err';
  if (hungry(a)) return 'hungry';
  if (['idle', 'starting', 'exited'].includes(a.status)) return 'idle';
  return 'run';
}

// A tile, kept per agent and updated in place, so its critter keeps moving.
const tileEls = new Map();
function tileFor(a) {
  let t = tileEls.get(a.id);
  if (!t) {
    const ring = h('i', { class: 'ring', 'aria-hidden': 'true' });
    const ch = h('i', { class: 'ch', 'aria-hidden': 'true' });
    const spr = h('span', { class: 'spr c dk', 'aria-hidden': 'true' });
    const tn = h('span', { class: 'tn' });
    const tm = h('span', { class: 'tm' });
    const qb = h('span', { class: 'qb', hidden: true });
    const el = h('a', { class: 'tile', href: `#home/${encodeURIComponent(a.id)}`, draggable: 'true', 'data-id': a.id }, ring, ch, qb, spr, tn, tm);
    wireDrag(el, a.id);
    t = { el, ring, ch, qb, spr, tn, tm, sig: '' };
    tileEls.set(a.id, t);
  }
  const meta = tileMeta(a);
  const cls = tileClass(a);
  const pct = a.status === 'running' ? ctxPct(a) : 0;
  const queued = a.queued || 0;
  const sig = `${cls}|${agentName(a)}|${meta}|${pct}|${chipColor(a)}|${queued}|${a.id === S.arg ? 'cur' : ''}`;
  if (t.sig !== sig) {
    t.sig = sig;
    const cur = a.id === S.arg && S.page === 'home';
    t.el.className = `tile ${cls}${cur ? ' cur' : ''}`;
    t.el.setAttribute('href', cur ? '#home' : `#home/${encodeURIComponent(a.id)}`);
    if (cur) t.el.setAttribute('aria-current', 'true'); else t.el.removeAttribute('aria-current');
    t.tn.textContent = agentName(a);
    t.tm.textContent = meta;
    t.ring.hidden = !pct;
    t.ring.style.setProperty('--p', `${pct}%`);
    t.ch.style.background = cls === 'wait' ? '#0A0A0A' : chipColor(a);
    // Prompts waiting for its turn to end: "+2" in the corner.
    t.qb.hidden = !queued;
    t.qb.textContent = `+${queued}`;
    t.qb.title = queued ? `${plural(queued, 'prompt')} queued: ${queued === 1 ? 'it goes' : 'they go'} in when this turn ends` : '';
    t.el.setAttribute('aria-label', `${agentName(a)}, ${a.status === 'waiting' ? 'needs you' : meta}${queued ? `, ${queued} queued` : ''}`);
  }
  setSprite(t.spr, poseOf(a), a.tool, a.hat);
  return t.el;
}

// A free spot: it starts an agent there. The first one on a page says so.
function freeSpot(slot, label) {
  const el = h('a', { class: 'tile free', href: `#start/${slot}`, 'aria-label': `A free spot: start an agent in spot ${slot % SPOTS + 1} of page ${Math.floor(slot / SPOTS) + 1}` },
    label ? h('span', { class: 'fs' }, icon('plus', 20, 1.8), h('span', { class: 'tm' }, 'Start an agent')) : null);
  wireDrop(el, slot);
  return el;
}

// Drag a tile onto another (they swap) or onto a free spot: the service
// keeps the order, so the boards follow.
let dragId = null;
function wireDrag(el, id) {
  el.addEventListener('dragstart', (e) => { dragId = id; e.dataTransfer.effectAllowed = 'move'; e.dataTransfer.setData('text/plain', id); });
  el.addEventListener('dragend', () => { dragId = null; document.querySelectorAll('.tile.drop').forEach((x) => x.classList.remove('drop')); });
  wireDrop(el, null, id);
}
function wireDrop(el, slot, id) {
  el.addEventListener('dragover', (e) => { if (dragId && dragId !== id) { e.preventDefault(); el.classList.add('drop'); } });
  el.addEventListener('dragleave', () => el.classList.remove('drop'));
  el.addEventListener('drop', async (e) => {
    e.preventDefault();
    el.classList.remove('drop');
    const target = slot ?? agentsList().find((a) => a.id === id)?.slot;
    if (!dragId || target == null) return;
    try { await api('POST', `/v1/agents/${encodeURIComponent(dragId)}/move`, { slot: target }); } catch (ex) { if (ex.status !== 401) toast(ex.message, true); }
    await load('state');
    updateDeck();
  });
}

// cells: the page's 16 spots, tiles and free ones, in order.
function cells(page) {
  const at = new Map(agentsList().map((a) => [a.slot, a]));
  const out = [];
  let labelled = false;
  for (let i = 0; i < SPOTS; i++) {
    const slot = page * SPOTS + i;
    const a = at.get(slot);
    if (a) out.push(tileFor(a));
    else { out.push(freeSpot(slot, !labelled)); labelled = true; }
  }
  return out;
}

// reconcile puts want into parent in order, moving only what moved.
function reconcile(parent, want) {
  want.forEach((el, i) => { if (parent.children[i] !== el) parent.insertBefore(el, parent.children[i] || null); });
  while (parent.children.length > want.length) parent.lastChild.remove();
}

// qrow: an agent that needs you, in the deck's side column.
function qrow(a) {
  const face = h('span', { class: 'face dk' });
  face.style.setProperty('--body', '#FF4D00');
  face.style.setProperty('--cut', '#0A0A0A');
  const spr = h('span', { class: 'spr c' });
  setSprite(spr, 'wait', a.tool, a.hat, { bubble: false });
  face.append(spr);
  return h('a', { class: 'qrow', href: `#${S.page === 'home' ? 'home' : 'agent'}/${encodeURIComponent(a.id)}` }, face,
    h('span', { class: 'grow' }, h('span', { class: 'eyebrow', title: agentName(a) }, agentName(a)), h('span', { class: 'mono q' }, askOf(a).q)));
}

// rrow: something ready for you, in the deck's side column.
function rrow(vis, eyebrow, title, href, onclick) {
  return h('a', { class: 'rrow', href, onclick }, h('span', { class: 'vis' }, vis), h('span', { class: 'grow' }, h('span', { class: 'eyebrow acc2' }, eyebrow), h('span', { class: 'nm' }, title)));
}

function readyRows() {
  const sys = S.system || {};
  const u = S.update || {};
  const rows = [];
  if (appUpdate(u) && S.laterUpdate !== u.version) rows.push(rrow(h('span', { class: 'mono p vv' }, u.version), u.state === 'ready' ? 'Update' : 'Update out', `agents deck ${u.version}`, '#updates'));
  for (const d of (S.devices?.devices || []).filter((x) => x.state === 'waiting')) rows.push(rrow(devScreen(d, 'xs'), 'Wants to join', d.name, `#board/${encodeURIComponent(d.id)}`));
  for (const p of (S.ports?.ports || []).filter((x) => x.state === 'new')) rows.push(rrow(icon('usb', 18, 1.8), 'New USB port', p.likely === 'esp32-usb' || p.likelyName ? 'ESP32 board' : p.id, `#new-board/${encodeURIComponent(p.id)}`));
  if (sys.firmware_updates) rows.push(rrow(icon('board', 18, 1.8), 'Board firmware', sys.firmware_updates === 1 ? 'One board can update' : `${say(sys.firmware_updates)} boards can update`, '#updates'));
  return rows;
}

function summaryStrip() {
  const sys = S.system || {};
  const need = agentsList().filter(needsYou).length;
  const u = S.update || {};
  const nUpd = (appUpdate(u) && S.laterUpdate !== u.version ? 1 : 0) + (sys.firmware_updates || 0);
  const nBoards = boardsWaiting(sys);
  const jump = (id) => (e) => { e.preventDefault(); document.getElementById(id)?.scrollIntoView({ block: 'nearest', behavior: 'smooth' }); };
  return h('nav', { class: 'strip', 'aria-label': 'Summary' },
    h('a', { href: need ? `#home/${encodeURIComponent(agentsList().find(needsYou).id)}` : '#home', class: need ? 'hotc' : '' }, h('span', { class: 'n' }, String(need)), h('span', { class: 'l' }, need === 1 ? 'agent' : 'agents', h('br'), need === 1 ? 'needs you' : 'need you')),
    h('a', { href: '#updates', onclick: nUpd && S.page === 'home' ? jump('ready') : null }, h('span', { class: 'n' }, String(nUpd)), h('span', { class: 'l' }, nUpd === 1 ? 'update' : 'updates', h('br'), 'ready')),
    h('a', { href: '#boards' }, h('span', { class: 'n' }, String(nBoards)), h('span', { class: 'l' }, nBoards === 1 ? 'board' : 'boards', h('br'), 'waiting')),
    h('a', { class: 'st', href: '#troubleshoot' },
      h('span', {}, h('span', { class: 'p' }, '$'), ' ', h('span', { class: 'w' }, 'agentctl status')),
      h('span', {}, connOK ? h('span', { class: 'okc' }, 'running') : h('span', { class: 'badc' }, 'not answering'), ` ${sys.version || ''}`)));
}

// The deck's refs while Home is open; updateDeck fills them from the state.
let deck = null;

function drawHome() {
  if (S.panel) return drawPanel();
  const list = agentsList();
  if (!list.length && S.state) return drawEmptyDeck();
  const grid = h('div', { class: 'grid4', 'aria-label': 'Agents' });
  const pager = h('div', { class: 'pager' });
  const side = h('div', { class: 'deckside' });
  deck = { grid, pager, side, strip: h('div', { class: 'stripwrap' }) };
  fillDeck();
  return [h('h1', { class: 'sr' }, 'Home'), deck.strip, h('div', { class: 'deckrow' }, h('div', { class: 'deckmain' }, grid, pager), side)];
}

function updateDeck() {
  syncBar();
  if (S.page === 'agent') return updateAgent();
  if (S.page !== 'home' || !deck || !deck.grid.isConnected) { if (S.page === 'home') draw(true); return; }
  if (!agentsList().length) { draw(true); return; }
  fillDeck();
}

function fillDeck() {
  const pages = pageCount();
  S.deckPage = Math.min(S.deckPage || 0, pages - 1);
  reconcile(deck.grid, cells(S.deckPage));
  slideIn(deck.grid);
  const onPage = agentsList().filter((a) => Math.floor((a.slot ?? 0) / SPOTS) === S.deckPage).length;
  const pageBtns = [];
  for (let p = 0; p < pages + (onPage === SPOTS && S.deckPage === pages - 1 ? 1 : 0); p++) {
    const hot = agentsList().some((a) => needsYou(a) && Math.floor((a.slot ?? 0) / SPOTS) === p);
    pageBtns.push(h('button', { type: 'button', class: `pg${hot ? ' hot' : ''}`, 'aria-current': p === S.deckPage ? 'page' : null, 'aria-label': `Page ${p + 1}${hot ? ', an agent there needs you' : ''}`,
      onclick: () => goPage(p) }, String(p + 1)));
  }
  deck.pager.replaceChildren(h('span', { class: 'kl' }, 'Page'), ...pageArrows(pageBtns, pageBtns.length),
    h('span', { class: 'sub' }, `${plural(onPage, 'agent')}${onPage < SPOTS ? ` · ${SPOTS - onPage} free` : ''}`), h('span', { class: 'sp' }),
    seg('View', [{ value: 'grid', text: 'Grid' }, { value: 'panel', text: 'Panel' }], 'grid', (v) => { if (v === 'panel') { S.panel = true; draw(true); document.activeElement?.blur(); } }, { key: 'view' }));
  const lost = agentsList().filter((a) => a.lost);
  put(deck.strip, summaryStrip(), lost.length ? h('div', { class: 'blk lostbar' }, h('div', { class: 'row thin' },
    h('span', { class: 'txt grow' }, `${lost.length === 1 ? 'One agent was' : `${say(lost.length)} agents were`} lost with tmux. Restore brings ${lost.length === 1 ? 'it' : 'them'} back where ${lost.length === 1 ? 'it' : 'they'} stopped.`),
    btn('Restore all', (e) => busy(e.currentTarget, async () => { await api('POST', '/v1/restore', {}); await load('state'); updateDeck(); }, 'Restoring'), 'p', { 'data-k': 'restore-all' }))) : null);
  const need = agentsList().filter(needsYou);
  const ready = readyRows();
  const needsSec = h('section', { class: 'sec', id: 'needs' }, h('div', { class: 'rule' }, h('h2', {}, 'Needs you')),
    need.length ? h('div', { class: 'qlist' }, need.map(qrow)) : h('p', { class: 'sub' }, 'When an agent asks for a yes or a pick, it shows up here.'));
  const readySec = h('section', { class: 'sec', id: 'ready' }, h('div', { class: 'rule' }, h('h2', {}, 'Ready for you'), h('span', { class: 'm' }, ready.length ? String(ready.length) : 'all up to date')),
    ready.length ? h('div', { class: 'qlist' }, ready) : null);
  const title = need.length ? h('p', { class: 'serif big' }, `${say(need.length)} need`, need.length === 1 ? 's ' : ' ', h('em', {}, 'you.'))
    : h('p', { class: 'serif big' }, 'Nothing needs ', h('em', {}, 'you.'));
  deck.side.replaceChildren(title, needsSec, readySec);
}

// Turning the deck's pages: the numbers, ‹ ›, and ← → all come here; the
// tiles slide in from the side you turned to.
function goPage(p) {
  const cur = S.deckPage || 0;
  if (p === cur || p < 0 || p >= Math.max(pageCount(), 1) + 1) return;
  S.slide = p > cur ? 1 : -1;
  S.deckPage = p;
  if (S.panel) updatePanel(); else updateDeck();
}
function slideIn(el) {
  if (!S.slide || !el) return;
  el.classList.remove('slide-next', 'slide-prev');
  void el.offsetWidth; // restart the animation
  el.classList.add(S.slide > 0 ? 'slide-next' : 'slide-prev');
  S.slide = 0;
}
function pageArrows(btns, count) {
  const cur = S.deckPage || 0;
  const arr = (dir, label, glyph) => h('button', { type: 'button', class: 'pg arr', 'aria-label': label, disabled: cur + dir < 0 || cur + dir >= count, onclick: () => goPage(cur + dir) }, glyph);
  return [arr(-1, 'Previous page (←)', '‹'), ...btns, arr(1, 'Next page (→)', '›')];
}
function panelPager() {
  const pages = pageCount();
  const btns = [];
  for (let p = 0; p < pages; p++) {
    const hot = agentsList().some((a) => needsYou(a) && Math.floor((a.slot ?? 0) / SPOTS) === p);
    btns.push(h('button', { type: 'button', class: `pg${hot ? ' hot' : ''}`, 'aria-current': p === (S.deckPage || 0) ? 'page' : null, 'aria-label': `Page ${p + 1}`, onclick: () => goPage(p) }, String(p + 1)));
  }
  return pageArrows(btns, pages);
}
// panelHead: the line over the panel's grid, as the panel draws it: how
// many agents, how many wait for you and how many run.
function panelHead() {
  const all = agentsList();
  const wait = all.filter(needsYou).length, run = all.filter((a) => a.status === 'running').length;
  return h('div', { class: 'pvhead mono' }, h('span', { class: 'pvface' }, critter({ pose: 'hi', body: '#FF4D00', cut: '#000' })), h('span', {}, `×${all.length}`), h('span', { class: 'sp' }),
    wait ? h('span', { class: 'acc' }, `${wait} wait`) : null, wait && run ? ' · ' : null, run ? h('span', { class: 'acc' }, `${run} run`) : null);
}
function updatePanel() {
  syncBar();
  const g = $('.pgrid');
  if (!g) return draw(true);
  reconcile(g, cells(S.deckPage || 0));
  slideIn(g);
  $('.pvhead')?.replaceWith(panelHead());
  $('.pvpager')?.replaceChildren(...panelPager());
}

// Nothing on the deck yet: start one in a project, or add a session.
function drawEmptyDeck() {
  const free = h('div', { class: 'grid4 small' });
  for (let i = 0; i < SPOTS; i++) free.append(freeSpot(i, false));
  const projects = S.projects?.projects || [];
  const live = (S.live?.sessions || []).filter((s) => !s.onBoard);
  const start = sec('Start an agent', 'in a project', ['Projects', '#agents/projects'],
    projects.length ? h('div', { class: 'qlist' }, projects.slice(0, 4).map((p) => rrow(projectFace(p), p.tool ? TOOL_NAMES[p.tool] : 'Claude Code', p.name, `#start/0/${encodeURIComponent(p.name)}`)))
      : h('p', { class: 'txt' }, 'Projects are the folders your agents work in. Add one, then start agents in it from here.'),
    h('div', { class: 'actions' }, projects.length ? link(`Start in ${projects[0].name}`, `#start/0/${encodeURIComponent(projects[0].name)}`, 'p') : null, link('Add a project', '#agents/projects', projects.length ? '' : 'p')));
  const add = live.length ? sec('Or add a Claude session', `${live.length} running elsewhere`, null, blk(...live.map((s) => row(s.title || s.folder || 'Claude session', `${s.background ? 'in the background' : 'in a terminal'} · ${ago(s.started)}`, [
    btn('Add', (e) => busy(e.currentTarget, async () => { await api('POST', `/v1/live/${encodeURIComponent(s.sessionId)}/add`, {}); await loadAll('state', 'live'); draw(true); }, 'Added to the deck'), '', { 'aria-label': `Add ${s.title || s.folder || 'this session'} to the deck` }),
  ], { name: true })))) : null;
  deck = null;
  return [h('header', { class: 'hd row2 lead-row' }, critter({ pose: 'idle', hat: { shape: 'propeller', color: 'blue' }, body: '#7A776F', cut: '#1A1A18', centre: true }),
    h('div', { class: 'hd' }, h('span', { class: 'eyebrow acc' }, 'Home'), h('h1', {}, 'Nothing on the deck ', h('em', {}, 'yet.')))),
  h('div', { class: 'deckrow' }, h('div', { class: 'deckmain narrow' }, free), h('div', { class: 'deckside wide' }, start, add))];
}

// Panel view: the page as the wall panel draws it, for a spare screen.
function drawPanel() {
  deck = null;
  const grid = h('div', { class: 'grid4 pgrid' }, cells(S.deckPage || 0));
  const leave = () => { S.panel = false; history.replaceState(null, '', location.pathname + location.hash); draw(true); };
  const legend = (cls, text) => h('div', { class: 'lg' }, h('span', { class: `sw4 ${cls}`, 'aria-hidden': 'true' }), h('span', { class: 'kl' }, text));
  return h('div', { class: 'panelview', role: 'dialog', 'aria-label': 'Panel view' },
    h('div', { class: 'pvbar' }, h('h1', { class: 'eyebrow acc' }, 'Panel view'), h('span', { class: 'sub' }, 'as the wall panel shows it'), h('span', { class: 'sp' }),
      seg('View', [{ value: 'grid', text: 'Grid' }, { value: 'panel', text: 'Panel' }], 'panel', (v) => { if (v === 'grid') { leave(); document.activeElement?.blur(); } }, { key: 'pview' }),
      h('button', { type: 'button', class: 'btn sm', 'data-k': 'pv-leave', onclick: leave }, icon('back', 14), 'Leave · Esc')),
    h('div', { class: 'pvbody' }, h('div', { class: 'pvside' }),
      h('div', { class: 'pvscreen' }, panelHead(), grid),
      h('div', { class: 'pvside' }, legend('w', 'needs you'), legend('r', 'context used'), legend('hg', 'hungry'), legend('i', 'idle'))),
    h('div', { class: 'pager pvpager' }, ...panelPager()));
}

// ---- an agent: its page from a tile -------------------------------------------------------

let agentView = null; // { key, id, update }
const sending = new Map(); // prompt id -> { key, status }

function agentKey(a) { return `${a.id}|${a.status}|${a.waiting?.id || ''}|${a.resumable ? 'r' : ''}|${a.account?.name || ''}|${(S.accounts || []).map((x) => x.name).join(',')}`; }
const post = (a, path, body) => api('POST', `/v1/agents/${encodeURIComponent(a.id)}/${path}`, body);

// noteEl: one line under the actions that says what happened.
function say2(note, text, bad) { note.textContent = text; note.classList.toggle('err', !!bad); }

function actButton(a, label, path, note, cls = '', ic = null) {
  return h('button', { type: 'button', class: `btn sm ${cls}`.trim(), onclick: async (e) => {
    const b = e.currentTarget;
    b.disabled = true;
    say2(note, `${label}: sending…`);
    try { await post(a, path, {}); say2(note, `${label}: sent`); } catch (ex) { say2(note, `${label} didn't work: ${ex.message}`, true); }
    if (b.isConnected) b.disabled = false;
  } }, ic ? icon(ic, 14) : null, label);
}

// twoStep: a button that asks for a second click (it stops an agent).
function twoStep(label, confirm, cls, run) {
  let armed = null;
  const b = h('button', { type: 'button', class: `btn sm ${cls}`.trim() }, label);
  b.addEventListener('click', async () => {
    if (confirm && !armed) {
      b.textContent = confirm;
      b.classList.add('armed');
      armed = setTimeout(() => { armed = null; b.textContent = label; b.classList.remove('armed'); }, 3000);
      return;
    }
    clearTimeout(armed); armed = null;
    b.disabled = true;
    await run();
    if (b.isConnected) { b.disabled = false; b.textContent = label; b.classList.remove('armed'); }
  });
  return b;
}

function hatPicker(a, onDone) {
  const hat = a.hat || { shape: '', color: 'ink', auto: true };
  const note = h('span', { class: 'sub' });
  const pick = async (body) => {
    try { await post(a, 'hat', body); await load('state'); onDone(); } catch (ex) { note.textContent = `Hat: ${ex.message}`; note.classList.add('err'); }
  };
  const shapes = HAT_SHAPES.map((shape) => {
    const b = h('button', { type: 'button', class: 'hatsw dk', role: 'radio', 'aria-checked': shape === hat.shape && !hat.auto ? 'true' : 'false', title: shape, 'aria-label': `Hat: ${shape}`, onclick: () => pick({ shape, color: hat.color || 'ink' }) });
    const spr = h('span', { class: 'spr c' });
    setSprite(spr, 'wait', a.tool, { shape, color: hat.color || 'ink' }, { bubble: false });
    b.append(spr);
    return b;
  });
  const colors = HAT_COLOR_NAMES.map((color) => {
    const b = h('button', { type: 'button', class: 'colsw', role: 'radio', 'aria-checked': color === hat.color && !hat.auto ? 'true' : 'false', title: color, 'aria-label': `Colour: ${color}`, onclick: () => pick({ shape: hat.shape || 'cap', color }) });
    b.style.background = HAT_COLORS[color][0];
    return b;
  });
  return h('div', { class: 'blk pad hatpick' },
    h('span', { class: 'sub' }, `For ${tilde(a.cwd || '')}. Every agent working there wears it.`),
    h('div', { class: 'hatrow', role: 'radiogroup', 'aria-label': 'Hat' }, shapes),
    h('div', { class: 'hatrow', role: 'radiogroup', 'aria-label': 'Colour' }, colors),
    h('div', { class: 'actions' }, btn(hat.auto ? 'Automatic' : 'Back to automatic', () => pick({ auto: true }), '', { disabled: hat.auto }), note));
}

// The deck in small, beside an agent: its page, the agent outlined.
function miniDeck(page, cur, landing) {
  const at = new Map(agentsList().map((a) => [a.slot, a]));
  const g = h('div', { class: 'mini', 'aria-label': `The deck, page ${page + 1}` });
  for (let i = 0; i < SPOTS; i++) {
    const slot = page * SPOTS + i;
    const a = at.get(slot);
    const isCur = (a && a.id === cur) || slot === landing;
    if (a) {
      const el = h('a', { href: `#agent/${encodeURIComponent(a.id)}`, class: `${a.status === 'waiting' ? 'w' : ''}${isCur ? ' cur' : ''}`.trim() || null, 'aria-label': agentName(a), title: agentName(a) });
      const spr = h('span', { class: 'spr c dk' });
      el.style.setProperty('--body', a.status === 'waiting' ? '#0A0A0A' : ['idle', 'exited', 'starting'].includes(a.status) ? '#7A776F' : '#FF4D00');
      el.style.setProperty('--cut', a.status === 'waiting' ? '#FF4D00' : '#141412');
      setSprite(spr, a.status === 'waiting' ? 'wait' : 'idle', a.tool, a.hat, { bubble: false });
      el.append(spr);
      g.append(el);
    } else g.append(h('a', { href: `#start/${slot}`, class: `f${isCur ? ' cur' : ''}`, 'aria-label': `Free spot ${i + 1}` }));
  }
  return g;
}

function drawAgent() {
  const a = agentsList().find((x) => x.id === S.arg);
  agentView = null;
  const back = h('a', { class: 'btn link', href: '#home', 'data-k': 'deck-back' }, icon('back', 14), 'Deck');
  if (!a) return [h('div', { class: 'crumbs' }, back), head('Deck', 'This agent isn\'t on the deck', 'any more.'), note('It ended or was removed.')];
  const body = h('div', { class: 'agentmain' });
  const side = h('div', { class: 'agentside' });
  const crumbs = h('div', { class: 'crumbs' });
  agentView = { key: '', id: a.id, body, side, crumbs, back };
  fillAgent();
  return [crumbs, h('div', { class: 'deckrow' }, body, side)];
}

function updateAgent() {
  if (!agentView || !agentView.body.isConnected) return;
  if (!agentsList().some((x) => x.id === agentView.id)) { draw(true); return; }
  fillAgent();
}

function fillAgent() {
  const a = agentsList().find((x) => x.id === agentView.id);
  // prev and next: among the waiting ones while this one waits, else the deck
  const list = a.status === 'waiting' ? agentsList().filter((x) => x.status === 'waiting') : bySlot();
  const i = list.findIndex((x) => x.id === a.id);
  const step = (d) => list[(i + d + list.length) % list.length];
  const nav = (d, label, path) => h('a', { class: 'ico', href: `#agent/${encodeURIComponent(step(d).id)}`, 'aria-label': label }, sv('svg', { width: 16, height: 16, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 2, 'stroke-linecap': 'round', 'aria-hidden': 'true' }, sv('path', { d: path })));
  put(agentView.crumbs, agentView.back, h('span', { class: 'kl' }, `/ ${agentName(a)}`), h('span', { class: 'sp' }),
    list.length > 1 ? nav(-1, 'Previous agent', 'M14.5 5.5L8 12l6.5 6.5') : null,
    h('span', { class: 'sub' }, `${i + 1} of ${list.length}${a.status === 'waiting' ? ' waiting' : ''}`),
    list.length > 1 ? nav(1, 'Next agent', 'M9.5 5.5L16 12l-6.5 6.5') : null);
  const page = Math.floor((a.slot ?? 0) / SPOTS);
  const others = agentsList().filter((x) => needsYou(x) && x.id !== a.id);
  put(agentView.side, h('span', { class: 'kl' }, 'The deck'), miniDeck(page, a.id),
    others.length ? h('section', { class: 'sec' }, h('div', { class: 'rule' }, h('h2', {}, a.status === 'waiting' ? 'Next waiting' : 'Needs you')), h('div', { class: 'qlist' }, others.slice(0, 4).map(qrow))) : null);
  const key = agentKey(a);
  if (agentView.key !== key) {
    const hadFocus = agentView.body.contains(document.activeElement) && document.activeElement.tagName !== 'INPUT';
    agentView.key = key;
    agentView.update = buildAgent(a, agentView.body);
    if (hadFocus) agentView.body.querySelector('h1')?.focus({ preventScroll: true });
  }
  agentView.update(a);
}

function agentHead(a) {
  const mascot = h('button', { type: 'button', class: `tile ${tileClass(a)} mascot`, 'aria-label': `Change the hat of ${agentName(a)}`, 'aria-expanded': 'false', disabled: !a.cwd });
  const spr = h('span', { class: 'spr c dk' });
  mascot.append(spr);
  const holder = h('div', {});
  mascot.addEventListener('click', () => {
    const open = mascot.getAttribute('aria-expanded') === 'true';
    mascot.setAttribute('aria-expanded', open ? 'false' : 'true');
    holder.replaceChildren(open ? '' : hatPicker(agentsList().find((x) => x.id === a.id) || a, () => { mascot.setAttribute('aria-expanded', 'false'); holder.replaceChildren(); updateDeck(); }));
  });
  const sw = h('span', { class: 'acsw', 'aria-hidden': 'true' });
  const eyebrow = h('span', { class: 'eyebrow dim' });
  const status = h('span', { class: 'chip st2' });
  const chips = h('div', { class: 'chips' });
  const el = h('div', { class: 'ahead' }, mascot, h('div', { class: 'grow' }, eyebrow, h('h1', { class: 'sm', tabindex: '-1' }, agentName(a)), chips));
  return {
    el: h('div', {}, el, holder),
    update: (x) => {
      setSprite(spr, poseOf(x), x.tool, x.hat);
      mascot.className = `tile ${tileClass(x)} mascot`;
      eyebrow.replaceChildren(...[TOOL_NAMES[x.tool] || x.tool || '', x.account ? ' · ' : '', x.account ? sw : '', x.account ? x.account.name : ''].filter((v) => v !== ''));
      if (x.account) sw.style.background = acctColor(x.account);
      const since = rel(x.statusSince);
      const st = { running: ['Working', 'acc'], waiting: ['Needs you', 'fill'], error: ['Error', 'bad'], idle: [x.unseen ? 'Hungry' : 'Idle', x.unseen ? 'acc' : 'off'], starting: ['Starting', 'off'], exited: ['Ended', 'off'] }[x.status] || [x.status, ''];
      status.className = `chip st2 ${st[1]}`;
      status.replaceChildren(h('i', { 'aria-hidden': 'true' }), `${st[0]}${since ? ` · ${since}` : ''}`);
      put(chips, status, x.queued ? chip(`${x.queued} queued`, 'acc', null, { 'data-k': 'queued-chip' }) : null, x.branch ? chip(x.branch) : null, x.modelLabel || x.model ? chip(x.modelLabel || x.model) : null,
        x.aiTitle && x.aiTitle !== agentName(x) ? chip(x.aiTitle, 'off') : null);
    },
  };
}

// who queued a prompt, from the user's side.
function queuedBy(by) {
  if (!by || by === 'local') return 'from this computer';
  if (by === 'web') return 'from the browser';
  if (by.startsWith('mcp:')) return `from ${by.slice(4)} over MCP`;
  return `from ${by}`;
}

// queueBlock: the prompts queued for an agent, each with Cancel. They go in
// one at a time, as each turn ends; it reads them again when the count or
// the status changes.
function queueBlock(a, note) {
  const list = h('div', { class: 'blk qlist2' });
  const el = h('section', { class: 'sec', 'data-sec': 'queued', hidden: true },
    h('div', { class: 'rule' }, h('h2', {}, 'Queued'), h('span', { class: 'm' }, 'each goes in when a turn ends')), list);
  let sig = null;
  const show = (items) => {
    el.hidden = !items.length;
    list.replaceChildren(...items.map((it, i) => row(it.text, `${i === 0 ? 'next' : `${i + 1}.`} · ${queuedBy(it.by)} · ${ago(it.queuedAt)}`,
      btn('Cancel', (e) => busy(e.currentTarget, async () => {
        let gone = false;
        try {
          await api('POST', `/v1/agents/${encodeURIComponent(a.id)}/queue/${encodeURIComponent(it.id)}/cancel`);
        } catch (ex) {
          if (ex.status !== 404) throw ex;
          gone = true;
        }
        sig = null;
        await load('state');
        updateDeck();
        return gone;
      }, (gone) => (gone ? 'It was sent already' : 'Cancelled: it won\'t be sent')), '', { 'aria-label': `Cancel the queued prompt: ${it.text.slice(0, 60)}`, 'data-k': `qcancel-${it.id}` }),
      { cls: 'qitem' })));
  };
  const update = async (x) => {
    const want = `${x.queued || 0}|${x.status}|${x.statusSince || ''}`;
    if (want === sig) return;
    sig = want;
    if (!x.queued) { show([]); return; }
    try { const r = await api('GET', `/v1/agents/${encodeURIComponent(x.id)}/queue`); if (sig === want) show(r?.queue || []); } catch (ex) { if (ex.status !== 401) say2(note, `Queued prompts: ${ex.message}`, true); }
  };
  return { el, update };
}

// buildAgent fills the agent's page for its state: working (stats, what it
// does, a prompt box) or asking (the question and its answers).
function buildAgent(a, body, view = agentView) {
  const hd = agentHead(a);
  const note = h('p', { class: 'sub', role: 'status', 'aria-live': 'polite' });
  const acts = h('div', { class: 'actions' });
  const external = codexThread(a);
  if (a.status !== 'exited') acts.append(actButton(a, external ? 'Open in Codex' : inApp(a) ? `Open in ${a.app}` : hungry(a) ? 'Review in terminal' : 'Focus', 'focus', note));
  else if (a.resumable) acts.append(actButton(a, 'Resume', 'resume', note, 'p'));
  if (a.status === 'running' && (!a.external || inApp(a))) acts.append(actButton(a, 'Interrupt', 'interrupt', note));
  if (a.tool === 'claude' && a.account) {
    for (const acc of S.accounts || []) {
      if (acc.name === a.account.name) continue;
      acts.append(twoStep(`Continue on ${acc.name}`, a.external ? `Again: a copy starts in ${acc.name}` : `Again: stops it, resumes in ${acc.name}`, '', async () => {
        say2(note, `Continue on ${acc.name}: sending…`);
        try { await post(a, 'handoff', { account: acc.name }); say2(note, a.external ? `Started in ${acc.name} as a new agent. This terminal keeps running.` : `Continuing in ${acc.name}.`); } catch (ex) { say2(note, `Didn't work: ${ex.message}`, true); }
      }));
    }
  }
  const quick = a.status === 'exited' || a.external;
  const rmLabel = external || inApp(a) ? 'Hide' : watched(a) ? 'Remove from deck' : a.status === 'exited' ? 'Remove' : 'Stop';
  acts.append(h('span', { class: 'sp' }), twoStep(rmLabel, quick ? '' : 'Click again to stop', 'd', async () => {
    say2(note, `${rmLabel}: sending…`);
    try { await post(a, 'dismiss', {}); say2(note, `${rmLabel}: done`); } catch (ex) { say2(note, `${rmLabel} didn't work: ${ex.message}`, true); }
  }));

  if (a.status === 'waiting' && a.waiting) {
    const w = a.waiting;
    const isQ = w.kind === 'question';
    const opts = [];
    const answer = async (key) => {
      if (sending.get(w.id)) return;
      sending.set(w.id, { key, status: 'sending' });
      opts.forEach((o) => { o.disabled = true; });
      say2(note, `Sending ${key}…`);
      try {
        await post(a, 'answer', { promptId: w.id, key });
        sending.set(w.id, { key, status: 'sent' });
        say2(note, `Sent ${key}. Waiting for the agent…`);
      } catch (ex) {
        sending.delete(w.id);
        opts.forEach((o) => { o.disabled = false; });
        say2(note, ex.status === 409 ? 'The question changed: showing the latest.' : `Not sent: ${ex.message}`, true);
      }
    };
    const list = h('div', { class: 'opts', role: 'group', 'aria-label': 'Answers' });
    for (const o of w.options || []) {
      const i = o.label.indexOf(' — ');
      const [t, sub] = i > 0 ? [o.label.slice(0, i), o.label.slice(i + 3)] : [o.label, ''];
      const b = h('button', { type: 'button', class: `opt${o.primary ? ' p' : ''}`, 'aria-keyshortcuts': o.key, 'data-k': `opt-${o.key}`, onclick: () => answer(o.key) },
        h('span', { class: 'k' }, o.key), h('span', {}, t, sub ? h('span', { class: 'h2' }, ` · ${sub}`) : null), o.selected ? h('span', { class: 'h' }, 'ticked') : null);
      opts.push(b);
      list.append(b);
    }
    const s = sending.get(w.id);
    if (s) { opts.forEach((o) => { o.disabled = true; }); say2(note, s.status === 'sent' ? `Sent ${s.key}. Waiting for the agent…` : `Sending ${s.key}…`); }
    view.answer = (key) => { if ((w.options || []).some((o) => o.key === key)) answer(key); };
    const ask = h('div', { class: 'askblk' },
      h('span', { class: 'eyebrow' }, askLabel(w)),
      h('span', { class: 'serif q' }, isQ ? w.title : askOf(a).q),
      !isQ && (w.detail || w.title) ? h('div', { class: 'term' }, h('div', { class: 'body' },
        w.detail ? h('div', {}, h('span', { class: 'p' }, '$ '), h('span', { class: 'w' }, w.detail)) : null,
        w.title ? h('div', { class: 'd' }, cap(w.title)) : null,
        w.context ? h('div', { class: 'd' }, w.context) : null)) : (isQ && w.detail ? h('p', { class: 'txt dark' }, w.detail) : null));
    const free = w.kind === 'input' || !(w.options || []).length;
    // Your own answer: the deck presses Esc on the question, then types the
    // text in as the next prompt (POST /prompt with interrupt). Agents in
    // another app's terminal can't be typed into, so they don't get the field.
    let own = null;
    if (!external && (!a.external || inApp(a))) {
      const fid = `own-${a.id}`;
      const input = h('input', { type: 'text', id: fid, placeholder: free ? 'What should it do?' : w.kind === 'permission' ? 'Tell it what to do instead' : 'Something other than the answers above', maxlength: '4000', autocomplete: 'off', 'data-k': fid });
      const sendOwn = async (e) => {
        e?.preventDefault();
        const text = input.value.trim();
        if (!text || sending.get(w.id)) return;
        sending.set(w.id, { key: 'your answer', status: 'sending' });
        opts.forEach((o) => { o.disabled = true; });
        say2(note, 'Sending your answer…');
        try {
          await post(a, 'prompt', { text, interrupt: true });
          sending.set(w.id, { key: 'your answer', status: 'sent' });
          input.value = '';
          say2(note, 'Sent your answer. Waiting for the agent…');
        } catch (ex) {
          sending.delete(w.id);
          opts.forEach((o) => { o.disabled = false; });
          say2(note, `Not sent: ${ex.message}`, true);
        }
      };
      input.addEventListener('keydown', (e) => { if (e.key === 'Enter') sendOwn(e); });
      own = h('div', { class: 'promptrow own' },
        h('div', { class: 'field' }, h('label', { for: fid }, 'Type your own answer'), input),
        h('button', { type: 'button', class: 'btn p sm', onclick: sendOwn }, 'Send'));
    }
    const hint = free ? (own ? 'Or answer it in its terminal.' : 'It wants free text: answer it in its terminal.')
      : `Answer here with 1 to ${(w.options || []).length} on the keyboard${own ? ', type your own' : ''}, or answer in its terminal.`;
    const qb = queueBlock(a, note);
    put(body, hd.el, ask, free ? null : list, own, h('p', { class: free && !own ? 'txt' : 'sub' }, hint), acts, note, qb.el);
    return (x) => { hd.update(x); qb.update(x); };
  }

  view.answer = null;
  const stat = (k) => { const v = h('span', { class: 'v' }); return [h('div', { class: 'stat' }, h('span', { class: 'kl' }, k), v), v]; };
  const [sCtx, vCtx] = stat('Context'), [sTok, vTok] = stat('Tokens'), [sCost, vCost] = stat('Cost'), [sTurns, vTurns] = stat('Turns');
  const prog = h('i', {});
  sCtx.append(h('div', { class: 'prog' }, prog));
  const doingBar = h('div', { class: 'bar' });
  const doing = h('div', { class: 'body' });
  const last = h('p', { class: 'txt' });
  const lastWhen = h('span', { class: 'sub' });
  const foot = h('p', { class: 'sub' });
  let promptBox = null;
  const qb = queueBlock(a, note);
  if (!external && a.status !== 'exited' && a.status !== 'error') {
    // Busy: queue it for when this turn ends (Enter), or stop the turn and
    // send it now. Idle: it goes in at once, after anything queued before it.
    const busyNow = a.status === 'running' || a.status === 'starting';
    const input = h('input', { type: 'text', id: `pr-${a.id}`, placeholder: busyNow ? 'Tell it what to do next (it goes in when this turn ends)' : 'Tell it what to do next', maxlength: '4000', autocomplete: 'off', 'data-k': `pr-${a.id}` });
    const send = async (e, mode = 'queue') => {
      e?.preventDefault();
      const text = input.value.trim();
      if (!text) return;
      say2(note, 'Sending…');
      try {
        const r = await post(a, 'prompt', { text, mode });
        input.value = '';
        say2(note, r?.status === 'queued' ? `Queued${r.position > 1 ? `, number ${r.position}` : ''}: it goes in when ${r.position > 1 ? 'the turns before it end' : 'this turn ends'}.` : 'Sent.');
        if (r?.status === 'queued') { await load('state'); updateDeck(); }
      } catch (ex) { say2(note, `Not sent: ${ex.message}`, true); }
    };
    input.addEventListener('keydown', (e) => { if (e.key === 'Enter') send(e); });
    promptBox = h('div', { class: 'promptrow' }, h('label', { for: `pr-${a.id}`, class: 'sr' }, 'Prompt'), h('div', { class: 'field' }, input),
      h('button', { type: 'button', class: 'btn p sm', 'data-k': 'pr-send', onclick: (e) => send(e) }, busyNow ? 'Queue' : 'Send'),
      a.status === 'running' && (!a.external || inApp(a)) ? h('button', { type: 'button', class: 'btn sm', 'data-k': 'pr-now', title: 'Stops this turn (Esc), then sends it', onclick: (e) => send(e, 'interrupt') }, 'Stop and send') : null);
  }
  put(body, hd.el, h('div', { class: 'blk stats4' }, sCtx, sTok, sCost, sTurns),
    h('div', { class: 'term' }, doingBar, doing),
    h('div', { class: 'grow' }, h('span', { class: 'kl' }, 'Last prompt'), last, lastWhen), promptBox, qb.el, acts, note, foot);
  return (x) => {
    hd.update(x);
    qb.update(x);
    const pct = ctxPct(x);
    vCtx.textContent = `${pct}%`;
    prog.style.width = `${pct}%`;
    const t = x.tokens || {};
    vTok.textContent = fmtN((t.input || 0) + (t.output || 0) + (t.cacheRead || 0) + (t.cacheWrite || 0));
    vTok.title = `in ${fmtN(t.input)} · out ${fmtN(t.output)} · cache read ${fmtN(t.cacheRead)} · cache write ${fmtN(t.cacheWrite)}`;
    vCost.textContent = codexThread(x) ? '–' : fmtCost(x.costUsd);
    vTurns.textContent = String(x.turns ?? 0);
    const subs = x.subagents || [];
    if (x.status === 'running') {
      doingBar.replaceChildren(h('i', {}), `doing now · ${subsOnly(x) || verb(x.activity?.tool)}${subs.length ? ` · ${plural(subs.length, 'subagent')}` : ''}`);
      doing.replaceChildren(h('div', {}, h('span', { class: 'p' }, '$ '), h('span', { class: 'w' }, x.activity?.detail || 'thinking…')),
        ...subs.slice(0, 4).map((s) => h('div', {}, h('span', { class: 'p' }, '↳ '), s.title, s.tool ? h('span', { class: 'd' }, ` ${verb(s.tool)}`) : null)));
    } else if (x.status === 'error') {
      doingBar.replaceChildren(h('i', {}), 'error');
      doing.replaceChildren(h('div', { class: 'badc' }, x.error?.message || 'unknown error'));
    } else {
      doingBar.replaceChildren(h('i', {}), 'status');
      doing.replaceChildren(h('div', { class: 'd' }, x.status === 'idle' ? `finished ${rel(x.statusSince)} ago${x.unseen ? ' · nobody has looked yet' : ''}` : x.status === 'starting' ? 'starting…' : `ended ${rel(x.statusSince)} ago${x.resumable ? ' · it can resume' : ''}`));
    }
    last.textContent = x.lastPrompt || '–';
    lastWhen.textContent = lastActive(x);
    foot.textContent = [tilde(x.cwd), S.state?.server?.name].filter(Boolean).join(' · ');
  };
}

// ---- the sidebar: an agent beside the deck ----------------------------------------------------
//
// A tile on Home opens its agent in an orange column on the right (#home/<id>);
// the deck (or Panel view) gives it room. The same answers, prompt box and
// queue as the agent's page; Full page ↗ has the rest.

const CTX_TICKS = 28;
let bar = null; // { el, body, id, key, update, answer }

function barEl() {
  if (bar) return bar;
  const body = h('div', { class: 'abbody' });
  const el = h('aside', { class: 'agentbar', id: 'agentbar', 'aria-label': 'Agent', 'aria-hidden': 'true', inert: '' }, h('div', { class: 'abin' }, body));
  document.body.append(el);
  bar = { el, body, id: '', key: '', update: null, answer: null };
  return bar;
}

function closeBar() { if (S.page === 'home' && S.arg) location.hash = '#home'; }

// syncBar opens, fills or closes the sidebar for the page and the state.
function syncBar() {
  const b = barEl();
  const want = S.page === 'home' ? S.arg : '';
  const a = want ? agentsList().find((x) => x.id === want) : null;
  if (want && !a && S.state) { history.replaceState(null, '', '#home'); S.arg = ''; } // it ended
  const open = !!a;
  document.documentElement.classList.toggle('barred', open);
  b.el.setAttribute('aria-hidden', open ? 'false' : 'true');
  b.el.inert = !open;
  fitDeck(open);
  if (!a) { b.key = ''; b.answer = null; return; } // the last agent stays while it slides out
  const key = agentKey(a);
  if (b.id !== a.id || b.key !== key) {
    const hadFocus = b.body.contains(document.activeElement) && document.activeElement.tagName !== 'INPUT';
    const swapped = b.id !== a.id;
    b.id = a.id; b.key = key;
    b.update = buildBar(a, b.body, b);
    if (swapped) b.el.querySelector('.abin').scrollTop = 0;
    if (hadFocus) b.body.querySelector('h2')?.focus({ preventScroll: true });
  }
  b.update(a);
}

// fitDeck scales the deck down (never up) so it keeps clear of the
// sidebar: a true scale, so names and critters shrink together.
function fitDeck(open) {
  const m = deck?.grid.isConnected ? deck.grid.parentElement : null;
  if (!m) return;
  let z = 1;
  if (open && window.innerWidth > 900) {
    const room = window.innerWidth - barEl().el.offsetWidth - m.getBoundingClientRect().left - 24;
    z = Math.max(0.5, Math.min(1, room / m.offsetWidth));
  }
  m.style.setProperty('--dz', z.toFixed(3));
}
window.addEventListener('resize', () => fitDeck(document.documentElement.classList.contains('barred')));

// buildBar fills the sidebar as the design draws it: the critter, tool and
// ×; the name in serif with its state; then the question and its answers, or
// what it does now and the last prompt; the prompt box and the queue; and at
// the bottom the numbers, Focus / Interrupt and Full page ↗.
function buildBar(a, body, view) {
  const label = (text, more) => h('span', { class: 'ablbl' }, text, more ? h('span', { class: 'dimx' }, ` · ${more}`) : null);
  const note = h('p', { class: 'abnote', role: 'status', 'aria-live': 'polite' });
  const external = codexThread(a);
  const typable = !external && (!a.external || inApp(a));

  // head
  const spr = h('span', { class: 'spr c dk' });
  const meta = h('span', { class: 'abmeta' });
  const tag = h('span', { class: 'abtag' });
  const sum = h('span', { class: 'absum' });
  const head = [
    h('div', { class: 'abtop' }, h('span', { class: 'abpic' }, spr), meta,
      h('button', { type: 'button', class: 'abx', 'aria-label': 'Close (Esc)', title: 'Close · Esc', 'data-k': 'bar-close', onclick: closeBar }, icon('x', 16, 2.2))),
    h('div', { class: 'abnmrow' }, h('h2', { class: 'serif abname', tabindex: '-1', title: agentName(a) }, agentName(a)), tag),
    sum,
  ];

  // an input joined to its button: the design's "own" row
  const ownRow = (placeholder, go, dark, onSend) => {
    const input = h('input', { type: 'text', placeholder, maxlength: '4000', autocomplete: 'off', 'aria-label': placeholder, 'data-k': `bar-in-${a.id}` });
    const send = (e) => { e?.preventDefault(); const text = input.value.trim(); if (text) onSend(text, input); };
    input.addEventListener('keydown', (e) => { if (e.key === 'Enter') send(e); });
    return h('div', { class: `abown${dark ? ' dark' : ''}` }, input, h('button', { type: 'button', class: 'go', onclick: send }, go));
  };

  // the queue, one line each: "+1 queued · "then open a PR" cancel"
  const qEl = h('div', { class: 'abq' });
  let qSig = null;
  const showQ = (items) => {
    put(qEl, items.map((it, i) => h('div', { class: 'abqd' }, h('span', { class: 'k2' }, i === 0 ? `+${items.length}` : `${i + 1}.`),
      h('span', { class: 'qt', title: it.text }, i === 0 ? `queued · “${it.text}”` : `“${it.text}”`),
      h('button', { type: 'button', class: 'abu', 'aria-label': `Cancel the queued prompt: ${it.text.slice(0, 60)}`, onclick: async () => {
        try { await api('POST', `/v1/agents/${encodeURIComponent(a.id)}/queue/${encodeURIComponent(it.id)}/cancel`); say2(note, 'Cancelled: it won\'t be sent'); } catch (ex) { say2(note, ex.status === 404 ? 'It was sent already' : ex.message, ex.status !== 404); }
        qSig = null; await load('state'); updateDeck();
      } }, 'cancel'))));
  };
  const updQ = async (x) => {
    const want = `${x.queued || 0}|${x.status}|${x.statusSince || ''}`;
    if (want === qSig) return;
    qSig = want;
    if (!x.queued) { showQ([]); return; }
    try { const r = await api('GET', `/v1/agents/${encodeURIComponent(x.id)}/queue`); if (qSig === want) showQ(r?.queue || []); } catch (ex) { if (ex.status !== 401) say2(note, `Queued prompts: ${ex.message}`, true); }
  };

  // middle: the question, or what it does
  let mid = [];
  let low = null; // the prompt box: at the bottom, over the numbers
  let liveUpd = () => {};
  view.answer = null;
  if (a.status === 'waiting' && a.waiting) {
    const w = a.waiting;
    const isQ = w.kind === 'question';
    const opts = [];
    const lock = (on) => opts.forEach((o) => { o.disabled = on; });
    const answer = async (key) => {
      if (sending.get(w.id)) return;
      sending.set(w.id, { key, status: 'sending' });
      lock(true);
      say2(note, `Sending ${key}…`);
      try { await post(a, 'answer', { promptId: w.id, key }); sending.set(w.id, { key, status: 'sent' }); say2(note, `Sent ${key}. Waiting for the agent…`); } catch (ex) {
        sending.delete(w.id); lock(false);
        say2(note, ex.status === 409 ? 'The question changed: showing the latest.' : `Not sent: ${ex.message}`, true);
      }
    };
    const list = h('div', { class: 'abopts', role: 'group', 'aria-label': 'Answers' });
    for (const o of w.options || []) {
      const i = o.label.indexOf(' — ');
      const [t, sub] = i > 0 ? [o.label.slice(0, i), o.label.slice(i + 3)] : [o.label, ''];
      const b = h('button', { type: 'button', class: `abopt${o.primary ? ' p' : ''}`, 'aria-keyshortcuts': o.key, 'data-k': `bar-opt-${o.key}`, onclick: () => answer(o.key) },
        h('span', { class: 'k' }, o.key), h('span', {}, t, sub ? h('span', { class: 'h2' }, ` · ${sub}`) : null), o.selected ? h('span', { class: 'h' }, 'ticked') : null);
      opts.push(b); list.append(b);
    }
    const s = sending.get(w.id);
    if (s) { lock(true); say2(note, s.status === 'sent' ? `Sent ${s.key}. Waiting for the agent…` : `Sending ${s.key}…`); }
    view.answer = (key) => { if ((w.options || []).some((o) => o.key === key)) answer(key); };
    const free = w.kind === 'input' || !(w.options || []).length;
    const own = typable ? ownRow(free ? 'What should it do?' : w.kind === 'permission' ? 'Tell it what to do instead' : 'Type your own answer', 'Send', false, async (text, input) => {
      if (sending.get(w.id)) return;
      sending.set(w.id, { key: 'your answer', status: 'sending' }); lock(true);
      say2(note, 'Sending your answer…');
      try { await post(a, 'prompt', { text, interrupt: true }); sending.set(w.id, { key: 'your answer', status: 'sent' }); input.value = ''; say2(note, 'Sent your answer. Waiting for the agent…'); } catch (ex) {
        sending.delete(w.id); lock(false); say2(note, `Not sent: ${ex.message}`, true);
      }
    }) : null;
    const ctxLine = [w.title && !isQ ? cap(w.title) : '', w.context && !isQ ? w.context : ''].filter(Boolean).join(' · ');
    mid = [label(askLabel(w)), h('b', { class: 'abqn' }, isQ ? w.title : askOf(a).q),
      !isQ && w.detail ? h('pre', { class: 'abcmd' }, h('span', { class: 'pr' }, '$ '), w.detail) : null,
      ctxLine ? h('span', { class: 'abctxl' }, ctxLine) : null,
      isQ && w.detail ? h('p', { class: 'ablast' }, w.detail) : null,
      free ? null : list, own,
      free && !own ? h('p', { class: 'abctxl' }, 'It wants free text: answer it in its terminal.') : null];
  } else {
    const doingLbl = h('span', { class: 'ablbl' });
    const doing = h('pre', { class: 'abcmd' });
    const plain = h('p', { class: 'abplain' });
    const lastLbl = h('span', { class: 'ablbl' });
    const last = h('p', { class: 'ablast' });
    // A long prompt shows 20 lines; "Show all" opens the rest.
    const more = h('button', { type: 'button', class: 'abu abmore', hidden: true, onclick: () => {
      const open = last.classList.toggle('open');
      more.textContent = open ? 'Show less' : 'Show all';
    } }, 'Show all');
    let box = null;
    if (!external && a.status !== 'exited' && a.status !== 'error') {
      const busyNow = a.status === 'running' || a.status === 'starting';
      box = ownRow(busyNow ? 'Tell it what to do next (goes in when this turn ends)' : 'Tell it what to do next', busyNow ? 'Queue' : 'Send', true, async (text, input) => {
        say2(note, 'Sending…');
        try {
          const r = await post(a, 'prompt', { text, mode: 'queue' });
          input.value = '';
          say2(note, r?.status === 'queued' ? `Queued${r.position > 1 ? `, number ${r.position}` : ''}: it goes in when ${r.position > 1 ? 'the turns before it end' : 'this turn ends'}.` : 'Sent.');
          if (r?.status === 'queued') { await load('state'); updateDeck(); }
        } catch (ex) { say2(note, `Not sent: ${ex.message}`, true); }
      });
    }
    mid = [doingLbl, doing, plain, lastLbl, last, more];
    low = box;
    liveUpd = (x) => {
      const subs = x.subagents || [];
      if (x.status === 'running') {
        doingLbl.replaceChildren(`Doing now · ${subsOnly(x) || verb(x.activity?.tool)}`, subs.length ? h('span', { class: 'dimx' }, ` · ${plural(subs.length, 'subagent')}`) : '');
        put(doing, h('div', {}, h('span', { class: 'pr' }, '$ '), x.activity?.detail || 'thinking…'),
          subs.slice(0, 4).map((sa) => h('div', { class: 'sub2' }, h('span', { class: 'pr' }, '↳ '), sa.title, sa.tool ? ` · ${verb(sa.tool)}` : '')));
        doing.hidden = false; plain.hidden = true;
      } else {
        doingLbl.textContent = x.status === 'error' ? 'Error' : 'Status';
        plain.textContent = x.status === 'error' ? (x.error?.message || 'unknown error')
          : x.status === 'idle' ? `Finished ${rel(x.statusSince)} ago${x.unseen ? ' · nobody has looked yet' : ''}`
            : x.status === 'starting' ? 'Starting…' : `Ended ${rel(x.statusSince)} ago${x.resumable ? ' · it can resume' : ''}`;
        doing.hidden = true; plain.hidden = false;
      }
      const when = x.lastActive ? rel(x.lastActive) : '';
      lastLbl.replaceChildren('Last prompt', when ? h('span', { class: 'dimx' }, ` · ${ageMin(x.lastActive) < 1 ? 'just now' : `${when} ago`}`) : '');
      if (last.textContent !== (x.lastPrompt || '–')) {
        last.textContent = x.lastPrompt || '–';
        requestAnimationFrame(() => { more.hidden = !last.classList.contains('open') && last.scrollHeight <= last.clientHeight + 2; });
      }
    };
  }

  // bottom: numbers, actions, keys
  const nums = barNumbers();
  const acts = h('div', { class: 'abacts' });
  if (a.status !== 'exited') acts.append(actButton(a, external ? 'Open in Codex' : inApp(a) ? `Open in ${a.app}` : hungry(a) ? 'Review in terminal' : 'Focus', 'focus', note));
  else if (a.resumable) acts.append(actButton(a, 'Resume', 'resume', note));
  if (a.status === 'running' && (!a.external || inApp(a))) acts.append(actButton(a, 'Interrupt', 'interrupt', note));
  acts.append(h('span', { class: 'sp' }), h('a', { class: 'abfull', href: `#agent/${encodeURIComponent(a.id)}`, 'data-k': 'bar-full', title: 'The agent\'s whole page: handoff, Stop and more' }, 'Full page ↗'));
  const n = (a.waiting?.options || []).length;
  const hint = h('span', { class: 'abhint' }, [a.status === 'waiting' && n ? `1–${n} answers` : '', 'Esc closes', '← → next agent'].filter(Boolean).join(' · '));

  put(body, head, mid, h('div', { class: 'abbottom' }, qEl, low, nums.el, acts, note, hint));
  return (x) => {
    setSprite(spr, poseOf(x), x.tool, x.hat, { bubble: false });
    put(meta, TOOL_NAMES[x.tool] || x.tool || '', x.account ? [' · ', h('i', { class: 'sw', style: `background:${acctColor(x.account)}` }), x.account.name] : null,
      x.branch ? h('span', { class: 'dimx' }, ` · ${x.branch}`) : null);
    const since = rel(x.statusSince);
    const st = { running: 'working', waiting: 'needs you', error: 'error', idle: x.unseen ? 'hungry' : 'idle', starting: 'starting', exited: 'ended' }[x.status] || x.status;
    tag.textContent = `${st}${since ? ` · ${since}` : ''}`;
    tag.className = `abtag${['running', 'waiting', 'error'].includes(x.status) || x.unseen ? '' : ' off'}`;
    const s2 = x.aiTitle && x.aiTitle !== agentName(x) ? x.aiTitle : '';
    sum.textContent = s2;
    sum.hidden = !s2;
    liveUpd(x);
    updQ(x);
    nums.update(x);
  };
}

// barNumbers: "context 56k / 200k ... 28%", 28 ticks, then tokens in and
// out, cache read and write, cost and turns, small.
function barNumbers() {
  const ticks = h('div', { class: 'abticks', 'aria-hidden': 'true' });
  for (let i = 0; i < CTX_TICKS; i++) ticks.append(h('i', {}));
  const used = h('small', {});
  const pctEl = h('b', {});
  const cell = (k, extra) => { const v = h('dd', {}); return [h('div', {}, h('dt', {}, k), v), v, extra]; };
  const [cIn, vIn] = cell('Tokens in'), [cOut, vOut] = cell('Tokens out'), [cRd, vRd] = cell('Cache read'),
    [cWr, vWr] = cell('Cache write'), [cCost, vCost] = cell('Cost'), [cTurns, vTurns] = cell('Turns');
  const el = h('section', { class: 'abnums', 'aria-label': 'Numbers' },
    h('div', { class: 'abctxh' }, h('span', {}, h('b', {}, 'Context'), used), pctEl), ticks,
    h('dl', {}, cIn, cOut, cRd, cWr, cCost, cTurns));
  return {
    el,
    update: (x) => {
      const pct = ctxPct(x);
      const c = x.context || {};
      used.textContent = c.window ? `${fmtN(c.used)} / ${fmtN(c.window)}` : '';
      pctEl.textContent = c.window ? `${pct}%` : '–';
      const lit = Math.round((pct / 100) * CTX_TICKS);
      [...ticks.children].forEach((t, i) => t.classList.toggle('on', i < lit));
      const t = x.tokens || {};
      vIn.textContent = fmtN(t.input); vOut.textContent = fmtN(t.output);
      vRd.textContent = fmtN(t.cacheRead); vWr.textContent = fmtN(t.cacheWrite);
      if (codexThread(x)) vCost.textContent = '–';
      else put(vCost, fmtCost(x.costUsd), h('small', {}, ' est'));
      vTurns.textContent = String(x.turns ?? 0);
    },
  };
}

// barStep: ← and → while the sidebar is open go to the agent before or after.
function barStep(d) {
  const list = bySlot();
  const i = list.findIndex((x) => x.id === S.arg);
  if (i < 0 || list.length < 2) return;
  location.hash = `#home/${encodeURIComponent(list[(i + d + list.length) % list.length].id)}`;
}

// ---- start an agent, in a project -------------------------------------------------------------

function projectFace(p) {
  const f = h('span', { class: 'face' });
  f.append(critter({ pose: 'hi', bot: p.tool === 'codex', hat: p.hat || null, body: '#FF4D00', cut: '#141412', centre: true }));
  return f;
}

function drawStart() {
  const [slotStr, nameArg] = S.arg.split('/');
  const slot = slotStr === '' || slotStr === undefined ? null : Number(slotStr);
  const projects = S.projects?.projects || [];
  const accs = S.agents?.accounts || [];
  const st = S.startForm && S.startForm.slot === slot ? S.startForm : (S.startForm = { slot, project: decodeURIComponent(nameArg || '') || projects[0]?.name || '', tool: '', account: '', prompt: '', folder: '' });
  const p = projects.find((x) => x.name === st.project) || null;
  const pfs = p ? (p.folders?.length ? p.folders : [p.folder]) : [];
  const folder = pfs.includes(st.folder) ? st.folder : pfs[0] || '';
  const tool = st.tool || p?.tool || 'claude';
  const account = st.account || p?.account || (accs.find((x) => x.default) || {}).name || '';
  const back = h('div', { class: 'crumbs' }, h('a', { class: 'btn link', href: '#home' }, icon('back', 14), 'Deck'), h('span', { class: 'kl' }, '/ Start an agent'));
  const main = h('div', { class: 'agentmain' }, h('h1', { class: 'sm' }, 'Start an ', h('em', {}, 'agent.')));
  if (!projects.length) {
    main.append(h('p', { class: 'txt' }, 'Agents start in a project: a folder of yours, with the tool and account they use. Add one in Agents first.'),
      h('div', { class: 'actions' }, link('Add a project', '#agents/projects', 'p'), link('Cancel', '#home')));
  } else {
    const radios = h('div', { class: 'qlist', role: 'radiogroup', 'aria-label': 'Project' }, projects.map((x) => {
      const on = x.name === st.project;
      return h('button', { type: 'button', class: `rrow pick2${on ? ' on' : ''}`, role: 'radio', 'aria-checked': on ? 'true' : 'false', 'data-k': `sp-${x.name}`,
        onclick: () => { st.project = x.name; st.tool = ''; st.account = ''; st.folder = ''; st.prompt = $('#fp')?.value || st.prompt; draw(true); } },
      projectFace(x), h('span', { class: 'grow' }, h('span', { class: 'nm' }, x.name), h('span', { class: 'sub' }, `${x.folders?.length > 1 ? plural(x.folders.length, 'folder') : x.folder}${x.tool ? ` · ${TOOL_NAMES[x.tool]}` : ''}`)));
    }));
    const err = h('p', { class: 'sub err', role: 'alert' });
    const keep = () => { st.prompt = $('#fp')?.value ?? st.prompt; };
    main.append(sec('Project', null, ['Manage projects', '#agents/projects'], radios),
      pfs.length > 1 ? h('div', { class: 'field' }, h('span', { class: 'kl' }, 'Folder'),
        seg('Folder', pfs.map((x) => ({ value: x, text: lastPart(x) })), folder, (v) => { keep(); st.folder = v; draw(true); }, { key: 'st-folder' })) :'',
      h('div', { class: 'fields' },
        h('div', { class: 'field' }, h('span', { class: 'kl' }, 'Runs'), seg('Runs', [{ value: 'claude', text: 'Claude Code' }, { value: 'codex', text: 'Codex' }, { value: 'shell', text: 'Shell' }], tool, (v) => { keep(); st.tool = v; draw(true); }, { key: 'st-tool' })),
        tool === 'claude' && accs.length ? h('div', { class: 'field' }, h('span', { class: 'kl' }, 'Claude account'), seg('Claude account', accs.map((x) => ({ value: x.name, text: x.name })), account, (v) => { keep(); st.account = v; draw(true); }, { key: 'st-acc' })) : null),
      h('div', { class: 'field' }, h('label', { for: 'fp' }, 'First prompt, if you like'), h('textarea', { id: 'fp', class: 'ta', maxlength: '4000', 'data-k': 'fp', oninput: (e) => { st.prompt = e.currentTarget.value; } }, st.prompt)),
      err,
      h('div', { class: 'actions' },
        btn('Start', async (e) => {
          err.textContent = '';
          keep();
          const b = e.currentTarget;
          b.disabled = true;
          try {
            const r = await api('POST', `/admin/v1/projects/${encodeURIComponent(p.name)}/start`, { prompt: st.prompt, tool, account: tool === 'claude' ? account : '', folder });
            S.startForm = null;
            if (slot != null && r?.id) {
              for (let i = 0; i < 20 && !agentsList().some((x) => x.id === r.id); i++) { await new Promise((res) => setTimeout(res, 250)); await load('state'); }
              await api('POST', `/v1/agents/${encodeURIComponent(r.id)}/move`, { slot }).catch(() => null);
            }
            toast(`Started in ${p.name}`);
            location.hash = r?.id ? `#agent/${encodeURIComponent(r.id)}` : '#home';
          } catch (ex) {
            err.textContent = ex.message;
          } finally {
            if (b.isConnected) b.disabled = false;
          }
        }, 'p', { disabled: !p || S.projects?.canStart === false, 'data-k': 'st-go' }),
        link('Cancel', '#home'),
        p ? h('span', { class: 'term cmd' }, h('span', { class: 'p' }, '$ '), `agentctl new ${folder}`) : null),
      S.projects?.canStart === false ? note('This service can\'t start agents (demo mode).') :'');
  }
  const live = (S.live?.sessions || []).filter((x) => !x.onBoard);
  if (live.length) {
    main.append(sec('Or add a Claude session', `${live.length} running elsewhere`, null, blk(...live.map((x) => row(x.title || x.folder || 'Claude session', `${x.background ? 'in the background' : 'in a terminal'} · started ${ago(x.started)}`, [
      btn('Add', (e) => busy(e.currentTarget, async () => {
        await api('POST', `/v1/live/${encodeURIComponent(x.sessionId)}/add`, slot != null ? { slot } : {});
        await load('state');
        location.hash = '#home';
      }, 'Added to the deck'), '', { 'aria-label': `Add ${x.title || x.folder || 'this session'} to the deck` }),
    ], { name: true })))));
  }
  const page = slot != null ? Math.floor(slot / SPOTS) : 0;
  const side = h('div', { class: 'agentside' }, h('span', { class: 'kl' }, 'It lands here'), miniDeck(page, null, slot ?? -1),
    h('span', { class: 'sub' }, slot != null ? `Page ${page + 1}, spot ${slot % SPOTS + 1}. The free spot you clicked.` : 'The first free spot.'));
  return [back, h('div', { class: 'deckrow' }, main, side)];
}

// ---- Ready for you: pieces Boards and Updates use too ---------------------------------------

// rcard: the card for something ready for you.
function rcard(vis, eyebrow, title, meta, acts, cls = '') {
  return h('div', { class: `rcard ${cls}`.trim() },
    vis ? h('div', { class: 'vis' }, vis) : null,
    h('div', { class: 'bd' },
      h('span', { class: 'eyebrow acc2' }, eyebrow),
      title ? h('span', { class: 'ttl' }, title) : null,
      meta ? h('span', { class: 'meta' }, meta) : null,
      acts && acts.length ? h('div', { class: 'act' }, acts) : null));
}

async function installApp(el) {
  const j = await busy(el, () => api('POST', '/admin/v1/update/apply', {}));
  if (j?.id) { putJob(j); S.appJob = j.id; await load('update'); draw(true); }
}

function waitingCard(d) {
  const how = d.connection === 'bluetooth' ? 'Over Bluetooth.' : d.connection === 'usb' ? 'Plugged in over USB.' : d.kind === 'network' ? 'Over the network.' : '';
  return rcard(devScreen(d, ''), 'Wants to join', d.name, [how, d.model ? `${d.model}.` : ''].filter(Boolean).join(' '), [
    btn('Approve', (e) => deviceAction(d, 'approve', {}, e.currentTarget, `${d.name} approved`), 'p'),
    btn('Not mine', (e) => deviceAction(d, 'remove', {}, e.currentTarget, `${d.name} removed`)),
  ]);
}

function newPortCard(p) {
  return rcard(h('span', { class: 'dash s72', 'aria-hidden': 'true' }, icon('usb', 26, 1.8)), 'New USB port', p.likely === 'esp32-usb' || p.likelyName ? 'ESP32 board' : 'USB serial port',
    `${p.path || p.id}. ${cap(portWords(p))}. Not opened yet.`, [
      btn('Set it up', () => { location.hash = `#new-board/${encodeURIComponent(p.id)}`; }, 'p'),
      btn('Not mine', (e) => portAction(p, 'forget', {}, e.currentTarget, 'It won\'t be asked about again')),
    ]);
}

// ---- General -----------------------------------------------------------------------------

async function patchSettings(body, btnEl) {
  const r = await busy(btnEl, () => api('PATCH', '/admin/v1/settings', body));
  if (!r) return;
  S.settings = r.settings;
  if (r.message) toast(r.message);
  else if (r.restartNeeded) toast('Saved. It applies when the service restarts.');
  else toast('Saved');
  draw(true);
  return r;
}

function drawGeneral() {
  const s = S.settings;
  const start = [];
  const login = 'Start when I log in';
  if (s.startAtLogin === null || s.startAtLogin === undefined) {
    start.push(row(login, 'This computer can\'t start it at login. Start the service in a terminal:', [toggle(login, false, () => {}, { disabled: true })],
      { name: true, pic: ico('power'), below: h('span', { class: 'term inl' }, h('span', { class: 'p' }, '$ '), h('span', { class: 'w' }, 'agentctl serve')) }));
  } else if (S.confirmLoginOff) {
    start.push(h('div', { class: 'row tint top' }, ico('power'), h('div', { class: 'grow' }, h('span', { class: 'eyebrow acc2' }, 'Turning off Start when I log in'),
      h('p', { class: 'txt' }, 'This also stops the service now. Boards show ', h('span', { class: 'mono' }, 'waiting for the deck'), ' until it starts again.')),
    h('div', { class: 'r' }, btn('Keep it on', () => { S.confirmLoginOff = false; draw(); }, '', { 'data-k': 'login-keep' }),
      btn('Turn off and stop', (e) => { S.confirmLoginOff = false; patchSettings({ startAtLogin: false }, e.currentTarget); }, 'd'))));
  } else {
    let what = 'The service. Without it, boards show "waiting for the deck".';
    if (s.trayAtLogin !== undefined) what = 'The service and the tray icon.';
    else if ((S.system?.platform || '').startsWith('darwin')) what = 'The service. To open the app at login too, add agents deck in System Settings, General, Login Items.';
    start.push(row(login, s.loginNote ? `${s.loginNote}.` : what, [toggle(login, s.startAtLogin, (on, el) => {
      if (!on) { S.confirmLoginOff = true; draw(); $('[data-k="login-keep"]')?.focus(); return; }
      patchSettings({ startAtLogin: true }, el);
    })], { name: true, pic: ico('power') }));
  }
  start.push(row('After a restart, bring agents back', 'The ones the deck started, where they stopped.', [
    seg('Bring agents back after a restart', [{ value: 'ask', text: 'Ask me' }, { value: 'auto', text: 'Automatically' }], s.restore, (v) => patchSettings({ restore: v })),
  ], { name: true, pic: ico('restart') }));

  const sample = agentsList().find((a) => a.status === 'waiting');
  const face = h('span', { class: 'face dk' });
  face.style.setProperty('--body', '#FF4D00');
  face.style.setProperty('--cut', '#141412');
  const spr = h('span', { class: 'spr c' });
  setSprite(spr, 'wait', sample?.tool || 'claude', sample?.hat || { shape: 'top hat', color: 'yellow' }, { bubble: false });
  face.append(spr);
  const notify = blk(
    h('div', { class: 'row notif' }, face, h('div', { class: 'grow' }, h('span', { class: 'nm' }, `${sample ? agentName(sample) : 'An agent'} needs you`), h('span', { class: 'sub' }, `${sample ? askOf(sample).q : 'Allow bash?'} · now`))),
    h('div', { class: 'row' },
      h('div', { class: 'field' }, h('span', { class: 'kl' }, 'Tell me'), seg('Notifications', [
        { value: 'off', text: 'Never' }, { value: 'attention', text: 'When one needs me' }, { value: 'finished', text: 'Also when done' },
      ], s.notifications, (v) => patchSettings({ notifications: v }))),
      h('div', { class: 'field' }, h('span', { class: 'kl' }, 'Sound'), h('div', { class: 'actions' },
        toggle('Play a sound', s.sound, (on, el) => patchSettings({ sound: on }, el), { disabled: s.notifications === 'off' }),
        h('span', { class: 'sub' }, 'Off while notifications are off.')))));

  const cli = s.cli || {};
  const cliBtn = cli.canChange
    ? btn(cli.installed ? 'Remove' : 'Install', (e) => busy(e.currentTarget, async () => {
      S.settings.cli = await api('POST', '/admin/v1/cli', { installed: !cli.installed });
      draw();
    }, cli.installed ? 'Removed the agentctl command' : 'Installed the agentctl command'), cli.installed ? '' : 'p')
    : null;
  const termChip = (...kids) => h('span', { class: 'term tchip', 'aria-hidden': 'true' }, ...kids);
  const term = blk(
    row('The agentctl command', cli.installed ? `Installed${cli.path ? ` at ${cli.path}` : ''}. Type agentctl in any terminal to start and list agents.` : 'Not installed. Install it to start and list agents from any terminal.', [cliBtn],
      { name: true, pic: termChip(h('span', { class: 'p' }, '$ '), h('span', { class: 'w' }, 'agentctl')) }),
    row('The deck in your browser', 'The same app, on this computer only.', [
      APP ? btn('Open in browser', () => { if (!appPost('dashboard')) location.href = '/'; }) : h('a', { class: 'btn sm', href: '/', target: '_blank', rel: 'noreferrer' }, 'Open in a new tab'),
    ], { name: true, pic: termChip('127.0.0.1') }));
  return [
    head('General', 'How the deck', 'starts.'),
    sec('Starting', null, null, blk(...start)),
    sec('Notifications', 'a click opens the agent', null, notify),
    sec('For the terminal and the browser', null, null, term),
  ];
}

// ---- Agents ------------------------------------------------------------------------------

// toolChip: is it connected, found, missing or off.
function toolChip(t) {
  const st = t.state || (t.on ? (t.agents ? 'connected' : t.found ? 'found' : 'missing') : 'off');
  const n = t.id === 'codex' ? plural(t.agents, 'session') : plural(t.agents, 'agent');
  switch (st) {
    case 'connected': return chip(n, 'ok st', 'check');
    case 'found': return chip('Found', 'ok st', 'check');
    case 'off': return chip('Turned off', 'off st');
    default: return chip('Not found', 'off st');
  }
}

function toolLine(t) {
  const st = t.state || '';
  if (st === 'found') return t.id === 'tmux' ? 'for agents you start here' : 'none on the deck';
  if (st === 'missing' || (!st && !t.found)) return 'install it to add it';
  if (st === 'off') return 'turned off in config.json';
  return t.detail || '';
}

function drawAgents() {
  const a = S.agents;
  const out = [head('Agents', 'Where agents come', 'from.')];
  const grid = h('div', { class: 'blk tools' });
  for (const t of a.tools) {
    const off = t.state === 'off' || t.state === 'missing' || (!t.state && (!t.found || !t.on));
    grid.append(h('div', { class: 'tool' },
      h('span', { class: `nm${off ? ' off' : ''}` }, t.name),
      h('span', { class: 'sub dt', title: t.detail || null }, toolLine(t)),
      h('span', {}, toolChip(t)),
      t.note ? h('span', { class: 'sub' }, t.note) : null));
  }
  out.push(sec('Tools on this computer', 'found by themselves', null, grid));
  out.push(projectsSection(a.accounts));

  const live = (S.live?.sessions || []).filter((s) => !s.onBoard);
  const rows = [];
  if (S.settings?.sessions) { // an older service has no such setting
    rows.push(row('Put them on the deck', 'From a terminal or the Claude app. Automatically: each one joins by itself. One you take off stays off.', [
      seg('Put sessions on the deck', [{ value: 'auto', text: 'Automatically' }, { value: 'pick', text: 'I choose' }], S.settings.sessions, (v) => patchSettings({ sessions: v })),
    ], { cls: 'tint' }));
  }
  if (S.errors.live) rows.push(row(`Couldn't list them: ${S.errors.live.message}`, null, [], { cls: 'thin' }));
  else if (!live.length) rows.push(h('div', { class: 'row thin' }, h('span', { class: 'txt dim' }, 'Nothing: every Claude session on this computer is on the deck.')));
  for (const s of live) {
    const where = s.background ? 'in the background' : 'in a terminal';
    rows.push(row(s.title || s.folder || 'Claude session', `Claude Code · started ${ago(s.started)} · ${where}`, [
      btn('Add to deck', (e) => busy(e.currentTarget, async () => {
        await api('POST', `/v1/live/${encodeURIComponent(s.sessionId)}/add`, {});
        await load('live'); draw();
      }, 'Added to the deck'), '', { 'aria-label': `Add ${s.title || s.folder || 'this session'} to the deck` }),
    ], { name: true, pic: h('span', { class: 'face' }, critter({ pose: 'idle', body: '#7A776F', cut: '#141412', centre: true })) }));
  }
  out.push(sec('Claude sessions started elsewhere', live.length ? `${live.length} not on the deck` : null, null, blk(...rows)));

  const acc = [];
  for (const ac of a.accounts) {
    const next = a.colors[(a.colors.indexOf(ac.color) + 1) % a.colors.length];
    const sw = h('span', { class: 'swatch', 'aria-hidden': 'true' });
    sw.style.background = (HAT_COLORS[ac.color] || HAT_COLORS.ink)[0];
    acc.push(row(ac.name, `${plural(ac.agents, 'agent')} · $${ac.costUsd.toFixed(2)} on the deck`, [
      ac.default ? chip('New agents start here') : btn('Start here', (e) => busy(e.currentTarget, async () => {
        await api('POST', `/admin/v1/accounts/${encodeURIComponent(ac.name)}/default`, {});
        await load('agents'); draw();
      }, `New agents start in ${ac.name}`), '', { 'aria-label': `Start new agents in ${ac.name}` }),
      btn('Colour', (e) => busy(e.currentTarget, async () => {
        await api('POST', `/admin/v1/accounts/${encodeURIComponent(ac.name)}/color`, { color: next });
        await load('agents'); draw();
      }), '', { 'aria-label': `Colour of ${ac.name}: ${ac.color}. Change to ${next}` }),
    ], { name: true, pic: sw }));
  }
  acc.push(h('div', { class: 'row thin' }, h('span', { class: 'sub' }, a.accounts.length ? 'Add another account in a terminal:' : 'One Claude account. To run agents in another, add it in a terminal:'),
    h('span', { class: 'mono w' }, h('span', { class: 'p' }, '$'), ' agentctl accounts add')));
  out.push(sec('Accounts', a.accounts.length ? plural(a.accounts.length, 'Claude account') : null, null, blk(...acc)));
  out.push(...mcpSection());
  return out;
}


// ---- Projects: the folders agents start in ----------------------------------------------------

let notProjects = [];
try { notProjects = JSON.parse(localStorage.getItem('settings.notProjects') || '[]'); } catch (e) { notProjects = []; }
const sameFolder = (cwd, folder) => !!cwd && (cwd === folder || tilde(cwd) === folder || tilde(cwd) === tilde(folder));
// A project's folders, in full where the service says (older ones have one).
const projFolders = (p) => (p.paths?.length ? p.paths : p.folders?.length ? p.folders : [p.path || p.folder]).filter(Boolean);
// An agent works in a project when its folder is one of the project's, or inside one (a worktree, a package).
const inFolder = (cwd, folder) => {
  if (!cwd || !folder) return false;
  const a = tilde(cwd).replace(/\/+$/, ''), b = tilde(folder).replace(/\/+$/, '');
  return a === b || a.startsWith(`${b}/`);
};
const inProject = (cwd, p) => projFolders(p).some((f) => inFolder(cwd, f));
const lastPart = (f) => tilde(f).split('/').filter(Boolean).pop() || f;

function projectForm(accs) {
  const f = S.projForm;
  const editing = f.orig;
  const err = h('p', { class: 'sub err', role: 'alert' });
  if (!f.folders?.length) f.folders = [f.folder || ''];
  const keep = () => { f.name = $('#pj-name')?.value ?? f.name; f.folders = f.folders.map((x, i) => $(`#pj-folder-${i}`)?.value ?? x); };
  const hat = f.hat || null;
  const shapeOpts = [{ value: '', text: 'The folder\'s own' }, ...HAT_SHAPES.map((x) => ({ value: x, text: x }))];
  const colors = HAT_COLOR_NAMES.map((color) => {
    const b = h('button', { type: 'button', class: 'colsw', role: 'radio', 'aria-checked': hat && hat.color === color ? 'true' : 'false', 'aria-label': `Colour: ${color}`, title: color, disabled: !hat,
      onclick: () => { keep(); f.hat = { shape: hat.shape, color }; draw(true); } });
    b.style.background = HAT_COLORS[color][0];
    return b;
  });
  const save = async (e) => {
    keep();
    err.textContent = '';
    const b = e.currentTarget;
    b.disabled = true;
    const body = { name: f.name, folders: f.folders.map((x) => x.trim()).filter(Boolean), tool: f.tool || '', account: f.tool && f.tool !== 'claude' ? '' : (f.account || ''), hat: f.hat || null };
    try {
      const r = editing ? await api('POST', `/admin/v1/projects/${encodeURIComponent(editing)}/update`, body) : await api('POST', '/admin/v1/projects', body);
      // Agents in a project wear its hat: it is each of its folders' hat.
      if (r?.hat) for (const folder of projFolders(r)) await api('POST', '/v1/hats', { folder, shape: r.hat.shape, color: r.hat.color }).catch(() => null);
      S.projForm = null; S.open.project = '';
      await load('projects');
      toast(editing ? 'Saved' : `Added ${r.name}`);
      draw(true);
    } catch (ex) { err.textContent = ex.message; } finally { if (b.isConnected) b.disabled = false; }
  };
  return h('div', { class: 'blk pad' },
    h('span', { class: 'eyebrow acc2' }, editing ? `Edit ${editing}` : 'New project'),
    h('div', { class: 'fields' },
      h('div', { class: 'field' }, h('label', { for: 'pj-name' }, 'Name'), h('input', { id: 'pj-name', type: 'text', value: f.name || '', maxlength: '40', autocomplete: 'off', 'data-k': 'pj-name' }))),
    h('div', { class: 'field' }, h('span', { class: 'kl' }, f.folders.length > 1 ? 'Folders · new agents start in the first' : 'Folder'),
      h('div', { class: 'folders' }, ...f.folders.map((x, i) => h('div', { class: 'folderrow' },
        h('label', { for: `pj-folder-${i}`, class: 'sr' }, `Folder ${i + 1}`),
        h('input', { id: `pj-folder-${i}`, type: 'text', value: x || '', placeholder: i ? '~/dev/my-app-server' : '~/dev/my-app', autocomplete: 'off', spellcheck: 'false', 'data-k': `pj-folder-${i}` }),
        f.folders.length > 1 ? h('button', { type: 'button', class: 'btn sm icon', title: 'Remove this folder', 'aria-label': `Remove folder ${i + 1}`, onclick: () => { keep(); f.folders.splice(i, 1); draw(true); } }, icon('x', 14)) : null)),
      f.folders.length < 20 ? h('button', { type: 'button', class: 'btn sm link', 'data-k': 'pj-folder-add', onclick: () => { keep(); f.folders.push(''); draw(true); $(`#pj-folder-${f.folders.length - 1}`)?.focus(); } }, icon('plus', 12, 2.2), 'Add a folder') : null)),
    h('div', { class: 'fields' },
      h('div', { class: 'field' }, h('span', { class: 'kl' }, 'Runs'), seg('Runs', [{ value: '', text: 'Claude Code' }, { value: 'codex', text: 'Codex' }, { value: 'shell', text: 'Shell' }], f.tool === 'claude' ? '' : (f.tool || ''), (v) => { keep(); f.tool = v; draw(true); }, { key: 'pj-tool' })),
      (!f.tool || f.tool === 'claude') && accs.length ? h('div', { class: 'field' }, h('span', { class: 'kl' }, 'Claude account'),
        seg('Claude account', [{ value: '', text: 'Default' }, ...accs.map((x) => ({ value: x.name, text: x.name }))], f.account || '', (v) => { keep(); f.account = v; draw(true); }, { key: 'pj-acc' })) : null),
    h('div', { class: 'fields' },
      h('div', { class: 'field' }, h('span', { class: 'kl' }, 'Hat'), h('div', { class: 'actions' },
        pickSel('Hat', shapeOpts, hat ? hat.shape : '', (v) => { keep(); f.hat = v ? { shape: v, color: (hat && hat.color) || 'blue' } : null; draw(true); }, 'pj-hat'),
        h('span', { class: 'hatprev dk' }, critter({ pose: 'hi', bot: f.tool === 'codex', hat, body: '#FF4D00', cut: '#222220', centre: true })))),
      h('div', { class: 'field' }, h('span', { class: 'kl' }, 'Colour'), h('div', { class: 'hatrow', role: 'radiogroup', 'aria-label': 'Hat colour' }, colors))),
    err,
    h('div', { class: 'actions' }, btn(editing ? 'Save' : 'Add the project', save, 'p', { 'data-k': 'pj-save' }),
      btn('Cancel', () => { S.projForm = null; S.open.project = ''; draw(true); }),
      editing ? h('span', { class: 'sp' }) : null,
      editing ? btn('Remove', (e) => busy(e.currentTarget, async () => {
        await api('POST', `/admin/v1/projects/${encodeURIComponent(editing)}/remove`, {});
        S.projForm = null; S.open.project = '';
        await load('projects'); draw(true);
      }, `Removed ${editing}`), 'd') : null));
}

function projectsSection(accs) {
  if (S.errors.projects || !S.projects) return null; // an older service has no projects
  const list = S.projects.projects || [];
  const agents = agentsList();
  const content = [h('div', { class: 'intro' }, h('p', { class: 'txt' }, 'What your agents work on: one or more folders each (an app, its website, its server). Start agents in one from Home; agents in a project share its hat.'),
    S.projForm ? null : h('button', { type: 'button', class: 'btn p', 'data-k': 'pj-add', onclick: () => { S.projForm = { tool: '', account: '' }; S.open.project = 'new'; draw(true); $('#pj-name')?.focus(); } }, icon('plus', 14, 2.2), 'Add a project'))];
  if (S.projForm) content.push(projectForm(accs));
  const rows = list.map((p) => {
    const n = agents.filter((a) => inProject(a.cwd, p)).length;
    const fs = p.folders?.length ? p.folders : [p.folder];
    const acc = accs.find((x) => x.name === p.account);
    const accChip = p.account ? h('span', { class: 'chip st' }, h('i', { 'aria-hidden': 'true' }), p.account) : h('span', { class: 'chip st off' }, 'Default account');
    if (acc) accChip.firstChild.style.background = (HAT_COLORS[acc.color] || HAT_COLORS.ink)[0];
    return row(p.name, `${fs.length > 1 ? fs.map(lastPart).join(', ') : p.folder} · ${n ? plural(n, 'agent') : 'no agents'}`, [
      chip(TOOL_NAMES[p.tool || 'claude'], 'st'), !p.tool || p.tool === 'claude' ? accChip : null,
      link('Start agent', `#start//${encodeURIComponent(p.name)}`, '', { 'aria-label': `Start an agent in ${p.name}` }),
      btn('Edit', () => { S.projForm = { ...p, folders: [...fs], orig: p.name, hat: p.hat ? { ...p.hat } : null }; S.open.project = p.name; draw(true); $('#pj-name')?.focus(); }, '', { 'aria-label': `Edit ${p.name}` }),
    ], { name: true, pic: projectFace(p) });
  });
  // Folders agents work in that aren't projects yet.
  const seen = [...new Set(agents.map((a) => a.cwd).filter(Boolean))]
    .filter((cwd) => !list.some((p) => inProject(cwd, p)) && !notProjects.includes(cwd)).slice(0, 3);
  for (const cwd of seen) {
    const name = cwd.split('/').filter(Boolean).pop() || cwd;
    rows.push(row(name, `${tilde(cwd)} · seen on the deck`, [
      btn('Keep it', (e) => busy(e.currentTarget, async () => {
        await api('POST', '/admin/v1/projects', { name: name.slice(0, 40), folder: cwd });
        await load('projects'); draw(true);
      }, `Added ${name}`), '', { 'aria-label': `Keep ${name} as a project` }),
      list.length ? pickSel(`Add ${name} to a project`, list.map((p) => ({ value: p.name, text: p.name })), '', async (v) => {
        const p = list.find((x) => x.name === v);
        if (!p) return;
        try {
          await api('POST', `/admin/v1/projects/${encodeURIComponent(p.name)}/update`, { ...p, folders: [...(p.folders?.length ? p.folders : [p.folder]), cwd], path: undefined, paths: undefined });
          if (p.hat) await api('POST', '/v1/hats', { folder: cwd, shape: p.hat.shape, color: p.hat.color }).catch(() => null);
          toast(`Added to ${p.name}`);
        } catch (ex) { toast(ex.message); }
        await load('projects'); draw(true);
      }, `pj-to-${name}`, { empty: 'Add to…' }) : null,
      btn('Not a project', () => {
        notProjects = [...notProjects, cwd].slice(-100);
        try { localStorage.setItem('settings.notProjects', JSON.stringify(notProjects)); } catch (e) { /* private window */ }
        draw(true);
      }),
    ], { name: true, off: true, cls: 'tint', pic: h('span', { class: 'face' }, critter({ pose: 'idle', body: '#7A776F', cut: '#141412', centre: true })) }));
  }
  if (rows.length) content.push(blk(...rows));
  const s = sec('Projects', list.length ? plural(list.length, 'project') : null, null, ...content);
  s.id = 'projects';
  return s;
}

// ---- MCP: tokens that let an agent run the deck's agents ------------------------------

const MCP_SCOPES = [
  { value: 'view', text: 'Look only', title: 'See the agents and what they ask' },
  { value: 'act', text: 'Can answer', title: 'Also send prompts, answer questions and interrupt' },
  { value: 'admin', text: 'Can manage', title: 'Also start new agents and watch other sessions' },
];

// A command with the token cut short on screen; Copy copies it whole.
const shortToken = (cmd, token) => (token ? cmd.split(token).join(`${token.slice(0, 4)}…`) : cmd);

function cmdBlock(title, cmd, token, label) {
  return h('div', { class: 'term' }, h('div', { class: 'bar' }, h('i', {}), title),
    h('div', { class: 'ln' }, h('div', { class: 'code' }, h('span', { class: 'p' }, '$ '), shortToken(cmd, token)),
      h('button', { type: 'button', class: 'btn xs', 'aria-label': label, onclick: () => copied(cmd) }, icon('copy', 13), 'Copy')));
}

function mcpSection() {
  if (S.errors.mcp || !S.mcp) return []; // an older service has no MCP
  const n = S.mcpNew;
  const tokens = S.mcp.tokens || [];
  const content = [];
  content.push(h('div', { class: 'intro' },
    h('p', { class: 'txt' }, 'Let one agent see the others, give them tasks and answer their questions. A token works on this computer only, and what it does is logged under its name.'),
    n ? null : h('button', { type: 'button', class: 'btn p', 'data-k': 'mcp-new', onclick: () => { S.mcpNew = { name: 'claude', scope: 'act' }; draw(); $('#mcp-name')?.focus(); } }, icon('plus', 14, 2.2), 'New token')));
  content.push(h('div', { class: 'term' }, h('div', { class: 'ln' }, h('span', { class: 'kl' }, 'Clients connect to'), h('span', { class: 'w' }, S.mcp.url),
    h('button', { type: 'button', class: 'btn xs', 'aria-label': 'Copy the address', onclick: () => copied(S.mcp.url) }, icon('copy', 13), 'Copy'))));
  if (n && n.token) {
    const stdio = n.commands?.stdio || '';
    const user = stdio.replace(/^claude mcp add /, 'claude mcp add -s user ');
    content.push(h('div', { class: 'blk pad' },
      h('span', { class: 'eyebrow acc2' }, 'Token made · shown only once'),
      h('p', { class: 'txt' }, `This token lets ${n.name} see and steer your agents (${scopeLower(n.scope)}). Copy it now: it is shown only once.`),
      h('div', { class: 'term hot' }, h('div', { class: 'bar' }, h('i', {}), `token · ${n.name} · ${scopeLower(n.scope)}`),
        h('div', { class: 'ln' }, h('span', { class: 'w', 'aria-label': 'The new token' }, n.token),
          h('button', { type: 'button', class: 'btn xs p', onclick: () => copied(n.token, 'Copied the token') }, icon('copy', 13), 'Copy token'))),
      h('span', { class: 'kl' }, 'Add it to Claude Code'),
      user ? cmdBlock('For all your projects', user, n.token, 'Copy the command for all your projects') : null,
      stdio ? cmdBlock('For one folder · run it inside that folder', stdio, n.token, 'Copy the command for one folder') : null,
      h('span', { class: 'kl' }, 'Other clients, over HTTP'),
      h('div', { class: 'term' },
        h('div', { class: 'ln' }, h('span', { class: 'k' }, 'address'), h('span', { class: 'w' }, n.url || S.mcp.url),
          h('button', { type: 'button', class: 'btn xs', 'aria-label': 'Copy the address', onclick: () => copied(n.url || S.mcp.url) }, icon('copy', 13), 'Copy')),
        h('div', { class: 'ln' }, h('span', { class: 'k' }, 'header'), h('span', { class: 'w' }, `Authorization: Bearer ${n.token.slice(0, 4)}…`),
          h('button', { type: 'button', class: 'btn xs', 'aria-label': 'Copy the header', onclick: () => copied(`Authorization: Bearer ${n.token}`) }, icon('copy', 13), 'Copy')),
        n.commands?.http ? h('div', { class: 'ln' }, h('span', { class: 'k' }, 'claude'), h('span', { class: 'code' }, shortToken(n.commands.http, n.token)),
          h('button', { type: 'button', class: 'btn xs', 'aria-label': 'Copy the command for HTTP', onclick: () => copied(n.commands.http) }, icon('copy', 13), 'Copy')) : null),
      h('div', { class: 'actions' }, btn('Done', () => { S.mcpNew = null; draw(); }, 'p', { 'data-k': 'mcp-done' }))));
  } else if (n) {
    const err = h('p', { class: 'sub err', role: 'alert' });
    content.push(h('div', { class: 'blk pad' },
      h('span', { class: 'eyebrow acc2' }, 'New token'),
      h('div', { class: 'fields' },
        h('div', { class: 'field' }, h('label', { for: 'mcp-name' }, 'A name for it'), h('input', { id: 'mcp-name', type: 'text', autocomplete: 'off', value: n.name || 'claude', 'data-k': 'mcp-name', maxlength: '64' })),
        h('div', { class: 'field' }, h('span', { class: 'kl' }, 'It may'),
          seg('What the token may do', MCP_SCOPES, n.scope, (v) => { n.scope = v; n.name = $('#mcp-name').value; draw(true); }, { key: 'mcp-scope' }),
          h('span', { class: 'sub' }, n.scope === 'admin' ? 'Also starts new agents and watches other sessions.' : n.scope === 'act' ? 'Answers, prompts and interrupts. Starting agents needs Can manage.' : 'Reads only.'))),
      err,
      h('div', { class: 'actions' },
        btn('Make the token', async (e) => {
          err.textContent = '';
          const b = e.currentTarget;
          b.disabled = true;
          try {
            const r = await api('POST', '/admin/v1/mcp/tokens', { name: $('#mcp-name').value.trim(), scope: n.scope });
            S.mcpNew = r;
            await load('mcp');
            draw(true);
          } catch (ex) {
            err.textContent = ex.message;
          } finally {
            if (b.isConnected) b.disabled = false;
          }
        }, 'p'),
        btn('Cancel', () => { S.mcpNew = null; draw(true); }))));
  }
  if (tokens.length) {
    content.push(blk(...tokens.map((t) => {
      const used = t.lastUsed ? `used ${ago(t.lastUsed)}` : 'never used';
      const meta = t.revoked ? `last ${used}` : `made ${ago(t.created)} · ${used}`;
      return row(t.name, meta, [
        t.revoked ? chip('Revoked', 'off st') : chip(scopeWords(t.scope), 'acc st'),
        t.revoked
          ? btn('Remove', (e) => busy(e.currentTarget, async () => {
            await api('POST', `/admin/v1/mcp/tokens/${encodeURIComponent(t.name)}/remove`, {});
            await load('mcp'); draw();
          }, 'Removed'), '', { 'aria-label': `Remove ${t.name}` })
          : btn('Revoke', (e) => busy(e.currentTarget, async () => {
            await api('POST', `/admin/v1/mcp/tokens/${encodeURIComponent(t.name)}/revoke`, {});
            await load('mcp'); draw();
          }, 'Revoked: it stops working now'), 'd', { 'aria-label': `Revoke ${t.name}` }),
      ], { name: true, off: t.revoked, pic: ico('key') });
    })));
  }
  return [sec('MCP access', tokens.length ? plural(tokens.length, 'token') : null, null, ...content)];
}

// ---- devices: actions --------------------------------------------------------------------

async function deviceAction(d, action, body, btnEl, okMsg) {
  const r = await busy(btnEl, () => api('POST', `/admin/v1/devices/${encodeURIComponent(d.id)}/${action}`, body), okMsg);
  await loadAll('devices', 'system');
  showSystem(S.system);
  if (action === 'remove' && r !== undefined && S.page === 'board' && S.arg === d.id) { location.hash = d.kind === 'network' ? '#network' : '#boards'; return r; }
  draw(true);
  return r;
}

// updateDevice installs what the board's line offers: the latest release,
// or the build made on this computer (source local).
async function updateDevice(d, btnEl) {
  const st = d.status || d.update || {};
  const body = st.source === 'local' ? { source: 'local', version: st.latest || st.version } : {};
  const job = await busy(btnEl, () => api('POST', `/admin/v1/devices/${encodeURIComponent(d.id)}/update`, body));
  if (job && job.id) {
    putJob(job);
    location.hash = `#flash/${job.id}`;
  }
}

function wifiForm(d, onDone) {
  const ssidId = `ssid-${d.id}`, pwId = `pw-${d.id}`;
  const err = h('p', { class: 'sub err', role: 'alert' });
  return h('div', { class: 'blk pad' },
    h('div', { class: 'fields' },
      h('div', { class: 'field' }, h('label', { for: ssidId }, 'Network name'), h('input', { id: ssidId, type: 'text', autocomplete: 'off', value: S.settings?.wifi?.ssid || S.devices?.wifiDefault || '', 'data-k': ssidId })),
      h('div', { class: 'field' }, h('label', { for: pwId }, 'Password'), h('input', { id: pwId, type: 'password', autocomplete: 'new-password', 'data-k': pwId }))),
    err,
    h('div', { class: 'actions' },
      h('span', { class: 'sub' }, `Sent over ${d.connection === 'bluetooth' ? 'the Bluetooth link' : 'the USB cable'}. The password is never kept on this computer.`),
      h('div', { class: 'r' },
        btn('Cancel', () => { S.open.wifi = ''; draw(); }),
        btn('Send', async (e) => {
          err.textContent = '';
          const b = e.currentTarget;
          b.disabled = true;
          try {
            const r = await api('POST', `/admin/v1/devices/${encodeURIComponent(d.id)}/wifi`, { ssid: $(`#${CSS.escape(ssidId)}`).value.trim(), password: $(`#${CSS.escape(pwId)}`).value });
            S.open.wifi = '';
            toast(r?.message || 'Sent');
            if (onDone) await onDone();
            draw(true);
          } catch (ex) {
            err.textContent = ex.message;
            if (b.isConnected) b.disabled = false;
          }
        }, 'p'))));
}

// ---- a board's firmware: updates, progress, and a file of your own -------------

// fwText: the status line in the page's words.
function fwText(st) {
  switch (st.state) {
    case 'update':
    case 'local': return st.ready ? `${st.text} · ${st.note}` : st.text;
    case 'current': return `Up to date · ${st.latest} is the latest release`;
    case 'nokey': return `Release ${st.latest} is out · this copy installs firmware you build: agentctl firmware add <folder>`;
    case 'error': return 'Couldn\'t check for updates';
    default: return st.text;
  }
}

// The stages an update over Wi-Fi reports, in words.
const OTA_WORDS = { offered: 'offered, waiting for the board', downloading: 'downloading', verifying: 'checking the firmware', restarting: 'restarting into it' };

function jobLine(d, j) {
  if (!j) return h('div', { class: 'fwline' }, h('span', { class: 'fw-t', role: 'status' }, 'An update is running'), link('Show progress', `#flash/${d.job}`));
  const what = j.port ? ({ download: 'downloading', connect: 'waiting for the board', erase: 'erasing', write: 'writing', verify: 'checking', reset: 'restarting' }[j.stage] || 'installing')
    : (OTA_WORDS[j.stage] || 'updating');
  const bar = h('div', {});
  bar.style.width = `${j.percent}%`;
  return h('div', { class: 'fwline' },
    h('span', { class: 'fw-t', role: 'status' }, `${j.file ? `Installing ${j.file}` : `Updating to ${j.version}`} · ${what} · ${j.percent}%`),
    h('div', { class: 'bar mini', role: 'progressbar', 'aria-label': `${d.name}: ${what}`, 'aria-valuenow': j.percent, 'aria-valuemin': '0', 'aria-valuemax': '100' }, bar),
    link('Show progress', `#flash/${j.id}`));
}

// A firmware file picked on this computer goes to a target: a board of
// yours (the device calls, over USB or Wi-Fi), or in "Set up a new board"
// the board on a USB port that has no agents deck firmware yet (the port
// calls, a full image over USB). key names its state in S.file and its
// elements; about is the board's line in the confirmation.
function deviceFileTarget(d) {
  return {
    key: d.id, name: d.name, base: `/admin/v1/devices/${encodeURIComponent(d.id)}`,
    about: `${d.name}${d.model ? ` · ${d.model}` : ''} · runs ${d.firmware || 'unknown firmware'}`,
  };
}

function portFileTarget(p, id) {
  const fs = id.flashSize ? ` · ${Math.round(id.flashSize / 1048576)} MB flash` : '';
  return {
    key: `port-${p.id}`, name: 'the board', base: `/admin/v1/ports/${encodeURIComponent(p.id)}`, port: true,
    about: `${id.chip}${fs} on USB port ${p.id} · no agents deck firmware yet`,
    onJob: (job) => { if (S.nb && S.nb.port === p.id) S.nb.job = job.id; },
  };
}

// fileButton: the hidden file input and the button that opens it. note
// says why it can't be used now.
function fileButton(t, canFile, why, kind = '') {
  const input = h('input', {
    type: 'file', accept: '.bin,application/octet-stream', class: 'sr', id: `file-${t.key}`, tabindex: '-1', 'aria-hidden': 'true',
    onchange: (e) => { const f = e.currentTarget.files[0]; if (f) uploadFile(t, f); },
  });
  return [input, btn('Choose file…', () => input.click(), kind, {
    disabled: !canFile || !!S.file[t.key], 'data-k': `pick-${t.key}`, title: canFile ? 'Install a .bin you built or downloaded' : why,
    'aria-label': 'Install a firmware file', 'aria-describedby': canFile ? null : `fwn-${t.key}`,
  })];
}

const MAX_FILE = 32 * 1048576;

async function uploadFile(t, f) {
  const st = { stage: 'reading', name: f.name, size: f.size };
  S.file[t.key] = st;
  draw(true);
  if (f.size > MAX_FILE) { st.stage = 'error'; st.error = 'The file is bigger than 32 MB, more than any board\'s flash.'; draw(true); return; }
  try {
    const r = await fetch(`${t.base}/firmware-file`, {
      method: 'POST', credentials: 'same-origin', cache: 'no-store',
      headers: { 'Content-Type': 'application/octet-stream', 'X-File-Name': encodeURIComponent(f.name) }, body: f,
    });
    if (r.status === 401) { showExpired(); delete S.file[t.key]; return; }
    const data = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(data.error || r.statusText);
    Object.assign(st, { stage: 'confirm', up: data, version: data.version || '' });
  } catch (e) {
    st.stage = 'error';
    st.error = e instanceof TypeError ? 'The service isn\'t answering. It may be restarting.' : e.message;
  }
  if (S.file[t.key] === st) draw(true);
}

async function installFile(t, st, btnEl) {
  const input = $(`#fv-${CSS.escape(t.key)}`);
  const version = input ? input.value.trim() : st.version;
  if (st.up.via === 'wifi' && !version) { st.error = 'Give the version the firmware reports.'; draw(true); input?.focus(); return; }
  st.error = '';
  const job = await busy(btnEl, async () => {
    try {
      return await api('POST', `${t.base}/install-file`, t.port ? { file: st.up.file } : { file: st.up.file, version });
    } catch (e) {
      if (e.status === 409 && e.data.job) return api('GET', `/admin/v1/jobs/${encodeURIComponent(e.data.job)}`);
      st.error = e.message;
      draw(true);
      return undefined;
    }
  });
  if (job && job.id) {
    delete S.file[t.key];
    putJob(job);
    t.onJob?.(job);
    location.hash = `#flash/${job.id}`;
  }
}

// filePanel: picking a file, what was read from it, and the confirmation.
function filePanel(t, st) {
  const close = () => { delete S.file[t.key]; draw(true); };
  const box = h('div', { class: `blk pad${st.stage === 'error' ? ' err' : ''}`, 'data-row': `file-${t.key}` });
  if (st.stage === 'reading') {
    box.append(h('p', { class: 'txt', role: 'status' }, `Reading ${st.name} (${mb(st.size)} MB)…`), note('Nothing is sent to the board until you confirm.'));
    return box;
  }
  if (st.stage === 'error') {
    box.append(h('span', { class: 'nm wrap' }, `${st.name} can't go on ${t.name}`), h('p', { class: 'txt', role: 'alert' }, st.error),
      note('Nothing was sent to the board.'),
      h('div', { class: 'actions' },
        btn('Pick another file', () => { delete S.file[t.key]; draw(true); $(`#file-${CSS.escape(t.key)}`)?.click(); }, 'p', { 'data-k': `fa-${t.key}` }),
        btn('Close', close, '', { 'data-k': `fx-${t.key}` })));
    return box;
  }
  const up = st.up;
  const wifi = up.via === 'wifi';
  const vId = `fv-${t.key}`;
  const facts = h('dl', { class: 'facts' },
    h('dt', {}, 'board'), h('dd', {}, t.about),
    h('dt', {}, 'file'), h('dd', {}, `${up.name} · ${mb(up.size)} MB · ${up.kind === 'app' ? 'app image' : 'full image'}`),
    h('dt', {}, 'chip'), h('dd', {}, up.chipChecked ? `${up.chip}, the same as the board's` : `${up.chip} (the board's chip isn't known here, so it isn't compared)`),
    h('dt', {}, 'version'), h('dd', {}, up.version ? `${up.version}, read from the file`
      : wifi ? h('span', {}, h('label', { for: vId, class: 'sr' }, 'Version the firmware reports'),
        h('input', { id: vId, type: 'text', value: st.version, placeholder: 'like 0.2.0-dev.5', maxlength: '23', autocomplete: 'off', spellcheck: 'false', 'data-k': vId,
          oninput: (e) => { st.version = e.currentTarget.value; } }))
        : 'not in the file'),
    h('dt', {}, 'how'), h('dd', {}, wifi
      ? 'Over Wi-Fi. The board downloads it over its encrypted connection, checks it and restarts into it. If it can\'t reach the deck afterwards, it goes back to the firmware it runs now.'
      : t.port ? 'Over USB, written at 0x0 through the cable: bootloader, partition table and app. Keep it plugged in; the screen stays dark until it restarts, then it pairs.'
        : 'Over USB, written at 0x0 through the cable. Keep it plugged in; the screen stays dark until it restarts.'));
  box.append(h('span', { class: 'eyebrow acc2' }, 'Install a firmware file'), h('span', { class: 'nm wrap' }, `Install ${up.name} on ${t.name}?`), facts);
  if (wifi && !up.version) {
    box.append(note('The file doesn\'t say its version. Type the one the firmware reports (AGENTS_DECK_VERSION when it was built), so the deck can tell when the board is back on it.'));
  }
  box.append(note('This file isn\'t checked against a release. Install it only if you built it or trust where it came from. The board still checks that the firmware is for it and arrived whole.', 'warn'));
  if (st.error) box.append(h('p', { class: 'note err', role: 'alert' }, st.error));
  box.append(h('div', { class: 'actions' },
    btn(wifi ? 'Install over Wi-Fi' : 'Install over USB', (e) => installFile(t, st, e.currentTarget), 'p', { 'data-k': `fi-${t.key}` }),
    btn('Cancel', close, '', { 'data-k': `fc-${t.key}` })));
  return box;
}

function portWords(p) {
  if (p.likely === 'esp32-usb') return 'looks like an ESP32-S3';
  if (p.likelyName) return `looks like an ESP32 board (${p.likelyName})`;
  return p.product || 'a USB serial port';
}

async function portAction(p, action, body, btnEl, okMsg) {
  const r = await busy(btnEl, () => api('POST', `/admin/v1/ports/${encodeURIComponent(p.id)}/${action}`, body), okMsg);
  await loadAll('ports', 'system');
  showSystem(S.system);
  draw(true);
  return r;
}

// ---- Boards: the tab bar, your boards, add a board, magic trick, USB ports ------------------

const TABS = [['', 'Your boards'], ['add', 'Add a board'], ['magic', 'Magic trick'], ['ports', 'USB ports']];

function boardTabs(cur) {
  const devs = S.devices?.devices || [];
  const boards = devs.filter((d) => d.kind === 'serial' && d.state !== 'waiting');
  const ports = (S.ports?.ports || []).filter((p) => p.state === 'new' || p.registered || p.state === 'ignored' || p.state === 'other');
  const counts = { '': boards.length, ports: ports.length };
  const nav = h('nav', { class: 'tabs', 'aria-label': 'Boards' });
  for (const [k, text] of TABS) {
    const c = counts[k];
    const hot = k === '' && boardsWaiting(S.system) > 0;
    nav.append(h('a', { href: k ? `#boards/${k}` : '#boards', class: k === 'magic' ? 'magic' : '', 'aria-current': k === cur ? 'page' : null },
      k === 'magic' ? icon('magic', 14) : null, text,
      c !== undefined ? h('span', { class: `c${hot ? ' hot' : ''}`, 'aria-label': hot ? `${boardsWaiting(S.system)} waiting` : `${c}` }, hot ? String(boardsWaiting(S.system)) : String(c)) : null));
  }
  return nav;
}

function boardCard(d) {
  const off = !d.online || d.state === 'revoked';
  const upd = hasUpdate(d) && !d.job;
  return h('a', { class: 'board', href: `#board/${encodeURIComponent(d.id)}`, 'aria-label': `${d.name}${upd ? ', update ready' : ''}` },
    h('span', { class: 'stage' }, h('span', {}, devScreen(d, 'lg'), upd ? h('span', { class: 'upd', 'aria-hidden': 'true' }, 'UPDATE') : null)),
    h('b', { class: off ? 'off' : '', title: d.name }, d.name),
    connChip(d));
}

function drawBoardsYours() {
  const out = [];
  const ports = S.ports.ports || [];
  const devs = S.devices.devices || [];
  const cards = [];
  devs.filter((x) => x.state === 'waiting').forEach((d) => cards.push(waitingCard(d)));
  ports.filter((x) => x.state === 'new').forEach((p) => cards.push(newPortCard(p)));
  if (cards.length) out.push(sec('Waiting for you', 'nothing opens a board until you say so', null, h('div', { class: 'pair' }, cards)));
  for (const j of S.jobList?.jobs || []) putJob(j);
  const boards = devs.filter((d) => d.kind === 'serial' && d.state !== 'waiting');
  const active = boards.some((d) => d.state === 'active');
  if (active && S.devices.releaseKey === false && !devs.some((d) => d.status?.state === 'local')) {
    out.push(h('div', { class: 'blk pad notice', 'data-k': 'nokey' },
      h('p', { class: 'ttl' }, 'This copy installs firmware you build on this computer, not releases.'),
      note('It was built from source, so it can\'t check that firmware from a release is genuine and never installs one. ' +
        'Build the firmware, then run agentctl firmware add <folder> with the folder the release script wrote: a board that runs an older version gets an Install button. ' +
        'A board\'s page also installs a single .bin.')));
  }
  if (active && S.devices.catalogError && !S.devices.latest) out.push(note(`Couldn't check for updates: ${S.devices.catalogError}`, 'err'));
  const online = boards.filter((d) => d.online).length;
  const grid = h('div', { class: 'boards' }, boards.map(boardCard),
    h('a', { class: 'board', href: '#boards/add' }, h('span', { class: 'stage' }, h('span', { class: 'dash s92', 'aria-hidden': 'true' }, icon('plus', 26, 1.8))),
      h('b', { class: 'off' }, 'ADD'), chip('USB · BT', 'off st')));
  const yours = sec('Your boards', boards.length ? `${online} of ${boards.length} online` : null, null, grid);
  if (!boards.length && !cards.length) {
    yours.append(note('No boards yet. Plug one in with a USB cable that carries data (many charging cables don\'t). It shows up here within a few seconds; nothing opens it until you say it\'s yours.'
      + (S.bt?.available ? ' A board with Bluetooth can be added without a cable: Add a board.' : '')));
  }
  out.push(yours);
  return out;
}

function drawBoardsAdd() {
  const bt = S.bt;
  const usb = h('div', { class: 'blk pad' }, h('div', { class: 'line top' }, ico('usb'), h('div', { class: 'grow' }, h('span', { class: 'nm' }, 'Plug it in over USB'),
    h('span', { class: 'txt' }, 'Use a cable that carries data. It shows up under Waiting for you, on Your boards, in a few seconds.'))));
  let btBlock;
  if (!bt) btBlock = h('div', { class: 'blk pad' }, h('span', { class: 'nm' }, 'Bluetooth'), note('This service has no Bluetooth.'));
  else if (!bt.available) {
    btBlock = h('div', { class: 'blk pad' }, h('div', { class: 'line' }, ico('bluetooth'), h('span', { class: 'nm wrap grow' }, 'Look for Bluetooth boards'), chip('Off', 'off st')),
      note(bt.why || 'Bluetooth isn\'t available.'), link('Network: Bluetooth', '#network', 'link small'));
  } else {
    const list = (S.btBoards?.boards || []);
    btBlock = h('div', { class: 'blk pad' },
      h('div', { class: 'line' }, ico('bluetooth'), h('div', { class: 'grow' }, h('span', { class: 'nm wrap' }, 'Look for Bluetooth boards'),
        h('span', { class: 'sub' }, bt.scanning ? 'Looking…' : 'A board in setup shows its name, deck- and six letters or digits, on its screen.')),
      bt.scanning ? chip('Looking', 'acc', null, { role: 'status' }) : null,
      toggle('Look for boards over Bluetooth', bt.scanning, (on, el) => btScan(on, el), { key: 'bt-scan' })));
    const adding = Object.keys(S.btAdd).filter((id) => !list.some((b) => b.id === id));
    for (const b of list) {
      if (b.device && S.jobs[S.btAdd[b.id]] && (S.devices?.devices || []).some((d) => d.id === b.device)) continue; // its page shows the add
      btBlock.append(nearbyRow(b));
    }
    for (const id of adding) { // a board being added that the list no longer shows
      const j = S.jobs[S.btAdd[id]];
      if (j) btBlock.append(addFlow({ id, name: 'the board' }, j));
    }
    if (bt.scanning && !list.length) btBlock.append(h('p', { class: 'note', role: 'status' }, 'Nothing yet. Put the board in setup from its screen and bring it closer.'));
  }
  return [sec('Add a board', null, null, h('div', { class: 'pair' }, usb, btBlock))];
}

function drawBoardsPorts() {
  const ports = S.ports.ports || [];
  const rows = [];
  for (const p of ports.filter((x) => x.state === 'new')) {
    rows.push(row(p.id, `New · ${portWords(p)} · not opened yet`, [
      btn('Set it up', () => { location.hash = `#new-board/${encodeURIComponent(p.id)}`; }, 'p'),
      btn('Not mine', (e) => portAction(p, 'forget', {}, e.currentTarget, 'It won\'t be asked about again')),
    ], { name: true, pic: ico('usb', 'acc') }));
  }
  for (const p of ports.filter((x) => x.registered)) {
    const meta = p.device ? `Registered · ${p.name || p.device}` : ({
      registered: 'Registered · waiting for the board to say hello',
      busy: `Registered · ${p.detail || 'in use by another program'}`,
      flashing: 'The service is writing to the board',
      unplugged: 'Registered · not plugged in',
    }[p.state] || p.detail || p.state);
    const right = [];
    if (p.state === 'flashing' && p.job) right.push(link('Show progress', `#flash/${p.job}`));
    if (!p.device && p.state !== 'flashing' && p.state !== 'unplugged') right.push(btn('Set it up', () => { location.hash = `#new-board/${encodeURIComponent(p.id)}`; }));
    if (p.state !== 'flashing') right.push(btn('Forget', (e) => portAction(p, 'forget', {}, e.currentTarget, 'The deck no longer opens it')));
    rows.push(row(p.id, meta, right, { name: true, off: p.state === 'unplugged', pic: ico('usb') }));
  }
  for (const p of ports.filter((x) => x.state === 'ignored' || x.state === 'other')) {
    rows.push(row(p.id, p.state === 'ignored' ? `You said it isn't a board · ${portWords(p)}` : cap(portWords(p)), [
      btn('It\'s a board', () => { location.hash = `#new-board/${encodeURIComponent(p.id)}`; }),
    ], { name: true, off: true, pic: ico('usb') }));
  }
  if (!rows.length) rows.push(h('div', { class: 'row thin' }, h('span', { class: 'txt dim' }, 'No USB serial ports right now.')));
  return [sec('USB ports', 'advanced', null, h('p', { class: 'txt' }, 'Forget a port, or tell the deck a port is a board.'), blk(...rows))];
}

function drawBoards() {
  const tab = TABS.some(([k]) => k === S.arg) ? S.arg : '';
  const body = { '': drawBoardsYours, add: drawBoardsAdd, magic: drawMagic, ports: drawBoardsPorts }[tab]();
  return [h('h1', { class: 'sr' }, 'Boards'), boardTabs(tab), ...body];
}

// ---- a board's page ------------------------------------------------------------------------

// The uplink preference: what the board uses when it isn't on USB.
const UPLINKS = [{ value: 'auto', text: 'Auto', icon: 'auto', title: 'USB, else Wi-Fi, else Bluetooth' }, { value: 'wifi', text: 'Wi-Fi', icon: 'wifi' }, { value: 'ble', text: 'Bluetooth', icon: 'bluetooth' }];

function upChip(u) {
  const cls = u.state === 'in use' ? 'ok' : u.state === 'connecting' ? 'acc' : (u.state === 'off' || u.state === 'not used' || u.state === 'not connected') ? 'off' : '';
  return chip(`${VIA[u.via] || u.via} ${u.state}`, cls, null, { title: u.detail || null, role: 'listitem' });
}

// A status dot: on (green), wait (orange), standby (hollow), off (grey).
const dot = (kind, label) => h('i', { class: `dot ${kind}`, role: 'img', 'aria-label': label, title: label });
const LANE_STATE = { 'in use': ['on', 'in use'], 'standing by': ['sb', 'standby'], connecting: ['wait', 'connecting'], 'not used': ['off', 'off'], off: ['off', 'off'], 'not connected': ['off', 'off'] };

// lane: one way a board reaches this computer, as a tile: its icon, a dot
// for its state, a short word, and what it is on (the Wi-Fi's name).
function lane(ic, label, kind, word, sub, acts) {
  return h('div', { class: `lane ${kind}`, role: 'listitem', 'aria-label': `${label}: ${word}${sub ? `, ${sub}` : ''}` },
    h('div', { class: 'lhd' }, icon(ic, 18, 1.8), h('span', { class: 'ln' }, label)),
    h('div', { class: 'lst' }, dot(kind, word), h('span', {}, word)),
    sub ? h('span', { class: 'lsub', title: sub }, sub) : h('span', { class: 'lsub dim' }, '·'),
    acts && acts.filter(Boolean).length ? h('div', { class: 'lact' }, acts) : null);
}

// linkStates: USB, Wi-Fi and Bluetooth for a board, each as [kind, word, sub].
function linkStates(d) {
  const ups = d.uplinks || [];
  const up = (via) => ups.find((u) => u.via === via);
  const state = (u) => LANE_STATE[u?.state] || ['off', 'off'];
  const out = {};
  const usb = up('usb');
  out.usb = [...(usb ? state(usb) : d.connection === 'usb' ? ['on', 'in use'] : ['off', 'unplugged']), d.connection === 'usb' ? (d.port || '').replace(/^.*\//, '') : null];
  if (d.canWifi !== false || d.wifi) {
    const w = up('wifi');
    out.wifi = [...(w ? state(w) : d.connection === 'wifi' ? ['on', 'in use'] : d.wifi?.ssid ? ['sb', 'standby'] : ['off', 'not set']), d.wifi?.ssid || (d.canWifi === false ? 'no Wi-Fi' : null)];
  }
  if (d.hasBluetooth) {
    const b = up('bluetooth');
    out.bluetooth = [...(b ? state(b) : d.bluetooth ? ['sb', 'standby'] : ['off', 'not linked']), d.bluetooth && d.bleState && d.bleState !== 'paired' ? d.bleState : null];
  }
  if (!d.online) for (const k of Object.keys(out)) out[k] = ['off', k === 'usb' ? 'unplugged' : 'off', out[k][2]];
  return out;
}

// linkDots: the same, small, on one line: a glyph and a dot each, the Wi-Fi's name.
function linkDots(d) {
  const st = linkStates(d);
  return h('span', { class: 'ldots', role: 'list', 'aria-label': 'Connections' }, Object.entries(st).map(([k, [kind, word, sub]]) =>
    h('span', { class: `ld ${kind}`, role: 'listitem', title: `${VIA[k]}: ${word}${sub ? ` · ${sub}` : ''}` }, icon(VIA_ICON[k], 13, 1.8), dot(kind, `${VIA[k]} ${word}`), k === 'wifi' && sub ? h('span', { class: 'ssid' }, sub) : null)));
}

function lanes(d) {
  const st = linkStates(d);
  const s = S.settings || {};
  const inHand = d.connection === 'usb' || d.connection === 'bluetooth';
  const out = [];
  out.push(lane('usb', 'USB', ...st.usb));
  if (st.wifi) {
    const [k, word] = st.wifi;
    const acts = [];
    if (d.online && inHand && d.canWifi !== false && S.open.wifi !== d.id) {
      if (s.wifi?.ssid && (d.wifi?.own || !d.wifi?.ssid)) acts.push(btn('Send', (e) => deviceAction(d, 'wifi', { useDefault: true }, e.currentTarget, (r) => r?.message || 'Sent'), 'xs', { title: `Send ${s.wifi.ssid} to it` }));
      acts.push(btn(d.wifi?.ssid ? 'Change' : 'Set up', () => { S.open.wifi = d.id; draw(); $(`#ssid-${CSS.escape(d.id)}`)?.focus(); }, 'xs', { title: 'Give it another Wi-Fi network' }));
    }
    out.push(lane('wifi', 'Wi-Fi', k, word, st.wifi[2], acts));
  }
  if (st.bluetooth) {
    const nb = (S.btBoards?.boards || []).find((x) => x.device === d.id);
    const acts = !d.bluetooth && !(nb && S.jobs[S.btAdd[nb.id]])
      ? [btn(S.btFind[d.id] ? 'Looking…' : 'Link', (e) => btAddFor(d, e.currentTarget), 'xs', { 'data-k': `btfor-${d.id}`, disabled: !!S.btFind[d.id], title: 'Add it over Bluetooth' })] : [];
    out.push(lane('bluetooth', 'Bluetooth', ...st.bluetooth, acts));
  }
  return h('div', { class: 'lanes', role: 'list', 'aria-label': `How ${d.name} reaches this computer` }, out);
}

// connectionTiles: the board page's Connection row: a tile per link, the
// preference off USB under them, the Wi-Fi form or Bluetooth flow when open.
function connectionTiles(d) {
  const content = [lanes(d)];
  if (d.hasBluetooth && d.state !== 'revoked') {
    const opts = UPLINKS.map((o) => (o.value === 'ble' && !d.bluetooth
      ? { ...o, disabled: true, title: 'Link it over Bluetooth first' } : o));
    content.push(h('div', { class: 'pref' }, h('span', { class: 'kl' }, 'Off USB'),
      seg(`When ${d.name} is off USB, use`, opts, d.uplinkPref || 'auto',
        (v) => deviceAction(d, 'uplink', { uplink: v }, null, `${d.name} uses ${UPLINKS.find((o) => o.value === v).text} off USB`),
        { key: `uplink-${d.id}` })));
  }
  for (const u of d.uplinks || []) {
    if (u.via === 'bluetooth' && u.state === 'connecting' && u.detail && u.detail !== 'waiting for it to come in range') content.push(h('p', { class: 'sub err' }, `Bluetooth: ${u.detail}`));
  }
  if (d.wifi?.note) content.push(h('p', { class: 'sub' }, `Wi-Fi: ${d.wifi.note}`));
  if (d.uplinkNote) content.push(h('p', { class: 'sub err' }, d.uplinkNote));
  const nb = (S.btBoards?.boards || []).find((b) => b.device === d.id);
  const job = nb && S.jobs[S.btAdd[nb.id]];
  const extra = S.open.wifi === d.id ? wifiForm(d, () => load('devices')) : job ? addFlow(nb, job) : null;
  return labelled('Connection', content, null, { extra });
}

function connectionLine(d) {
  const ups = d.uplinks || [];
  const content = [];
  if (ups.length) content.push(h('div', { class: 'chips', role: 'list', 'aria-label': `How ${d.name} reaches this computer` }, ups.map(upChip)));
  else content.push(h('span', { class: 'sub' }, d.online ? `Connected${VIA[d.connection] ? ` over ${VIA[d.connection]}` : ''}` : d.lastSeen ? `Last seen ${ago(d.lastSeen)}` : 'Never connected'));
  if (d.hasBluetooth && d.state !== 'revoked') {
    const opts = UPLINKS.map((o) => (o.value === 'ble' && !d.bluetooth
      ? { ...o, disabled: true, title: 'Add the board over Bluetooth on this computer first' } : o));
    content.push(h('div', { class: 'actions' }, h('span', { class: 'sub' }, 'Off USB, use'),
      seg(`When ${d.name} is off USB, use`, opts, d.uplinkPref || 'auto',
        (v) => deviceAction(d, 'uplink', { uplink: v }, null, `${d.name} uses ${UPLINKS.find((o) => o.value === v).text} off USB`),
        { key: `uplink-${d.id}` }),
      h('span', { class: 'sub' }, 'Auto: USB, else Wi-Fi, else Bluetooth.')));
  }
  // A Bluetooth link that keeps failing says why (the board forgot this
  // computer, and so on): the person has something to do.
  for (const u of ups) {
    if (u.via === 'bluetooth' && u.state === 'connecting' && u.detail && u.detail !== 'waiting for it to come in range') {
      content.push(h('p', { class: 'sub err' }, `Bluetooth: ${u.detail}`));
    }
  }
  if (d.uplinkNote) content.push(h('p', { class: 'sub err' }, d.uplinkNote));
  return labelled('Connection', content);
}

function wifiLine(d) {
  const s = S.settings || {};
  const inHand = d.connection === 'usb' || d.connection === 'bluetooth';
  let meta;
  if (d.wifi?.ssid) meta = `On ${d.wifi.ssid} · ${d.wifi.state || 'unknown'}.`;
  else if (d.canWifi === false && d.board) meta = 'This board has no Wi-Fi.';
  else meta = 'No Wi-Fi yet.';
  if (d.wifi?.note) meta += ` ${cap(d.wifi.note)}.`;
  const right = [];
  if (d.online && inHand && d.canWifi !== false && S.open.wifi !== d.id) {
    if (s.wifi?.ssid && (d.wifi?.own || !d.wifi?.ssid)) right.push(btn('Send Wi-Fi', (e) => deviceAction(d, 'wifi', { useDefault: true }, e.currentTarget, (r) => r?.message || 'Sent')));
    right.push(btn(d.wifi?.ssid ? 'Use another network' : 'Give it Wi-Fi', () => { S.open.wifi = d.id; draw(); $(`#ssid-${CSS.escape(d.id)}`)?.focus(); }));
  } else if (!inHand && d.canWifi !== false) meta += ' Change it while it is on USB or Bluetooth.';
  return labelled('Wi-Fi', h('span', { class: 'sub' }, meta), right, { extra: S.open.wifi === d.id ? wifiForm(d, () => load('devices')) : null });
}

function bluetoothLine(d) {
  const nb = (S.btBoards?.boards || []).find((b) => b.device === d.id);
  const job = nb && S.jobs[S.btAdd[nb.id]];
  if (job) return labelled('Bluetooth', h('span', { class: 'sub' }, 'Adding it over Bluetooth.'), null, { extra: addFlow(nb, job) });
  if (d.bluetooth) return labelled('Bluetooth', h('span', { class: 'sub' }, `Linked${d.bleState ? ` · ${d.bleState}` : ''}. For when there is no cable or Wi-Fi.`));
  return labelled('Bluetooth', h('span', { class: 'sub' }, S.btFind[d.id] ? 'Looking for it over Bluetooth…' : 'Not linked. For when there is no cable or Wi-Fi.'),
    [btn('Add over Bluetooth', (e) => btAddFor(d, e.currentTarget), '', { 'data-k': `btfor-${d.id}`, disabled: !!S.btFind[d.id] })]);
}

function firmwareLine(d) {
  const st = d.status;
  const content = [];
  if (d.job) content.push(jobLine(d, S.jobs[d.job]));
  else if (st) content.push(h('span', { class: `sub${st.state === 'error' ? ' err' : ''}` }, fwText(st)));
  else content.push(h('span', { class: 'sub' }, d.firmware ? `Runs ${d.firmware}.` : 'Its version isn\'t known.'));
  const t = deviceFileTarget(d);
  const canFile = (d.fileVia || []).length > 0;
  const upNote = st && (st.state === 'update' || st.state === 'local') && !st.ready ? st.note : '';
  if (upNote && upNote === d.fileNote) content.push(h('p', { class: 'sub', id: `fwn-${t.key}` }, `To update or install a file: ${upNote}.`));
  else {
    if (upNote) content.push(h('p', { class: 'sub' }, `${st.state === 'local' ? 'To install it' : 'To update'}: ${upNote}.`));
    if (!canFile && d.fileNote) content.push(h('p', { class: d.online ? 'sub' : 'sr', id: `fwn-${t.key}` }, `A firmware file: ${d.fileNote}.`));
  }
  const right = [...fileButton(t, canFile, d.fileNote)];
  if (d.connection === 'usb' && d.port) right.push(btn('Reinstall', () => { location.hash = `#new-board/${encodeURIComponent(d.port)}`; }, '', { title: 'Install the firmware again over the cable' }));
  return labelled('Firmware', content, right, { extra: S.file[t.key] ? filePanel(t, S.file[t.key]) : null });
}

function renameLine(d) {
  const nameId = `name-${d.id}`;
  const save = (e) => deviceAction(d, 'rename', { name: $(`#${CSS.escape(nameId)}`).value }, e?.currentTarget, 'Renamed').then(() => { S.open.rename = ''; draw(true); });
  return h('div', { class: 'rename' },
    h('label', { for: nameId, class: 'sr' }, 'Name'),
    h('input', { id: nameId, type: 'text', value: d.name, maxlength: '40', 'data-k': nameId, onkeydown: (e) => { if (e.key === 'Enter') save(); if (e.key === 'Escape') { S.open.rename = ''; draw(true); } } }),
    btn('Save', save, 'p'), btn('Cancel', () => { S.open.rename = ''; draw(true); }));
}

function drawBoard() {
  const d = (S.devices.devices || []).find((x) => x.id === S.arg);
  const backTo = d && d.kind === 'network' ? ['#network', 'Network'] : ['#boards', 'All boards'];
  const back = h('a', { class: 'btn link', href: backTo[0] }, icon('back', 14), backTo[1]);
  if (!d) return [back, head('Boards', 'Not here', 'any more.'), note('This board was removed, or the link is old.')];
  for (const j of S.jobList?.jobs || []) putJob(j);
  const st = d.status;
  const chips = [connChip(d)];
  if (d.online && VIA[d.connection]) chips[0] = chip(VIA[d.connection], 'ok', VIA_ICON[d.connection], { title: `Connected over ${VIA[d.connection]}` });
  if (d.firmware) chips.push(chip(`Firmware ${d.firmware}`));
  if (d.kind === 'network') chips.push(chip(scopeWords(d.scope), 'acc'));
  let side = null;
  if (d.state === 'waiting') {
    side = rcard(null, 'Wants to join', null, 'Approve it to show your deck on it.', [
      btn('Approve', (e) => deviceAction(d, 'approve', {}, e.currentTarget, `${d.name} approved`), 'p')], 'flat');
  } else if (hasUpdate(d) && !d.job) {
    side = rcard(null, 'Update ready', null, `${d.firmware || '?'} → ${st.latest}. ${cap(st.note || '')}.`, [
      btn(st.state === 'local' ? `Install ${st.latest}` : `Update to ${st.latest}`, (e) => updateDevice(d, e.currentTarget), 'p', {
        'data-k': `upd-${d.id}`, 'aria-label': st.state === 'local' ? `Install ${st.latest}, built on this computer, on ${d.name} ${st.note}` : null,
      })], 'flat');
  }
  const title = S.open.rename === d.id ? renameLine(d)
    : h('div', { class: 'nmrow' }, h('h1', { class: 'sm' }, d.name), d.state !== 'revoked' ? h('button', { type: 'button', class: 'btn link', 'data-k': `ren-${d.id}`, onclick: () => { S.open.rename = d.id; draw(); $(`#name-${CSS.escape(d.id)}`)?.select(); } }, 'Rename') : null);
  const header = h('header', { class: 'bhead' },
    h('span', { class: 'm6' }, devScreen(d, 'xl')),
    h('div', { class: 'grow' }, h('span', { class: 'eyebrow acc' }, d.model || (d.kind === 'network' ? 'Phone or panel' : d.type || 'Board')), title, h('div', { class: 'chips' }, chips)),
    side);
  if (S.open.rename === d.id) header.querySelector('.grow').prepend(h('h1', { class: 'sr' }, d.name));

  const rows = [];
  if (d.kind === 'serial' && d.state !== 'revoked') {
    rows.push(connectionTiles(d));
  } else {
    rows.push(labelled('Connection', h('div', { class: 'lanes', role: 'list' }, lane('wifi', 'Network', d.online ? 'on' : 'off', d.online ? 'connected' : d.lastSeen ? `seen ${ago(d.lastSeen)}` : 'never', null))));
  }
  if (d.state !== 'revoked') {
    rows.push(labelled('Access', seg(`What ${d.name} may do`, SCOPES, d.scope, (v) => deviceAction(d, 'scope', { scope: v }, null, `${d.name} may now: ${scopeLower(v)}`), { key: `scope-${d.id}` })));
  }
  if (d.state === 'active' && S.settings?.magic) rows.push(magicLine(d));
  if (d.kind === 'serial' && d.state === 'active') rows.push(firmwareLine(d));
  const foot = h('div', { class: 'actions end' }, d.state === 'revoked' ? h('span', { class: 'sub grow' }, 'Cut off: it can\'t connect any more.') : null,
    d.state !== 'revoked' ? btn('Cut it off', (e) => deviceAction(d, 'revoke', {}, e.currentTarget, `${d.name} can't connect any more`), 'd', { title: 'It stops connecting now, and stays in the list' }) : null,
    btn('Remove', (e) => deviceAction(d, 'remove', {}, e.currentTarget, `${d.name} removed`), 'd', { title: 'Forget it; it pairs again over USB' }));
  return [back, header, blk(...rows), foot];
}

// ---- Magic trick ---------------------------------------------------------------------------

const CORNERS = [
  { value: 'top-left', text: 'Top left' }, { value: 'top-right', text: 'Top right' },
  { value: 'bottom-left', text: 'Bottom left' }, { value: 'bottom-right', text: 'Bottom right' },
];

// roleOf: this device's part in the magic trick: giver, receiver or ''.
function roleOf(d) {
  const m = S.settings?.magic;
  if (!m) return '';
  if (m.giver === d.id) return 'giver';
  if (m.receiver === d.id) return 'receiver';
  return '';
}

// setRole gives d a part (or none). One giver and one receiver: picking a
// part for d takes it from the device that had it.
function setRole(d, role) {
  const m = S.settings?.magic;
  if (!m) return;
  const next = { ...m };
  if (next.giver === d.id) next.giver = '';
  if (next.receiver === d.id) next.receiver = '';
  if (role === 'giver') next.giver = d.id;
  if (role === 'receiver') next.receiver = d.id;
  if (!next.giver || !next.receiver) next.on = false; // it needs both
  patchSettings({ magic: next });
}

// pickSel: a choice in a list (a styled select).
function pickSel(label, options, current, onChange, key, opts = {}) {
  return h('select', { class: 'pick', 'aria-label': label, 'data-k': key, disabled: opts.disabled, onchange: (e) => onChange(e.target.value) },
    opts.empty ? h('option', { value: '', selected: !current }, opts.empty) : null,
    ...options.map((o) => h('option', { value: o.value, selected: o.value === current }, o.text)));
}

// magicLine: the part and its corners, on the device's own page.
function magicLine(d) {
  const m = S.settings.magic;
  const role = roleOf(d);
  const save = (patch) => patchSettings({ magic: { ...m, ...patch } });
  const content = [seg(`${d.name}'s part in the magic trick`, [
    { value: '', text: 'Not in it' },
    { value: 'giver', text: 'Giver', title: 'Its agents fly to the receiver' },
    { value: 'receiver', text: 'Receiver', disabled: !d.motion, title: d.motion ? 'Knock on it to take the agents' : 'Needs a motion sensor (the 1.8 inch AMOLED)' },
  ], role, (v) => setRole(d, v), { key: `role-${d.id}` })];
  const corner = (label, key) => h('div', { class: 'actions' }, h('span', { class: 'kl' }, label), pickSel(`${d.name}: ${label}`, CORNERS, m[key], (v) => save({ [key]: v }), `${key}-${d.id}`));
  if (role === 'giver') content.push(corner('They leave to', 'giverLeave'), corner('They come back from', 'giverArrive'));
  if (role === 'receiver') content.push(corner('They arrive from', 'receiverArrive'), corner('They leave to', 'receiverLeave'));
  if (role && !m.on) content.push(h('span', { class: 'sub' }, 'The trick is off.'));
  return labelled('Magic trick', content, [h('a', { class: 'btn xs', href: '#boards/magic', title: 'Set up the magic trick' }, icon('magic', 13), 'Set up')]);
}

// The magic trick: a knock on the receiver (a board with a motion sensor)
// takes the agents the giver shows, the next knock gives them back. Off by
// default; while off, nothing happens.
function drawMagic() {
  const m = S.settings?.magic;
  if (!m) return [sec('Magic trick', null, null, note('This service has no magic trick: update agents deck.'))];
  const devs = (S.devices?.devices || []).filter((d) => d.state === 'active');
  const receivers = devs.filter((d) => d.motion);
  const save = (patch, el) => patchSettings({ magic: { ...m, ...patch } }, el);
  const ready = m.giver && m.receiver;
  const devOpts = (list) => list.map((d) => ({ value: d.id, text: d.name }));
  const giver = devs.find((d) => d.id === m.giver), receiver = devs.find((d) => d.id === m.receiver);
  const side = (eyebrow, words, dev, picker, corners) => h('div', { class: 'blk pad' },
    h('div', { class: 'grow' }, h('span', { class: 'eyebrow acc2' }, eyebrow), h('span', { class: 'sub' }, words)),
    h('div', { class: 'mrole' }, h('span', { class: 'm6' }, dev ? devScreen(dev, 'mid') : screen('', 'mid')),
      h('div', { class: 'pickcol' }, picker, ...corners)));
  const cornerPick = (label, key) => [h('span', { class: 'kl' }, label), pickSel(label, CORNERS, m[key], (v) => save({ [key]: v }), `magic-${key}`)];
  return [sec('Magic trick', 'knock to move agents', null,
    blk(h('div', { class: 'row' }, ico('magic', 'acc'),
      h('div', { class: 'grow' }, h('span', { class: 'ttl' }, 'Knock on the receiver to take the giver\'s agents'), h('span', { class: 'txt dim' }, ready ? 'Knock again to give them back. Off: nothing happens.' : 'Pick a giver and a receiver first.')),
      toggle('Magic trick', m.on, (on, el) => save({ on }, el), { disabled: !ready && !m.on }))),
    h('div', { class: 'pair' },
      side('Giver', 'Any screen. Its agents fly off, and their places stay empty until they come back.', giver,
        pickSel('Choose the giver', devOpts(devs.filter((d) => d.id !== m.receiver)), m.giver, (v) => save({ giver: v }), 'magic-giver', { empty: 'Choose a device' }),
        [...cornerPick('On the giver, they leave to', 'giverLeave'), ...cornerPick('On the giver, they come back from', 'giverArrive')]),
      side('Receiver', receivers.length ? 'A board with a motion sensor: you knock on it, the agents fly onto it.' : 'Needs a board with a motion sensor (the 1.8 inch AMOLED). Plug one in and it shows up here.', receiver,
        pickSel('Choose the receiver', devOpts(receivers.filter((d) => d.id !== m.giver)), m.receiver, (v) => save({ receiver: v }), 'magic-receiver', { empty: 'Choose a device', disabled: !receivers.length }),
        [...cornerPick('On the receiver, they arrive from', 'receiverArrive'), ...cornerPick('On the receiver, they leave to', 'receiverLeave')])),
    blk(...[
      'On the receiver, pull the menu down from the top edge and switch Magic trick on.',
      'Knock once on the receiver to take the agents.',
      'Knock again on its grid to give them back. Then switch it off in the same menu.',
    ].map((t, i) => h('div', { class: 'row thin' }, h('span', { class: 'num' }, String(i + 1)), h('span', { class: 'txt' }, t)))))];
}

// ---- Bluetooth: boards nearby, and adding one -------------------------------------------------

// signal: how well the computer hears a board, in words; the number in the tooltip.
function signal(rssi) {
  if (!rssi) return null;
  const w = rssi >= -60 ? 'strong' : rssi >= -75 ? 'ok' : 'weak';
  return h('span', { class: w === 'weak' ? 'err' : '', title: `${rssi} dBm` }, `signal ${w}`);
}

const labelOf = (id) => (S.devices?.devices || []).find((d) => d.id === id)?.name || id;

async function btScan(on, el) {
  await busy(el, () => api('POST', '/admin/v1/bluetooth/scan', { on })); // a refusal says why in a toast
  await loadAll('bt', 'btBoards');
  draw(true);
}

// btAddFor: add a board you already have over Bluetooth, from its own page:
// look for it (a few seconds at most), then add it as Look for boards does.
async function btAddFor(d, el) {
  if (!S.bt?.available) { toast(S.bt?.why || 'Bluetooth isn\'t available on this computer.', true); return; }
  S.btFind[d.id] = true;
  draw(true);
  try {
    if (!S.bt.scanning) await api('POST', '/admin/v1/bluetooth/scan', { on: true });
    for (let i = 0; i < 24; i++) {
      await loadAll('bt', 'btBoards');
      const b = (S.btBoards?.boards || []).find((x) => x.device === d.id);
      if (b) {
        delete S.btFind[d.id];
        if (b.added) { toast(`${d.name} is linked over Bluetooth already.`); return; }
        await btAddBoard(b, el);
        return;
      }
      await new Promise((r) => setTimeout(r, 500));
    }
    toast(`${d.name} didn't show up over Bluetooth. Bring it closer and check Bluetooth is on in its drawer (pull down from the top of its screen).`, true);
  } catch (e) {
    if (e.status !== 401) toast(e.message, true);
  } finally {
    delete S.btFind[d.id];
    draw(true);
  }
}

async function btAddBoard(b, el) {
  const scope = S.btScope[b.id] || 'act';
  let job;
  try {
    if (el) el.disabled = true;
    job = await api('POST', `/admin/v1/bluetooth/boards/${encodeURIComponent(b.id)}/add`, { scope });
  } catch (e) {
    if (e.status === 409 && e.data.job) job = await api('GET', `/admin/v1/jobs/${encodeURIComponent(e.data.job)}`).catch(() => null);
    else if (e.status !== 401) toast(e.message, true);
  } finally {
    if (el && el.isConnected) el.disabled = false;
  }
  if (job && job.id) { putJob(job); S.btAdd[b.id] = job.id; draw(true); }
}

const ADD_STEPS = [
  ['connect', 'Connect to the board'],
  ['passkey', 'Type the code from the board'],
  ['connected', 'Paired: checking with the board'],
  ['done', 'Added'],
];

// addFlow: adding a board, step by step, from the job's events.
function addFlow(b, j) {
  const close = () => { delete S.btAdd[b.id]; draw(true); };
  const title = { running: `Adding ${b.name}`, done: `Added ${b.name}`, cancelled: `Stopped adding ${b.name}` }[j.state] || `Couldn't add ${b.name}`;
  const box = h('div', { class: `blk pad${j.state === 'failed' ? ' err' : ''}`, 'data-row': `bt-${b.id}` }, h('span', { class: 'eyebrow acc2' }, title));
  if (j.state === 'running') {
    if (j.stage === 'passkey') {
      box.append(h('p', { class: 'passkey', role: 'status' }, 'Type the code shown on the board into the window this computer opens.'),
        note('The board shows a 6-digit code and its name. The window is the computer\'s own: this page never sees the code.'));
    } else {
      box.append(h('p', { class: 'txt', role: 'status' }, j.message || 'Connecting…'));
    }
    const at = ADD_STEPS.findIndex(([k]) => k === j.stage);
    const list = h('ol', { class: 'list', 'aria-label': 'Steps' });
    ADD_STEPS.forEach(([, text], i) => {
      const st = i < at ? '' : i === at ? 'cur' : 'todo';
      list.append(h('li', { class: st }, h('i', { 'aria-hidden': 'true' }), text, st === 'cur' ? h('span', { class: 'sr' }, '(now)') : null));
    });
    box.append(list, h('div', { class: 'actions' },
      btn('Cancel', (e) => busy(e.currentTarget, () => api('POST', `/admin/v1/jobs/${encodeURIComponent(j.id)}/cancel`, {})), '', { 'data-k': `btc-${b.id}` })));
  } else if (j.state === 'done') {
    box.append(h('p', { class: 'txt', role: 'status' }, j.message || `${b.name} is one of your boards now.`),
      h('div', { class: 'actions' }, btn('Done', close, 'p', { 'data-k': `btd-${b.id}` })));
  } else {
    box.append(h('p', { class: 'txt', role: 'alert' }, j.state === 'cancelled' ? 'Stopped. Nothing was added.' : (j.error || j.message || 'It didn\'t work.')),
      note('Bring the board close, check that it still shows its name in setup, and try again. If it was paired with this computer before, remove it in System Settings, Bluetooth first.'),
      h('div', { class: 'actions' },
        btn('Try again', (e) => btAddBoard(b, e.currentTarget), 'p', { 'data-k': `btr-${b.id}` }),
        btn('Close', close, '', { 'data-k': `btx-${b.id}` })));
  }
  return box;
}

function nearbyRow(b) {
  const j = S.jobs[S.btAdd[b.id]];
  if (j) return addFlow(b, j);
  const right = [];
  let state;
  if (b.added) state = `Yours: ${labelOf(b.device)}`;
  else if (b.setup || b.boardId) {
    // Paired but not added here: a bond this computer may still hold (an
    // add that didn't finish). Adding it again needs no code then.
    if (b.setup) state = b.device ? `In setup · was ${labelOf(b.device)}` : 'In setup';
    else state = b.device ? `Paired, not added here · ${labelOf(b.device)}` : 'Paired with this computer or another';
    right.push(seg(`What ${b.name} may do`, SCOPES.slice(0, 2), S.btScope[b.id] || 'act', (v) => { S.btScope[b.id] = v; draw(); }, { key: `bts-${b.id}` }),
      btn('Add', (e) => btAddBoard(b, e.currentTarget), 'p', { 'data-k': `bta-${b.id}` }));
  } else state = 'Couldn\'t read it: bring it closer';
  const what = [b.model || b.board || 'a board', b.firmware ? `firmware ${b.firmware}` : ''].filter(Boolean).join(' · ');
  const sig = signal(b.rssi);
  const meta = h('span', {}, what, sig ? ' · ' : '', sig, ` · ${state}`);
  return h('div', { class: 'found', 'data-row': `bt-${b.id}` }, thumb(screen(boardOf(b.board), 'xs', !b.setup && !b.added)),
    h('div', { class: 'grow' }, h('span', { class: `nm${!b.setup && !b.added ? ' off' : ''}` }, b.name || b.id), h('span', { class: 'sub' }, meta)), ...right);
}

// ---- New board, and any flash in progress --------------------------------------------------

function stepsAt(n, show) {
  const ol = $('#nb-steps');
  ol.hidden = !show;
  ol.querySelectorAll('li').forEach((li) => {
    const s = Number(li.dataset.step);
    li.classList.toggle('past', s < n);
    if (s === n) li.setAttribute('aria-current', 'step'); else li.removeAttribute('aria-current');
  });
}

function bootHelp(extra) {
  return blk(row('Board not answering?', 'Hold BOOT, press RST, let go of BOOT, then try again. On boards without buttons, unplug it, hold BOOT while plugging it back in.', extra || []));
}

function boardCards(list, chosen, onPick) {
  const group = h('div', { class: 'cards', role: 'radiogroup', 'aria-label': 'Which board is it?' });
  list.forEach((b, i) => {
    const on = b.board === chosen;
    const spec = [b.flash_size ? `${Math.round(b.flash_size / 1048576)} MB` : null, b.wifi ? 'Wi-Fi' : 'USB only',
      b.source === 'local' ? `${b.version} built here` : null].filter(Boolean).join(' · ');
    group.append(h('button', {
      type: 'button', class: 'card', role: 'radio', 'aria-checked': on ? 'true' : 'false',
      tabindex: on || (!chosen && i === 0) ? '0' : '-1', 'data-k': `card-${b.board}`,
      onclick: () => onPick(b.board),
      onkeydown: (e) => radioKeys(e, group, (v) => onPick(v), list.map((x) => ({ value: x.board }))),
    }, h('span', { class: 'pic' }, screen(boardOf(`${b.board} ${b.name}`), '')), h('span', { class: 'nm' }, b.name), h('small', {}, spec)));
  });
  return group;
}

async function nbIdentify() {
  const nb = S.nb;
  nb.step = 2; nb.identity = null; nb.error = null; nb.looking = true;
  draw();
  try {
    nb.identity = await api('POST', `/admin/v1/ports/${encodeURIComponent(nb.port)}/identify`, {});
    const id = nb.identity;
    const offers = nbOffers(id);
    if (id.board) nb.choice = id.board.board;
    else if (id.source === 'session' && id.local?.length) nb.choice = id.local[0].board;
    else if (offers.length === 1) nb.choice = offers[0].board;
  } catch (e) {
    nb.error = e;
  }
  nb.looking = false;
  if (S.nb === nb) draw();
}

// nbOffers: what can go on the board in "Set up a new board", one card per
// board: the latest release's (when this copy can install releases) or the
// build made on this computer, whichever is newer.
function nbOffers(id) {
  const out = id.releaseKey ? id.candidates.map((b) => ({ ...b, source: 'release' })) : [];
  for (const b of id.local || []) {
    const i = out.findIndex((x) => x.board === b.board);
    if (i < 0) out.push({ ...b, source: 'local' });
    else if (newerVersion(b.version, out[i].version)) out[i] = { ...b, source: 'local' };
  }
  return out;
}

// newerVersion: a is newer than b by semver precedence (0.2.0-dev.10 > 0.2.0-dev.7 > 0.1.1).
function newerVersion(a, b) {
  const parse = (v) => {
    const m = /^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+.*)?$/.exec(v || '');
    return m ? { n: [+m[1], +m[2], +m[3]], pre: m[4] ? m[4].split('.') : [] } : null;
  };
  const x = parse(a), y = parse(b);
  if (!x || !y) return false;
  for (let i = 0; i < 3; i++) if (x.n[i] !== y.n[i]) return x.n[i] > y.n[i];
  if (!x.pre.length || !y.pre.length) return !x.pre.length && y.pre.length > 0;
  for (let i = 0; i < Math.min(x.pre.length, y.pre.length); i++) {
    const p = x.pre[i], q = y.pre[i];
    if (p === q) continue;
    const pn = /^\d+$/.test(p), qn = /^\d+$/.test(q);
    if (pn && qn) return +p > +q;
    if (pn !== qn) return qn;
    return p > q;
  }
  return x.pre.length > y.pre.length;
}

async function nbInstall(btnEl, offer) {
  const nb = S.nb;
  const body = offer && offer.source === 'local' ? { board: offer.board, version: offer.version, source: 'local' } : { board: nb.choice };
  const job = await busy(btnEl, () => api('POST', `/admin/v1/ports/${encodeURIComponent(nb.port)}/flash`, body));
  if (job && job.id) {
    putJob(job);
    nb.job = job.id;
    location.hash = `#flash/${job.id}`;
  }
}

function drawNewBoard() {
  const nb = S.nb;
  const p = (S.ports.ports || []).find((x) => x.id === nb.port);
  $('#h-newboard').textContent = 'Set up a new board';
  if (!p) {
    stepsAt(1, true);
    return [errorBox('This port isn\'t there any more. Is the board still plugged in?', () => load('ports').then(draw)), link('Back to Boards', '#boards')];
  }
  if (!p.registered) {
    stepsAt(1, true);
    return [
      blk(row(`USB port ${p.id}`, `${cap(portWords(p))}${p.vid ? ` · ${p.vid}:${p.pid}` : ''} · not opened yet`, [], { name: true, pic: ico('usb', 'acc') })),
      h('div', { class: 'blk pad' },
        h('p', { class: 'ttl' }, 'Is this one of your boards?'),
        note('Yes lets agents deck open this port and look at the board. It never opens a port you haven\'t said yes to.'),
        h('div', { class: 'actions' },
          btn('Yes, it\'s mine', async (e) => {
            const r = await busy(e.currentTarget, () => api('POST', `/admin/v1/ports/${encodeURIComponent(p.id)}/register`, {}));
            if (!r) return;
            await load('ports');
            nbIdentify();
          }, 'p', { 'data-k': 'nb-yes' }),
          btn('Not mine', async (e) => { await portAction(p, 'forget', {}, e.currentTarget, 'It won\'t be asked about again'); location.hash = '#boards'; }))),
    ];
  }
  if (!nb.identity && !nb.error && !nb.looking) { nbIdentify(); }
  stepsAt(2, true);
  if (nb.looking) {
    return [blk(row(`USB port ${p.id}`, 'Looking at the board. This takes a few seconds; its screen may flicker.', [], { name: true, pic: ico('usb', 'acc') })), h('p', { class: 'note', role: 'status' }, 'Asking the board…')];
  }
  if (nb.error) {
    const out = [errorBox(nb.error.message)];
    if (nb.error.data?.bootHelp) out.push(bootHelp([btn('Try again', () => nbIdentify(), 'p')]));
    else out.push(h('div', { class: 'actions' }, btn('Try again', () => nbIdentify(), 'p'), link('Cancel', '#boards')));
    return out;
  }
  const id = nb.identity;
  const out = [];
  if (id.source === 'session') {
    const d = id.device || {};
    out.push(blk(row(`${d.model || d.type || 'A board'} · firmware ${d.firmware || 'unknown'}`, `It already runs agents deck, as ${d.name || 'a board'}.${id.latest ? ` The latest firmware is ${id.latest}.` : ''}`, [], { name: true, pic: thumb(screen(boardOf(`${d.board || ''} ${d.type || ''}`), 'xs')) })));
  } else {
    const fs = id.flashSize ? ` · ${Math.round(id.flashSize / 1048576)} MB flash` : '';
    const n = nbOffers(id).length;
    const none = id.releaseKey ? 'No board in the latest release or built on this computer uses this chip.' : 'No firmware built on this computer is for this chip.';
    out.push(blk(row(`${id.chip}${fs}`, `No agents deck firmware on it yet. ${n === 0 ? none : n === 1 ? 'One board matches this chip.' : `${n} boards match this chip.`}`, [], { name: true, pic: ico('board') })));
  }
  if (id.note) out.push(note(id.note));
  // A file from this computer: through the board's own calls when it runs
  // agents deck, else through the port's (a full image for a blank chip).
  const dev = id.source === 'session' ? id.device : null;
  const ft = dev ? { ...deviceFileTarget(dev), onJob: (job) => { if (S.nb === nb) nb.job = job.id; } }
    : id.source === 'probe' ? portFileTarget(p, id) : null;
  const canFile = dev ? (dev.fileVia || []).length > 0 : !!ft;
  const fileNote = dev ? (dev.fileNote || 'approve the board first') : '';
  const offers = nbOffers(id);
  if (offers.length) out.push(sec('Which board is it?', offers.length === 1 ? 'one fits' : `${offers.length} fit this chip`, null, boardCards(offers, nb.choice, (v) => { nb.choice = v; draw(); })));
  if (!id.releaseKey && !offers.length) {
    out.push(note('This copy of agents deck was built from source, so it can\'t check firmware downloaded from a release and won\'t install it. ' +
      'Firmware you build on this computer installs in one click: run agentctl firmware add <folder> with the folder the release script wrote, then look at the board again. ' +
      'Or install a single .bin you built or downloaded with Choose file below.'));
    if (!dev && id.candidates.length) {
      out.push(note(`Pick the full image, the one written at 0x0: from a release, ${id.candidates.map((b) => `agents-deck-${b.board}-<version>.bin`).join(' or ')}.`));
    }
    out.push(note('By hand in a terminal, agentctl flash <port> <image.bin> does the same.'));
  }
  if (ft && !canFile && fileNote) out.push(h('p', { class: 'note', id: `fwn-${ft.key}` }, `To install a firmware file: ${fileNote}.`));
  const chosen = offers.find((b) => b.board === nb.choice);
  const label = !chosen ? 'Pick the board first'
    : chosen.source === 'local' ? `Install ${chosen.version}, built on this computer` : `Install firmware ${chosen.version || id.latest}`;
  if (chosen && chosen.source === 'local') {
    out.push(note(`${chosen.name} ${chosen.version} was built on this computer (agentctl firmware add). ` +
      'It isn\'t a release, so no signature is checked; its files are checked against the manifest that came with them. The full image is written at 0x0 over the cable.'));
  }
  const acts = [];
  if (offers.length) acts.push(btn(label, (e) => nbInstall(e.currentTarget, chosen), 'p', { disabled: !chosen, 'data-k': 'nb-install' }));
  if (ft) acts.push(...fileButton(ft, canFile, fileNote, offers.length ? '' : 'p'));
  acts.push(link('Cancel', '#boards'));
  out.push(h('div', { class: 'actions' }, ...acts,
    offers.length ? h('span', { class: 'sub' }, 'Picked the wrong one? Nothing breaks: install again with the right board.') : null));
  if (ft && S.file[ft.key]) out.push(filePanel(ft, S.file[ft.key]));
  return out;
}

const FLASH_STEPS = [
  { at: ['download'], todo: (j) => `Download firmware ${j.version || ''} from GitHub`, done: (j) => `Downloaded firmware ${j.version || ''}`, side: (j) => (j.steps?.[0]?.match(/\(([^)]+)\)/) || [])[1] || '' },
  { at: ['download'], todo: () => 'Check its signature and checksum', done: () => 'Checked its signature and checksum', side: () => 'ok' },
  { at: ['connect'], todo: () => 'Board answers in install mode', done: () => 'Board answered in install mode', side: (j) => j.chip || S.nb?.identity?.chip || '' },
  { at: ['erase', 'write'], todo: () => 'Write', done: () => 'Written', side: () => '' },
  { at: ['verify'], todo: () => 'Verify what was written', done: () => 'Verified what was written', side: () => '' },
  { at: ['reset'], todo: () => 'Restart and pair', done: () => 'Restarted', side: () => '' },
];
const STAGE_ORDER = ['download', 'connect', 'erase', 'write', 'verify', 'reset'];

function timeLeft(j) {
  if (j.stage !== 'write' || !j.total || !j.done) { delete S.rates[j.id]; return ''; }
  const now = Date.now();
  const r = S.rates[j.id];
  if (!r) { S.rates[j.id] = { t: now, done: j.done }; return ''; }
  const rate = (j.done - r.done) / ((now - r.t) / 1000);
  if (!(rate > 0)) return '';
  const s = Math.round((j.total - j.done) / rate);
  return s < 60 ? ` · about ${Math.max(5, Math.round(s / 5) * 5)} seconds left` : ` · about ${Math.round(s / 60)} minutes left`;
}

// otaSteps: the stages a board reports while it updates over Wi-Fi.
const OTA_STEPS = ['offered', 'downloading', 'verifying', 'restarting', 'done'];
function otaSteps(j) {
  const at = OTA_STEPS.indexOf(j.stage);
  const text = {
    offered: 'Offered to the board', downloading: 'The board downloads it', verifying: 'The board checks it',
    restarting: 'The board restarts into it', done: `Back online with ${j.version}`,
  };
  const list = h('ol', { class: 'list', 'aria-label': 'Steps' });
  OTA_STEPS.forEach((k, i) => {
    let st = 'todo';
    if (j.state === 'done' || i < at) st = 'done';
    else if (i === at && j.state === 'running') st = 'cur';
    else if (i === at) st = 'bad';
    list.append(h('li', { class: st === 'done' ? '' : st }, h('i', { 'aria-hidden': 'true' }), text[k],
      k === 'downloading' && (st === 'cur' || st === 'bad') && j.stage === 'downloading' ? h('span', {}, `${Math.max(0, Math.round((j.percent - 5) * 100 / 80))}%`) : null,
      st === 'cur' ? h('span', { class: 'sr' }, '(now)') : null));
  });
  return list;
}

function progressBlock(j, what, foot) {
  const bar = h('div', {});
  bar.style.width = `${j.percent}%`;
  return h('div', { class: 'blk pad progress' },
    h('div', { class: 'big' }, h('div', { class: 'pct' }, `${j.percent}%`), h('div', { class: 'what' }, what)),
    h('div', { class: 'bar', role: 'progressbar', 'aria-label': j.message || 'Installing firmware', 'aria-valuenow': j.percent, 'aria-valuemin': '0', 'aria-valuemax': '100' }, bar),
    foot);
}

function doneBlock(text, acts) {
  return h('div', { class: 'rcard flat', role: 'status' }, h('div', { class: 'bd' }, h('span', { class: 'eyebrow acc2' }, 'Done'), h('span', { class: 'meta' }, text), h('div', { class: 'act' }, acts)));
}

function drawFlash() {
  const j = S.jobs[S.arg];
  if (!j) {
    stepsAt(0, false);
    return [h('p', { class: 'note' }, 'Loading…')];
  }
  const dev = (S.devices?.devices || []).find((d) => d.id === j.device);
  const fromSetup = S.nb && S.nb.job === j.id;
  const known = [...(S.nb?.identity?.candidates || []), ...(S.nb?.identity?.local || [])];
  const boardName = known.find((b) => b.board === j.board)?.name || dev?.model || j.board || 'the board';
  $('#h-newboard').textContent = j.file ? `${j.state === 'done' ? 'Installed' : 'Installing'} ${j.file} on ${dev?.name || j.device || boardName}`
    : j.kind === 'board-update' ? `${j.state === 'done' ? 'Updated' : 'Updating'} ${dev?.name || j.device}` : `${j.state === 'done' ? 'Installed on' : 'Installing on'} ${boardName}`;
  const ended = j.state !== 'running';
  stepsAt(ended && j.state === 'done' ? 4 : 3, fromSetup);
  const out = [];
  const backBtn = (cls = '') => link(dev ? `Back to ${dev.name}` : 'Back to Boards', dev ? `#board/${encodeURIComponent(dev.id)}` : '#boards', cls);
  if (!j.port && j.kind === 'board-update') { // over the air: the board downloads it itself
    if (!ended) {
      out.push(progressBlock(j, `over Wi-Fi · ${j.file ? `${j.file} as ` : ''}${j.version}`, [
        h('div', { class: 'txt', role: 'status' }, j.message),
        note('The board keeps showing your agents while it downloads. If the new firmware doesn\'t reach the deck, it goes back to the old one by itself.')]));
      out.push(otaSteps(j));
      out.push(h('div', { class: 'actions' }, btn('Stop following', (e) => busy(e.currentTarget, () => api('POST', `/admin/v1/jobs/${encodeURIComponent(j.id)}/cancel`, {})))));
    } else if (j.state === 'done') {
      out.push(otaSteps(j));
      out.push(doneBlock(j.message, [backBtn('p')]));
    } else {
      out.push(otaSteps(j));
      out.push(errorBox(j.state === 'cancelled' ? 'Stopped following the board. It may still finish the update by itself.' : j.error));
      out.push(backBtn());
    }
    return out;
  }
  const words = { download: 'downloading', connect: 'waiting for the board', erase: 'erasing', write: 'writing', verify: 'checking', reset: 'restarting' }[j.stage] || j.stage || '';
  // "writing 0.4 of 1.8 MB": the stage, then how far into it (the big
  // number is the whole job, download and checks included).
  const size = j.total && j.done && (j.stage === 'write' || j.stage === 'verify') ? ` ${mb(j.done)} of ${mb(j.total)} MB` : '';
  if (!ended) out.push(progressBlock(j, `${words}${size}${timeLeft(j)}`, h('div', { class: 'txt' }, 'Keep the cable plugged in. The board\'s screen stays dark until it restarts.')));
  const cur = STAGE_ORDER.indexOf(j.stage);
  const list = h('ol', { class: 'list', 'aria-label': 'Steps' });
  // A file picked on this computer, or a build made on it: no download and
  // no signature to check.
  const pre = j.file || j.source === 'local' ? 1 : 2;
  const steps = j.file ? [{ at: ['download'], todo: () => `Use ${j.file}`, done: () => `Used ${j.file}`, side: () => 'not checked against a release' }, ...FLASH_STEPS.slice(2)]
    : j.source === 'local' ? [{ at: ['download'], todo: () => `Use ${j.version}, built on this computer`, done: () => `Used ${j.version}, built on this computer`, side: () => 'checked against its manifest' }, ...FLASH_STEPS.slice(2)]
      : FLASH_STEPS;
  steps.forEach((s, i) => {
    const at = Math.min(...s.at.map((x) => STAGE_ORDER.indexOf(x)));
    const last = Math.max(...s.at.map((x) => STAGE_ORDER.indexOf(x)));
    let state = 'todo';
    if (j.state === 'done' || cur > last || (i < pre && cur > 0)) state = 'done';
    else if (cur >= at && cur <= last && !ended) state = 'cur';
    const text = state === 'done' ? s.done(j) : s.todo(j);
    list.append(h('li', { class: state === 'done' ? '' : state }, h('i', { 'aria-hidden': 'true' }), text,
      state === 'done' ? h('span', {}, s.side(j)) : null,
      state === 'cur' ? h('span', { class: 'sr' }, '(now)') : null));
  });
  out.push(list);
  if (j.state === 'done') {
    const b = known.find((x) => x.board === j.board);
    out.push(doneBlock(j.file ? `${dev?.name || j.device || (j.board ? boardName : 'The board')} restarts into ${j.file}, pairs and shows your agents in a few seconds.` : `${boardName} runs agents deck ${j.version || ''}. It restarts, pairs and shows your agents in a few seconds.`, [
      b && b.wifi ? link('Give it Wi-Fi', '#network') : null,
      link('Back to Boards', '#boards', 'p')]));
  } else if (ended) {
    out.push(errorBox(j.state === 'cancelled' ? j.message : `It didn't work: ${j.error}`));
    out.push(bootHelp([
      link('Back to Boards', '#boards'),
      j.port && j.kind === 'flash' && !j.file ? btn('Try again', async (e) => {
        const job = await busy(e.currentTarget, () => api('POST', `/admin/v1/ports/${encodeURIComponent(j.port)}/flash`,
          j.source === 'local' ? { board: j.board, version: j.version, source: 'local' } : { board: j.board, version: j.version }));
        if (job?.id) { putJob(job); if (S.nb) S.nb.job = job.id; location.hash = `#flash/${job.id}`; }
      }, 'p') : null,
    ]));
  } else {
    out.push(bootHelp([btn('Stop', (e) => busy(e.currentTarget, () => api('POST', `/admin/v1/jobs/${encodeURIComponent(j.id)}/cancel`, {})), '', { 'data-k': 'flash-stop' })]));
  }
  return out;
}

// ---- Network -------------------------------------------------------------------------------

function drawNetwork() {
  const s = S.settings;
  const out = [head('Network', 'No cable', 'needed.')];
  const pending = s.network !== s.networkRunning;
  out.push(h('div', { class: 'cards' }, h('div', { class: 'blk fit' },
    row('Paired devices over the network',
      pending ? `${s.network ? 'On' : 'Off'} after the service restarts.` : 'Encrypted · only devices you paired',
      [pending ? btn('Restart now', (e) => restartService(e.currentTarget), 'sm') : null,
        toggle('Let paired devices connect over the network', s.network, (on, el) => patchSettings({ network: on }, el))],
      { pic: ico('lock') }))));

  const err = h('p', { class: 'sub err', role: 'alert' });
  const editing = !s.wifi?.ssid || S.open.wifiMain;
  const wifiCard = h('div', { class: 'cards' }, h('div', { class: 'blk fit' }, row(h('span', { class: 'ssidbig' }, s.wifi?.ssid), null, [
    chip('2.4 GHz', '', null, { title: 'Boards use 2.4 GHz Wi-Fi only' }),
    btn('Change', () => { S.open.wifiMain = true; draw(); $('#ssid')?.focus(); }, 'sm'),
    btn('Forget', (e) => patchSettings({ wifi: { forget: true } }, e.currentTarget), 'sm')], { pic: ico('wifi', 'ok') })));
  if (!editing) out.push(sec('Wi-Fi for boards', 'sent to boards on USB or Bluetooth', null, wifiCard));
  else out.push(sec('Wi-Fi for boards', 'sent to boards on USB or Bluetooth', null, h('div', { class: 'blk pad' },
    h('div', { class: 'fields' },
      h('div', { class: 'field' }, h('label', { for: 'ssid' }, 'Network name'), h('input', { id: 'ssid', type: 'text', autocomplete: 'off', value: s.wifi?.ssid || '', 'data-k': 'ssid' })),
      h('div', { class: 'field' }, h('label', { for: 'pw' }, 'Password'), h('input', { id: 'pw', type: 'password', autocomplete: 'new-password', placeholder: s.wifi?.ssid ? 'type it again to change it' : '', 'data-k': 'pw' }))),
    err,
    h('div', { class: 'actions' },
      h('span', { class: 'sub grow' }, '2.4 GHz only.'),
      s.wifi?.ssid ? btn('Cancel', () => { S.open.wifiMain = false; draw(true); }) : null,
      btn('Save and send to boards', async (e) => {
        err.textContent = '';
        const b = e.currentTarget;
        b.disabled = true;
        try {
          const r = await api('PATCH', '/admin/v1/settings', { wifi: { ssid: $('#ssid').value.trim(), password: $('#pw').value, send: true } });
          S.settings = r.settings;
          const n = (r.sent || []).length;
          toast(n ? `Saved and sent to ${plural(n, 'board')} on USB or Bluetooth` : 'Saved. Boards get it when they\'re plugged in or on Bluetooth.');
          $('#pw').value = '';
          S.open.wifiMain = false;
          draw(true);
        } catch (ex) {
          err.textContent = ex.message;
        } finally {
          if (b.isConnected) b.disabled = false;
        }
      }, 'p')))));

  const boards = (S.devices?.devices || []).filter((d) => d.kind === 'serial' && d.state === 'active');
  const onWifi = boards.filter((d) => d.wifi?.state === 'connected' || d.connection === 'wifi').length;
  const rows = boards.map((d) => {
    const right = [];
    const inHand = d.connection === 'usb' || d.connection === 'bluetooth';
    if (inHand && d.canWifi !== false && S.open.wifi !== d.id) {
      if (s.wifi?.ssid && (d.wifi?.own || !d.wifi?.ssid)) {
        right.push(btn('Send Wi-Fi', (e) => deviceAction(d, 'wifi', { useDefault: true }, e.currentTarget, (r) => r?.message || 'Sent'), 'sm', { title: `Send ${s.wifi.ssid} to it` }));
      }
      right.push(btn('Change Wi-Fi', () => { S.open.wifi = d.id; draw(); $(`#ssid-${CSS.escape(d.id)}`)?.focus(); }, 'sm', { title: 'Give it another network' }));
    } else if (!d.online) right.push(chip('Offline', 'off st', 'offline', { title: d.canWifi !== false ? 'Plug it in to change its Wi-Fi' : null }));
    return row(h('a', { href: `#board/${encodeURIComponent(d.id)}` }, d.name), null, right, { cls: 'fixr', name: true, off: !d.online, below: linkDots(d), pic: thumb(devScreen(d, 'xs')), extra: S.open.wifi === d.id ? wifiForm(d, () => load('devices')) : null });
  });
  if (!rows.length) rows.push(h('div', { class: 'row thin' }, h('span', { class: 'txt dim' }, 'No boards yet.')));
  out.push(sec('Boards on Wi-Fi', boards.length ? `${onWifi} of ${boards.length}` : null, ['Boards', '#boards'], blk(...rows)));

  out.push(...bluetoothNetwork());
  out.push(phones(s));
  return out;
}

// bluetoothNetwork: whether Bluetooth is on, and what to do when it isn't.
function bluetoothNetwork() {
  const bt = S.bt;
  if (!bt) return [];
  let title = 'Bluetooth is on';
  let meta = 'Boards use it when there is no cable and no Wi-Fi. New boards can be set up through it.';
  if (!bt.available) {
    title = { none: 'Bluetooth isn\'t available here', unauthorized: 'Bluetooth needs your permission', off: 'Bluetooth is off' }[bt.helper === 'none' ? 'none' : bt.radio] || 'Bluetooth is starting';
    meta = bt.why || '';
  }
  const content = [h('div', { class: 'cards' }, h('div', { class: 'blk fit' }, row(bt.available ? h('span', { class: 'ttl' }, 'Bluetooth ', dot('on', 'on')) : title, bt.available ? null : meta,
    bt.available ? [link('Add a board', '#boards/add', '', { title: 'New boards can be set up over Bluetooth' })] : [chip(bt.radio === 'unauthorized' ? 'Blocked' : 'Off', 'off st')], { pic: ico('bluetooth', bt.available ? 'ok' : '') })))];
  if (bt.radio === 'unauthorized' || (bt.available === false && bt.helper !== 'none' && bt.radio !== 'off')) {
    content.push(note('macOS asks once whether "agents deck Bluetooth" may use Bluetooth. If the answer was no, turn it on in System Settings, Privacy & Security, Bluetooth.'));
  }
  if (bt.lastError) content.push(h('p', { class: 'sub' }, `Last problem: ${bt.lastError}`));
  return [sec('Bluetooth', null, null, ...content)];
}

// phones: pairing a phone or a desk panel, and the ones paired.
function phones(s) {
  const devs = (S.devices?.devices || []).filter((d) => d.kind === 'network');
  const content = [];
  const pairBtn = h('button', { type: 'button', class: 'btn p sm', disabled: !s.networkRunning, 'data-k': 'pair', onclick: () => { S.pairing = { form: true }; draw(); $('#pair-name')?.focus(); } }, icon('plus', 14, 2.2), 'Pair a phone or panel');
  pairBtn.title = 'With a one-time code; it sees the deck while it is on your network';
  if (!S.pairing) content.push(blk(row('Pair a phone or panel', 'With a one-time code', [pairBtn], { pic: ico('phone') })));
  if (!s.networkRunning && !S.pairing) content.push(note('Turn on Paired devices over the network above to pair one.'));
  if (S.pairing?.form) {
    const scope = S.pairing.scope || 'view';
    content.push(h('div', { class: 'blk pad' },
      h('span', { class: 'eyebrow acc2' }, 'Pair a phone or panel'),
      h('div', { class: 'fields' },
        h('div', { class: 'field' }, h('label', { for: 'pair-name' }, 'A name for it'), h('input', { id: 'pair-name', type: 'text', value: S.pairing.name || 'phone', 'data-k': 'pair-name' })),
        h('div', { class: 'field' }, h('span', { class: 'kl' }, 'It may'), seg('What it may do', SCOPES.slice(0, 2), scope, (v) => { S.pairing.scope = v; S.pairing.name = $('#pair-name').value; draw(); }, { key: 'pair-scope' }))),
      h('div', { class: 'actions' },
        btn('Make a code', async (e) => {
          const r = await busy(e.currentTarget, () => api('POST', '/admin/v1/pair', { name: $('#pair-name').value.trim(), scope }));
          if (r) { S.pairing = { code: r }; draw(); }
        }, 'p'),
        btn('Cancel', () => { S.pairing = null; draw(); }))));
  } else if (S.pairing?.code) {
    const c = S.pairing.code;
    const code = String(c.code || '');
    const half = code.length === 6 ? [code.slice(0, 3), code.slice(3)] : [code, ''];
    content.push(h('div', { class: 'blk pad' },
      h('span', { class: 'eyebrow acc2' }, 'Pair a phone or panel · the code'),
      h('div', { class: 'term hot', 'aria-label': 'Pairing details' }, h('div', { class: 'bar' }, h('i', {}), 'pairing · works once, for 2 min'),
        h('div', { class: 'body' },
          h('div', {}, h('span', { class: 'd' }, 'address  '), h('span', { class: 'w' }, c.url)),
          h('div', { class: 'big' }, half[0], half[1] ? ' ' : '', h('span', { class: 'p' }, half[1])),
          h('div', {}, h('span', { class: 'd' }, 'deck  '), c.ca))),
      h('span', { class: 'sub' }, 'Type these in the app on the phone or panel.'),
      h('div', { class: 'actions' }, btn('Done', () => { S.pairing = null; draw(); }, 'p'))));
  }
  if (devs.length) {
    content.push(blk(...devs.map((d) => {
      const seen = d.online ? 'seen now' : d.lastSeen ? `seen ${ago(d.lastSeen)}` : 'never connected';
      const right = [d.state === 'revoked' ? chip('Cut off', 'off st') : d.state === 'waiting' ? chip('Waiting', 'fill st') : chip(scopeWords(d.scope), 'acc st')];
      if (d.state === 'waiting') right.push(btn('Approve', (e) => deviceAction(d, 'approve', {}, e.currentTarget, `${d.name} approved`), 'p'));
      if (d.state === 'revoked') right.push(btn('Remove', (e) => deviceAction(d, 'remove', {}, e.currentTarget, `${d.name} removed`)));
      else right.push(btn('Cut off', (e) => deviceAction(d, 'revoke', {}, e.currentTarget, `${d.name} can't connect any more`), 'd', { 'aria-label': `Cut off ${d.name}` }));
      return row(h('a', { href: `#board/${encodeURIComponent(d.id)}` }, d.name), null, right, { below: h('span', { class: 'ldots' }, h('span', { class: `ld ${d.online ? 'on' : 'off'}` }, dot(d.online ? 'on' : 'off', d.online ? 'connected' : 'not connected'), h('span', { class: 'ssid' }, d.online ? 'connected' : seen))), name: true, off: d.state === 'revoked', pic: ico(/panel/i.test(d.model || d.type || '') ? 'panel' : 'phone') });
    })));
  }
  return sec('Phones and panels', devs.length ? `${devs.length} paired` : null, null, ...content);
}

async function restartService(btnEl) {
  const r = await busy(btnEl, () => api('POST', '/admin/v1/restart', {}));
  if (r) {
    toast(r.message);
    setTimeout(() => refresh(true), 4000);
  }
}

// ---- Updates -------------------------------------------------------------------------------

function notesLines(notes) {
  return (notes || '').split('\n').map((l) => l.replace(/^\s*(?:[-*]|#+)\s*/, '').trim()).filter(Boolean).slice(0, 6);
}

function drawUpdates() {
  const u = S.update;
  const job = S.jobs[u.job || S.appJob] || null;
  const showUpd = appUpdate(u) && S.laterUpdate !== u.version;
  let title;
  if (job && job.state === 'running') title = head('Updates', `Installing ${job.version || u.version}`, 'now.');
  else if (showUpd) title = head('Updates', `${u.version} is`, u.state === 'ready' ? 'ready.' : 'out.');
  else if (u.state === 'none') title = head('Updates', 'You have the', 'latest.');
  else title = head('Updates', `You have ${u.current}.`, null);
  const out = [title];
  if (job && job.state === 'running') {
    out.push(h('div', { class: 'blk pad' }, h('span', { class: 'eyebrow acc2' }, 'Installing'), h('span', { class: 'nm' }, `agents deck ${job.version || u.version}`),
      h('p', { class: 'txt', role: 'status' }, job.message || 'Replacing the app. It restarts by itself.'), chip('Installing', 'acc', 'dot')));
  } else if (showUpd) {
    const notes = notesLines(u.notes);
    const can = u.state === 'ready' || (u.state === 'available' && !u.message);
    out.push(h('div', { class: 'term hot' }, h('div', { class: 'bar' }, h('i', {}), u.state === 'ready' ? 'update · downloaded and checked' : 'update · out'),
      h('div', { class: 'body' },
        h('div', {}, h('span', { class: 'p' }, '$ '), h('span', { class: 'w' }, 'agentctl update')),
        h('div', {}, h('span', { class: 'd' }, u.current), ' → ', h('span', { class: 'w' }, u.version)),
        ...(notes.length ? notes : ['A new version is out.']).map((l) => h('div', {}, h('span', { class: 'p' }, '+ '), l))),
      h('div', { class: 'ln' },
        can ? btn('Install and restart', (e) => installApp(e.currentTarget), 'p') : u.page ? h('a', { class: 'btn sm p', href: u.page, rel: 'noreferrer' }, 'Download page') : null,
        btn('Later', () => { S.laterUpdate = u.version; draw(); }),
        h('span', { class: 'sub' }, u.message || 'Agents keep running. A few seconds.'))));
  } else if (u.state === 'disabled') {
    out.push(blk(row('This copy doesn\'t update itself', u.message || 'Update it the way you installed it.', [u.page ? h('a', { class: 'btn sm', href: u.page, rel: 'noreferrer' }, 'Download page') : null], { name: true, pic: ico('board') })));
  } else if (u.state === 'none' && u.version) {
    out.push(blk(row(`${u.version} is the latest`, u.checked ? `Checked ${ago(u.checked)}.` : '', [chip('Up to date', 'ok st', 'dot')], { name: true, pic: ico('check') })));
  }
  if (job && job.state === 'failed') {
    out.push(h('div', { class: 'blk pad err', role: 'alert' }, h('span', { class: 'eyebrow acc2' }, 'Didn\'t install'), h('span', { class: 'nm' }, 'The update didn\'t install'),
      h('p', { class: 'txt' }, `${job.error}. Nothing changed.`), h('div', { class: 'actions' }, btn('Try again', (e) => installApp(e.currentTarget), 'p'))));
  }
  if (u.error) {
    out.push(h('div', { class: 'term' }, h('div', { class: 'body' }, h('div', {}, h('span', { class: 'p' }, '$ '), h('span', { class: 'w' }, 'agentctl update')), h('div', { class: 'badc' }, `The last check didn't work: ${u.error}`))));
  }
  out.push(sec('How to update', 'from GitHub, checked before use', null, blk(row('The app and its service', u.checked ? `Last checked ${ago(u.checked)}.` : 'Not checked yet.', [
    seg('Update mode', [{ value: 'notify', text: 'Tell me' }, { value: 'auto', text: 'Automatic' }, { value: 'off', text: 'Never check' }], u.mode, async (v) => {
      const r = await patchSettings({ updates: v });
      if (r) { await load('update'); draw(); }
    }),
    btn('Check now', (e) => busy(e.currentTarget, async () => {
      S.update = await api('POST', '/admin/v1/update/check', {});
      S.laterUpdate = '';
      draw();
    }, () => (S.update.state === 'none' ? 'Up to date' : `Found ${S.update.version}`)), '', { 'data-k': 'check-now' }),
  ], { name: true, pic: ico('restart') }))));

  const devs = S.devices?.devices || [];
  for (const j of S.jobList?.jobs || []) putJob(j);
  const withUpd = devs.filter((d) => d.update || d.job);
  const rows = withUpd.map((d) => {
    if (d.job) return row(h('a', { href: `#board/${encodeURIComponent(d.id)}` }, d.name), null, [], { name: true, pic: thumb(devScreen(d, 'xs')), below: jobLine(d, S.jobs[d.job]) });
    const how = { usb: 'over USB · about a minute', wifi: 'over Wi-Fi · about 2 minutes', unplugged: 'plug it in to update' }[d.update.how];
    const from = d.update.source === 'local' ? ' built on this computer' : '';
    const meta = `${d.firmware} → ${d.update.version}${from} · ${d.update.ready ? how : d.update.note || how}`;
    return row(h('a', { href: `#board/${encodeURIComponent(d.id)}` }, d.name), meta,
      d.update.ready ? [btn(from ? 'Install' : 'Update', (e) => updateDevice(d, e.currentTarget), 'p')] : [!d.online ? chip('Offline', 'off st', 'offline') : null],
      { pic: thumb(devScreen(d, 'xs')), off: !d.online, name: true });
  });
  const latest = S.devices?.latest;
  const current = latest ? devs.filter((d) => d.kind === 'serial' && d.firmware === latest).length : 0;
  let foot = '';
  if (!devs.some((d) => d.kind === 'serial')) foot = 'No boards yet.';
  else if (!withUpd.length && latest) foot = `Every board is on ${latest}.`;
  else if (current) foot = `${current === 1 ? 'One board is' : `${say(current)} boards are`} already on ${latest}.`;
  if (!latest && S.devices?.catalogError) foot += ` Couldn't read the latest firmware list: ${S.devices.catalogError}`;
  out.push(sec('Board firmware', 'boards never update by themselves', null, rows.length ? blk(...rows) : null, foot ? h('span', { class: 'sub' }, foot) : null));
  return out;
}

// ---- Troubleshoot ----------------------------------------------------------------------------

function drawTroubleshoot() {
  const d = S.diag;
  const sys = S.system || {};
  const out = [connOK ? head('Troubleshoot', 'Running for', `${d.uptime}.`) : head('Troubleshoot', 'The service isn\'t', 'answering.')];
  const stat = (k, v, cls) => h('div', { class: 'stat' }, h('span', { class: 'kl' }, k), h('span', { class: `v ${cls || ''}`.trim() }, v));
  out.push(h('div', { class: 'blk stats4' },
    stat('Service', connOK ? [h('span', { class: 'sq ok' }), 'running'] : [h('span', { class: 'sq' }), 'down']),
    stat('Up for', d.uptime), stat('Agents', String(d.agents)), stat('Boards online', `${d.boardsOnline} of ${d.boards}`)));
  out.push(blk(
    row('Restart the service', `Running since ${new Date(d.started).toLocaleString()}. Agents keep running. Boards reconnect in seconds.`, [btn('Restart', (e) => restartService(e.currentTarget))], { name: true, pic: ico('restart') }),
    row('Copy a report', 'Versions, system, board models. No names, folders or prompts.', [
      btn(S.showReport ? 'Hide it' : 'Show', () => { S.showReport = !S.showReport; draw(); }, '', { 'aria-expanded': S.showReport ? 'true' : 'false', 'aria-controls': 'report', 'data-k': 'show-report' }),
      btn('Copy report', async () => {
        const ok = await copyText(d.report);
        toast(ok ? 'Copied the report' : 'Couldn\'t copy: use Show and copy it by hand', !ok);
      }, 'p'),
    ], { name: true, pic: ico('copy') })));
  if (S.showReport) out.push(h('pre', { class: 'report', id: 'report', tabindex: '0' }, d.report));
  const openData = (e) => busy(e.currentTarget, () => api('POST', '/admin/v1/open-data-folder', {}));
  const logSec = sec('Latest from the log', 'problems in red', null);
  logSec.querySelector('.rule').append(h('button', { type: 'button', class: 'btn link small', onclick: openData }, 'Open the log folder →'));
  if (!d.log.length) {
    logSec.append(h('div', { class: 'term' }, h('div', { class: 'body' }, h('div', {}, h('span', { class: 'p' }, '$ '), h('span', { class: 'w' }, 'tail agentctl.log')), h('div', { class: 'd' }, 'nothing yet'))),
      h('p', { class: 'txt' }, 'Or the service writes it elsewhere: ', h('span', { class: 'mono' }, 'agentctl serve'), ' in a terminal prints it there.'));
  } else {
    const ul = h('ul', { class: 'log', 'aria-label': 'The last lines of the log' });
    d.log.forEach((l) => ul.append(h('li', { class: /error|fail|stopped answering|refused/i.test(l) ? 'em' : '' }, l)));
    logSec.append(ul);
  }
  out.push(logSec);
  const inData = d.logFile && d.dataDir && d.logFile.startsWith(d.dataDir);
  out.push(sec('Where its files are', null, null, blk(
    row('Data folder', d.dataDir, [btn('Show', openData, '', { 'aria-label': 'Show the data folder' })], { name: true, pic: ico('board') }),
    row('Log', d.logFile, inData ? [btn('Show', openData, '', { 'aria-label': 'Show the log' })] : [], { name: true, pic: ico('copy') })),
  sys.platform ? h('span', { class: 'sub' }, `agents deck ${sys.version} · ${sys.platform} · installed as ${sys.install}`) : null));
  return out;
}

// ---- About ----------------------------------------------------------------------------------

function drawAbout() {
  const sys = S.system || {};
  const u = S.update || {};
  const kinds = { app: 'the app', windows: 'the Windows installer', deb: 'a .deb package', zip: 'agentctl.exe from the .zip', archive: 'install.sh', source: 'built from source' };
  const os = { darwin: 'macOS', linux: 'Linux', windows: 'Windows' }[(sys.platform || '').split('/')[0]] || sys.platform || '';
  const newer = u.version && u.version !== sys.version && appUpdate(u);
  const lk = (href, title, sub) => h('a', { class: 'lk', href, rel: 'noreferrer', target: '_blank' }, h('span', { class: 'lki' }, icon('out', 16)), h('span', { class: 'serif' }, title), h('span', { class: 'sub' }, sub));
  const agents = agentsList().filter((a) => a.status !== 'exited').length;
  return [
    h('header', { class: 'abouthd' }, h('span', { class: 'aboutspr' }, critter({ hat: { shape: 'propeller', color: 'blue' }, spin: true, cut: '#1A1A18', centre: true })),
      h('div', { class: 'hd' }, h('span', { class: 'eyebrow acc' }, 'About'), h('h1', {}, 'Your agents, on a ', h('em', {}, 'deck.')),
        h('div', { class: 'actions' }, h('span', { class: 'nm' }, `agents deck ${sys.version || ''}`), h('span', { class: 'sub' }, `${os} · installed with ${kinds[sys.install] || sys.install || 'unknown'}`),
          newer ? chip(`${u.version} is the latest`, 'acc') : null, newer ? h('a', { class: 'btn link small', href: '#updates' }, 'See the update →') : null))),
    h('div', { class: 'lks' },
      lk('https://github.com/brushknight/agents-deck', 'Source and issues', 'github.com/brushknight/agents-deck · the licence'),
      lk('https://github.com/brushknight/agents-deck/tree/main/docs', 'Documentation', 'How the deck, boards and API work.'),
      lk('https://brushknight.com/agents-deck', 'The web flasher', 'Firmware on a board from Chrome or Edge.')),
    h('div', { class: 'term' }, h('div', { class: 'bar' }, h('i', {}), 'In a terminal'), h('div', { class: 'body' },
      h('div', {}, h('span', { class: 'p' }, '$ '), h('span', { class: 'w' }, 'agentctl version')),
      h('div', { class: 'd' }, `agentctl ${sys.version || ''}`),
      h('div', {}, h('span', { class: 'p' }, '$ '), h('span', { class: 'w' }, 'agentctl status')),
      h('div', { class: 'd' }, connOK ? h('span', { class: 'okc' }, 'running') : h('span', { class: 'badc' }, 'not answering'), ` · ${plural(agents, 'agent')} · ${sys.boards?.online ?? 0} of ${sys.boards?.total ?? 0} boards`))),
  ];
}

// ---- pages and routing ------------------------------------------------------------------------

const DRAW = {
  home: ['home-body', drawHome], agent: ['agent-body', drawAgent], start: ['start-body', drawStart], general: ['general-body', drawGeneral], agents: ['agents-body', drawAgents], boards: ['boards-body', drawBoards],
  board: ['board-body', drawBoard], newboard: ['newboard-body', drawNewBoard], flash: ['newboard-body', drawFlash], network: ['network-body', drawNetwork],
  updates: ['updates-body', drawUpdates], troubleshoot: ['troubleshoot-body', drawTroubleshoot], about: ['about-body', drawAbout],
};

// draw redraws the open page. The timer's redraw (force false) leaves a page
// alone while you type in it; a redraw after your own action doesn't.
function draw(force = true) {
  const page = S.page;
  const [id, fn] = DRAW[page];
  const err = firstError(page);
  if (err) {
    if (err.status === 401) return;
    render(id, () => errorBox(err.status === 0 ? "The service isn't answering. It may be restarting: this page tries again by itself." : err.message, () => refresh(true)));
    return;
  }
  if (needs(page).some((n) => !S[n])) return;
  render(id, fn, force);
}

function parseHash() {
  const raw = decodeURIComponent((location.hash || '').slice(1));
  const [page, ...rest] = raw.split('/');
  const arg = rest.join('/');
  if (page === 'new-board') return ['newboard', arg];
  if (page === 'flash') return ['flash', arg];
  if (page === 'board' && arg) return ['board', arg];
  if (page === 'agent' && arg) return ['agent', arg];
  if (page === 'home' && arg) return ['home', arg];
  if (page === 'start') return ['start', arg];
  if (page === 'agents') return ['agents', arg];
  if (page === 'help') return ['troubleshoot', ''];
  if (page === 'boards') return ['boards', arg];
  return [PAGES.includes(page) ? page : 'home', ''];
}

const SECTION = { flash: 'newboard' };
const NAV = { newboard: 'boards', flash: 'boards', board: 'boards', agent: 'home', start: 'home' };

async function route(initial) {
  const [page, arg] = parseHash();
  // On Home, a tile only opens or swaps the sidebar: the deck stays as it is.
  if (!initial && page === 'home' && S.page === 'home' && arg !== S.arg && (deck?.grid.isConnected || (S.panel && $('.pgrid')))) {
    const opening = !S.arg && arg;
    S.arg = arg;
    if (S.panel) updatePanel(); else updateDeck();
    if (opening) bar?.el.querySelector('h2')?.focus({ preventScroll: true });
    else if (!arg) document.querySelector(`.tile[data-id="${CSS.escape(bar?.id || '')}"]`)?.focus({ preventScroll: true });
    return;
  }
  const changed = page !== S.page || arg !== S.arg;
  S.page = page; S.arg = arg;
  if (page === 'newboard' && (!S.nb || S.nb.port !== arg)) S.nb = { port: arg, step: 1, choice: '' };
  if (changed) { S.open = { wifi: '', rename: '', project: '' }; S.confirmLoginOff = false; agentView = null; }
  if (page !== 'start') S.startForm = null;
  if (page !== 'home') S.panel = false; // Panel view is a way to see Home
  const section = SECTION[page] || page;
  document.querySelectorAll('.page > section').forEach((s) => { s.hidden = s.dataset.page !== section; });
  let navPage = NAV[page] || page;
  if (page === 'board') {
    const d = (S.devices?.devices || []).find((x) => x.id === arg);
    if (d && d.kind === 'network') navPage = 'network';
  }
  document.querySelectorAll('.nav a').forEach((a) => {
    if (a.dataset.page === navPage) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
  });
  document.title = `agents deck · ${navPage}`;
  drawTop();
  if (changed) $('#main').scrollTop = 0;
  if (page === 'flash' && arg && !S.jobs[arg]) {
    try { S.jobs[arg] = await api('GET', `/admin/v1/jobs/${encodeURIComponent(arg)}`); } catch (e) { S.errors.job = e; }
  }
  await loadAll(...needs(page), ...extras(page));
  draw();
  syncBar();
  if (page === 'agents' && arg === 'projects') document.getElementById('projects')?.scrollIntoView({ block: 'start' });
  if (changed && !initial) {
    const hd = document.querySelector(`.page > section[data-page="${section}"] h1`);
    if (hd) { hd.tabIndex = -1; hd.focus({ preventScroll: true }); }
  }
}

// refresh reloads the open page's data now and then (boards come and go,
// agents start waiting).
async function refresh(force) {
  if (document.hidden && !force) return;
  const page = S.page;
  if (!page) return;
  const [sys] = await Promise.all([load('system'), load('state')]);
  if (S.errors.system?.status === 404) {
    $('#banner').hidden = false;
    $('#banner').textContent = 'This service is too old for Settings: update agents deck.';
    return;
  }
  showSystem(sys);
  const busyUI = S.open.wifi || S.open.rename || S.open.project || S.pairing || S.confirmLoginOff || (S.mcpNew && !S.mcpNew.token);
  if (!force && (busyUI || ['general', 'about'].includes(page))) return;
  await loadAll(...needs(page), ...extras(page));
  if (page !== S.page) return;
  if ((page === 'home' && deck?.grid.isConnected) || (page === 'agent' && agentView)) updateDeck();
  else draw(false);
}

// While it looks for boards over Bluetooth, the list follows every 2 seconds.
setInterval(async () => {
  if (document.hidden || S.page !== 'boards' || S.arg !== 'add' || !S.bt?.scanning) return;
  await loadAll('bt', 'btBoards');
  if (S.page === 'boards') draw(false);
}, 2000);

// ---- the deck's live state -----------------------------------------------------------------

// The service sends its whole state on every change (/v1/events); the deck,
// an agent's page and the counts follow it, at most once a frame.
let stateES = null, stateQueued = false;
function listenState() {
  if (stateES) stateES.close();
  stateES = new EventSource('/v1/events');
  stateES.addEventListener('state', (e) => {
    try { S.state = JSON.parse(e.data); } catch (x) { return; }
    if (S.state?.server?.time) setSkew(Date.parse(S.state.server.time) - Date.now());
    if (stateQueued) return;
    stateQueued = true;
    requestAnimationFrame(() => {
      stateQueued = false;
      showSystem(S.system);
      if (['home', 'agent'].includes(S.page) && !S.panel) updateDeck();
      else if (S.page === 'home' && S.panel) updatePanel();
    });
  });
  stateES.onerror = () => { if (stateES.readyState === EventSource.CLOSED) setTimeout(listenState, 3000); };
}

// Keys on the deck: Esc goes back to the tiles, 1 to 9 answer the agent
// that asks, ← and → turn the deck's pages.
document.addEventListener('keydown', (e) => {
  if (e.metaKey || e.ctrlKey || e.altKey) return;
  const typing = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName || '');
  if (e.key === 'Escape') {
    if (S.page === 'home' && S.arg && (!typing || bar?.el.contains(document.activeElement))) { document.activeElement?.blur(); closeBar(); return; }
    if (S.page === 'home' && S.panel) { S.panel = false; draw(true); return; }
    if ((S.page === 'agent' || S.page === 'start') && !typing) location.hash = '#home';
    return;
  }
  if (!typing && S.page === 'agent' && agentView?.answer && /^[1-9]$/.test(e.key)) { e.preventDefault(); agentView.answer(e.key); }
  if (!typing && S.page === 'home' && S.arg && bar?.answer && /^[1-9]$/.test(e.key)) { e.preventDefault(); bar.answer(e.key); }
  if (!typing && S.page === 'home' && S.arg && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) { e.preventDefault(); barStep(e.key === 'ArrowRight' ? 1 : -1); return; }
  const inSwitch = document.activeElement?.getAttribute('role') === 'radio' && !document.activeElement.closest('.pager, .pvbar');
  if (!typing && S.page === 'home' && (e.key === 'ArrowLeft' || e.key === 'ArrowRight') && !inSwitch) {
    e.preventDefault();
    const next = (S.deckPage || 0) + (e.key === 'ArrowRight' ? 1 : -1);
    if (next >= 0 && next < pageCount()) goPage(next);
  }
});

// ---- live job progress -----------------------------------------------------------------------

let es = null;
function listen() {
  if (es) es.close();
  es = new EventSource('/admin/v1/events');
  es.addEventListener('system', (e) => { try { showSystem(JSON.parse(e.data)); setConn(true); } catch (x) { /* ignore */ } });
  es.addEventListener('job', (e) => {
    let j;
    try { j = JSON.parse(e.data); } catch (x) { return; }
    putJob(j);
    if (S.page === 'flash' && S.arg === j.id) draw();
    if ((S.page === 'boards' || S.page === 'board') && j.device && j.state === 'running') draw(false);
    if (j.kind === 'bluetooth-add' && (S.page === 'boards' || S.page === 'board') && Object.values(S.btAdd).includes(j.id)) draw(true);
    if (j.kind === 'app-update' && j.state === 'failed') toast(`The update didn't install: ${j.error}`, true);
    if ((S.page === 'updates' || S.page === 'home') && j.kind === 'app-update') draw(true);
    if (j.state !== 'running' && ['boards', 'board', 'updates', 'home'].includes(S.page)) refresh(true);
  });
  es.onerror = () => {
    if (es.readyState === EventSource.CLOSED) setTimeout(listen, 3000);
  };
}

// ---- start ------------------------------------------------------------------------------------

$('#logo').replaceWith(Object.assign(critter({ hat: { shape: 'propeller', color: 'blue' }, spin: true, cut: '#141412' }), { id: 'logo' }));
let folded = false;
try { folded = localStorage.getItem('settings.sidebar') === 'folded'; } catch (e) { folded = false; }
setFolded(folded);
$('#fold').addEventListener('click', () => setFolded(!document.documentElement.classList.contains('folded')));

window.addEventListener('hashchange', () => route(false));
document.addEventListener('visibilitychange', () => { if (!document.hidden) refresh(true); });
setInterval(() => refresh(false), 4000);

(async () => {
  const [sys] = await Promise.all([load('system'), load('state')]);
  if (S.errors.system?.status === 404) {
    $('#banner').hidden = false;
    $('#banner').textContent = 'This service is too old for Settings: update agents deck.';
  }
  showSystem(sys);
  if (new URLSearchParams(location.search).has('panel')) S.panel = true;
  await route(true);
  listen();
  listenState();
})();
