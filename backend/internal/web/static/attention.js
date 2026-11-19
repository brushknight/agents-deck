// The "needs you" screen: who is in which spot. No DOM here, so
// attention_test.mjs (node --test) can check it. app.js draws the result.
//
// The service decides who is on the screen and where (docs/protocol.md, "The
// Needs you grid"): every agent carries its spot in attentionSlot, 1..16 on a
// 4×4 grid, or 0 when it isn't there. So every screen shows the same agents in
// the same spots. needsYou() only reads that.

export const SPOTS = 16;

// The rule the service uses: working (running, starting), asking (waiting), or
// finished and not looked at yet (hungry).
export const wantsYou = (a) => a.status === 'running' || a.status === 'starting' || a.status === 'waiting'
  || (a.status === 'idle' && !!a.unseen);

// needsYou(agents) -> { spots, count, asking, server }
//   spots   16 entries, the agent in each spot (spot n at index n-1) or null
//   count   every agent on the screen, with the ones waiting for a spot (it is full)
//   asking  of those, how many ask you something
//   server  false for an older service that sends no spots: then the agents that
//           need you fill the spots in deck order (no 5 s stay, no remembered spots)
export function needsYou(agents) {
  const spots = new Array(SPOTS).fill(null);
  let placed = 0;
  for (const a of agents) {
    const s = a.attentionSlot;
    if (Number.isInteger(s) && s >= 1 && s <= SPOTS && !spots[s - 1]) { spots[s - 1] = a; placed++; }
  }
  const wanting = agents.filter(wantsYou);
  if (!placed && wanting.length) {
    const list = [...wanting].sort((x, y) => x.slot - y.slot);
    list.slice(0, SPOTS).forEach((a, i) => { spots[i] = a; });
    return { spots, count: list.length, asking: list.filter((a) => a.status === 'waiting').length, server: false };
  }
  const members = [...spots.filter(Boolean), ...wanting.filter((a) => !a.attentionSlot)];
  return { spots, count: members.length, asking: members.filter((a) => a.status === 'waiting').length, server: true };
}

// packed(spots, cells): for a screen with fewer cells than 16 (a phone), the
// agents in order of their spot, lowest first; the ones that don't fit aren't shown.
export const packed = (spots, cells) => spots.filter(Boolean).slice(0, Math.max(0, cells));

// ---------------------------------------------------------------- fixture
// ?fixture mode has no service: simulate(prev, agents, now, rand) gives each
// agent the attentionSlot the service would (store/attention.go), so the page
// can be tried on its own. The page itself never arranges.
export const LINGER_MS = 5000;

// simulate -> { state, wake }: state goes back in as prev next time; wake is
// when a lingering agent leaves (ms), or null.
export function simulate(prev, agents, now, rand = Math.random) {
  const ids = new Set(agents.map((a) => a.id));
  const keep = (o) => Object.fromEntries(Object.entries(o || {}).filter(([id]) => ids.has(id)));
  const memory = keep(prev.memory);   // id -> the spot it had last
  const shown = keep(prev.shown);     // id -> its spot now
  const leaveAt = keep(prev.leaveAt); // id -> when it leaves
  const joined = keep(prev.joined);   // id -> when it came on the screen
  const order = new Map(agents.map((a, i) => [a.id, i]));
  const members = [];
  for (const a of agents) {
    if (wantsYou(a)) delete leaveAt[a.id];
    else if (a.id in joined) {
      if (!(a.id in leaveAt)) leaveAt[a.id] = now + LINGER_MS;
      if (now >= leaveAt[a.id]) { delete leaveAt[a.id]; delete joined[a.id]; delete shown[a.id]; continue; }
    } else continue;
    if (!(a.id in joined)) joined[a.id] = now;
    members.push(a.id);
  }
  const taken = new Set();
  for (const id of members) {
    const s = shown[id];
    if (Number.isInteger(s) && !taken.has(s)) taken.add(s); else delete shown[id];
  }
  const free = () => Array.from({ length: SPOTS }, (_, i) => i + 1).filter((s) => !taken.has(s));
  const waiting = members.filter((id) => !(id in shown))
    .sort((x, y) => joined[x] - joined[y] || order.get(x) - order.get(y))
    .slice(0, free().length);
  const put = (id, s) => { taken.add(s); shown[id] = s; memory[id] = s; };
  const rest = [];
  for (const id of waiting) {
    if (Number.isInteger(memory[id]) && !taken.has(memory[id])) put(id, memory[id]); else rest.push(id);
  }
  for (const id of rest) {
    const f = free();
    put(id, id in memory ? f[Math.min(f.length - 1, Math.floor(rand() * f.length))] : f[0]);
  }
  for (const a of agents) a.attentionSlot = shown[a.id] || 0;
  const due = Object.values(leaveAt);
  return { state: { memory, shown, leaveAt, joined }, wake: due.length ? Math.min(...due) : null };
}
