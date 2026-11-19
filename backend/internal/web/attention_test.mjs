// The "needs you" screen's spots (static/attention.js), no browser needed:
//   node --test backend/internal/web/attention_test.mjs
// go test ./internal/web runs it too when node is installed.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { arrange, LINGER_MS, wantsYou } from './static/attention.js';

const ag = (spec) => Object.entries(spec).map(([id, status]) => ({ id, status }));
const never = () => { throw new Error('no random spot expected'); };

// run steps the screen through a list of [agents, capacity, now] and returns the last result.
function run(steps, rand = never, state = {}) {
  let r;
  for (const [agents, cap, now] of steps) { r = arrange(state, agents, cap, now, rand); state = r.state; }
  return r;
}

test('working and asking agents are on it, the rest are not', () => {
  assert.ok(wantsYou({ status: 'running' }));
  assert.ok(wantsYou({ status: 'starting' }));
  assert.ok(wantsYou({ status: 'waiting' }));
  for (const s of ['idle', 'error', 'exited', '']) assert.ok(!wantsYou({ status: s }));
  const r = arrange({}, ag({ a: 'running', b: 'idle', c: 'waiting', d: 'error', e: 'starting' }), 16, 0, never);
  assert.deepEqual(r.spots.slice(0, 4), ['a', 'c', 'e', null]);
  assert.equal(r.count, 3);
  assert.equal(r.more, 0);
  assert.equal(r.spots.length, 16);
});

test('newcomers take the lowest free spot; nobody moves up when one leaves', () => {
  let r = arrange({}, ag({ a: 'running', b: 'running', c: 'running' }), 4, 0, never);
  assert.deepEqual(r.spots, ['a', 'b', 'c', null]);
  r = arrange(r.state, ag({ a: 'running', b: 'idle', c: 'running' }), 4, 1000, never);
  assert.deepEqual(r.spots, ['a', 'b', 'c', null], 'b lingers');
  r = arrange(r.state, ag({ a: 'running', b: 'idle', c: 'running' }), 4, 1000 + LINGER_MS, never);
  assert.deepEqual(r.spots, ['a', null, 'c', null], 'b left, c stays put');
  r = arrange(r.state, ag({ a: 'running', b: 'idle', c: 'running', d: 'waiting' }), 4, 7000, never);
  assert.deepEqual(r.spots, ['a', 'd', 'c', null], 'd takes the lowest free spot');
});

test('an agent stays 5 seconds after it stops, and wake says when it leaves', () => {
  let r = arrange({}, ag({ a: 'waiting' }), 4, 0, never);
  r = arrange(r.state, ag({ a: 'idle' }), 4, 100, never);
  assert.equal(r.spots[0], 'a');
  assert.equal(r.wake, 100 + LINGER_MS);
  r = arrange(r.state, ag({ a: 'idle' }), 4, 100 + LINGER_MS - 1, never);
  assert.equal(r.spots[0], 'a');
  r = arrange(r.state, ag({ a: 'idle' }), 4, 100 + LINGER_MS, never);
  assert.equal(r.spots[0], null);
  assert.equal(r.count, 0);
  assert.equal(r.wake, null);
});

test('back to work within 5 seconds: it never leaves', () => {
  let r = arrange({}, ag({ a: 'running', b: 'running' }), 4, 0, never);
  r = arrange(r.state, ag({ a: 'idle', b: 'running' }), 4, 1000, never);
  r = arrange(r.state, ag({ a: 'waiting', b: 'running' }), 4, 4000, never);
  assert.equal(r.wake, null);
  r = arrange(r.state, ag({ a: 'waiting', b: 'running' }), 4, 60000, never);
  assert.deepEqual(r.spots.slice(0, 2), ['a', 'b']);
  // and the 5 seconds start over the next time it stops
  r = arrange(r.state, ag({ a: 'idle', b: 'running' }), 4, 61000, never);
  assert.equal(r.wake, 61000 + LINGER_MS);
});

test('a returning agent gets its old spot back when it is free', () => {
  const r = run([
    [ag({ a: 'running', b: 'running', c: 'running' }), 4, 0],
    [ag({ a: 'running', b: 'idle', c: 'running' }), 4, 1],
    [ag({ a: 'running', b: 'idle', c: 'running' }), 4, 1 + LINGER_MS],
    [ag({ a: 'idle', b: 'idle', c: 'running' }), 4, 10000],
    [ag({ a: 'idle', b: 'idle', c: 'running' }), 4, 20000], // a left too: 0 and 1 are free
    [ag({ a: 'idle', b: 'running', c: 'running' }), 4, 30000],
  ]);
  assert.deepEqual(r.spots, [null, 'b', 'c', null], 'b goes back to 1, not the lowest free 0');
});

test('its old spot taken: a random free spot, which it then remembers', () => {
  let r = run([
    [ag({ a: 'running', b: 'running' }), 4, 0],
    [ag({ a: 'idle', b: 'running' }), 4, 1],
    [ag({ a: 'idle', b: 'running' }), 4, 1 + LINGER_MS], // a left spot 0
    [ag({ a: 'idle', b: 'running', n: 'running' }), 4, 10000], // n takes 0
  ]);
  assert.deepEqual(r.spots, ['n', 'b', null, null]);
  // free spots are 2 and 3; rand 0.9 picks the last, so not simply the first
  r = arrange(r.state, ag({ a: 'running', b: 'running', n: 'running' }), 4, 11000, () => 0.9);
  assert.deepEqual(r.spots, ['n', 'b', null, 'a']);
  assert.equal(r.state.memory.a, 3);
  // leave and come back once more: spot 3 again, no random pick
  r = run([
    [ag({ a: 'idle', b: 'running', n: 'running' }), 4, 12000],
    [ag({ a: 'idle', b: 'running', n: 'running' }), 4, 12000 + LINGER_MS],
    [ag({ a: 'running', b: 'running', n: 'running' }), 4, 20000],
  ], never, r.state);
  assert.deepEqual(r.spots, ['n', 'b', null, 'a']);
});

