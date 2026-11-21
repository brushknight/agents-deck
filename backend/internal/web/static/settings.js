// agents deck Settings. Every control calls the service's management API
// (/admin/v1, docs/protocol.md): the same calls the CLI makes. Plain DOM, no
// innerHTML: names and messages from the service are always text.

const PAGES = ['general', 'agents', 'boards', 'network', 'updates', 'troubleshoot', 'about'];
const SCOPES = [
  { value: 'view', text: 'look only' },
  { value: 'act', text: 'can answer' },
  { value: 'admin', text: 'can manage' },
];
const scopeWords = (s) => (SCOPES.find((x) => x.value === s) || { text: s }).text;

// In the agents deck app the page sits in a native window: the shell
// injects window.agentsDeckApp at document start (and marks <html> with
// .in-app and .app-<platform>), so the page drops its own top bar. Its user
// agent carries "AgentsDeckApp/<platform>" too, for older injections.
const APP = (() => {
  const a = window.agentsDeckApp;
  if (a && typeof a.platform === 'string') return a.platform;
  const m = /AgentsDeckApp\/(\w+)/.exec(navigator.userAgent);
  return m ? m[1] : '';
})();
if (APP) document.documentElement.classList.add('in-app', `app-${APP}`);

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
  system: null, settings: null, devices: null, ports: null, update: null, agents: null, live: null, diag: null,
  jobs: {},
  open: { more: '', wifi: '', name: '' },
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
  btFind: {}, // device id -> looking for that board over Bluetooth, to add it from its row
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

class ApiError extends Error {
  constructor(status, msg, data) { super(msg); this.status = status; this.data = data || {}; }
}

let connOK = true;
function setConn(ok, why) {
  connOK = ok;
  const c = $('#conn');
  c.textContent = ok ? '' : 'not connected';
  c.classList.toggle('bad', !ok);
  const b = $('#banner');
  if (ok) { b.hidden = true; } else { b.hidden = false; b.textContent = why || "The service isn't answering. It may be restarting: this page tries again by itself."; }
  $('#foot-state').textContent = ok ? 'service running' : 'service not answering';
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
  if (s < 3600) return `${Math.round(s / 60)} minutes ago`;
  if (s < 2 * 3600) return 'an hour ago';
  if (s < 24 * 3600) return `${Math.round(s / 3600)} hours ago`;
  if (s < 48 * 3600) return 'yesterday';
  return `${Math.round(s / 86400)} days ago`;
}

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

// ---- controls ------------------------------------------------------------------------

function toggle(label, on, onChange, opts = {}) {
  return h('button', {
    class: 'tg', type: 'button', role: 'switch', 'aria-checked': on ? 'true' : 'false', 'aria-label': label,
    disabled: opts.disabled, 'data-k': opts.key || label,
    onclick: (e) => onChange(!on, e.currentTarget),
  });
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
    }, o.text));
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

// row: a title, a line of meta, and controls on the right. opts.name marks
// the title as a name (a board's, an account's): one line, cut with … and
// whole in its tooltip.
function row(title, meta, right, opts = {}) {
  return h('div', { class: `row${opts.hot ? ' hot' : ''}${opts.tall ? ' tall' : ''}`, 'data-row': opts.id },
    opts.pic,
    h('div', { class: 'tx' },
      h('div', { class: `t${opts.off ? ' off' : ''}${opts.name ? ' name' : ''}`, id: opts.titleId, title: opts.name && typeof title === 'string' ? title : null }, title),
      meta ? h('div', { class: 'm' }, meta) : null),
    right && right.length !== 0 ? h('div', { class: 'r' }, right) : null,
    opts.extra);
}

const lab = (text) => h('div', { class: 'lab' }, text);
const btn = (text, onclick, cls, attrs = {}) => h('button', { type: 'button', class: cls, onclick, ...attrs }, text);

