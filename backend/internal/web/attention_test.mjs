// The "needs you" screen (static/attention.js), no browser needed:
//   node --test backend/internal/web/attention_test.mjs
// go test ./internal/web runs it too when node is installed.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { needsYou, packed, simulate, wantsYou, LINGER_MS, SPOTS } from './static/attention.js';

const ids = (spots) => spots.map((a) => (a ? a.id : null));

test('who needs you: working, asking, or hungry', () => {
  assert.ok(wantsYou({ status: 'running' }));
  assert.ok(wantsYou({ status: 'starting' }));
  assert.ok(wantsYou({ status: 'waiting' }));
  assert.ok(wantsYou({ status: 'idle', unseen: true }));
  for (const s of ['idle', 'error', 'exited', '']) assert.ok(!wantsYou({ status: s }));
});

test('each agent goes in the spot the service gave it', () => {
  const agents = [
    { id: 'a', slot: 0, status: 'running', attentionSlot: 3 },
    { id: 'b', slot: 1, status: 'waiting', attentionSlot: 16 },
    { id: 'c', slot: 2, status: 'idle', attentionSlot: 1 }, // stopped: still there for 5 s
    { id: 'd', slot: 3, status: 'idle', attentionSlot: 0 },
  ];
  const r = needsYou(agents);
  assert.equal(r.spots.length, SPOTS);
  assert.deepEqual(ids(r.spots), ['c', null, 'a', null, null, null, null, null, null, null, null, null, null, null, null, 'b']);
  assert.equal(r.count, 3);
  assert.equal(r.asking, 1);
  assert.ok(r.server);
});

test('a full grid: the ones waiting for a spot count too', () => {
  const agents = Array.from({ length: 18 }, (_, i) => ({ id: `a${i}`, slot: i, status: 'running', attentionSlot: i < 16 ? i + 1 : 0 }));
  agents[17].status = 'waiting';
  const r = needsYou(agents);
  assert.equal(r.spots.filter(Boolean).length, 16);
  assert.equal(r.count, 18);
  assert.equal(r.asking, 1);
});

test('bad or doubled spots are ignored', () => {
  const r = needsYou([
    { id: 'a', slot: 0, status: 'running', attentionSlot: 2 },
    { id: 'b', slot: 1, status: 'running', attentionSlot: 2 },
    { id: 'c', slot: 2, status: 'running', attentionSlot: 17 },
  ]);
  assert.deepEqual(ids(r.spots).slice(0, 3), [null, 'a', null]);
});

test('an older service sends no spots: the agents that need you, in deck order', () => {
  const r = needsYou([
    { id: 'z', slot: 5, status: 'waiting' },
    { id: 'a', slot: 1, status: 'running' },
    { id: 'i', slot: 0, status: 'idle' },
  ]);
  assert.equal(r.server, false);
  assert.deepEqual(ids(r.spots).slice(0, 3), ['a', 'z', null]);
  assert.equal(r.count, 2);
  // nobody needs you: an empty screen either way
  const none = needsYou([{ id: 'i', slot: 0, status: 'idle' }]);
  assert.ok(none.server);
  assert.equal(none.count, 0);
});

test('a smaller screen shows them by spot, lowest first', () => {
  const spots = new Array(SPOTS).fill(null);
  spots[9] = { id: 'j' }; spots[2] = { id: 'c' }; spots[0] = { id: 'a' };
  assert.deepEqual(packed(spots, 2).map((a) => a.id), ['a', 'c']);
  assert.deepEqual(packed(spots, 8).map((a) => a.id), ['a', 'c', 'j']);
});

// ?fixture mode stands in for the service with simulate(): the same rules.
const fleet = (spec) => Object.entries(spec).map(([id, status], slot) => ({ id, slot, status }));
const spotsOf = (agents) => Object.fromEntries(agents.filter((a) => a.attentionSlot).map((a) => [a.id, a.attentionSlot]));
const never = () => { throw new Error('no random spot expected'); };

test('fixture: lowest free spot, the 5 s stay, the old spot back, else a random one', () => {
  let agents = fleet({ a: 'running', b: 'running', c: 'running' });
  let r = simulate({}, agents, 0, never);
  assert.deepEqual(spotsOf(agents), { a: 1, b: 2, c: 3 });
  agents = fleet({ a: 'running', b: 'idle', c: 'running' });
  r = simulate(r.state, agents, 1000, never);
  assert.equal(r.wake, 1000 + LINGER_MS);
  assert.deepEqual(spotsOf(agents), { a: 1, b: 2, c: 3 }, 'b stays 5 s');
  r = simulate(r.state, agents, 1000 + LINGER_MS, never);
  assert.deepEqual(spotsOf(agents), { a: 1, c: 3 }, 'b left, c stays put');
  agents = fleet({ a: 'running', b: 'running', c: 'running' });
  r = simulate(r.state, agents, 9000, never);
  assert.deepEqual(spotsOf(agents), { a: 1, b: 2, c: 3 }, 'b back in its old spot');
  // b leaves, n takes spot 2, b comes back: a random free spot
  agents = fleet({ a: 'running', b: 'idle', c: 'running' });
  r = simulate(r.state, agents, 10000, never);
  r = simulate(r.state, agents, 10000 + LINGER_MS, never);
  agents = [...fleet({ a: 'running', b: 'idle', c: 'running' }), { id: 'n', slot: 9, status: 'running' }];
  r = simulate(r.state, agents, 20000, never);
  assert.equal(spotsOf(agents).n, 2);
  agents = [...fleet({ a: 'running', b: 'running', c: 'running' }), { id: 'n', slot: 9, status: 'running' }];
  r = simulate(r.state, agents, 21000, () => 0.99);
  assert.equal(spotsOf(agents).b, 16);
});