test('a full screen: the rest wait with a count, oldest first', () => {
  let r = run([
    [ag({ a: 'running', b: 'running' }), 2, 0],
    [ag({ a: 'running', b: 'running', c: 'running' }), 2, 1000],
    [ag({ a: 'running', b: 'running', c: 'running', d: 'waiting' }), 2, 2000],
  ]);
  assert.deepEqual(r.spots, ['a', 'b']);
  assert.equal(r.more, 2);
  assert.equal(r.count, 4);
  // d sits earlier in deck order, but c has waited longer
  r = arrange(r.state, ag({ d: 'waiting', a: 'idle', b: 'running', c: 'running' }), 2, 3000, never);
  r = arrange(r.state, ag({ d: 'waiting', a: 'idle', b: 'running', c: 'running' }), 2, 3000 + LINGER_MS, never);
  assert.deepEqual(r.spots, ['c', 'b'], 'c (first-timer) takes the freed spot');
  assert.equal(r.more, 1);
});

test('a waiting agent that stops working while it waits for a spot drops out after 5 seconds', () => {
  let r = arrange({}, ag({ a: 'running', b: 'running' }), 1, 0, never);
  assert.equal(r.more, 1);
  r = arrange(r.state, ag({ a: 'running', b: 'idle' }), 1, 1000, never);
  assert.equal(r.more, 1);
  r = arrange(r.state, ag({ a: 'running', b: 'idle' }), 1, 1000 + LINGER_MS, never);
  assert.equal(r.more, 0);
  assert.equal(r.count, 1);
});

test('the grid shrinks: agents in spots that are gone are placed again, the others keep theirs', () => {
  let r = arrange({}, ag({ a: 'running', b: 'running', c: 'running', d: 'running', e: 'running' }), 16, 0, never);
  r = run([
    [ag({ a: 'running', b: 'idle', c: 'idle', d: 'running', e: 'running' }), 16, 1],
    [ag({ a: 'running', b: 'idle', c: 'idle', d: 'running', e: 'running' }), 16, 1 + LINGER_MS],
  ], never, r.state);
  assert.deepEqual(r.spots.slice(0, 5), ['a', null, null, 'd', 'e']);
  // 16 -> 4 spots: e (spot 4) is gone; free are 1 and 2, rand 0 picks 1
  r = arrange(r.state, ag({ a: 'running', b: 'idle', c: 'idle', d: 'running', e: 'running' }), 4, 10000, () => 0);
  assert.deepEqual(r.spots, ['a', 'e', null, 'd']);
  assert.equal(r.state.memory.e, 1);
  // and back to 16: nobody moves
  r = arrange(r.state, ag({ a: 'running', b: 'idle', c: 'idle', d: 'running', e: 'running' }), 16, 11000, never);
  assert.deepEqual(r.spots.slice(0, 5), ['a', 'e', null, 'd', null]);
});

test('after a reload, remembered spots come back, and newcomers do not take them', () => {
  const memory = { a: 5, b: 0, gone: 3 };
  const r = arrange({ memory }, ag({ n: 'running', a: 'running', b: 'waiting' }), 16, 0, never);
  assert.equal(r.spots[5], 'a');
  assert.equal(r.spots[0], 'b');
  assert.equal(r.spots[1], 'n', 'n took the lowest spot nobody remembers');
  assert.ok(!('gone' in r.state.memory), 'ids no longer on the deck are forgotten');
});

test('agents that leave the deck leave the screen at once', () => {
  let r = arrange({}, ag({ a: 'running', b: 'running' }), 4, 0, never);
  r = arrange(r.state, ag({ b: 'running' }), 4, 10, never);
  assert.deepEqual(r.spots, [null, 'b', null, null]);
  assert.equal(r.count, 1);
});

test('no room at all', () => {
  const r = arrange({}, ag({ a: 'running' }), 0, 0, never);
  assert.deepEqual(r.spots, []);
  assert.equal(r.more, 1);
});

test('finished and not looked at yet (hungry): stays until you check it', () => {
  assert.ok(wantsYou({ status: 'idle', unseen: true }));
  assert.ok(!wantsYou({ status: 'idle', unseen: false }));
  const hungry = [{ id: 'a', status: 'idle', unseen: true }, { id: 'b', status: 'running' }];
  // Long after it finished, it is still in its spot.
  let r = run([[ag({ a: 'running', b: 'running' }), 16, 0], [hungry, 16, 1000], [hungry, 16, 10 * 60 * 1000]]);
  assert.deepEqual(r.spots.slice(0, 2), ['a', 'b']);
  // Checked: it leaves 5 seconds later, and b does not move.
  const seen = [{ id: 'a', status: 'idle', unseen: false }, { id: 'b', status: 'running' }];
  const t = 10 * 60 * 1000;
  let state = r.state;
  r = arrange(state, seen, 16, t + 1000, never); state = r.state;
  assert.deepEqual(r.spots.slice(0, 2), ['a', 'b']);
  r = arrange(state, seen, 16, t + 1000 + LINGER_MS, never);
  assert.deepEqual(r.spots.slice(0, 2), [null, 'b']);
});
