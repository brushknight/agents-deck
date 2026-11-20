#!/usr/bin/env node
// The browser pictures in docs/images (web-*-v3.png, settings-*-v3.png), taken
// from `agentctl serve --demo`: the made-up fleet in docs/fixtures/state.json
// and the demo's made-up boards, ports and release.
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
const label = (page, text) => `[...document.querySelectorAll('section[data-page=${page}] .lab')].find((e) => e.textContent.trim() === ${JSON.stringify(text)})`;
// from the label's top to the next label's top (or the end of the page's section)
const fromLabel = (page, text) => `(() => { const a = ${label(page, text)}, labs = [...document.querySelectorAll('section[data-page=${page}] .lab')];
  const next = labs[labs.indexOf(a) + 1], ra = a.getBoundingClientRect(), sec = a.closest('section').getBoundingClientRect();
  const bottom = next ? next.getBoundingClientRect().y - 16 : sec.bottom;
  return new DOMRect(sec.x, ra.y, sec.width, bottom - ra.y); })()`;

await send('Page.enable');
await send('Runtime.enable');
await send('Network.enable');
await send('Network.setCookie', { name: 'agentsterm_session', value: cookie, url: base + '/', httpOnly: true, sameSite: 'Strict' });

// The deck, the Needs you page (the mark left of the page bars) and a prompt.
await view(900);
await go('/');
await shot('web-board-v3');
await js(`document.querySelector('#pagebar .attn-mark').click()`);
await sleep(1200);
await shot('web-needs-you-v3');
await go('/');
await click('.tile', 'infra-network', true);
await sleep(1000);
await shot('web-permission-v3');

// Settings: Boards as it opens, the new board on USB, Updates, Network, Bluetooth.
await go('/settings#boards', 3500);
await shot('settings-boards-v3');
await click('button', 'Set it up');
await sleep(1500);
await click('button', "Yes, it's mine");
await sleep(3000);
await shot('settings-new-board-v3', { x: 0, y: 0, width: 1440, height: 760 });

await view(2400);
await go('/settings#updates', 3500);
await shot('settings-updates-v3', pad(await rect(`(() => { const a = (${label('updates', 'how to update')}).getBoundingClientRect(),
  s = document.querySelector('section[data-page=updates]').getBoundingClientRect(); return new DOMRect(s.x, a.y, s.width, s.bottom - a.y); })()`)));
await go('/settings#network', 3500);
await shot('settings-network-v3', pad(await rect(`document.querySelector('section[data-page=network]').getBoundingClientRect()`), 24));

await view(3400);
await go('/settings#boards', 3500);
await js(`(() => { let r = [...document.querySelectorAll('section[data-page=boards] *')].find((e) => e.children.length === 0 && e.textContent.trim() === 'Look for boards');
  for (let i = 0; i < 5 && r; i++, r = r.parentElement) { const c = r.querySelector('input,button,[role=switch]'); if (c) { c.click(); return; } }
  throw new Error('no Look for boards switch'); })()`);
await sleep(7000); // the demo's boards turn up one by one
await shot('settings-bluetooth-v3', pad(await rect(fromLabel('boards', 'bluetooth'))));

ws.close();
await Promise.all(procs.map((p) => new Promise((r) => { if (p.exitCode !== null) r(); else { p.once('exit', r); p.kill(); } })));
rmSync(tmp, { recursive: true, force: true });
process.exit(0);
