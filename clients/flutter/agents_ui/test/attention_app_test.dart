import 'dart:io';
import 'dart:math' as math;

import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

/// The "Needs you" screen inside the deck: getting there, who is on it, the
/// sticky spots and the 5 s leave (on a clock the test moves by hand).
class _Clock {
  DateTime t = DateTime.utc(2026, 10, 8, 12);
  DateTime call() => t;
}

void main() {
  setUpAll(() async {
    TestWidgetsFlutterBinding.ensureInitialized();
    debugAgentsSetupHostname = 'panel';
    for (final (family, file) in [
      ('RobotoMono', 'RobotoMono.ttf'),
      ('ShareTechMono', 'ShareTechMono-Regular.ttf'),
    ]) {
      final data = File('fonts/$file').readAsBytesSync();
      final loader = FontLoader('packages/agents_deck_ui/$family')
        ..addFont(Future.value(ByteData.view(data.buffer)));
      await loader.load();
    }
  });

  late _Clock clock;
  setUp(() => clock = _Clock());

  Future<AttentionSpots> pump(WidgetTester tester, AgentsSource source) async {
    tester.view.physicalSize = const Size(720, 720);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    final spots = AttentionSpots(
      capacity: 16,
      random: math.Random(7),
      now: clock.call,
    );
    await tester.pumpWidget(
      MaterialApp(
        theme: ThemeData.dark().copyWith(scaffoldBackgroundColor: DeckHud.bg),
        home: Scaffold(
          body: AgentsApp(source: source, animate: false, attention: spots),
        ),
      ),
    );
    await tester.pump();
    return spots;
  }

  final screen = find.byKey(const Key('attention-screen'));
  Finder tile(String id) => find.byKey(ValueKey('attention-tile-$id'));

  Future<void> swipeToAttention(WidgetTester tester) async {
    await tester.fling(
      find.byKey(const Key('agents-pages')),
      const Offset(500, 0),
      1500,
    );
    await tester.pumpAndSettle();
  }

  /// The demo fleet with [status] set on the agents in [ids].
  Map<String, dynamic> withStatus(
    FakeAgentsSource source,
    Iterable<String> ids,
    String status,
  ) {
    final json = source.toJson();
    for (final a in (json['agents'] as List).cast<Map<String, dynamic>>()) {
      if (ids.contains(a['id'])) {
        a['status'] = status;
        if (status != 'waiting') a['waiting'] = null;
      }
    }
    return json;
  }

  // The demo fleet: seven agents are working or asking, in slot order
  // k3f9a2, h0m3l4, t3st5a, k8sn0d, 4uthfl, plug1n, fw0t4x.
  const busy = [
    'k3f9a2',
    'h0m3l4',
    't3st5a',
    'k8sn0d',
    '4uthfl',
    'plug1n',
    'fw0t4x',
  ];

  testWidgets('opens on the normal deck, with a count of who needs you', (
    tester,
  ) async {
    await pump(tester, FakeAgentsSource.demo());
    expect(screen, findsNothing);
    expect(find.byType(AgentTile), findsNWidgets(11));
    expect(find.text('Needs you'), findsNothing);
    final count = find.byKey(const Key('agents-attention-count'));
    expect(tester.widget<Text>(count).data, '7');
  });

  testWidgets('swipe right reaches "Needs you"; swipe left comes back', (
    tester,
  ) async {
    await pump(tester, FakeAgentsSource.demo());
    await swipeToAttention(tester);
    expect(screen, findsOneWidget);
    expect(find.text('Needs you'), findsOneWidget);
    expect(find.text('2 asking · 5 working'), findsOneWidget);
    for (final id in busy) {
      expect(tile(id), findsOneWidget, reason: id);
    }
    expect(find.byKey(const Key('attention-spot-empty')), findsNWidgets(9));
    // The count is for the normal view only.
    expect(find.byKey(const Key('agents-attention-count')), findsNothing);
    // Nothing further left.
    await swipeToAttention(tester);
    expect(screen, findsOneWidget);

    await tester.fling(
      find.byKey(const Key('agents-pages')),
      const Offset(-500, 0),
      1500,
    );
    await tester.pumpAndSettle();
    expect(screen, findsNothing);
    expect(find.byType(AgentTile), findsNWidgets(11));
    expect(tester.takeException(), isNull);
  });

  testWidgets('the left arrow, the count and a scroll get there too', (
    tester,
  ) async {
    await pump(tester, FakeAgentsSource.demo());
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowLeft);
    await tester.pumpAndSettle();
    expect(screen, findsOneWidget);
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
    await tester.pumpAndSettle();
    expect(screen, findsNothing);

    await tester.tap(find.byKey(const Key('agents-attention-count')));
    await tester.pumpAndSettle();
    expect(screen, findsOneWidget);
    await tester.tap(find.byKey(const Key('agents-page-0')));
    await tester.pumpAndSettle();
    expect(screen, findsNothing);

    // A sideways trackpad or mouse scroll.
    final pointer = TestPointer(1, PointerDeviceKind.mouse);
    final at = tester.getCenter(find.byKey(const Key('agents-pages')));
    await tester.sendEventToBinding(pointer.hover(at));
    await tester.sendEventToBinding(pointer.scroll(const Offset(-500, 0)));
    await tester.pumpAndSettle();
    expect(screen, findsOneWidget);
    await tester.sendEventToBinding(pointer.scroll(const Offset(500, 0)));
    await tester.pumpAndSettle();
    expect(screen, findsNothing);
  });

  testWidgets('a card opens from the "Needs you" screen as on the deck', (
    tester,
  ) async {
    await pump(tester, FakeAgentsSource.demo());
    await swipeToAttention(tester);
    await tester.tap(tile('k3f9a2'));
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.byType(AgentDetail), findsOneWidget);
    expect(find.byKey(const Key('agent-interrupt')), findsOneWidget);
  });

  testWidgets('a finished agent stays 5 s, then its spot is left empty', (
    tester,
  ) async {
    final source = FakeAgentsSource.demo();
    await pump(tester, source);
    await swipeToAttention(tester);
    final where = {for (final id in busy) id: tester.getTopLeft(tile(id))};

    source.load(withStatus(source, ['t3st5a'], 'idle'));
    await tester.pump();
    expect(tile('t3st5a'), findsOneWidget);

    clock.t = clock.t.add(const Duration(seconds: 4));
    await tester.pump(const Duration(seconds: 4));
    expect(tile('t3st5a'), findsOneWidget);

    clock.t = clock.t.add(const Duration(seconds: 1));
    await tester.pump(const Duration(seconds: 1));
    expect(tile('t3st5a'), findsNothing);
    expect(find.byKey(const Key('attention-spot-empty')), findsNWidgets(10));
    // Nobody else moved.
    for (final id in busy.where((id) => id != 't3st5a')) {
      expect(tester.getTopLeft(tile(id)), where[id], reason: id);
    }
    // It is still on the normal deck.
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
    await tester.pumpAndSettle();
    expect(find.text('api-tests'), findsOneWidget);
    expect(
      tester.widget<Text>(find.byKey(const Key('agents-attention-count'))).data,
      '6',
    );
  });

  testWidgets('back to work inside 5 s: it never leaves its spot', (
    tester,
  ) async {
    final source = FakeAgentsSource.demo();
    await pump(tester, source);
    await swipeToAttention(tester);
    final at = tester.getTopLeft(tile('t3st5a'));

    source.load(withStatus(source, ['t3st5a'], 'idle'));
    clock.t = clock.t.add(const Duration(seconds: 3));
    await tester.pump(const Duration(seconds: 3));
    source.load(withStatus(source, ['t3st5a'], 'running'));
    clock.t = clock.t.add(const Duration(seconds: 10));
    await tester.pump(const Duration(seconds: 10));
    expect(tester.getTopLeft(tile('t3st5a')), at);
  });

  testWidgets('sticky spots: back to its own spot, else a free one', (
    tester,
  ) async {
    final source = FakeAgentsSource.demo();
    final spots = await pump(tester, source);
    await swipeToAttention(tester);
    expect(spots.spotOf('t3st5a'), 2);
    final at = tester.getTopLeft(tile('t3st5a'));

    source.load(withStatus(source, ['t3st5a'], 'idle'));
    clock.t = clock.t.add(const Duration(seconds: 5));
    await tester.pump(const Duration(seconds: 5));
    expect(tile('t3st5a'), findsNothing);

    // Back to work: its old spot is free, so it goes there.
    source.load(withStatus(source, ['t3st5a'], 'running'));
    await tester.pump();
    expect(tester.getTopLeft(tile('t3st5a')), at);

    // It leaves again; a newcomer (docs-site) takes the lowest free spot,
    // which is t3st5a's. t3st5a then lands somewhere else that is free.
    source.load(withStatus(source, ['t3st5a'], 'idle'));
    clock.t = clock.t.add(const Duration(seconds: 5));
    await tester.pump(const Duration(seconds: 5));
    source.load(withStatus(source, ['d0cs1t'], 'running'));
    await tester.pump();
    expect(spots.spotOf('d0cs1t'), 2);
    source.load(withStatus(source, ['d0cs1t', 't3st5a'], 'running'));
    await tester.pump();
    final spot = spots.spotOf('t3st5a')!;
    expect(spot, greaterThanOrEqualTo(7));
    expect(spots.rememberedSpotOf('t3st5a'), spot);
    expect(tile('t3st5a'), findsOneWidget);
    expect(tile('d0cs1t'), findsOneWidget);
  });

  testWidgets('full: 16 spots and "+N more"; a freed spot seats the next', (
    tester,
  ) async {
    final fixture = FakeAgentsSource.demo().toJson();
    final base = (fixture['agents'] as List).cast<Map<String, dynamic>>();
    final agents = [
      for (var i = 0; i < 20; i++)
        {
          ...base[0],
          'id': 'a${i.toString().padLeft(5, '0')}',
          'slot': i,
          'title': 'agent-$i',
          'status': 'running',
        },
    ];
    final source = FakeAgentsSource.fromState({...fixture, 'agents': agents});
    await pump(tester, source);
    expect(
      tester.widget<Text>(find.byKey(const Key('agents-attention-count'))).data,
      '20',
    );
    await swipeToAttention(tester);
    expect(find.byKey(const Key('agents-page-attention')), findsOneWidget);
    expect(
      find.descendant(of: screen, matching: find.byType(AgentTile)),
      findsNWidgets(16),
    );
    expect(find.text('+4 more'), findsOneWidget);

    final json = source.toJson();
    (json['agents'] as List)[3]['status'] = 'idle';
    source.load(json);
    clock.t = clock.t.add(const Duration(seconds: 5));
    await tester.pump(const Duration(seconds: 5));
    expect(tile('a00003'), findsNothing);
    expect(tile('a00016'), findsOneWidget); // waited longest
    expect(find.text('+3 more'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('nobody working or asking: a plain empty screen, no count', (
    tester,
  ) async {
    final source = FakeAgentsSource.demo();
    source.load(withStatus(source, busy, 'idle'));
    await pump(tester, source);
    expect(find.byKey(const Key('agents-attention-count')), findsNothing);
    expect(find.byKey(const Key('agents-page-attention')), findsOneWidget);
    await swipeToAttention(tester);
    expect(find.text('Nothing needs you right now'), findsOneWidget);
    expect(find.byKey(const Key('attention-spot-empty')), findsNWidgets(16));
  });

  testWidgets('the leave timer stops with the board', (tester) async {
    final source = FakeAgentsSource.demo();
    await pump(tester, source);
    source.load(withStatus(source, ['t3st5a'], 'idle'));
    await tester.pump();
    await tester.pumpWidget(const SizedBox());
    // A pending timer would fail the test here.
  });

  testWidgets('spots outlive the board when the host keeps none', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(720, 720);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    final source = FakeAgentsSource.demo();
    Widget app() => MaterialApp(
      home: Scaffold(body: AgentsApp(source: source, animate: false)),
    );
    await tester.pumpWidget(app());
    await swipeToAttention(tester);
    final at = tester.getTopLeft(tile('plug1n'));
    await tester.pumpWidget(const SizedBox());
    // Rebuilt from scratch: a different agent is busy now, plug1n keeps 5.
    source.load(withStatus(source, ['k3f9a2'], 'idle'));
    await tester.pumpWidget(app());
    await tester.pump();
    expect(screen, findsNothing); // opens on the deck again
    await swipeToAttention(tester);
    expect(tester.getTopLeft(tile('plug1n')), at);
    await tester.pumpWidget(const SizedBox());
  });
}
