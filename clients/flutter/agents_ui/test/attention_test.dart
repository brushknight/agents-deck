import 'dart:math' as math;

import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter_test/flutter_test.dart';

/// A random source that hands out the numbers it is given, in order, and
/// records each bound it was asked for.
class _Dice implements math.Random {
  _Dice(this.rolls);

  final List<int> rolls;
  final bounds = <int>[];

  @override
  int nextInt(int max) {
    bounds.add(max);
    final v = rolls.isEmpty ? 0 : rolls.removeAt(0);
    return v % max;
  }

  @override
  bool nextBool() => false;

  @override
  double nextDouble() => 0;
}

class _Clock {
  DateTime t = DateTime.utc(2026, 10, 8, 12);
  DateTime call() => t;
  void advance(Duration d) => t = t.add(d);
}

void main() {
  late _Clock clock;
  late _Dice dice;

  AttentionSpots spots(int capacity, {List<int> rolls = const []}) {
    dice = _Dice([...rolls]);
    return AttentionSpots(capacity: capacity, random: dice, now: clock.call);
  }

  setUp(() => clock = _Clock());

  test('who is on it: starting, running and waiting', () {
    for (final s in AgentStatus.values) {
      expect(
        wantsAttention(s),
        s == AgentStatus.starting ||
            s == AgentStatus.running ||
            s == AgentStatus.waiting,
        reason: '$s',
      );
    }
  });

  test('newcomers take the lowest free spots, in fleet order', () {
    final s = spots(4);
    expect(s.update([('a', true), ('b', false), ('c', true)]), isTrue);
    expect(s.spots, ['a', 'c', null, null]);
    expect(s.count, 2);
    expect(s.waiting, 0);
    // Nothing new: no change reported.
    expect(s.update([('a', true), ('b', false), ('c', true)]), isFalse);
  });

  test('stays 5 s after it stops, then leaves and its spot stays empty', () {
    final s = spots(4)..update([('a', true), ('b', true), ('c', true)]);
    clock.advance(const Duration(seconds: 1));
    s.update([('a', true), ('b', false), ('c', true)]);
    expect(s.spots, ['a', 'b', 'c', null]);
    expect(s.isLeaving('b'), isTrue);
    expect(s.nextLeave, clock.t.add(const Duration(seconds: 5)));
    expect(s.untilNextLeave, const Duration(seconds: 5));

    clock.advance(const Duration(milliseconds: 4999));
    expect(s.tick(), isFalse);
    expect(s.spots, ['a', 'b', 'c', null]);
    // A new state while lingering doesn't restart the 5 s.
    s.update([('a', true), ('b', false), ('c', true)]);
    expect(s.untilNextLeave, const Duration(milliseconds: 1));

    clock.advance(const Duration(milliseconds: 1));
    expect(s.tick(), isTrue);
    expect(s.spots, ['a', null, 'c', null]); // nobody moved
    expect(s.nextLeave, isNull);
    expect(s.count, 2);
  });

  test('back to work inside the 5 s: it never leaves', () {
    final s = spots(4)..update([('a', true), ('b', true)]);
    s.update([('a', true), ('b', false)]);
    clock.advance(const Duration(seconds: 3));
    s.update([('a', true), ('b', true)]);
    expect(s.isLeaving('b'), isFalse);
    expect(s.nextLeave, isNull);
    clock.advance(const Duration(seconds: 10));
    expect(s.tick(), isFalse);
    expect(s.spots, ['a', 'b', null, null]);
  });

  test('leaving is also applied on the next update, timer or not', () {
    final s = spots(2)..update([('a', true)]);
    s.update([('a', false)]);
    clock.advance(const Duration(seconds: 6));
    expect(s.update([('a', false)]), isTrue);
    expect(s.spots, [null, null]);
  });

  test('a returning agent goes back to its old spot when it is free', () {
    final s = spots(4)..update([('a', true), ('b', true), ('c', true)]);
    s.update([('a', true), ('b', false), ('c', true)]);
    clock.advance(const Duration(seconds: 5));
    s.tick();
    expect(s.spots, ['a', null, 'c', null]);
    expect(s.rememberedSpotOf('b'), 1);
    s.update([('a', true), ('b', true), ('c', true)]);
    expect(s.spots, ['a', 'b', 'c', null]);
    expect(dice.bounds, isEmpty); // no random pick needed
  });

  test('old spot taken: a random free spot (not the first), remembered', () {
    final s = spots(5, rolls: [1])
      ..update([('a', true), ('b', true), ('c', true)]);
    // b leaves; d arrives and takes the lowest free spot: b's.
    s.update([('a', true), ('b', false), ('c', true)]);
    clock.advance(const Duration(seconds: 5));
    s.tick();
    s.update([('a', true), ('b', false), ('c', true), ('d', true)]);
    expect(s.spots, ['a', 'd', 'c', null, null]);

    // b comes back: spot 1 is d's, so a random free one among [3, 4].
    s.update([('a', true), ('b', true), ('c', true), ('d', true)]);
    expect(dice.bounds, [2]);
    expect(s.spots, ['a', 'd', 'c', null, 'b']);
    expect(s.rememberedSpotOf('b'), 4);

    // From now on spot 4 is b's.
    s.update([('a', true), ('b', false), ('c', true), ('d', true)]);
    clock.advance(const Duration(seconds: 5));
    s.tick();
    s.update([('a', true), ('b', true), ('c', true), ('d', true)]);
    expect(s.spots, ['a', 'd', 'c', null, 'b']);
    expect(dice.bounds, [2]);
  });

  test('full: newcomers wait, then take spots oldest first', () {
    final s = spots(2)..update([('a', true), ('b', true)]);
    s.update([('a', true), ('b', true), ('c', true), ('d', true)]);
    expect(s.spots, ['a', 'b']);
    expect(s.waiting, 2);
    expect(s.count, 4);

    // b stops; c still waits until b has left.
    s.update([('a', true), ('b', false), ('c', true), ('d', true)]);
    expect(s.spots, ['a', 'b']);
    clock.advance(const Duration(seconds: 5));
    expect(s.tick(), isTrue);
    expect(s.spots, ['a', 'c']); // c waited longest
    expect(s.waiting, 1);

    // d stops while waiting: it lingers in the queue, then goes.
    s.update([('a', true), ('b', false), ('c', true), ('d', false)]);
    clock.advance(const Duration(seconds: 5));
    s.tick();
    expect(s.waiting, 0);
    expect(s.count, 2);
  });

  test('an agent gone from the fleet leaves at once and is forgotten', () {
    final s = spots(3)..update([('a', true), ('b', true)]);
    expect(s.update([('b', true)]), isTrue);
    expect(s.spots, [null, 'b', null]);
    expect(s.rememberedSpotOf('a'), isNull);
    // It comes back as a newcomer: lowest free spot.
    s.update([('b', true), ('a', true)]);
    expect(s.spots, ['a', 'b', null]);
  });

  test('a smaller grid re-places only the agents whose spot is gone', () {
    final s = spots(6, rolls: [1])
      ..update([
        ('a', true),
        ('b', true),
        ('c', true),
        ('d', true),
        ('e', true),
      ]);
    s.update([
      ('a', false),
      ('b', true),
      ('c', false),
      ('d', true),
      ('e', true),
    ]);
    clock.advance(const Duration(seconds: 5));
    s.tick();
    expect(s.spots, [null, 'b', null, 'd', 'e', null]);

    expect(s.resize(4), isTrue);
    // e's spot 4 is gone: a random free spot of [0, 2] (roll 1 = spot 2).
    expect(s.spots, [null, 'b', 'e', 'd']);
    expect(dice.bounds, [2]);
    expect(s.rememberedSpotOf('e'), 2);
  });

  test('a smaller grid with no room: the displaced wait first', () {
    final s = spots(4)
      ..update([('a', true), ('b', true), ('c', true), ('d', true)]);
    s.update([('a', true), ('b', true), ('c', true), ('d', true), ('x', true)]);
    expect(s.waiting, 1);
    s.resize(2);
    expect(s.spots, ['a', 'b']);
    expect(s.waiting, 3);
    // Room again: c and d (they were on the screen) before x.
    s.resize(4);
    expect(s.spots, ['a', 'b', 'c', 'd']);
    expect(s.waiting, 1);
  });

  test('a bigger grid keeps every spot and seats whoever waits', () {
    final s = spots(2)..update([('a', true), ('b', true), ('c', true)]);
    expect(s.waiting, 1);
    s.resize(4);
    expect(s.spots, ['a', 'b', 'c', null]);
    expect(s.waiting, 0);
    expect(s.resize(4), isFalse);
  });

  test('updateFrom reads statuses from a fleet state, in slot order', () {
    final source = FakeAgentsSource.demo();
    final state = source.snapshot.value.state!;
    final s = spots(16)..updateFrom(state);
    final want = [
      for (final a in state.agents)
        if (onNeedsYou(a)) a.id,
    ];
    expect(s.spots.whereType<String>().toList(), want);
    expect(s.count, want.length);
    source.dispose();
  });

  test(
    'finished and not looked at yet (hungry): on the screen until checked',
    () {
      Agent a(AgentStatus status, {bool unseen = false}) =>
          Agent(id: 'a', slot: 0, title: 'a', status: status, unseen: unseen);
      expect(onNeedsYou(a(AgentStatus.running)), isTrue);
      expect(onNeedsYou(a(AgentStatus.waiting)), isTrue);
      expect(onNeedsYou(a(AgentStatus.idle, unseen: true)), isTrue);
      expect(onNeedsYou(a(AgentStatus.idle)), isFalse);
      expect(onNeedsYou(a(AgentStatus.exited, unseen: true)), isFalse);
    },
  );
}
