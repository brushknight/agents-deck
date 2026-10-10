import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter_test/flutter_test.dart';

Future<void> _pumpApp(WidgetTester tester, AgentsSource source) async {
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

void _expectCall(
  (String, String, Map<String, dynamic>?) call,
  String verb,
  String agentId,
  Map<String, dynamic>? body,
) {
  expect(call.$1, verb);
  expect(call.$2, agentId);
  expect(call.$3, body);
}

/// Tap a tile and let the 170 ms morph land.
Future<void> _open(WidgetTester tester, String title) async {
  await tester.tap(find.text(title));
  await tester.pump(const Duration(milliseconds: 100));
  await tester.pump(const Duration(milliseconds: 300));
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

  testWidgets('grid: 11 fixture agents by slot + 5 empty slots, no overflow', (
    tester,
  ) async {
    await _pumpApp(tester, FakeAgentsSource.demo());
    expect(find.byType(AgentTile), findsNWidgets(11));
    expect(find.byType(AgentsEmptyTile), findsNWidgets(5));
    // Slot order: checkout-api first, billing-jobs last.
    final xs = [
      for (final t in [
        'checkout-api',
        'infra-network',
        'docs-site',
        'api-tests',
      ])
        tester.getCenter(find.text(t)).dx,
    ];
    expect(xs, orderedEquals([...xs]..sort()));
    expect(
      tester.getCenter(find.text('billing-jobs')).dy,
      greaterThan(tester.getCenter(find.text('checkout-api')).dy),
    );
    // Meta lines per status.
    expect(find.text('edit · 41% ctx'), findsOneWidget);
    expect(find.text('allow bash?'), findsOneWidget);
    expect(find.text('pick 1 of 3'), findsOneWidget);
    expect(find.text('hungry · feed me'), findsOneWidget);
    expect(find.text('done · 1h ago'), findsOneWidget);
    expect(find.text('api 529 overloaded · retrying'), findsOneWidget);
    expect(find.text('plan · 27% ctx'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('tap opens the running agent card; esc closes it', (
    tester,
  ) async {
    await _pumpApp(tester, FakeAgentsSource.demo());
    await _open(tester, 'checkout-api');

    expect(find.text('doing now'), findsOneWidget);
    expect(find.text('running · 38m'), findsOneWidget);
    expect(find.text('410k / 1m'), findsOneWidget);
    expect(find.text('41%'), findsOneWidget);
    expect(find.text('84k'), findsOneWidget);
    expect(find.text('3.1m'), findsOneWidget);
    expect(find.text('\$4.12'), findsOneWidget);
    expect(find.text('opus 5.5 · ~/dev/checkout-api · main'), findsOneWidget);
    expect(find.text('workstation'), findsOneWidget);
    expect(find.byKey(const Key('agent-interrupt')), findsOneWidget);
    expect(tester.takeException(), isNull);

    await tester.sendKeyEvent(LogicalKeyboardKey.escape);
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.text('doing now'), findsNothing);
  });

  testWidgets('no back button on the panel (it closes cards by swiping)', (
    tester,
  ) async {
    await _pumpApp(tester, FakeAgentsSource.demo());
    await _open(tester, 'checkout-api');
    expect(find.text('doing now'), findsOneWidget);
    expect(find.byKey(const Key('agent-back')), findsNothing);
  });

  testWidgets('the back button closes the card when asked for', (tester) async {
    tester.view.physicalSize = const Size(720, 720);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentsApp(
            source: FakeAgentsSource.demo(),
            animate: false,
            backButton: true,
          ),
        ),
      ),
    );
    await tester.pump();
    await _open(tester, 'checkout-api');
    expect(find.text('doing now'), findsOneWidget);
    await tester.tap(find.byKey(const Key('agent-back')));
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.text('doing now'), findsNothing);
  });

  testWidgets('a free cell adds a hand-started session into that cell', (
    tester,
  ) async {
    final source = FakeAgentsSource.demo();
    await _pumpApp(tester, source);
    // The demo fleet leaves slot 9 free (row 3, second cell).
    await tester.tap(find.byType(AgentsEmptyTile).first);
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump(const Duration(milliseconds: 400));
    expect(find.byKey(const Key('live-picker')), findsOneWidget);
    expect(find.text('notes cleanup'), findsOneWidget);
    await tester.tap(find.text('notes cleanup'));
    await tester.pump();
    final call = source.calls.lastWhere((c) => c.$1 == 'addLive');
    expect(call.$2, '7a1c9e20-5d3b-4f6a-9b2e-1c4d5e6f7a8b');
    expect(call.$3, {'slot': 9});
    await tester.pump(const Duration(milliseconds: 400));
    expect(find.byKey(const Key('live-picker')), findsNothing);
    // It's on the deck now, watched: no answering, a "remove from deck".
    expect(find.text('notes cleanup'), findsOneWidget);
    await _open(tester, 'notes cleanup');
    expect(find.text('remove from deck'), findsOneWidget);
    expect(find.byKey(const Key('agent-interrupt')), findsNothing);
  });

  testWidgets('tapping the mascot picks a hat for its folder', (tester) async {
    final source = FakeAgentsSource.demo();
    await _pumpApp(tester, source);
    await _open(tester, 'checkout-api');
    await tester.tap(find.byKey(const Key('agent-hat')));
    await tester.pump();
    expect(find.byKey(const Key('hat-picker')), findsOneWidget);
    await tester.tap(find.byKey(const Key('hat-shape-cowboy')));
    await tester.pump();
    var call = source.calls.lastWhere((c) => c.$1 == 'hat');
    expect(call.$2, 'k3f9a2');
    expect(call.$3, {'shape': 'cowboy', 'color': 'blue'});
    await tester.tap(find.byKey(const Key('hat-color-teal')));
    await tester.pump();
    call = source.calls.lastWhere((c) => c.$1 == 'hat');
    expect(call.$3, {'shape': 'cowboy', 'color': 'teal'});
    // Same folder (api-tests, auth-flow): same hat.
    final hats = {
      for (final a in source.snapshot.value.state!.agents)
        if (a.cwd == '/Users/sam/dev/checkout-api') a.hat,
    };
    expect(hats, {const AgentHat(shape: 'cowboy', color: 'teal', auto: false)});
    await tester.tap(find.byKey(const Key('hat-auto')));
    await tester.pump();
    expect(source.calls.last.$3, {'auto': true});
    expect(
      source.snapshot.value.state!.byId('k3f9a2')!.hat,
      const AgentHat(shape: 'propeller', color: 'blue'),
    );
    await tester.tap(find.byKey(const Key('hat-done')));
    await tester.pump();
    expect(find.byKey(const Key('hat-picker')), findsNothing);
    expect(find.text('doing now'), findsOneWidget);
  });

  testWidgets('idle and error cards: no interrupt, error message shown', (
    tester,
  ) async {
    await _pumpApp(tester, FakeAgentsSource.demo());
    await _open(tester, 'docs-site');
    expect(find.text('hungry · 12m'), findsOneWidget);
    expect(find.text('rewrite install guide'), findsOneWidget);
    expect(find.byKey(const Key('agent-interrupt')), findsNothing);
    expect(find.byKey(const Key('agent-focus')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('permission: an option answers at once, "sent", back to grid', (
    tester,
  ) async {
    final source = FakeAgentsSource.demo();
    await _pumpApp(tester, source);
    await _open(tester, 'infra-network');

    expect(find.text('run this command?'), findsOneWidget);
    expect(
      find.text('terraform apply -target=module.staging_net'),
      findsOneWidget,
    );
    expect(find.text('bash · ~/dev/infra-network/terraform'), findsOneWidget);
    expect(find.text('needs you · 2m'), findsOneWidget);
    expect(tester.takeException(), isNull);

    await tester.tap(find.byKey(const Key('agent-option-2')));
    await tester.pump();
    expect(source.calls, hasLength(1));
    _expectCall(source.calls.single, 'answer', 'h0m3l4', {
      'promptId': 'p17',
      'key': '2',
    });
    // Optimistic feedback: the pressed option reads "sent" while the card
    // holds the answered prompt (the fleet has moved on already).
    expect(find.text('2  sent'), findsOneWidget);
    // A second tap during the hold does nothing.
    await tester.tap(find.byKey(const Key('agent-option-1')));
    await tester.pump();
    expect(source.calls, hasLength(1));

    await tester.pump(const Duration(milliseconds: 600)); // hold -> close
    await tester.pump(const Duration(milliseconds: 300)); // morph back
    expect(find.text('run this command?'), findsNothing);
    expect(find.byType(AgentTile), findsNWidgets(11));
    // The tile followed the answer: infra-network is working again.
    expect(find.text('allow bash?'), findsNothing);
  });

  testWidgets('permission: 409 says "prompt changed", refreshes, re-enables', (
    tester,
  ) async {
    final source = FakeAgentsSource.demo()
      ..respond = (verb, id) =>
          const AgentsResult(AgentsResultKind.conflict, 'prompt changed');
    await _pumpApp(tester, source);
    await _open(tester, 'infra-network');

    await tester.tap(find.byKey(const Key('agent-option-1')));
    await tester.pump();
    expect(find.byKey(const Key('agent-note')), findsOneWidget);
    expect(find.text('prompt changed'), findsOneWidget);
    expect(source.refreshes, 1);
    // Still on the card, buttons live again.
    await tester.tap(find.byKey(const Key('agent-option-3')));
    await tester.pump();
    expect(source.calls, hasLength(2));
    expect(source.calls.last.$3, {'promptId': 'p17', 'key': '3'});
    await tester.pump(const Duration(seconds: 1));
  });

  testWidgets('permission: a failed send flashes and shows the reason', (
    tester,
  ) async {
    final source = FakeAgentsSource.demo()
      ..respond = (verb, id) =>
          const AgentsResult(AgentsResultKind.failed, 'timed out');
    await _pumpApp(tester, source);
    await _open(tester, 'infra-network');
    await tester.tap(find.byKey(const Key('agent-option-1')));
    await tester.pump();
    expect(find.text('timed out'), findsOneWidget);
    expect(
      find.text('run this command?'),
      findsOneWidget,
      reason: 'stays open',
    );
    await tester.pump(const Duration(seconds: 1));
  });

  testWidgets('question: primary preselected, pick another, send it', (
    tester,
  ) async {
    final source = FakeAgentsSource.demo();
    await _pumpApp(tester, source);
    await _open(tester, 'auth-flow');

    expect(
      find.text('which session strategy should the login flow use?'),
      findsOneWidget,
    );
    expect(find.text('rotating refresh tokens'), findsOneWidget);
    expect(
      find.text('short access token, refresh rotates on use'),
      findsOneWidget,
    );
    expect(find.text('send 1'), findsOneWidget);
    expect(tester.takeException(), isNull);

    await tester.tap(find.byKey(const Key('agent-option-3')));
    await tester.pump();
    expect(source.calls, isEmpty, reason: 'selecting does not send');
    expect(find.text('send 3'), findsOneWidget);

    await tester.tap(find.byKey(const Key('agent-send')));
    await tester.pump();
    expect(source.calls, hasLength(1));
    _expectCall(source.calls.single, 'answer', '4uthfl', {
      'promptId': 'p18',
      'key': '3',
    });
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.text('send 3'), findsNothing);
  });

  testWidgets('focus and interrupt post to the agent', (tester) async {
    final source = FakeAgentsSource.demo();
    await _pumpApp(tester, source);
    await _open(tester, 'checkout-api');
    await tester.tap(find.byKey(const Key('agent-focus')));
    await tester.pump();
    _expectCall(source.calls.last, 'focus', 'k3f9a2', null);
    expect(find.text('focused'), findsOneWidget);
    await tester.pump(const Duration(milliseconds: 500));

    await tester.tap(find.byKey(const Key('agent-interrupt')));
    await tester.pump();
    _expectCall(source.calls.last, 'interrupt', 'k3f9a2', null);
    // Live: the agent went idle, the card follows (no interrupt any more).
    expect(find.byKey(const Key('agent-interrupt')), findsNothing);
    await tester.pump(const Duration(milliseconds: 500));
  });

  testWidgets('stop needs a second tap on a live agent, then removes it', (
    tester,
  ) async {
    final source = FakeAgentsSource.demo();
    await _pumpApp(tester, source);
    await _open(tester, 'docs-site'); // idle
    expect(find.byKey(const Key('agent-interrupt')), findsNothing);
    await tester.tap(find.byKey(const Key('agent-remove')));
    await tester.pump();
    expect(find.text('tap again to stop'), findsOneWidget);
    expect(source.calls.where((c) => c.$1 == 'dismiss'), isEmpty);

    // The confirmation lapses on its own.
    await tester.pump(const Duration(seconds: 4));
    expect(find.text('stop'), findsOneWidget);

    await tester.tap(find.byKey(const Key('agent-remove')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('agent-remove')));
    await tester.pump();
    _expectCall(source.calls.last, 'dismiss', 'd0cs1t', null);
    await tester.pump(const Duration(seconds: 1));
    await tester.pump(
      const Duration(milliseconds: 300),
    ); // the card's close animation
    expect(find.text('docs-site'), findsNothing); // back on the grid, tile gone
  });

  testWidgets('an exited agent is removed with one tap and has no focus', (
    tester,
  ) async {
    final source = FakeAgentsSource.demo();
    final json = source.toJson();
    (json['agents'] as List).cast<Map<String, dynamic>>().firstWhere(
      (a) => a['id'] == 'h4ptic',
    )['status'] = 'exited';
    source.load(json);
    await _pumpApp(tester, source);
    await _open(tester, 'search-index');
    expect(find.byKey(const Key('agent-focus')), findsNothing);
    expect(find.text('remove'), findsOneWidget);
    await tester.tap(find.byKey(const Key('agent-remove')));
    await tester.pump();
    _expectCall(source.calls.last, 'dismiss', 'h4ptic', null);
    await tester.pump(const Duration(seconds: 1));
    await tester.pump(
      const Duration(milliseconds: 300),
    ); // the card's close animation
    expect(find.text('search-index'), findsNothing);
  });

  testWidgets('a resumable exited agent offers resume next to remove', (
    tester,
  ) async {
    final source = FakeAgentsSource.demo();
    final json = source.toJson();
    final sk = (json['agents'] as List).cast<Map<String, dynamic>>().firstWhere(
      (a) => a['id'] == 'h4ptic',
    );
    sk['status'] = 'exited';
    sk['resumable'] = true;
    source.load(json);
    await _pumpApp(tester, source);
    expect(find.text('ended · resumable'), findsOneWidget);
    await _open(tester, 'search-index');
    expect(find.byKey(const Key('agent-remove')), findsOneWidget);
    await tester.tap(find.byKey(const Key('agent-resume')));
    await tester.pump();
    _expectCall(source.calls.last, 'resume', 'h4ptic', null);
    // Live: the agent came back (idle), so the card swaps to stop/focus.
    expect(find.byKey(const Key('agent-resume')), findsNothing);
    expect(find.byKey(const Key('agent-focus')), findsOneWidget);
    await tester.pump(const Duration(milliseconds: 500));
  });

  testWidgets('long-press drag onto another tile swaps the two agents', (
    tester,
  ) async {
    final source = FakeAgentsSource.demo();
    await _pumpApp(tester, source);
    final from = tester.getCenter(find.text('infra-network'));
    final to = tester.getCenter(find.text('checkout-api'));
    final g = await tester.startGesture(from);
    await tester.pump(const Duration(milliseconds: 500)); // long-press delay
    await g.moveTo(to);
    await tester.pump();
    await g.up();
    await tester.pumpAndSettle(const Duration(milliseconds: 100));
    final call = source.calls.lastWhere((c) => c.$1 == 'move');
    expect((call.$2, call.$3?['slot']), ('h0m3l4', 0)); // onto slot 0
    // The grid re-renders in the new order: infra-network is now the first tile.
    expect(
      tester.getCenter(find.text('infra-network')).dx,
      lessThan(tester.getCenter(find.text('checkout-api')).dx),
    );
  });

  testWidgets('dropping on a free cell moves the agent there, leaving a gap', (
    tester,
  ) async {
    final source = FakeAgentsSource.demo();
    await _pumpApp(tester, source);
    final from = tester.getCenter(find.text('infra-network'));
    final empty = tester.getCenter(find.byType(AgentsEmptyTile).first);
    final g = await tester.startGesture(from);
    await tester.pump(const Duration(milliseconds: 500));
    await g.moveTo(empty);
    await tester.pump();
    await g.up();
    await tester.pumpAndSettle(const Duration(milliseconds: 100));
    final call = source.calls.lastWhere((c) => c.$1 == 'move');
    expect(call.$2, 'h0m3l4');
    // infra-network now sits in that free cell; its old cell is empty.
    expect(
      tester.getCenter(find.byKey(const ValueKey('agent-tile-h0m3l4'))),
      empty,
    );
    expect(
      tester.getCenter(find.byType(AgentsEmptyTile).first).dy,
      lessThan(empty.dy),
    );
  });

  testWidgets('subagents at work show as a corner critter on the tile', (
    tester,
  ) async {
    await _pumpApp(tester, FakeAgentsSource.demo());
    // Only checkout-api has subagents (three) in the demo fleet.
    expect(find.byKey(const Key('agent-tile-subagents')), findsOneWidget);
    expect(find.text('×3'), findsOneWidget);
  });

  testWidgets('a quick tap still opens the agent (no drag)', (tester) async {
    final source = FakeAgentsSource.demo();
    await _pumpApp(tester, source);
    await _open(tester, 'checkout-api');
    expect(find.byKey(const Key('agent-focus')), findsOneWidget);
    expect(source.calls.where((c) => c.$1 == 'move'), isEmpty);
  });

  testWidgets('a finished, unreviewed agent is hungry until it is focused', (
    tester,
  ) async {
    final source =
        FakeAgentsSource.demo(); // docs-site is unseen in the fixture
    await _pumpApp(tester, source);
    expect(find.text('hungry · feed me'), findsOneWidget);
    expect(find.byType(HungryStripes), findsOneWidget);
    await _open(tester, 'docs-site');
    expect(find.text('review in terminal'), findsOneWidget);
    // The daemon clears it once its terminal has been looked at.
    final json = source.toJson();
    (json['agents'] as List).cast<Map<String, dynamic>>().firstWhere(
      (a) => a['id'] == 'd0cs1t',
    )['unseen'] = false;
    source.load(json);
    await tester.pump();
    expect(find.text('review in terminal'), findsNothing);
    await tester.pump(const Duration(milliseconds: 500));
  });

  testWidgets('card follows the live state; a vanished agent closes it', (
    tester,
  ) async {
    final source = FakeAgentsSource.demo();
    await _pumpApp(tester, source);
    await _open(tester, 'docs-site');
    expect(find.text('hungry · 12m'), findsOneWidget);

    // A permission prompt appears on the open agent: layout swaps in place.
    final json = source.toJson();
    final docs = (json['agents'] as List)
        .cast<Map<String, dynamic>>()
        .firstWhere((a) => a['id'] == 'd0cs1t');
    docs['status'] = 'waiting';
    docs['waiting'] = {
      'id': 'p99',
      'kind': 'permission',
      'title': 'edit this file?',
      'detail': 'docs/install.md',
      'context': 'edit · ~/dev/docs-site',
      'options': [
        {'key': '1', 'label': 'yes', 'primary': true},
        {'key': '2', 'label': 'no'},
      ],
    };
    source.load(json);
    await tester.pump();
    expect(find.text('edit this file?'), findsOneWidget);

    (json['agents'] as List).removeWhere((a) => (a as Map)['id'] == 'd0cs1t');
    source.load(json);
    await tester.pump();
    expect(find.text('agent gone'), findsOneWidget);
    await tester.pump(const Duration(milliseconds: 1500));
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.text('agent gone'), findsNothing);
    expect(find.byType(AgentTile), findsNWidgets(10));
  });

  testWidgets('not paired: full-screen pairing card', (tester) async {
    final source = FakeAgentsSource.demo()
      ..setLink(AgentsLink.unconfigured, keepState: false);
    await _pumpApp(tester, source);
    expect(find.byKey(const Key('agents-pairing')), findsOneWidget);
    expect(find.text('pair with your mac'), findsOneWidget);
    expect(
      find.textContaining('agentctl pair', findRichText: true),
      findsWidgets,
    );
    expect(
      find.textContaining('https://panel.local:8443', findRichText: true),
      findsOneWidget,
    );
    expect(find.byType(AgentTile), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('offline: last fleet dimmed + "offline · retrying" chip', (
    tester,
  ) async {
    final source = FakeAgentsSource.demo()
      ..setLink(AgentsLink.offline, problem: 'connection refused');
    await _pumpApp(tester, source);
    expect(find.byType(AgentTile), findsNWidgets(11));
    expect(find.byKey(const Key('agents-offline')), findsOneWidget);
    expect(
      find.text('offline · retrying · connection refused'),
      findsOneWidget,
    );

    // Never connected yet: no fleet to show, just the notice.
    source.setLink(
      AgentsLink.offline,
      problem: 'certificate mismatch',
      keepState: false,
    );
    await tester.pump();
    expect(find.byType(AgentTile), findsNothing);
    expect(find.text('offline · retrying'), findsOneWidget);
    expect(find.text('certificate mismatch'), findsOneWidget);

    source.setLink(AgentsLink.connecting, keepState: false);
    await tester.pump();
    expect(find.text('connecting'), findsOneWidget);
  });

  testWidgets('connected with no agents: friendly empty state', (tester) async {
    final source = FakeAgentsSource.fromState({
      'server': {'name': 'workstation', 'version': '0.1.0'},
      'agents': <Object>[],
    });
    await _pumpApp(tester, source);
    expect(find.text('no agents running'), findsOneWidget);
    expect(find.text('agentctl new'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('more than 16 agents: pages of 16 with bars underneath', (
    tester,
  ) async {
    final fixture = FakeAgentsSource.demo().toJson();
    final base = (fixture['agents'] as List).cast<Map<String, dynamic>>();
    final agents = [
      for (var i = 0; i < 20; i++)
        {
          ...base[i % base.length],
          'id': 'a${i.toString().padLeft(5, '0')}',
          'slot': i,
          'title': 'agent-$i',
        },
    ];
    final source = FakeAgentsSource.fromState({...fixture, 'agents': agents});
    await _pumpApp(tester, source);
    expect(find.byType(AgentTile), findsNWidgets(16));
    expect(find.text('agent-15'), findsOneWidget);
    expect(find.text('agent-16'), findsNothing);
    expect(find.byKey(const Key('agents-page-0')), findsOneWidget);
    expect(find.byKey(const Key('agents-page-1')), findsOneWidget);

    // A bar takes you to its page; so do the arrow keys and a swipe.
    await tester.tap(find.byKey(const Key('agents-page-1')));
    await tester.pumpAndSettle();
    expect(find.text('agent-16'), findsOneWidget);
    expect(find.text('agent-0'), findsNothing);
    expect(find.byType(AgentsEmptyTile), findsNWidgets(12));
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowLeft);
    await tester.pumpAndSettle();
    expect(find.text('agent-0'), findsOneWidget);
    await tester.fling(
      find.byKey(const Key('agents-pages')),
      const Offset(-500, 0),
      1500,
    );
    await tester.pumpAndSettle();
    expect(find.text('agent-16'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('16 agents or fewer: one page, no bars', (tester) async {
    await _pumpApp(tester, FakeAgentsSource.demo());
    expect(find.byKey(const Key('agents-pager')), findsNothing);
  });

  test('fixture copy under test/fixtures matches the demo fleet ids', () {
    final j = jsonDecode(
      File('test/fixtures/agents_state.json').readAsStringSync(),
    );
    final ids = [for (final a in (j['agents'] as List)) (a as Map)['id']];
    final demo = FakeAgentsSource.demo().snapshot.value.state!.agents.map(
      (a) => a.id,
    );
    expect(demo, ids);
  });
}