function shapeOf(s) {
  s = (s || '').toLowerCase();
  if (/round/.test(s)) return 'round';
  if (/strip|1\.47/.test(s)) return 'strip';
  if (/t4|tall/.test(s)) return 'tall';
  if (/amoled|1\.8/.test(s)) return 'small';
  if (/phone/.test(s)) return 'phone';
  return 'square';
}
// The boards the page has a real screen picture for (static/boards/<board>.png).
function boardOf(s) {
  s = (s || '').toLowerCase();
  if (/t4s3|t4-s3/.test(s)) return 'lilygo-t4-s3';
  if (/1\.47|lcd-147|strip/.test(s)) return 'waveshare-lcd-147';
  if (/amoled|1\.8/.test(s)) return 'waveshare-touch-amoled-18';
  if (/2\.1|lcd-21/.test(s)) return 'waveshare-touch-lcd-21';
  if (/2\.8|lcd-28c|round/.test(s)) return 'waveshare-touch-lcd-28c';
  if (/lcd-4\b|4[″"]|square/.test(s)) return 'waveshare-touch-lcd-4';
  return '';
}
// frame is a board's outline; with a picture it is the board's own screen in a bezel.
const frame = (shape, board) => h('i', { class: `sh-${shape}${board ? ` bd bd-${board}` : ''}` });
const pic = (shape, off, board) => h('div', { class: `g${off ? ' off' : ''}`, 'aria-hidden': 'true' }, frame(shape, board));

// render swaps a page body, keeping focus on the same control when it is
// still there, and leaves it alone while you type in it.
function render(id, build, force) {
  const body = document.getElementById(id);
  const active = document.activeElement;
  if (active && body.contains(active) && (active.tagName === 'INPUT' || active.tagName === 'TEXTAREA') && !force) return;
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

function errorBox(msg, retry) {
  return h('div', { class: 'box err', role: 'alert' },
    h('p', {}, msg),
    retry ? h('div', { class: 'actions' }, btn('Try again', retry)) : null);
}

// putJob keeps the newest news of a job: the event stream can tell that a
// job ended before the answer that started it arrives.
function putJob(j) {
  const old = S.jobs[j.id];
  if (old && old.state !== 'running' && j.state === 'running') return;
  S.jobs[j.id] = j;
}

// ---- data ------------------------------------------------------------------------------

async function load(what) {
  const paths = {
    system: '/admin/v1/system', settings: '/admin/v1/settings', devices: '/admin/v1/devices',
    ports: '/admin/v1/ports', update: '/admin/v1/update', agents: '/admin/v1/agents', diag: '/admin/v1/diagnostics',
    live: '/v1/live', bt: '/admin/v1/bluetooth', btBoards: '/admin/v1/bluetooth/boards', jobList: '/admin/v1/jobs',
  };
  try {
    S[what] = await api('GET', paths[what]);
    delete S.errors[what];
  } catch (e) {
    S.errors[what] = e;
  }
  return S[what];
}

const loadAll = (...names) => Promise.all(names.map(load));

function needs(page) {
  return {
    general: ['settings'], agents: ['agents', 'live'], boards: ['devices', 'ports', 'settings'], network: ['settings', 'devices'],
    updates: ['update', 'devices'], troubleshoot: ['diag', 'system'], about: ['system', 'update'],
    newboard: ['ports', 'devices'], flash: ['devices', 'ports'],
  }[page] || [];
}

// extras: what a page shows when it can, and does without (an older service
// has no Bluetooth calls).
function extras(page) {
  return { boards: ['bt', 'btBoards', 'jobList'], network: ['bt'] }[page] || [];
}

function firstError(page) {
  for (const n of needs(page)) if (S.errors[n]) return S.errors[n];
  return null;
}

// ---- system: badges and the sidebar footer -----------------------------------------

function showSystem(sys) {
  if (!sys) return;
  S.system = sys;
  $('#foot-version').textContent = `agents deck ${sys.version}`;
  const nb = $('#n-boards');
  const newPorts = sys.ports?.new || 0;
  const pending = sys.boards?.pending || 0;
  nb.hidden = !(newPorts || pending);
  nb.textContent = newPorts ? `${newPorts} new` : `${pending} waiting`;
  const nu = $('#n-updates');
  const n = (['ready', 'available'].includes(sys.update?.state) ? 1 : 0) + (sys.firmware_updates || 0);
  nu.hidden = !n;
  nu.textContent = String(n);
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
  const out = [];
  out.push(lab('starting'));
  if (s.startAtLogin === null || s.startAtLogin === undefined) {
    out.push(row('Start when I log in', 'Not available on this computer: start the service with agentctl serve.', []));
  } else if (S.confirmLoginOff) {
    out.push(row('Start when I log in', 'Turning this off also stops the service now. Boards then show "waiting for the deck" until you start it again from the app or with agentctl serve.', [
      btn('Keep it on', () => { S.confirmLoginOff = false; draw(); }, '', { 'data-k': 'login-keep' }),
      btn('Turn off and stop', (e) => { S.confirmLoginOff = false; patchSettings({ startAtLogin: false }, e.currentTarget); }, 'p'),
    ]));
  } else {
    let what = 'The service. Without it, boards show "waiting for the deck".';
    if (s.trayAtLogin !== undefined) what = 'The service and the tray icon. Without it, boards show "waiting for the deck".';
    else if ((S.system?.platform || '').startsWith('darwin')) what += ' To open the app at login too, add agents deck in System Settings > General > Login Items.';
    const note = s.loginNote ? `${s.loginNote}.` : what;
    out.push(row('Start when I log in', note, [toggle('Start when I log in', s.startAtLogin, (on, el) => {
      if (!on) { S.confirmLoginOff = true; draw(); $('[data-k="login-keep"]')?.focus(); return; }
      patchSettings({ startAtLogin: true }, el);
    })]));
  }
  out.push(row('After a restart, bring agents back', 'Agents you started with agentctl are resumed where they stopped.', [
    seg('Restore agents', [{ value: 'ask', text: 'ask me' }, { value: 'auto', text: 'automatically' }], s.restore, (v) => patchSettings({ restore: v })),
  ]));
  out.push(lab('notifications'));
  out.push(row('Tell me', 'A click on a notification opens that agent\'s card.', [
    seg('Notifications', [
      { value: 'off', text: 'never' }, { value: 'attention', text: 'when an agent needs me' }, { value: 'finished', text: 'also when one finishes' },
    ], s.notifications, (v) => patchSettings({ notifications: v })),
  ]));
  out.push(row('Play a sound', null, [toggle('Play a sound', s.sound, (on, el) => patchSettings({ sound: on }, el), { disabled: s.notifications === 'off' })]));
  out.push(lab('for the terminal'));
  const cli = s.cli || {};
  const cliMeta = cli.installed
    ? `Installed${cli.path ? ` at ${cli.path}` : ''}. Type agentctl in any terminal to start and list agents.`
    : 'Not installed. With it you can type agentctl in any terminal to start and list agents.';
  const cliBtn = cli.canChange
    ? btn(cli.installed ? 'Remove' : 'Install', (e) => busy(e.currentTarget, async () => {
      S.settings.cli = await api('POST', '/admin/v1/cli', { installed: !cli.installed });
      draw();
    }, cli.installed ? 'Removed the agentctl command' : 'Installed the agentctl command'))
    : null;
  out.push(row('The agentctl command', cliMeta, [cliBtn]));
  out.push(row('The dashboard in your browser', 'The same deck and these settings, on this computer only.', [
    APP ? btn('Open in browser', () => { if (!appPost('dashboard')) location.href = '/'; }) : h('a', { class: 'b', href: '/' }, 'Open'),
  ]));
  return out;
}

// ---- Agents ------------------------------------------------------------------------------

// What a tool's card says on its first line: is it connected, found, missing or off.
function toolState(t) {
  const n = (k) => `${t.agents} ${k}${t.agents === 1 ? '' : 's'}`;
  switch (t.state || (t.on ? (t.agents ? 'connected' : t.found ? 'found' : 'missing') : 'off')) {
    case 'connected': return t.id === 'tmux' ? `connected · ${n('agent')}` : `connected · ${n(t.id === 'codex' ? 'session' : 'agent')}`;
    case 'found': return t.id === 'tmux' ? 'found · for agents you start here' : 'found · none on the deck';
    case 'off': return 'turned off';
    default: return 'not found';
  }
}

function drawAgents() {
  const a = S.agents;
  const out = [lab('tools')];
  const grid = h('div', { class: 'tools' });
  for (const t of a.tools) {
    const state = t.state || '';
    const off = state === 'off' || state === 'missing' || (!state && (!t.found || !t.on));
    const chip = off ? 'chip' : `chip ${t.id === 'claude' ? 'c-claude' : t.id === 'codex' ? 'c-codex' : 'c-on'}`;
    grid.append(h('div', { class: `tool${off ? ' off' : ''}` },
      h('div', { class: 'hd' }, h('span', { class: chip, 'aria-hidden': 'true' }), h('span', { class: 't' }, t.name)),
      h('div', { class: 'st' }, toolState(t)),
      t.detail ? h('div', { class: 'dt', title: t.detail }, t.detail) : null,
      t.note ? h('div', { class: 'nt' }, t.note) : null));
  }
  out.push(grid);

  out.push(lab('running here, not on the deck'));
  const live = (S.live?.sessions || []).filter((s) => !s.onBoard);
  if (S.errors.live) out.push(h('p', { class: 'note' }, `Couldn't list them: ${S.errors.live.message}`));
  else if (!live.length) out.push(h('p', { class: 'note' }, 'Nothing: every Claude session on this computer is on the deck.'));
  for (const s of live) {
    const where = s.background ? 'in the background' : 'in a terminal';
    out.push(row(s.title || s.folder || 'Claude session', `Claude Code · started ${ago(s.started)} · ${where}`, [
      btn('Add to deck', (e) => busy(e.currentTarget, async () => {
        await api('POST', `/v1/live/${encodeURIComponent(s.sessionId)}/add`, {});
        await load('live'); draw();
      }, 'Added to the deck')),
    ], { name: true }));
  }

  out.push(lab('accounts'));
  if (!a.accounts.length) out.push(h('p', { class: 'note' }, 'One Claude account. Add another with agentctl accounts add, to run agents in it.'));
  for (const ac of a.accounts) {
    const next = a.colors[(a.colors.indexOf(ac.color) + 1) % a.colors.length];
    out.push(row(ac.name, ac.default ? 'new agents start here' : null, [
      `${ac.agents} agent${ac.agents === 1 ? '' : 's'} · $${ac.costUsd.toFixed(2)}`,
      btn('Colour', (e) => busy(e.currentTarget, async () => {
        await api('POST', `/admin/v1/accounts/${encodeURIComponent(ac.name)}/color`, { color: next });
        await load('agents'); draw();
      }), '', { 'aria-label': `Colour of ${ac.name}: ${ac.color}. Change to ${next}` }),
    ], { pic: h('span', { class: `chip c-${ac.color}`, 'aria-hidden': 'true' }), name: true }));
  }
  return out;
}

// ---- Boards ------------------------------------------------------------------------------

const VIA = { usb: 'USB', wifi: 'Wi-Fi', bluetooth: 'Bluetooth' };

function deviceMeta(d) {
  const parts = [];
  parts.push(d.model || (d.kind === 'network' ? 'phone or panel' : d.type || 'board'));
  if (d.online) parts.push(VIA[d.connection] ? `connected over ${VIA[d.connection]}` : d.connection === 'network' ? 'over the network' : 'connected');
  else parts.push(d.lastSeen ? `last seen ${ago(d.lastSeen)}` : 'never connected');
  if (d.firmware) parts.push(`firmware ${d.firmware}`);
  if (d.state === 'revoked') parts.push('cut off');
  else parts.push(scopeWords(d.scope));
  return parts.join(' · ');
}

async function deviceAction(d, action, body, btnEl, okMsg) {
  const r = await busy(btnEl, () => api('POST', `/admin/v1/devices/${encodeURIComponent(d.id)}/${action}`, body), okMsg);
  await loadAll('devices', 'system');
  showSystem(S.system);
  draw(true);
  return r;
}

// updateDevice installs what the board's line offers: the latest release,
// or the build made on this computer (source local).
async function updateDevice(d, btnEl) {
  const st = d.status || {};
  const body = st.source === 'local' ? { source: 'local', version: st.latest } : {};
  const job = await busy(btnEl, () => api('POST', `/admin/v1/devices/${encodeURIComponent(d.id)}/update`, body));
  if (job && job.id) {
    putJob(job);
    location.hash = `#flash/${job.id}`;
  }
}

function wifiForm(d, onDone) {
  const ssidId = `ssid-${d.id}`, pwId = `pw-${d.id}`;
  const err = h('p', { class: 'm err', role: 'alert' });
  const form = h('div', { class: 'inline-form' },
    h('div', { class: 'grid2' },
      h('div', {}, h('label', { for: ssidId }, 'Network name'), h('input', { id: ssidId, type: 'text', autocomplete: 'off', value: S.settings?.wifi?.ssid || S.devices?.wifiDefault || '', 'data-k': ssidId })),
      h('div', {}, h('label', { for: pwId }, 'Password'), h('input', { id: pwId, type: 'password', autocomplete: 'new-password', 'data-k': pwId }))),
    err,
    h('div', { class: 'actions' },
      h('p', { class: 'note' }, `Sent over ${d.connection === 'bluetooth' ? 'the Bluetooth link' : 'the USB cable'}. The password is never kept on this computer.`),
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
  return form;
}

function morePanel(d) {
  const nameId = `name-${d.id}`;
  const lines = [];
  lines.push(h('div', { class: 'line' }, h('label', { for: nameId, class: 'sr' }, 'Name'), h('span', { 'aria-hidden': 'true' }, 'name'),
    h('input', { id: nameId, type: 'text', value: d.name, maxlength: '40', 'data-k': nameId }),
    btn('Rename', (e) => deviceAction(d, 'rename', { name: $(`#${CSS.escape(nameId)}`).value }, e.currentTarget, 'Renamed'))));
  if (d.state !== 'revoked') {
    lines.push(h('div', { class: 'line' }, h('span', {}, 'it may'),
      seg(`What ${d.name} may do`, SCOPES, d.scope, (v) => deviceAction(d, 'scope', { scope: v }, null, `${d.name} may now: ${scopeWords(v)}`), { key: `scope-${d.id}` })));
  }
  const fw = [h('span', {}, 'firmware'), d.firmware || 'unknown'];
  if (d.connection === 'usb' && d.port) {
    fw.push(btn('Install firmware again', () => { location.hash = `#new-board/${encodeURIComponent(d.port)}`; }));
  }
  if (d.kind === 'serial') lines.push(h('div', { class: 'line' }, ...fw));
  if (d.state === 'active') lines.push(...magicLines(d));
  const acts = [];
  if (d.state === 'waiting') acts.push(btn('Approve', (e) => deviceAction(d, 'approve', {}, e.currentTarget, `${d.name} approved`), 'p'));
  if (d.state !== 'revoked') acts.push(btn('Cut it off', (e) => deviceAction(d, 'revoke', {}, e.currentTarget, `${d.name} can't connect any more`)));
  acts.push(btn('Remove', (e) => deviceAction(d, 'remove', {}, e.currentTarget, `${d.name} removed`)));
  lines.push(h('div', { class: 'line' }, h('span', {}, ''), ...acts));
  return h('div', { class: 'more-panel', id: `more-${d.id}` }, lines);
}

function moreButton(d) {
  const open = S.open.more === d.id;
  return btn('···', () => { S.open.more = open ? '' : d.id; draw(); }, 'more', {
    'aria-label': `More for ${d.name}`, 'aria-expanded': open ? 'true' : 'false', 'aria-controls': `more-${d.id}`, 'data-k': `more-${d.id}`,
  });
}

// The uplink preference: what the board uses when it isn't on USB.
const UPLINKS = [{ value: 'auto', text: 'Auto' }, { value: 'wifi', text: 'Wi-Fi' }, { value: 'ble', text: 'Bluetooth' }];

// uplinkLine: under a board, every way it reaches this computer (a chip
// each, with its state) and, for a board with Bluetooth, the preference.
function uplinkLine(d) {
  if (d.kind !== 'serial' || d.state === 'revoked') return null;
  const ups = d.uplinks || [];
  if (!d.hasBluetooth && ups.length < 2) return null; // one way in, no choice: the row says it
  const line = h('div', { class: 'uplinks' });
  const chips = h('div', { class: 'chips', role: 'list', 'aria-label': `How ${d.name} reaches this computer` });
  for (const u of ups) {
    chips.append(h('span', { class: `up up-${u.state.replace(/\s+/g, '-')}`, role: 'listitem', title: u.detail || null },
      `${VIA[u.via] || u.via} · ${u.state}`));
  }
  line.append(chips);
  if (d.hasBluetooth) {
    const opts = UPLINKS.map((o) => (o.value === 'ble' && !d.bluetooth
      ? { ...o, disabled: true, title: 'Add the board over Bluetooth on this computer first' } : o));
    line.append(h('div', { class: 'pref' }, h('span', { class: 'note', 'aria-hidden': 'true' }, 'uplink'),
      seg(`Uplink for ${d.name}`, opts, d.uplinkPref || 'auto',
        (v) => deviceAction(d, 'uplink', { uplink: v }, null, `${d.name} uses ${UPLINKS.find((o) => o.value === v).text.toLowerCase()} off USB`),
        { key: `uplink-${d.id}` })));
    line.append(h('p', { class: 'note' }, 'Auto: USB, else Wi-Fi, else Bluetooth.'));
    // Not added over Bluetooth on this computer yet: add it from here, in one go.
    const nb = (S.btBoards?.boards || []).find((b) => b.device === d.id);
    const job = nb && S.jobs[S.btAdd[nb.id]];
    if (job) line.append(addFlow(nb, job));
    else if (!d.bluetooth) {
      line.append(h('div', { class: 'actions' }, h('div', { class: 'r' },
        h('span', { class: 'note' }, S.btFind[d.id] ? 'Looking for it over Bluetooth…' : 'Not linked over Bluetooth yet.'),
        btn('Add over Bluetooth', (e) => btAddFor(d, e.currentTarget), 'p', { 'data-k': `btfor-${d.id}`, disabled: !!S.btFind[d.id] }))));
    }
  }
  // A Bluetooth link that keeps failing says why (the board forgot this
  // computer, and so on): the person has something to do.
  for (const u of ups) {
    if (u.via === 'bluetooth' && u.state === 'connecting' && u.detail && u.detail !== 'waiting for it to come in range') {
      line.append(h('p', { class: 'note err' }, `Bluetooth: ${u.detail}`));
    }
  }
  if (d.uplinkNote) line.append(h('p', { class: 'note err' }, d.uplinkNote));
  return line;
}

function deviceRow(d) {
  const shape = d.kind === 'network' ? 'phone' : shapeOf(d.board || d.type || d.model);
  const right = [];
  if (d.online) {
    if ((d.connection === 'usb' || d.connection === 'bluetooth') && d.canWifi && !(d.wifi && d.wifi.ssid) && S.open.wifi !== d.id) {
      right.push(btn('Give it Wi-Fi', () => { S.open.wifi = d.id; draw(); $(`#ssid-${CSS.escape(d.id)}`)?.focus(); }));
    }
  } else {
    right.push(btn('Remove', (e) => deviceAction(d, 'remove', {}, e.currentTarget, `${d.name} removed`)));
  }
  right.push(moreButton(d));
  const extra = [fwLine(d), uplinkLine(d), magicRowLine(d)];
  if (S.open.wifi === d.id) extra.push(wifiForm(d, () => load('devices')));
  if (S.open.more === d.id) extra.push(morePanel(d));
  return row(d.name, deviceMeta(d), right, { pic: pic(shape, !d.online, d.kind === 'network' ? '' : boardOf(`${d.board} ${d.type} ${d.model} ${d.detail || ''}`)), off: !d.online, id: d.id, extra, name: true });
}

// ---- a board's firmware: one line about updates, Update, and a file of your own -------------

// fwText: the status line in the page's words. The reason the release list
// couldn't be read, and that this build has no release key, are said once
// above the list.
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
const FW_MARK = { update: 'go', local: 'go', newer: 'ok', current: 'ok', own: 'ok', error: 'bad', nokey: 'off' };

// The stages an update over Wi-Fi reports, in words.
const OTA_WORDS = { offered: 'offered, waiting for the board', downloading: 'downloading', verifying: 'checking the firmware', restarting: 'restarting into it' };

function jobLine(d, j) {
  if (!j) return [h('span', { class: 'fw-t', role: 'status' }, 'An update is running'), h('a', { class: 'b', href: `#flash/${d.job}` }, 'Show progress')];
  const what = j.port ? ({ download: 'downloading', connect: 'waiting for the board', erase: 'erasing', write: 'writing', verify: 'checking', reset: 'restarting' }[j.stage] || 'installing')
    : (OTA_WORDS[j.stage] || 'updating');
  const bar = h('div', {});
  bar.style.width = `${j.percent}%`;
  return [
    h('span', { class: 'fw-t', role: 'status' }, `${j.file ? `Installing ${j.file}` : `Updating to ${j.version}`} · ${what} · ${j.percent}%`),
    h('div', { class: 'bar mini', role: 'progressbar', 'aria-label': `${d.name}: ${what}`, 'aria-valuenow': j.percent, 'aria-valuemin': '0', 'aria-valuemax': '100' }, bar),
    h('a', { class: 'b', href: `#flash/${j.id}` }, 'Show progress'),
  ];
}

// fwLine: under a board, what its firmware is against the latest release
// and what you can do about it; a running update shows its progress.
function fwLine(d) {
  if (d.kind !== 'serial' || d.state !== 'active') return null;
  const line = h('div', { class: 'fw', id: `fw-${d.id}` });
  if (d.job) {
    line.append(...jobLine(d, S.jobs[d.job]));
    return line;
  }
  const st = d.status;
  if (st) line.append(h('span', { class: `fw-t fw-${FW_MARK[st.state] || 'off'}` }, h('i', { 'aria-hidden': 'true' }), fwText(st)));
  const acts = h('div', { class: 'r' });
  if (st && st.state === 'update' && st.ready) {
    acts.append(btn(`Update to ${st.latest}`, (e) => updateDevice(d, e.currentTarget), 'a', { 'data-k': `upd-${d.id}` }));
  }
  if (st && st.state === 'local' && st.ready) {
    acts.append(btn('Install', (e) => updateDevice(d, e.currentTarget), 'a', {
      'data-k': `upd-${d.id}`, 'aria-label': `Install ${st.latest}, built on this computer, on ${d.name} ${st.note}`,
    }));
  }
  const t = deviceFileTarget(d);
  const canFile = (d.fileVia || []).length > 0;
  acts.append(...fileButton(t, canFile, d.fileNote));
  line.append(acts);
  const upNote = st && (st.state === 'update' || st.state === 'local') && !st.ready ? st.note : '';
  const upWord = st && st.state === 'local' ? 'To install it' : 'To update';
  if (upNote && upNote === d.fileNote) { // one reason for both (only on Bluetooth)
    line.append(h('p', { class: 'note', id: `fwn-${t.key}` }, `To update or install a file: ${upNote}.`));
    if (S.file[t.key]) line.append(filePanel(t, S.file[t.key]));
    return line;
  }
  if (upNote) line.append(h('p', { class: 'note' }, `${upWord}: ${upNote}.`));
  if (!canFile && d.fileNote && d.online) line.append(h('p', { class: 'note', id: `fwn-${t.key}` }, `A firmware file: ${d.fileNote}.`));
  else if (!canFile && d.fileNote) line.append(h('span', { class: 'sr', id: `fwn-${t.key}` }, d.fileNote));
  if (S.file[t.key]) line.append(filePanel(t, S.file[t.key]));
  return line;
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
function fileButton(t, canFile, note, kind = '') {
  const input = h('input', {
    type: 'file', accept: '.bin,application/octet-stream', class: 'sr', id: `file-${t.key}`, tabindex: '-1', 'aria-hidden': 'true',
    onchange: (e) => { const f = e.currentTarget.files[0]; if (f) uploadFile(t, f); },
  });
  return [input, btn('Install a firmware file…', () => input.click(), kind, {
    disabled: !canFile || !!S.file[t.key], 'data-k': `pick-${t.key}`, title: canFile ? 'Install a .bin you built or downloaded' : note,
    'aria-describedby': canFile ? null : `fwn-${t.key}`,
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
  const box = h('div', { class: `box filebox${st.stage === 'error' ? ' err' : ''}`, 'data-row': `file-${t.key}` });
  if (st.stage === 'reading') {
    box.append(h('p', { role: 'status' }, `Reading ${st.name} (${mb(st.size)} MB)…`), h('p', { class: 'note' }, 'Nothing is sent to the board until you confirm.'));
    return box;
  }
  if (st.stage === 'error') {
    box.append(h('div', { class: 't' }, `${st.name} can't go on ${t.name}`), h('p', { role: 'alert' }, st.error),
      h('p', { class: 'note' }, 'Nothing was sent to the board.'),
      h('div', { class: 'actions' }, h('div', { class: 'r' },
        btn('Close', close, '', { 'data-k': `fx-${t.key}` }),
        btn('Pick another file', () => { delete S.file[t.key]; draw(true); $(`#file-${CSS.escape(t.key)}`)?.click(); }, 'p', { 'data-k': `fa-${t.key}` }))));
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
      : wifi ? h('span', { class: 'vin' }, h('label', { for: vId, class: 'sr' }, 'Version the firmware reports'),
        h('input', { id: vId, type: 'text', value: st.version, placeholder: 'like 0.2.0-dev.5', maxlength: '23', autocomplete: 'off', spellcheck: 'false', 'data-k': vId,
          oninput: (e) => { st.version = e.currentTarget.value; } }))
        : 'not in the file'),
    h('dt', {}, 'how'), h('dd', {}, wifi
      ? 'Over Wi-Fi. The board downloads it over its encrypted connection, checks it and restarts into it. If it can\'t reach the deck afterwards, it goes back to the firmware it runs now.'
      : t.port ? 'Over USB, written at 0x0 through the cable: bootloader, partition table and app. Keep it plugged in; the screen stays dark until it restarts, then it pairs.'
        : 'Over USB, written at 0x0 through the cable. Keep it plugged in; the screen stays dark until it restarts.'));
  box.append(h('div', { class: 't' }, `Install ${up.name} on ${t.name}?`), facts);
  if (wifi && !up.version) {
    box.append(h('p', { class: 'note' }, 'The file doesn\'t say its version. Type the one the firmware reports (AGENTS_DECK_VERSION when it was built), so the deck can tell when the board is back on it.'));
  }
  box.append(h('p', { class: 'note warn' }, 'This file isn\'t checked against a release. Install it only if you built it or trust where it came from. The board still checks that the firmware is for it and arrived whole.'));
  if (st.error) box.append(h('p', { class: 'note err', role: 'alert' }, st.error));
  box.append(h('div', { class: 'actions' }, h('div', { class: 'r' },
    btn('Cancel', close, '', { 'data-k': `fc-${t.key}` }),
    btn(wifi ? 'Install over Wi-Fi' : 'Install over USB', (e) => installFile(t, st, e.currentTarget), 'p', { 'data-k': `fi-${t.key}` }))));
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

function drawBoards() {
  const out = [];
  const ports = S.ports.ports || [];
  const devs = S.devices.devices || [];
  for (const p of ports.filter((x) => x.state === 'new')) {
    out.push(row('A new board was plugged in', `USB port · ${portWords(p)} · not opened yet`, [
      btn('Not mine', (e) => portAction(p, 'forget', {}, e.currentTarget, 'It won\'t be asked about again')),
      btn('Set it up', () => { location.hash = `#new-board/${encodeURIComponent(p.id)}`; }, 'k'),
    ], { hot: true, pic: pic('new') }));
  }
  for (const d of devs.filter((x) => x.state === 'waiting')) {
    out.push(row(`${d.name} is waiting for you`, `${d.model || d.type || 'board'} · ${d.connection === 'bluetooth' ? 'over Bluetooth' : 'plugged in over USB'} · approve it to show your deck`, [
      btn('Remove', (e) => deviceAction(d, 'remove', {}, e.currentTarget, `${d.name} removed`)),
      btn('Approve', (e) => deviceAction(d, 'approve', {}, e.currentTarget, `${d.name} approved`), 'k'),
    ], { hot: true, pic: pic(shapeOf(d.board || d.type), false, boardOf(`${d.board} ${d.type}`)) }));
  }
  for (const j of S.jobList?.jobs || []) putJob(j);
  const boards = devs.some((d) => d.kind === 'serial' && d.state === 'active');
  if (boards && S.devices.releaseKey === false && !devs.some((d) => d.status?.state === 'local')) {
    out.push(h('div', { class: 'box notice', 'data-k': 'nokey' },
      h('p', {}, 'This copy installs firmware you build on this computer, not releases.'),
      h('p', { class: 'note' }, 'It was built from source, so it can\'t check that firmware from a release is genuine and never installs one. ' +
        'Build the firmware, then run agentctl firmware add <folder> with the folder the release script wrote: a board that runs an older version gets an Install button here. ' +
        'Install a firmware file… on a board takes a single .bin.')));
  }
  if (boards && S.devices.catalogError && !S.devices.latest) {
    out.push(h('p', { class: 'note err' }, `Couldn't check for updates: ${S.devices.catalogError}`));
  }
  const on = devs.filter((d) => d.online && d.state !== 'waiting');
  const off = devs.filter((d) => !d.online && d.state !== 'waiting');
  if (on.length) { out.push(lab('connected')); on.forEach((d) => out.push(deviceRow(d))); }
  if (off.length) { out.push(lab('not connected')); off.forEach((d) => out.push(deviceRow(d))); }
  out.push(...bluetoothPanel());

  const reg = ports.filter((p) => p.registered && !p.device);
  if (reg.length) {
    out.push(lab('usb ports you registered'));
    for (const p of reg) {
      const meta = {
        registered: 'registered · waiting for the board to say hello',
        busy: `registered · ${p.detail || 'in use by another program'}`,
        flashing: 'the service is writing to the board',
        unplugged: 'registered · not plugged in',
      }[p.state] || p.detail || p.state;
      const right = [];
      if (p.state === 'flashing' && p.job) right.push(h('a', { class: 'b', href: `#flash/${p.job}` }, 'Show progress'));
      if (p.state !== 'flashing' && p.state !== 'unplugged') right.push(btn('Set it up', () => { location.hash = `#new-board/${encodeURIComponent(p.id)}`; }));
      if (p.state !== 'flashing') right.push(btn('Forget', (e) => portAction(p, 'forget', {}, e.currentTarget, 'The deck no longer opens it')));
      out.push(row(p.id, meta, right, { pic: pic('new', p.state === 'unplugged'), name: true }));
    }
  }
  if (!devs.length && !ports.some((p) => p.state === 'new' || p.registered)) {
    out.unshift(h('div', { class: 'box' },
      h('p', {}, 'No boards yet.'),
      h('p', { class: 'note' }, 'Plug one in with a USB cable that carries data (many charging cables don\'t). It shows up here within a few seconds; nothing opens it until you say it\'s yours.'
        + (S.bt?.available ? ' A board with Bluetooth can be added without a cable: Look for boards, below.' : ''))));
  }
  const others = ports.filter((p) => p.state === 'ignored' || p.state === 'other');
  if (others.length) {
    const det = h('details', { class: 'quiet' }, h('summary', {}, `Other USB serial ports (${others.length})`));
    for (const p of others) {
      det.append(row(p.id, p.state === 'ignored' ? `you said it isn't a board · ${portWords(p)}` : portWords(p), [
        btn('It\'s a board', () => { location.hash = `#new-board/${encodeURIComponent(p.id)}`; }),
      ]));
    }
    out.push(det);
  }
  out.push(...drawMagic());
  return out;
}

// ---- Magic trick ---------------------------------------------------------------------------

const CORNERS = [
  { value: 'top-left', text: 'top left' }, { value: 'top-right', text: 'top right' },
  { value: 'bottom-left', text: 'bottom left' }, { value: 'bottom-right', text: 'bottom right' },
];
const cornerWords = (c) => (CORNERS.find((x) => x.value === c) || {}).text || c;

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
  if (!next.giver || !next.receiver) next.on = false;   // it needs both
  patchSettings({ magic: next });
}

// magicLines: the part and its corners, on the device's own panel.
function magicLines(d) {
  const m = S.settings?.magic;
  if (!m) return [];
  const role = roleOf(d);
  const save = (patch) => patchSettings({ magic: { ...m, ...patch } });
  const out = [h('div', { class: 'line' }, h('span', {}, 'magic trick'),
    seg(`${d.name}'s part in the magic trick`, [
      { value: '', text: 'not in it' },
      { value: 'giver', text: 'giver' },
      { value: 'receiver', text: 'receiver', disabled: !d.motion, title: d.motion ? null : 'Needs a motion sensor (the 1.8 inch AMOLED)' },
    ], role, (v) => setRole(d, v), { key: `role-${d.id}` }))];
  const corner = (label, key) => h('div', { class: 'line' }, h('span', {}, label),
    seg(`${d.name}: ${label}`, CORNERS, m[key], (v) => save({ [key]: v }), { key: `${key}-${d.id}` }));
  if (role === 'giver') out.push(corner('they leave to', 'giverLeave'), corner('come back from', 'giverArrive'));
  if (role === 'receiver') out.push(corner('they arrive from', 'receiverArrive'), corner('leave to', 'receiverLeave'));
  return out;
}

// magicRowLine: one line under the device's row when it has a part.
function magicRowLine(d) {
  const m = S.settings?.magic;
  const role = roleOf(d);
  if (!role) return null;
  const how = role === 'giver'
    ? `leaves to ${cornerWords(m.giverLeave)}, back from ${cornerWords(m.giverArrive)}`
    : `arrives from ${cornerWords(m.receiverArrive)}, leaves to ${cornerWords(m.receiverLeave)}`;
  return h('div', { class: 'm' }, `Magic trick: ${role} · ${how}${m.on ? '' : ' · the trick is off'}`);
}

// The magic trick: a knock on the receiver (a board with a motion sensor)
// takes the agents the giver shows, the next knock gives them back. Off by
// default; while off, nothing happens.
function drawMagic() {
  const m = S.settings?.magic;
  if (!m) return [];
  const devs = (S.devices?.devices || []).filter((d) => d.state === 'active');
  const receivers = devs.filter((d) => d.motion);
  const save = (patch, el) => patchSettings({ magic: { ...m, ...patch } }, el);
  const pick = (label, list, current, key) => h('select', {
    class: 'pick', 'aria-label': label, 'data-k': `magic-${key}`,
    onchange: (e) => save({ [key]: e.target.value }),
  },
  h('option', { value: '', selected: !current }, 'choose a device'),
  ...list.map((d) => h('option', { value: d.id, selected: d.id === current }, d.name)));
  const corners = (label, current, key) => seg(label, [
    { value: 'top-left', text: 'top left' }, { value: 'top-right', text: 'top right' },
    { value: 'bottom-left', text: 'bottom left' }, { value: 'bottom-right', text: 'bottom right' },
  ], current, (v) => save({ [key]: v }), { key: `magic-${key}` });
  const out = [lab('magic trick')];
  const ready = m.giver && m.receiver;
  out.push(row('Magic trick',
    'Knock on the receiver to take the agents the giver shows; knock again to give them back. Off: nothing happens.',
    [toggle('Magic trick', m.on, (on, el) => save({ on }, el), { disabled: !ready && !m.on })]));
  out.push(row('Giver', 'Any screen: the agents it shows fly off it, and their places stay empty until they come back.',
    [pick('Giver', devs.filter((d) => d.id !== m.receiver), m.giver, 'giver')]));
  out.push(row('Receiver', receivers.length
    ? 'A board with a motion sensor: you knock on it, the agents fly onto it.'
    : 'Needs a board with a motion sensor (the 1.8 inch AMOLED). Plug one in and it shows up here.',
  [pick('Receiver', receivers.filter((d) => d.id !== m.giver), m.receiver, 'receiver')]));
  out.push(row('On the giver, they leave to', null, [corners('On the giver, they leave to', m.giverLeave, 'giverLeave')]));
  out.push(row('On the giver, they come back from', null, [corners('On the giver, they come back from', m.giverArrive, 'giverArrive')]));
  out.push(row('On the receiver, they arrive from', null, [corners('On the receiver, they arrive from', m.receiverArrive, 'receiverArrive')]));
  out.push(row('On the receiver, they leave to', null, [corners('On the receiver, they leave to', m.receiverLeave, 'receiverLeave')]));
  if (m.on) out.push(h('p', { class: 'note' }, 'To start: pull the receiver\'s menu down from the top edge and switch Magic trick on. Knock once to take the agents, knock again on its grid to give them back. Then switch it off in the same menu.'));
  return out;
}

// ---- Bluetooth: boards nearby, and adding one -------------------------------------------------

// signal: how well the computer hears a board, in words; the number in the tooltip.
function signal(rssi) {
  if (!rssi) return null;
  const w = rssi >= -60 ? 'strong' : rssi >= -75 ? 'ok' : 'weak';
  return h('span', { class: `sig sig-${w}`, title: `${rssi} dBm` }, `signal ${w}`);
}

const labelOf = (id) => (S.devices?.devices || []).find((d) => d.id === id)?.name || id;

async function btScan(on, el) {
  await busy(el, () => api('POST', '/admin/v1/bluetooth/scan', { on })); // a refusal says why in a toast
  await loadAll('bt', 'btBoards');
  draw(true);
}

// btAddFor: add a board you already have over Bluetooth, from its own row:
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
  const box = h('div', { class: `box add${j.state === 'failed' ? ' err' : ''}`, 'data-row': `bt-${b.id}` },
    h('div', { class: 't' }, title));
  if (j.state === 'running') {
    if (j.stage === 'passkey') {
      box.append(h('p', { class: 'passkey', role: 'status' }, 'Type the code shown on the board into the window this computer opens.'),
        h('p', { class: 'note' }, 'The board shows a 6-digit code and its name. The window is the computer\'s own: this page never sees the code.'));
    } else {
      box.append(h('p', { role: 'status' }, j.message || 'Connecting…'));
    }
    const at = ADD_STEPS.findIndex(([k]) => k === j.stage);
    const list = h('ol', { class: 'list', 'aria-label': 'Steps' });
    ADD_STEPS.forEach(([, text], i) => {
      const st = i < at ? '' : i === at ? 'cur' : 'todo';
      list.append(h('li', { class: st }, h('i', { 'aria-hidden': 'true' }), text, st === 'cur' ? h('span', { class: 'sr' }, '(now)') : null));
    });
    box.append(list, h('div', { class: 'actions' }, h('div', { class: 'r' },
      btn('Cancel', (e) => busy(e.currentTarget, () => api('POST', `/admin/v1/jobs/${encodeURIComponent(j.id)}/cancel`, {})), '', { 'data-k': `btc-${b.id}` }))));
  } else if (j.state === 'done') {
    box.append(h('p', { role: 'status' }, j.message || `${b.name} is one of your boards now.`),
      h('div', { class: 'actions' }, h('div', { class: 'r' }, btn('Done', close, 'p', { 'data-k': `btd-${b.id}` }))));
  } else {
    box.append(h('p', { role: 'alert' }, j.state === 'cancelled' ? 'Stopped. Nothing was added.' : (j.error || j.message || 'It didn\'t work.')),
      h('p', { class: 'note' }, 'Bring the board close, check that it still shows its name in setup, and try again. If it was paired with this computer before, remove it in System Settings, Bluetooth first.'),
      h('div', { class: 'actions' }, h('div', { class: 'r' },
        btn('Close', close, '', { 'data-k': `btx-${b.id}` }),
        btn('Try again', (e) => btAddBoard(b, e.currentTarget), 'p', { 'data-k': `btr-${b.id}` }))));
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
    right.push(h('span', { class: 'note', 'aria-hidden': 'true' }, 'it may'),
      seg(`What ${b.name} may do`, SCOPES.slice(0, 2), S.btScope[b.id] || 'act', (v) => { S.btScope[b.id] = v; draw(); }, { key: `bts-${b.id}` }),
      btn('Add', (e) => btAddBoard(b, e.currentTarget), 'p', { 'data-k': `bta-${b.id}` }));
  } else state = 'Couldn\'t read it: bring it closer';
  const what = [b.model || b.board || 'a board', b.firmware ? `firmware ${b.firmware}` : ''].filter(Boolean).join(' · ');
  const sig = signal(b.rssi);
  const meta = h('span', {}, what, sig ? ' · ' : '', sig, ` · ${state}`);
  return row(b.name || b.id, meta, right, { pic: pic(shapeOf(b.board), !b.setup, boardOf(b.board)), off: !b.setup && !b.added, id: `bt-${b.id}`, name: true });
}

// bluetoothPanel: look for boards nearby and add one. When this computer
// can't use Bluetooth, one line says why.
function bluetoothPanel() {
  const bt = S.bt;
  if (!bt) return [];
  const out = [lab('bluetooth')];
  if (!bt.available) {
    out.push(h('p', { class: 'note' }, bt.why || 'Bluetooth isn\'t available.'));
    return out;
  }
  out.push(row('Look for boards', bt.scanning ? 'Looking…' : 'Add a board without a cable: one in setup shows its name, deck- and six letters or digits, on its screen.', [
    toggle('Look for boards over Bluetooth', bt.scanning, (on, el) => btScan(on, el), { key: 'bt-scan' }),
  ]));
  const list = (S.btBoards?.boards || []);
  const adding = Object.keys(S.btAdd).filter((id) => !list.some((b) => b.id === id));
  for (const b of list) {
    if (b.device && S.jobs[S.btAdd[b.id]] && (S.devices?.devices || []).some((d) => d.id === b.device)) continue;   // its row shows the add
    out.push(nearbyRow(b));
  }
  for (const id of adding) { // a board being added that the list no longer shows
    const j = S.jobs[S.btAdd[id]];
    if (j) out.push(addFlow({ id, name: 'the board' }, j));
  }
  if (bt.scanning && !list.length) out.push(h('p', { class: 'note', role: 'status' }, 'Nothing yet. Put the board in setup from its screen and bring it closer.'));
  return out;
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
  return row('Board not answering?', 'Hold BOOT, press RST, let go of BOOT, then try again. On boards without buttons, unplug it, hold BOOT while plugging it back in.', extra || []);
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
    }, h('span', { class: 'pic', 'aria-hidden': 'true' }, frame(shapeOf(b.board + ' ' + b.name), boardOf(b.board + ' ' + b.name))),
    h('span', { class: 'nm' }, b.name), h('span', { class: 'sp' }, spec)));
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
    return [errorBox('This port isn\'t there any more. Is the board still plugged in?', () => load('ports').then(draw)), h('a', { class: 'b', href: '#boards' }, 'Back to Boards')];
  }
  if (!p.registered) {
    stepsAt(1, true);
    return [
      row(`USB port ${p.id}`, `${portWords(p)}${p.vid ? ` · ${p.vid}:${p.pid}` : ''} · not opened yet`, []),
      h('p', {}, 'Is this one of your boards?'),
      h('p', { class: 'note' }, 'Yes lets agents deck open this port and look at the board. It never opens a port you haven\'t said yes to.'),
      h('div', { class: 'actions' }, h('div', { class: 'r' },
        btn('Not mine', async (e) => { await portAction(p, 'forget', {}, e.currentTarget, 'It won\'t be asked about again'); location.hash = '#boards'; }),
        btn('Yes, it\'s mine', async (e) => {
          const r = await busy(e.currentTarget, () => api('POST', `/admin/v1/ports/${encodeURIComponent(p.id)}/register`, {}));
          if (!r) return;
          await load('ports');
          nbIdentify();
        }, 'p', { 'data-k': 'nb-yes' }))),
    ];
  }
  if (!nb.identity && !nb.error && !nb.looking) { nbIdentify(); }
  stepsAt(2, true);
  if (nb.looking) {
    return [row(`USB port ${p.id}`, 'Looking at the board. This takes a few seconds; its screen may flicker.', []), h('p', { class: 'note', role: 'status' }, 'Asking the board…')];
  }
  if (nb.error) {
    const out = [errorBox(nb.error.message)];
    if (nb.error.data?.bootHelp) out.push(bootHelp([btn('Try again', () => nbIdentify(), 'p')]));
    else out.push(h('div', { class: 'actions' }, h('div', { class: 'r' }, h('a', { class: 'b', href: '#boards' }, 'Cancel'), btn('Try again', () => nbIdentify(), 'p'))));
    return out;
  }
  const id = nb.identity;
  const out = [];
  if (id.source === 'session') {
    const d = id.device || {};
    out.push(row(`${d.model || d.type || 'A board'} · firmware ${d.firmware || 'unknown'}`, `It already runs agents deck, as ${d.name || 'a board'}.${id.latest ? ` The latest firmware is ${id.latest}.` : ''}`, []));
  } else {
    const fs = id.flashSize ? ` · ${Math.round(id.flashSize / 1048576)} MB flash` : '';
    const n = nbOffers(id).length;
    const none = id.releaseKey ? 'No board in the latest release or built on this computer uses this chip.' : 'No firmware built on this computer is for this chip.';
    out.push(row(`${id.chip}${fs}`, `No agents deck firmware on it yet. ${n === 0 ? none : n === 1 ? 'One board matches this chip.' : `${n} boards match this chip.`}`, []));
  }
  if (id.note) out.push(h('p', { class: 'note' }, id.note));
  // A file from this computer: through the board's own calls when it runs
  // agents deck, else through the port's (a full image for a blank chip).
  const dev = id.source === 'session' ? id.device : null;
  const ft = dev ? { ...deviceFileTarget(dev), onJob: (job) => { if (S.nb === nb) nb.job = job.id; } }
    : id.source === 'probe' ? portFileTarget(p, id) : null;
  const canFile = dev ? (dev.fileVia || []).length > 0 : !!ft;
  const fileNote = dev ? (dev.fileNote || 'approve the board first') : '';
  const offers = nbOffers(id);
  if (offers.length) out.push(boardCards(offers, nb.choice, (v) => { nb.choice = v; draw(); }));
  if (!id.releaseKey && !offers.length) {
    out.push(h('p', { class: 'note' }, 'This copy of agents deck was built from source, so it can\'t check firmware downloaded from a release and won\'t install it. ' +
      'Firmware you build on this computer installs in one click: run agentctl firmware add <folder> with the folder the release script wrote, then look at the board again. ' +
      'Or install a single .bin you built or downloaded with the Install a firmware file button below.'));
    if (!dev && id.candidates.length) {
      out.push(h('p', { class: 'note' }, `Pick the full image, the one written at 0x0: from a release, ${id.candidates.map((b) => `agents-deck-${b.board}-<version>.bin`).join(' or ')}.`));
    }
    out.push(h('p', { class: 'note' }, 'By hand in a terminal, agentctl flash <port> <image.bin> does the same.'));
  }
  if (ft && !canFile && fileNote) out.push(h('p', { class: 'note', id: `fwn-${ft.key}` }, `To install a firmware file: ${fileNote}.`));
  const chosen = offers.find((b) => b.board === nb.choice);
  const label = !chosen ? 'Pick the board first'
    : chosen.source === 'local' ? `Install ${chosen.version}, built on this computer` : `Install firmware ${chosen.version || id.latest}`;
  if (chosen && chosen.source === 'local') {
    out.push(h('p', { class: 'note' }, `${chosen.name} ${chosen.version} was built on this computer (agentctl firmware add). ` +
      'It isn\'t a release, so no signature is checked; its files are checked against the manifest that came with them. The full image is written at 0x0 over the cable.'));
  }
  const right = [h('a', { class: 'b', href: '#boards' }, 'Cancel')];
  if (ft) right.push(...fileButton(ft, canFile, fileNote, offers.length ? '' : 'p'));
  if (offers.length) right.push(btn(label, (e) => nbInstall(e.currentTarget, chosen), 'p', { disabled: !chosen, 'data-k': 'nb-install' }));
  out.push(h('div', { class: 'actions' },
    offers.length ? h('p', { class: 'note' }, 'Picked the wrong one? Nothing breaks: install again with the right board.') : null,
    h('div', { class: 'r' }, ...right)));
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

function drawFlash() {
  const j = S.jobs[S.arg];
  if (!j) {
    stepsAt(0, false);
    return [h('p', { class: 'loading' }, 'Loading…')];
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
  if (!j.port && j.kind === 'board-update') { // over the air: the board downloads it itself
    if (!ended) {
      const bar = h('div', {});
      bar.style.width = `${j.percent}%`;
      out.push(h('div', { class: 'progress' },
        h('div', { class: 'big' }, h('div', { class: 'pct' }, `${j.percent}%`), h('div', { class: 'what' }, `over Wi-Fi · ${j.file ? `${j.file} as ` : ''}${j.version}`)),
        h('div', { class: 'bar', role: 'progressbar', 'aria-label': j.message || 'Updating over Wi-Fi', 'aria-valuenow': j.percent, 'aria-valuemin': '0', 'aria-valuemax': '100' }, bar),
        h('div', { role: 'status' }, j.message),
        h('div', { class: 'note' }, 'The board keeps showing your agents while it downloads. If the new firmware doesn\'t reach the deck, it goes back to the old one by itself.')));
      out.push(otaSteps(j));
      out.push(h('div', { class: 'actions' }, h('div', { class: 'r' },
        btn('Stop following', (e) => busy(e.currentTarget, () => api('POST', `/admin/v1/jobs/${encodeURIComponent(j.id)}/cancel`, {}))))));
    } else if (j.state === 'done') {
      out.push(otaSteps(j));
      out.push(h('div', { class: 'box', role: 'status' }, h('h2', {}, 'Done'), h('p', {}, j.message),
        h('div', { class: 'actions' }, h('div', { class: 'r' }, h('a', { class: 'b p', href: '#boards' }, 'Back to Boards')))));
    } else {
      out.push(otaSteps(j));
      out.push(errorBox(j.state === 'cancelled' ? 'Stopped following the board. It may still finish the update by itself.' : j.error));
      out.push(h('a', { class: 'b', href: '#boards' }, 'Back to Boards'));
    }
    return out;
  }
  const words = { download: 'downloading', connect: 'waiting for the board', erase: 'erasing', write: 'writing', verify: 'checking', reset: 'restarting' }[j.stage] || j.stage || '';
  // "writing 0.4 of 1.8 MB": the stage, then how far into it (the big
  // number is the whole job, download and checks included).
  const size = j.total && j.done && (j.stage === 'write' || j.stage === 'verify') ? ` ${mb(j.done)} of ${mb(j.total)} MB` : '';
  const pct = h('div', { class: 'pct' }, `${j.percent}%`);
  const bar = h('div', {});
  bar.style.width = `${j.percent}%`;
  if (!ended) {
    out.push(h('div', { class: 'progress' },
      h('div', { class: 'big' }, pct, h('div', { class: 'what' }, `${words}${size}${timeLeft(j)}`)),
      h('div', { class: 'bar', role: 'progressbar', 'aria-label': j.message || 'Installing firmware', 'aria-valuenow': j.percent, 'aria-valuemin': '0', 'aria-valuemax': '100' }, bar),
      h('div', {}, 'Keep the cable plugged in. The board\'s screen stays dark until it restarts.')));
  }
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
    out.push(h('div', { class: 'box', role: 'status' },
      h('h2', {}, 'Done'),
      h('p', {}, j.file ? `${dev?.name || j.device || (j.board ? boardName : 'The board')} restarts into ${j.file}, pairs and shows your agents in a few seconds.` : `${boardName} runs agents deck ${j.version || ''}. It restarts, pairs and shows your agents in a few seconds.`),
      h('div', { class: 'actions' }, h('div', { class: 'r' },
        b && b.wifi ? h('a', { class: 'b', href: '#network' }, 'Give it Wi-Fi') : null,
        h('a', { class: 'b p', href: '#boards' }, 'Back to Boards')))));
  } else if (ended) {
    out.push(h('div', { class: 'box err', role: 'alert' }, h('p', {}, j.state === 'cancelled' ? j.message : `It didn't work: ${j.error}`)));
    out.push(bootHelp([
      h('a', { class: 'b', href: '#boards' }, 'Back to Boards'),
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
  const out = [];
  const pending = s.network !== s.networkRunning;
  out.push(row('Let paired devices connect over the network',
    pending ? (s.network ? 'On after the service restarts.' : 'Off after the service restarts.') + ' Encrypted, and only devices you paired.' : 'Encrypted, and only devices you paired. Off means USB only.',
    [pending ? btn('Restart now', (e) => restartService(e.currentTarget)) : null,
      toggle('Let paired devices connect over the network', s.network, (on, el) => patchSettings({ network: on }, el))]));

  out.push(lab('wi-fi for boards'));
  const err = h('p', { class: 'm err', role: 'alert' });
  const box = h('div', { class: 'box' },
    h('div', { class: 'grid2' },
      h('div', {}, h('label', { for: 'ssid' }, 'Network name'), h('input', { id: 'ssid', type: 'text', autocomplete: 'off', value: s.wifi?.ssid || '', 'data-k': 'ssid' })),
      h('div', {}, h('label', { for: 'pw' }, 'Password'), h('input', { id: 'pw', type: 'password', autocomplete: 'new-password', placeholder: s.wifi?.ssid ? 'type it again to change it' : '', 'data-k': 'pw' }))),
    err,
    h('div', { class: 'actions' },
      h('p', { class: 'note' }, 'Boards get this while they\'re on USB or Bluetooth. They use 2.4 GHz Wi-Fi only.'),
      h('div', { class: 'r' },
        s.wifi?.ssid ? btn('Forget', (e) => patchSettings({ wifi: { forget: true } }, e.currentTarget)) : null,
        btn('Save and send to boards', async (e) => {
          err.textContent = '';
          const b = e.currentTarget;
          b.disabled = true;
          try {
            const r = await api('PATCH', '/admin/v1/settings', { wifi: { ssid: $('#ssid').value.trim(), password: $('#pw').value, send: true } });
            S.settings = r.settings;
            const n = (r.sent || []).length;
            toast(n ? `Saved and sent to ${n} board${n === 1 ? '' : 's'} on USB or Bluetooth` : 'Saved. Boards get it when they\'re plugged in or on Bluetooth.');
            $('#pw').value = '';
            draw(true);
          } catch (ex) {
            err.textContent = ex.message;
          } finally {
            if (b.isConnected) b.disabled = false;
          }
        }, 'p'))));
  out.push(box);
  const boards = (S.devices?.devices || []).filter((d) => d.kind === 'serial' && d.state === 'active');
  for (const d of boards) {
    let meta;
    if (d.wifi?.ssid) meta = `on ${d.wifi.ssid} · ${d.wifi.state || 'unknown'}`;
    else if (d.connection === 'usb') meta = d.canWifi === false && d.board ? 'on USB · this board has no Wi-Fi' : 'on USB · has no Wi-Fi yet';
    else if (d.connection === 'bluetooth') meta = d.canWifi === false && d.board ? 'on Bluetooth · this board has no Wi-Fi' : 'on Bluetooth · has no Wi-Fi yet';
    else if (d.connection === 'wifi') meta = 'on Wi-Fi';
    else meta = 'not connected';
    if (d.wifi?.note) meta += ` · ${d.wifi.note}`;
    const right = [];
    const inHand = d.connection === 'usb' || d.connection === 'bluetooth';
    if (inHand && d.canWifi !== false && S.open.wifi !== d.id) {
      right.push(btn('Use another network', () => { S.open.wifi = d.id; draw(); $(`#ssid-${CSS.escape(d.id)}`)?.focus(); }));
      if (s.wifi?.ssid && (d.wifi?.own || !d.wifi?.ssid)) {
        right.push(btn('Send Wi-Fi', (e) => deviceAction(d, 'wifi', { useDefault: true }, e.currentTarget, (r) => r?.message || 'Sent')));
      }
    } else if (!inHand) {
      meta += ' · plug it in, or connect it over Bluetooth, to change its Wi-Fi';
    }
    out.push(row(d.name, meta, right, { extra: S.open.wifi === d.id ? wifiForm(d, () => load('devices')) : null, name: true }));
  }

  out.push(...bluetoothNetwork());

  out.push(lab('phone'));
  if (!S.pairing) {
    out.push(row('Pair a phone', 'Shows a code to enter in the phone. The phone sees the deck while it\'s on your network.', [
      btn('Show code', () => { S.pairing = { form: true }; draw(); $('#pair-name')?.focus(); }, '', { disabled: !s.networkRunning }),
    ]));
    if (!s.networkRunning) out.push(h('p', { class: 'note' }, 'Turn on Let paired devices connect over the network (and restart the service) to pair a phone.'));
  } else if (S.pairing.form) {
    let scope = S.pairing.scope || 'view';
    out.push(h('div', { class: 'box' },
      h('div', { class: 'grid2' },
        h('div', {}, h('label', { for: 'pair-name' }, 'A name for it'), h('input', { id: 'pair-name', type: 'text', value: S.pairing.name || 'phone', 'data-k': 'pair-name' })),
        h('div', {}, h('label', {}, 'It may'), seg('What the phone may do', SCOPES.slice(0, 2), scope, (v) => { S.pairing.scope = v; S.pairing.name = $('#pair-name').value; draw(); }))),
      h('div', { class: 'actions' }, h('div', { class: 'r' },
        btn('Cancel', () => { S.pairing = null; draw(); }),
        btn('Make a code', async (e) => {
          const r = await busy(e.currentTarget, () => api('POST', '/admin/v1/pair', { name: $('#pair-name').value.trim(), scope }));
          if (r) { S.pairing = { code: r }; draw(); }
        }, 'p')))));
  } else {
    const c = S.pairing.code;
    out.push(h('div', { class: 'box' },
      h('p', {}, `Enter these in the app on the phone. The code works once, for 2 minutes.`),
      h('div', { class: 'code-box', 'aria-label': 'Pairing details' }, `address ${c.url}`, h('br'), `code ${c.code}`, h('br'), `deck ${c.ca}`),
      h('div', { class: 'actions' }, h('div', { class: 'r' }, btn('Done', () => { S.pairing = null; draw(); }, 'p')))));
  }
  return out;
}

// bluetoothNetwork: whether Bluetooth is on, and what to do when it isn't.
function bluetoothNetwork() {
  const bt = S.bt;
  if (!bt) return [];
  const out = [lab('bluetooth')];
  let title = 'Bluetooth is on';
  let meta = 'Boards you add over Bluetooth use it when they have no cable and no Wi-Fi, and a new board can be set up through it.';
  if (!bt.available) {
    title = { none: 'Bluetooth isn\'t available here', unauthorized: 'Bluetooth needs your permission', off: 'Bluetooth is off' }[bt.helper === 'none' ? 'none' : bt.radio] || 'Bluetooth is starting';
    meta = bt.why || '';
  }
  out.push(row(title, meta, bt.available ? [h('a', { class: 'b', href: '#boards' }, 'Add a board')] : [], { off: !bt.available }));
  if (bt.radio === 'unauthorized' || (bt.available === false && bt.helper !== 'none' && bt.radio !== 'off')) {
    out.push(h('p', { class: 'note' }, 'macOS asks once whether "agents deck Bluetooth" may use Bluetooth. If the answer was no, turn it on in System Settings, Privacy & Security, Bluetooth.'));
  }
  if (bt.lastError) out.push(h('p', { class: 'note' }, `Last problem: ${bt.lastError}`));
  return out;
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
  $('#updates-sub').textContent = `You have ${u.current}${u.install === 'source' ? ' (built from source)' : ''}. ${u.checked ? `Last checked ${ago(u.checked)}.` : 'Not checked yet.'}`;
  const out = [];
  const job = S.jobs[u.job || S.appJob] || null;
  if (job && job.state === 'running') {
    out.push(row(`Installing agents deck ${job.version || u.version}`, job.message, [], { hot: true, tall: true }));
  } else if ((u.state === 'ready' || u.state === 'available') && S.laterUpdate !== u.version) {
    const notes = notesLines(u.notes);
    const can = u.state === 'ready' || (u.state === 'available' && !u.message);
    const meta = h('div', {}, ...(notes.length ? notes.flatMap((l, i) => (i ? [h('br'), l] : [l])) : ['A new version is out.']));
    const right = [btn('Later', () => { S.laterUpdate = u.version; draw(); }, 'o')];
    if (can) right.push(btn('Install and restart', async (e) => {
      const j = await busy(e.currentTarget, () => api('POST', '/admin/v1/update/apply', {}));
      if (j?.id) { putJob(j); S.appJob = j.id; await load('update'); draw(true); }
    }, 'k'));
    else if (u.page) right.push(h('a', { class: 'b', href: u.page, rel: 'noreferrer' }, 'Download page'));
    out.push(h('div', { class: 'row hot tall' },
      h('div', { class: 'tx' }, h('div', { class: 't' }, `agents deck ${u.version} ${u.state === 'ready' ? 'is ready' : 'is out'}`), h('div', { class: 'm' }, meta)),
      h('div', { class: 'r' }, right)));
    out.push(h('p', { class: 'note' }, u.message || 'Your agents keep running while the app restarts. It takes a few seconds.'));
  } else if (u.state === 'disabled') {
    out.push(row('This copy doesn\'t update itself', u.message || '', []));
  } else if (u.state === 'none' && u.version) {
    out.push(h('p', { class: 'note' }, `agents deck is up to date (${u.version} is the latest).`));
  }
  if (job && job.state === 'failed') out.push(errorBox(`The update didn't install: ${job.error}`));
  if (u.error) out.push(h('p', { class: 'note err' }, `The last check didn't work: ${u.error}`));

  out.push(lab('how to update'));
  out.push(row('The app and its service', 'Downloads come from GitHub and are checked before they\'re used.', [
    seg('Update mode', [{ value: 'notify', text: 'tell me' }, { value: 'auto', text: 'automatic' }, { value: 'off', text: 'never check' }], u.mode, async (v) => {
      const r = await patchSettings({ updates: v });
      if (r) { await load('update'); draw(); }
    }),
    btn('Check now', (e) => busy(e.currentTarget, async () => {
      S.update = await api('POST', '/admin/v1/update/check', {});
      S.laterUpdate = '';
      draw();
    }, () => (S.update.state === 'none' ? 'Up to date' : `Found ${S.update.version}`)), '', { 'data-k': 'check-now' }),
  ]));

  out.push(lab('boards'));
  const devs = S.devices?.devices || [];
  const withUpd = devs.filter((d) => d.update);
  for (const d of withUpd) {
    const how = { usb: 'over USB · about a minute', wifi: 'over Wi-Fi · about 2 minutes', unplugged: 'not connected · plug it in to update' }[d.update.how];
    const from = d.update.source === 'local' ? ' built on this computer' : '';
    const meta = `${d.firmware} → ${d.update.version}${from} · ${d.update.ready ? how : d.update.note || how}`;
    out.push(row(d.name, meta, d.update.ready ? [btn(from ? 'Install' : 'Update', (e) => updateDevice(d, e.currentTarget), 'a')] : [],
      { pic: pic(shapeOf(d.board || d.type), !d.online, boardOf(`${d.board} ${d.type}`)), off: !d.online, name: true }));
  }
  const latest = S.devices?.latest;
  const current = latest ? devs.filter((d) => d.kind === 'serial' && d.firmware === latest).length : 0;
  let foot = 'Boards never update by themselves.';
  if (!devs.some((d) => d.kind === 'serial')) foot += ' No boards yet.';
  else if (!withUpd.length && latest) foot += ` Every board is on ${latest}.`;
  else if (current) foot += ` ${current === 1 ? 'One board is' : `${current} boards are`} already on ${latest}.`;
  if (!latest && S.devices?.catalogError) foot += ` Couldn't read the latest firmware list: ${S.devices.catalogError}`;
  out.push(h('p', { class: 'note' }, foot));
  return out;
}

// ---- Troubleshoot ----------------------------------------------------------------------------

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

function drawTroubleshoot() {
  const d = S.diag;
  const sys = S.system || {};
  const out = [];
  const stats = h('div', { class: 'stats' });
  [['service', connOK ? 'running' : 'not answering'], ['up for', d.uptime], ['agents', String(d.agents)], ['boards online', `${d.boardsOnline} of ${d.boards}`]]
    .forEach(([k, v]) => stats.append(h('div', { class: 'stat' }, h('span', {}, k), h('b', {}, v))));
  out.push(stats);
  out.push(row('Restart the service', `Running since ${new Date(d.started).toLocaleString()}. Agents keep running. Boards reconnect in a few seconds.`, [btn('Restart', (e) => restartService(e.currentTarget))]));
  out.push(row('Copy a report', 'Versions, your system and board models. No project names, folders or prompts.', [
    btn(S.showReport ? 'Hide it' : 'Show what\'s in it', () => { S.showReport = !S.showReport; draw(); }, '', { 'aria-expanded': S.showReport ? 'true' : 'false', 'aria-controls': 'report', 'data-k': 'show-report' }),
    btn('Copy', async (e) => {
      const ok = await copyText(d.report);
      toast(ok ? 'Copied the report' : 'Couldn\'t copy: use Show what\'s in it and copy it by hand', !ok);
    }, 'p'),
  ]));
  if (S.showReport) out.push(h('pre', { class: 'report', id: 'report', tabindex: '0' }, d.report));
  out.push(h('div', { class: 'actions' }, h('div', { class: 'lab' }, 'latest from the log'),
    h('div', { class: 'r' }, btn('Open the log folder', (e) => busy(e.currentTarget, () => api('POST', '/admin/v1/open-data-folder', {}))))));
  if (!d.log.length) out.push(h('p', { class: 'note' }, 'The log is empty, or the service writes it somewhere else (agentctl serve in a terminal prints it there).'));
  else {
    const ul = h('ul', { class: 'log', 'aria-label': 'The last lines of the log' });
    d.log.forEach((l) => ul.append(h('li', { class: /error|fail|stopped answering|refused/i.test(l) ? 'em' : '' }, l)));
    out.push(ul);
  }
  out.push(lab('where its files are'));
  out.push(row('Data folder', d.dataDir, []));
  out.push(row('Log', d.logFile, []));
  if (sys.platform) out.push(h('p', { class: 'note' }, `agents deck ${sys.version} · ${sys.platform} · installed as ${sys.install}`));
  return out;
}

// ---- About ----------------------------------------------------------------------------------

function drawAbout() {
  const sys = S.system || {};
  const u = S.update || {};
  const kinds = { app: 'the app', windows: 'the Windows installer', deb: 'a .deb package', zip: 'agentctl.exe from the .zip', archive: 'install.sh', source: 'built from source' };
  return [
    row(`agents deck ${sys.version || ''}`, `${sys.platform || ''} · installed with ${kinds[sys.install] || sys.install || 'unknown'}${u.version && u.version !== sys.version ? ` · ${u.version} is the latest` : ''}`, []),
    row('Source, licence and issues', 'github.com/brushknight/agents-deck', [h('a', { class: 'b', href: 'https://github.com/brushknight/agents-deck', rel: 'noreferrer' }, 'Open')]),
    row('Documentation', 'How the deck, the boards and the API work.', [h('a', { class: 'b', href: 'https://github.com/brushknight/agents-deck/tree/main/docs', rel: 'noreferrer' }, 'Open')]),
    row('The web flasher', 'Put the firmware on a board from Chrome or Edge, with nothing installed. Boards here does the same.', [h('a', { class: 'b', href: 'https://brushknight.com/agents-deck', rel: 'noreferrer' }, 'Open')]),
    h('p', { class: 'note' }, 'In a terminal: agentctl version, agentctl status.'),
  ];
}

// ---- pages and routing ------------------------------------------------------------------------

const DRAW = {
  general: ['general-body', drawGeneral], agents: ['agents-body', drawAgents], boards: ['boards-body', drawBoards],
  newboard: ['newboard-body', drawNewBoard], flash: ['newboard-body', drawFlash], network: ['network-body', drawNetwork],
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
  if (page === 'new-board') return ['newboard', rest.join('/')];
  if (page === 'flash') return ['flash', rest.join('/')];
  return [PAGES.includes(page) ? page : 'general', ''];
}

async function route(initial) {
  const [page, arg] = parseHash();
  const changed = page !== S.page || arg !== S.arg;
  S.page = page; S.arg = arg;
  if (page === 'newboard' && (!S.nb || S.nb.port !== arg)) S.nb = { port: arg, step: 1, choice: '' };
  if (changed) { S.open = { more: '', wifi: '', name: '' }; S.confirmLoginOff = false; }
  const section = page === 'flash' ? 'newboard' : page;
  document.querySelectorAll('main > section').forEach((s) => { s.hidden = s.dataset.page !== section; });
  const navPage = section === 'newboard' ? 'boards' : section;
  document.querySelectorAll('.nav a').forEach((a) => {
    if (a.dataset.page === navPage) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
  });
  document.title = `agents deck · ${navPage}`;
  if (page === 'flash' && arg && !S.jobs[arg]) {
    try { S.jobs[arg] = await api('GET', `/admin/v1/jobs/${encodeURIComponent(arg)}`); } catch (e) { S.errors.job = e; }
  }
  await loadAll(...needs(page), ...extras(page));
  draw();
  if (changed && !initial) {
    const hd = document.querySelector(`main > section[data-page="${section}"] h1`);
    if (hd) { hd.tabIndex = -1; hd.focus(); }
  }
}

// refresh reloads the open page's data now and then (boards come and go).
async function refresh(force) {
  if (document.hidden && !force) return;
  const page = S.page;
  if (!page) return;
  const sys = await load('system');
  if (S.errors.system?.status === 404) {
    $('#banner').hidden = false;
    $('#banner').textContent = 'This service is too old for Settings: update agents deck.';
    return;
  }
  showSystem(sys);
  const busyUI = S.open.more || S.open.wifi || S.pairing || S.confirmLoginOff;
  if (!force && (busyUI || ['general', 'about'].includes(page))) return;
  await loadAll(...needs(page), ...extras(page));
  if (page === S.page) draw(false);
}

// While it looks for boards over Bluetooth, the list follows every 2 seconds.
setInterval(async () => {
  if (document.hidden || S.page !== 'boards' || !S.bt?.scanning) return;
  await loadAll('bt', 'btBoards');
  if (S.page === 'boards') draw(false);
}, 2000);

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
    if (S.page === 'boards' && j.device && j.state === 'running') draw(false);
    if (j.kind === 'bluetooth-add' && S.page === 'boards' && Object.values(S.btAdd).includes(j.id)) draw(true);
    if (j.kind === 'app-update' && j.state === 'failed') toast(`The update didn't install: ${j.error}`, true);
    if (S.page === 'updates' && j.kind === 'app-update') draw(true);
    if (j.state !== 'running' && ['boards', 'updates'].includes(S.page)) refresh(true);
  });
  es.onerror = () => {
    if (es.readyState === EventSource.CLOSED) setTimeout(listen, 3000);
  };
}

// ---- start ------------------------------------------------------------------------------------

window.addEventListener('hashchange', () => route(false));
document.addEventListener('visibilitychange', () => { if (!document.hidden) refresh(true); });
setInterval(() => refresh(false), 4000);

(async () => {
  const sys = await load('system');
  if (S.errors.system?.status === 404) {
    $('#banner').hidden = false;
    $('#banner').textContent = 'This service is too old for Settings: update agents deck.';
  }
  showSystem(sys);
  await route(true);
  listen();
})();
