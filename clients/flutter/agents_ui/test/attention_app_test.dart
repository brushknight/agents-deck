import 'dart:io';

import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

/// The "Needs you" screen inside the deck: getting there, and every agent
/// drawn in the spot the service gave it (attentionSlot).
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

  Future<void> pump(WidgetTester tester, AgentsSource source) async {
    tester.view.physicalSize = const Size(720, 720);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(
        theme: ThemeData.dark().copyWith(scaffoldBackgroundColor: DeckHud.bg),
        home: Scaffold(body: AgentsApp(source: source, animate: false)),
      ),
    );
    await tester.pump();
  }

  /// The demo fleet with everyone already looked at, so only the agents
  /// working or asking are on the screen, with the spots the service gives.
  FakeAgentsSource seenDemo() {
    final json = FakeAgentsSource.demo().toJson();
    for (final a in (json['agents'] as List).cast<Map<String, dynamic>>()) {
      a['unseen'] = false;
      a['attentionSlot'] = 0;
    }
    return FakeAgentsSource.fromState(json, attention: true);
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

  /// The fleet of [source] with each listed agent's attentionSlot (and
  /// status) set as a service would send them.
  Map<String, dynamic> withSpots(
    FakeAgentsSource source,
    Map<String, (String, int)> spots,
  ) {
    final json = source.toJson();
    for (final a in (json['agents'] as List).cast<Map<String, dynamic>>()) {
      if (spots[a['id']] case (final status, final spot)) {
        a['status'] = status;
        if (status != 'waiting') a['waiting'] = null;
        a['attentionSlot'] = spot;
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
    await pump(tester, seenDemo());
    expect(screen, findsNothing);
    expect(find.byType(AgentTile), findsNWidgets(11));
    final count = find.byKey(const Key('agents-attention-count'));
    expect(tester.widget<Text>(count).data, '7');
  });

  testWidgets('swipe right reaches "Needs you"; swipe left comes back', (
    tester,
  ) async {
    await pump(tester, seenDemo());
    await swipeToAttention(tester);
    expect(screen, findsOneWidget);
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
    await pump(tester, seenDemo());
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
    await pump(tester, seenDemo());
    await swipeToAttention(tester);
    await tester.tap(tile('k3f9a2'));
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.byType(AgentDetail), findsOneWidget);
    expect(find.byKey(const Key('agent-interrupt')), findsOneWidget);
  });

  testWidgets('each agent sits in the spot the service gave it', (
    tester,
  ) async {
    final source = FakeAgentsSource.fromState(seenDemo().toJson());
    source.load(
      withSpots(source, {
        'k3f9a2': ('running', 16),
        'h0m3l4': ('waiting', 1),
        't3st5a': ('running', 2),
        'k8sn0d': ('running', 5),
        '4uthfl': ('idle', 0),
        'plug1n': ('idle', 0),
        'fw0t4x': ('idle', 0),
      }),
    );
    await pump(tester, source);
    await swipeToAttention(tester);
    final one = tester.getTopLeft(tile('h0m3l4'));
    final two = tester.getTopLeft(tile('t3st5a'));
    final five = tester.getTopLeft(tile('k8sn0d'));
    final sixteen = tester.getTopLeft(tile('k3f9a2'));
    expect(two.dy, one.dy);
    expect(two.dx, greaterThan(one.dx));
    expect(five.dx, one.dx);
    expect(five.dy, greaterThan(one.dy));
    expect(sixteen.dx, greaterThan(two.dx));
    expect(sixteen.dy, greaterThan(five.dy));
    expect(find.byKey(const Key('attention-spot-empty')), findsNWidgets(12));

    // The service moves one and lets another go: the rest stay put.
    source.load(
      withSpots(source, {'t3st5a': ('idle', 0), 'k3f9a2': ('running', 3)}),
    );
    await tester.pump();
    expect(tile('t3st5a'), findsNothing);
    expect(tester.getTopLeft(tile('h0m3l4')), one);
    expect(tester.getTopLeft(tile('k8sn0d')), five);
    expect(tester.getTopLeft(tile('k3f9a2')).dy, one.dy);
    expect(tester.getTopLeft(tile('k3f9a2')).dx, greaterThan(two.dx));
  });

  testWidgets('full: 16 spots, and the count includes the ones waiting', (
    tester,
  ) async {
    final fixture = seenDemo().toJson();
    final base = (fixture['agents'] as List).cast<Map<String, dynamic>>();
    final agents = [
      for (var i = 0; i < 20; i++)
        {
          ...base[0],
          'id': 'a${i.toString().padLeft(5, '0')}',
          'slot': i,
          'title': 'agent-$i',
          'status': 'running',
          'attentionSlot': i < 16 ? 16 - i : 0,
        },
    ];
    await pump(
      tester,
      FakeAgentsSource.fromState({...fixture, 'agents': agents}),
    );
    expect(
      tester.widget<Text>(find.byKey(const Key('agents-attention-count'))).data,
      '20',
    );
    await swipeToAttention(tester);
    expect(
      find.descendant(of: screen, matching: find.byType(AgentTile)),
      findsNWidgets(16),
    );
    expect(tile('a00016'), findsNothing);
    expect(
      tester.getTopLeft(tile('a00015')).dy,
      lessThan(tester.getTopLeft(tile('a00000')).dy),
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets(
    'an older service sends no spots: the busy agents in deck order',
    (tester) async {
      final json = seenDemo().toJson();
      for (final a in (json['agents'] as List).cast<Map<String, dynamic>>()) {
        a.remove('attentionSlot');
      }
      await pump(tester, FakeAgentsSource.fromState(json));
      expect(
        tester
            .widget<Text>(find.byKey(const Key('agents-attention-count')))
            .data,
        '7',
      );
      await swipeToAttention(tester);
      final at = [for (final id in busy) tester.getTopLeft(tile(id))];
      for (var i = 1; i < at.length; i++) {
        final before = at[i - 1], here = at[i];
        expect(
          here.dy > before.dy || (here.dy == before.dy && here.dx > before.dx),
          isTrue,
          reason: busy[i],
        );
      }
    },
  );

  testWidgets('nobody working or asking: a plain empty screen, no count', (
    tester,
  ) async {
    final source = seenDemo();
    source.load(withSpots(source, {for (final id in busy) id: ('idle', 0)}));
    await pump(tester, source);
    expect(find.byKey(const Key('agents-attention-count')), findsNothing);
    expect(find.byKey(const Key('agents-page-attention')), findsOneWidget);
    await swipeToAttention(tester);
    expect(find.byKey(const Key('attention-spot-empty')), findsNWidgets(16));
  });
}
