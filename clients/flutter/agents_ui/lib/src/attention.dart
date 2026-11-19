/// Who sits where on the "Needs you" screen: the agents that are working or
/// asking, each in a spot of its own that never moves while it stays. Plain
/// Dart, no widgets: the clock and the random source come in from outside so
/// tests can drive both.
library;

import 'dart:math' as math;

import 'agents_model.dart';

/// Working (starting, running) or asking (waiting): the statuses that put an
/// agent on the "Needs you" screen.
bool wantsAttention(AgentStatus s) =>
    s == AgentStatus.starting ||
    s == AgentStatus.running ||
    s == AgentStatus.waiting;

/// Who is on the "Needs you" screen: an agent that is working or asking,
/// and one that finished a turn you haven't looked at yet (hungry). It
/// stays until you check it.
bool onNeedsYou(Agent a) => wantsAttention(a.status) || a.hungry;

/// The spots of the "Needs you" screen.
///
/// - An agent joins while [wantsAttention]; once it stops, it stays for
///   [linger], then leaves. Back to working or asking inside [linger] and it
///   never leaves.
/// - Each agent owns a spot (an index); leaving empties the spot and nobody
///   else moves.
/// - New agents take the lowest free spot. An agent that was here before
///   goes back to its last spot when that is free, otherwise to a random free
///   spot, which becomes its spot.
/// - With every spot taken, newcomers wait ([waiting]) and get spots oldest
///   first as spots free up.
/// - [resize] re-places only the agents whose spot no longer exists.
class AttentionSpots {
  AttentionSpots({
    required int capacity,
    math.Random? random,
    DateTime Function()? now,
    this.linger = const Duration(seconds: 5),
  }) : assert(capacity > 0),
       _random = random ?? math.Random(),
       _now = now ?? DateTime.now,
       _spots = List<String?>.filled(capacity, null);

  final math.Random _random;
  final DateTime Function() _now;

  /// How long an agent stays after it stops working or asking.
  final Duration linger;

  List<String?> _spots;

  /// Agent id to the spot it had last, kept after it leaves (forgotten once
  /// the agent is gone from the fleet).
  final _remembered = <String, int>{};

  /// Agents on the screen or waiting for a spot.
  final _members = <String>{};

  /// Members without a spot, oldest first.
  final _queue = <String>[];

  /// Members that stopped working or asking: when they leave.
  final _leaveAt = <String, DateTime>{};

  /// Spot index to agent id (null = empty). Never longer than [capacity].
  List<String?> get spots => List.unmodifiable(_spots);

  int get capacity => _spots.length;

  /// Agents that belong on the screen but have no spot (it is full).
  int get waiting => _queue.length;

  /// Every agent on the screen, plus [waiting].
  int get count => _members.length;

  /// The spot [id] is in now, or null.
  int? spotOf(String id) {
    final i = _spots.indexOf(id);
    return i < 0 ? null : i;
  }

  /// The spot [id] goes back to (its last one), or null if it has none.
  int? rememberedSpotOf(String id) => _remembered[id];

  /// Whether [id] stopped and is about to leave.
  bool isLeaving(String id) => _leaveAt.containsKey(id);

  /// When the next lingering agent leaves; null when none is lingering. The
  /// caller sets one timer for it and calls [tick].
  DateTime? get nextLeave {
    DateTime? first;
    for (final t in _leaveAt.values) {
      if (first == null || t.isBefore(first)) first = t;
    }
    return first;
  }

  /// How long until [nextLeave] on this object's clock (never negative);
  /// null when nobody is lingering.
  Duration? get untilNextLeave {
    final t = nextLeave;
    if (t == null) return null;
    final d = t.difference(_now());
    return d.isNegative ? Duration.zero : d;
  }

  /// Applies a fleet: every agent's id and whether it wants attention, in
  /// the order newcomers should be placed. Ids not listed are gone: they
  /// leave at once and are forgotten. True when [spots] or [waiting] changed.
  bool update(Iterable<(String id, bool active)> fleet) {
    final before = _signature();
    final now = _now();
    final seen = <String>{};
    for (final (id, active) in fleet) {
      if (!seen.add(id)) continue;
      if (active) {
        _leaveAt.remove(id);
        if (_members.add(id)) _queue.add(id);
      } else if (_members.contains(id)) {
        _leaveAt.putIfAbsent(id, () => now.add(linger));
      }
    }
    for (final id in [..._members]) {
      if (!seen.contains(id)) _drop(id);
    }
    _remembered.removeWhere((id, _) => !seen.contains(id));
    _expire(now);
    _fill();
    return _signature() != before;
  }

  /// [update] from a fleet state, in slot order.
  bool updateFrom(AgentsState state) =>
      update([for (final a in state.agents) (a.id, onNeedsYou(a))]);

  /// Lets lingering agents whose time is up leave. True when anything
  /// changed.
  bool tick() {
    final before = _signature();
    _expire(_now());
    _fill();
    return _signature() != before;
  }

  /// A new grid size. Agents whose spot still exists keep it; the others are
  /// placed again by the usual rules (before anyone already waiting).
  bool resize(int capacity) {
    assert(capacity > 0);
    if (capacity == _spots.length) return false;
    final before = _signature();
    final old = _spots;
    _spots = List<String?>.filled(capacity, null);
    final displaced = <String>[];
    for (final (i, id) in old.indexed) {
      if (id == null) continue;
      if (i < capacity) {
        _spots[i] = id;
      } else {
        displaced.add(id);
      }
    }
    _queue.insertAll(0, displaced);
    _fill();
    return _signature() != before;
  }

  void _drop(String id) {
    _members.remove(id);
    _queue.remove(id);
    _leaveAt.remove(id);
    final i = _spots.indexOf(id);
    if (i >= 0) _spots[i] = null;
  }

  void _expire(DateTime now) {
    final due = [
      for (final MapEntry(:key, :value) in _leaveAt.entries)
        if (!value.isAfter(now)) key,
    ];
    due.forEach(_drop);
  }

  /// Gives waiting members spots, oldest first, while any are free.
  void _fill() {
    while (_queue.isNotEmpty) {
      final free = [
        for (var i = 0; i < _spots.length; i++)
          if (_spots[i] == null) i,
      ];
      if (free.isEmpty) return;
      final id = _queue.removeAt(0);
      final last = _remembered[id];
      final int spot;
      if (last == null) {
        spot = free.first;
      } else if (last < _spots.length && _spots[last] == null) {
        spot = last;
      } else {
        spot = free[_random.nextInt(free.length)];
      }
      _spots[spot] = id;
      _remembered[id] = spot;
    }
  }

  String _signature() => '${_spots.join(',')}|${_queue.length}';
}
