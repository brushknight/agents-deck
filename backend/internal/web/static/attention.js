// The "needs you" screen: who is on it and in which spot. No DOM here, so
// attention_test.mjs (node --test) can check it. app.js draws the result.
//
// An agent is on the screen while it works (running, starting) or asks you
// something (waiting), and for LINGER_MS after that. Spots are sticky: an agent
// keeps its spot while it is on the screen, and nothing moves up when another
// one leaves. A newcomer takes the lowest free spot; an agent that was here
// before gets its old spot back when that is free, else a random free one
// (which it then remembers). When the screen is full the rest wait, oldest
// first, for a spot to free up.

export const LINGER_MS = 5000;

export const wantsYou = (a) => a.status === 'running' || a.status === 'starting' || a.status === 'waiting'
  || (a.status === 'idle' && !!a.unseen); // finished, not looked at yet (hungry): stays until you check it

// arrange(prev, agents, capacity, now, rand) -> { state, spots, more, count, wake }
//   prev      the previous state (or {}); only prev.memory needs to survive a reload
//   agents    every agent on the deck, in deck order ({ id, status })
//   capacity  columns x rows of the screen
//   now       a clock in ms
//   rand      () -> [0, 1), for the random free spot
// spots[i] is the id of the agent in spot i, or null. more counts agents on the
// screen that have no spot yet (it is full). count is everyone on the screen,
// placed or waiting. wake is when to call again for a lingering agent to
// leave (ms), or null.
export function arrange(prev, agents, capacity, now, rand = Math.random) {
  const cap = Math.max(0, Math.floor(capacity) || 0);
  const ids = new Set(agents.map((a) => a.id));
  const keep = (o) => Object.fromEntries(Object.entries(o || {}).filter(([id]) => ids.has(id)));
  const memory = keep(prev.memory);   // id -> the spot it had last
  const shown = keep(prev.shown);     // id -> its spot now
  const leaveAt = keep(prev.leaveAt); // id -> when it leaves (stopped working or asking)
  const joined = keep(prev.joined);   // id -> when it came on the screen
  const order = new Map(agents.map((a, i) => [a.id, i]));

  let wake = null;
  const members = [];
  for (const a of agents) {
    if (wantsYou(a)) {
      delete leaveAt[a.id];
    } else if (a.id in joined) {
      if (!(a.id in leaveAt)) leaveAt[a.id] = now + LINGER_MS;
      if (now >= leaveAt[a.id]) {
        delete leaveAt[a.id]; delete joined[a.id]; delete shown[a.id];
        continue;
      }
      wake = wake == null ? leaveAt[a.id] : Math.min(wake, leaveAt[a.id]);
    } else {
      continue;
    }
    if (!(a.id in joined)) joined[a.id] = now;
    members.push(a.id);
  }

  // Spots that still exist keep their agent; the others are placed again.
  const spots = new Array(cap).fill(null);
  for (const id of Object.keys(shown)) {
    const i = shown[id];
    if (Number.isInteger(i) && i >= 0 && i < cap && spots[i] == null) spots[i] = id;
    else delete shown[id];
  }
  const free = () => spots.flatMap((v, i) => (v == null ? [i] : []));
  const waiting = members.filter((id) => !(id in shown))
    .sort((x, y) => joined[x] - joined[y] || order.get(x) - order.get(y));
  const picked = waiting.slice(0, free().length); // oldest first
  const put = (id, i) => { spots[i] = id; shown[id] = i; memory[id] = i; };
  // Old spots first, so a newcomer doesn't take the spot a returning agent had.
  const rest = [];
  for (const id of picked) {
    const m = memory[id];
    if (Number.isInteger(m) && m >= 0 && m < cap && spots[m] == null) put(id, m);
    else rest.push(id);
  }
  for (const id of rest) {
    const f = free();
    if (!(id in memory)) put(id, f[0]);
    else put(id, f[Math.min(f.length - 1, Math.floor(rand() * f.length))]);
  }
  return {
    state: { memory, shown, leaveAt, joined },
    spots,
    more: waiting.length - picked.length,
    count: members.length,
    wake,
  };
}
