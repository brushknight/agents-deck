#!/usr/bin/env node
// The browser pictures in docs/images (web-*-v4.png, settings-*-v4.png), taken
// from `agentctl serve --demo`: the made-up fleet in docs/fixtures/state.json
// and the demo's made-up boards, ports and release. One app: Home is the deck,
// an agent's page answers its questions, the rest is Settings.
//
// Usage, from the repository's root (Node 22 or later, Google Chrome, Go):
//
//   node docs/tools/web-shots.mjs [out dir]     (default: docs/images)
//
// It builds agentctl into a temporary folder and runs the demo there with a
// data folder of its own (AGENTSTERM_HOME), the web UI on 127.0.0.1:17340 and
// the network listener off, so it never meets the service you run, your
// Keychain or a serial port. A stand-in `scutil` gives the demo the host name
// "workstation" instead of this computer's. Chrome runs headless with a
// throwaway profile. Everything is stopped and removed at the end.
import { spawn, execFileSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync, chmodSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const root = resolve(new URL('../..', import.meta.url).pathname);
const out = resolve(process.argv[2] || join(root, 'docs/images'));
const chrome = process.env.CHROME || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const webPort = 17340, cdpPort = 19222;
const base = `http://127.0.0.1:${webPort}`;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// A short temporary folder: the service's unix socket must fit in 104 bytes.
const tmp = mkdtempSync(join(process.env.SHOTS_TMP || tmpdir(), 'ad-shots-'));
const home = join(tmp, 'home'), bin = join(tmp, 'bin'), tmpd = join(tmp, 'tmp');
for (const d of [home, bin, tmpd]) mkdirSync(d, { recursive: true, mode: 0o700 });
writeFileSync(join(home, 'config.json'), JSON.stringify({ localAddr: `127.0.0.1:${webPort}`, deviceAddr: '' }));
writeFileSync(join(bin, 'scutil'), '#!/bin/sh\necho workstation\n');
chmodSync(join(bin, 'scutil'), 0o755);
execFileSync('go', ['build', '-trimpath', '-ldflags', '-X main.version=0.2.0', '-o', join(bin, 'agentctl'), './cmd/agentctl'],
  { cwd: join(root, 'backend'), stdio: 'inherit' });

const procs = [];
const cleanup = () => {
  for (const p of procs) { try { p.kill(); } catch { /* gone */ } }
};
// On a failure: stop both and leave the folder (its serve.log may help).
process.on('exit', cleanup);

procs.push(spawn(join(bin, 'agentctl'), ['serve', '--demo', '--log', join(tmp, 'serve.log')], {
  cwd: tmp, stdio: 'ignore',
  env: { HOME: home, PATH: `${bin}:/usr/bin:/bin`, TMPDIR: tmpd, AGENTSTERM_HOME: home },
}));
procs.push(spawn(chrome, ['--headless=new', `--remote-debugging-port=${cdpPort}`, '--remote-debugging-address=127.0.0.1',
  `--user-data-dir=${join(tmp, 'chrome')}`, '--no-first-run', '--no-default-browser-check', '--disable-extensions',
  '--disable-sync', '--hide-scrollbars', '--window-size=1440,900', 'about:blank'], { stdio: 'ignore' }));

let page;
for (let i = 0; i < 40 && !page; i++) {
  await sleep(250);
  try { page = (await (await fetch(`http://127.0.0.1:${cdpPort}/json/list`)).json()).find((t) => t.type === 'page'); } catch { /* not yet */ }
}
for (let i = 0; i < 40; i++) { try { await fetch(base + '/'); break; } catch { await sleep(250); } }
const cookie = readFileSync(join(home, 'web-session'), 'utf8').trim();

const ws = new WebSocket(page.webSocketDebuggerUrl);
await new Promise((r) => ws.addEventListener('open', r));
let id = 0;
const pending = new Map();
ws.addEventListener('message', (e) => {
  const m = JSON.parse(e.data);
  if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id); }
});
const send = (method, params = {}) => new Promise((res, rej) => {
  const n = ++id;
  pending.set(n, (m) => (m.error ? rej(new Error(`${method}: ${m.error.message}`)) : res(m.result)));
  ws.send(JSON.stringify({ id: n, method, params }));
});
const js = async (expr) => {
  const r = await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true });
  if (r.exceptionDetails) throw new Error(`${expr}: ${r.exceptionDetails.exception?.description || r.exceptionDetails.text}`);
  return r.result.value;
};
const view = (height) => send('Emulation.setDeviceMetricsOverride', { width: 1440, height, deviceScaleFactor: 2, mobile: false });
const go = async (path, wait = 3000) => { await send('Page.navigate', { url: base + path }); await sleep(wait); };
// click the first element matching sel whose text is (or, with part, contains) text
const click = (sel, text, part = false) => js(`(() => {
  const el = [...document.querySelectorAll(${JSON.stringify(sel)})].find((e) => ${part ? `e.textContent.includes(${JSON.stringify(text)})` : `e.textContent.trim() === ${JSON.stringify(text)}`});
  if (!el) throw new Error('nothing to click: ' + ${JSON.stringify(`${sel} ${text}`)});
  el.click(); return true; })()`);
