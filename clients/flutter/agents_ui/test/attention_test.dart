import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter_test/flutter_test.dart';

/// [NeedsYou]: reading the service's "Needs you" spots, and the fallback for
/// an older service that sends none.
void main() {
  Map<String, dynamic> agent(
    String id,
    int slot,
    String status, {
    int spot = 0,
    bool unseen = false,
  }) => {
    'id': id,
    'slot': slot,
    'title': id,
    'status': status,
    'attentionSlot': spot,
    'unseen': unseen,
  };

  AgentsState state(List<Map<String, dynamic>> agents) => AgentsState.fromJson({
    'server': {'name': 'test', 'version': '0', 'time': '2026-10-08T12:00:00Z'},
    'agents': agents,
  });

  List<String?> ids(NeedsYou n) => [for (final a in n.spots) a?.id];

  test('who needs you: working, asking, or hungry', () {
    final s = state([
      agent('r', 0, 'running'),
      agent('s', 1, 'starting'),
      agent('w', 2, 'waiting'),
      agent('h', 3, 'idle', unseen: true),
      agent('i', 4, 'idle'),
      agent('e', 5, 'error'),
      agent('x', 6, 'exited'),
    ]);
    expect(
      [for (final a in s.agents) onNeedsYou(a)],
      [true, true, true, true, false, false, false],
    );
  });

  test('each agent goes in the spot the service gave it', () {
    final n = NeedsYou.of(
      state([
        agent('a', 0, 'running', spot: 3),
        agent('b', 1, 'waiting', spot: 16),
        agent('c', 2, 'idle', spot: 1), // stopped: there for its last 5 s
        agent('d', 3, 'idle'),
      ]),
    );
    expect(n.fromService, isTrue);
    expect(n.spots, hasLength(16));
    expect(ids(n), ['c', null, 'a', ...List.filled(12, null), 'b']);
    expect(n.count, 3);
  });

  test('a full grid: the ones waiting for a spot count too', () {
    final n = NeedsYou.of(
      state([
        for (var i = 0; i < 18; i++)
          agent('a$i', i, 'running', spot: i < 16 ? i + 1 : 0),
      ]),
    );
    expect(n.spots.whereType<Agent>(), hasLength(16));
    expect(n.count, 18);
  });

  test('bad or doubled spots are ignored', () {
    final n = NeedsYou.of(
      state([
        agent('a', 0, 'running', spot: 2),
        agent('b', 1, 'running', spot: 2),
        agent('c', 2, 'running', spot: 17),
      ]),
    );
    expect(ids(n).take(3), [null, 'a', null]);
  });

  test(
    'an older service sends no spots: those who need you, in deck order',
    () {
      final n = NeedsYou.of(
        state([
          agent('z', 5, 'waiting'),
          agent('a', 1, 'running'),
          agent('i', 0, 'idle'),
        ]),
      );
      expect(n.fromService, isFalse);
      expect(ids(n).take(3), ['a', 'z', null]);
      expect(n.count, 2);
      final none = NeedsYou.of(state([agent('i', 0, 'idle')]));
      expect(none.fromService, isTrue);
      expect(none.count, 0);
      expect(none.spots.whereType<Agent>(), isEmpty);
    },
  );

  test('a smaller screen shows them by spot, lowest first', () {
    final n = NeedsYou.of(
      state([
        agent('j', 0, 'running', spot: 10),
        agent('c', 1, 'running', spot: 3),
        agent('a', 2, 'running', spot: 1),
      ]),
    );
    expect([for (final a in n.packed(2)) a.id], ['a', 'c']);
    expect([for (final a in n.packed(8)) a.id], ['a', 'c', 'j']);
  });

  test('the demo gives spots like the service', () {
    final source = FakeAgentsSource.demo();
    final n = NeedsYou.of(source.snapshot.value.state!);
    expect(n.fromService, isTrue);
    // The busy agents and the hungry one, in deck order.
    expect(ids(n).take(9), [
      'k3f9a2',
      'h0m3l4',
      'd0cs1t',
      't3st5a',
      'k8sn0d',
      '4uthfl',
      'plug1n',
      'fw0t4x',
      null,
    ]);
    // One stops: its spot is left free, nobody moves; back: the same spot.
    final json = source.toJson();
    final list = (json['agents'] as List).cast<Map<String, dynamic>>();
    list.firstWhere((a) => a['id'] == 't3st5a')['status'] = 'idle';
    source.load(json);
    expect(ids(NeedsYou.of(source.snapshot.value.state!))[3], isNull);
    expect(ids(NeedsYou.of(source.snapshot.value.state!))[4], 'k8sn0d');
    list.firstWhere((a) => a['id'] == 't3st5a')['status'] = 'running';
    source.load(json);
    expect(ids(NeedsYou.of(source.snapshot.value.state!))[3], 't3st5a');
  });
}