// a rectangle in page coordinates: the element's, or from one element's top to another's top
const rect = (expr) => js(`(() => { const r = (${expr}); return r && { x: r.x, y: r.y + scrollY, width: r.width, height: r.height }; })()`);
const shot = async (name, clip) => {
  const params = { format: 'png' };
  if (clip) params.clip = { ...clip, scale: 1 }, params.captureBeyondViewport = true;
  const r = await send('Page.captureScreenshot', params);
  writeFileSync(join(out, `${name}.png`), Buffer.from(r.data, 'base64'));
  console.log(`${join(out, name)}.png`);
};
const pad = (r, p = 24) => ({ x: r.x - p, y: r.y - p, width: r.width + 2 * p, height: r.height + 2 * p });
// the main column, from its top to the bottom of what the page shows
const mainRect = `(() => { const main = document.getElementById('main'), m = main.getBoundingClientRect();
  const bottom = Math.max(m.top + 400, ...[...main.querySelectorAll('*')].filter((e) => e.offsetParent).map((e) => e.getBoundingClientRect().bottom));
  return new DOMRect(m.x, m.y, m.width, Math.min(m.bottom, bottom + 40) - m.y); })()`;
// a viewport tall enough for the whole page (the main column scrolls by itself)
const fit = async () => {
  const hgt = await js(`Math.ceil(document.getElementById('main').scrollHeight)`);
  await view(Math.max(900, hgt)); await sleep(600);
};

await send('Page.enable');
await send('Runtime.enable');
await send('Network.enable');
await send('Network.setCookie', { name: 'agentsterm_session', value: cookie, url: base + '/', httpOnly: true, sameSite: 'Strict' });

// Home: the deck with Needs you beside it; an agent asking for permission.
await view(900);
await go('/#home');
await shot('web-board-v4');
await shot('web-needs-you-v4', pad(await rect(`(() => { const n = document.getElementById('needs').getBoundingClientRect(), t = document.getElementById('needs').parentElement.getBoundingClientRect();
  return new DOMRect(t.x, t.y, t.width, n.bottom - t.y - 12); })()`)));
await go('/#agent/h0m3l4', 2000);
await shot('web-permission-v4');

// Boards as it opens, then the new board on USB: Set it up, Yes, it's mine.
await go('/#boards', 3500);
await shot('settings-boards-v4');
await click('button', 'Set it up');
await sleep(1500);
await click('button', "Yes, it's mine");
await sleep(3000);
await shot('settings-new-board-v4', await rect(mainRect));

// Updates and Network, the whole page.
await go('/#updates', 3500);
await fit();
await shot('settings-updates-v4', await rect(mainRect));
await view(900);
await go('/#network', 3500);
await fit();
await shot('settings-network-v4', await rect(mainRect));

// Boards, Add a board: Look for Bluetooth boards, with the demo's boards found.
await view(900);
await go('/#boards/add', 3000);
await js(`(() => { const s = document.querySelector('[aria-label="Look for boards over Bluetooth"]');
  if (!s) throw new Error('no Look for boards switch'); s.click(); })()`);
await sleep(7000); // the demo's boards turn up one by one
await fit();
await shot('settings-bluetooth-v4', await rect(mainRect));

ws.close();
await Promise.all(procs.map((p) => new Promise((r) => { if (p.exitCode !== null) r(); else { p.once('exit', r); p.kill(); } })));
rmSync(tmp, { recursive: true, force: true });
process.exit(0);
