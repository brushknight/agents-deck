import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter_test/flutter_test.dart';

/// Agents board screens at the panel's exact 720x720 with the real fonts and
/// frozen critters (layout review, like home_golden_test.dart):
///   flutter test --update-goldens test/agents_golden_test.dart
void main() {
  setUpAll(() async {
    TestWidgetsFlutterBinding.ensureInitialized();
    // Never bake the developer machine's hostname into the PNGs.
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

  Future<void> pumpBoard(WidgetTester tester, FakeAgentsSource source) async {
    tester.view.physicalSize = const Size(720, 720);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(
        debugShowCheckedModeBanner: false,
        theme: ThemeData.dark().copyWith(scaffoldBackgroundColor: DeckHud.bg),
        home: Scaffold(body: AgentsApp(source: source, animate: false)),
      ),
    );
    await tester.pump();
  }

  testWidgets('agents grid golden', skip: !autoUpdateGoldenFiles, (
    tester,
  ) async {
    await pumpBoard(tester, FakeAgentsSource.demo());
    await expectLater(
      find.byType(AgentsApp),
      matchesGoldenFile('goldens/agents-grid.png'),
    );
  });

  testWidgets('agents hat picker golden', skip: !autoUpdateGoldenFiles, (
    tester,
  ) async {
    await pumpBoard(tester, FakeAgentsSource.demo());
    await tester.tap(find.text('checkout-api'));
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump(const Duration(milliseconds: 400));
    await tester.tap(find.byKey(const Key('agent-hat')));
    await tester.pump();
    await expectLater(
      find.byType(AgentsApp),
      matchesGoldenFile('goldens/agents-hats.png'),
    );
  });

  testWidgets('agents paged golden', skip: !autoUpdateGoldenFiles, (
    tester,
  ) async {
    final fixture = FakeAgentsSource.demo().toJson();
    final base = (fixture['agents'] as List).cast<Map<String, dynamic>>();
    await pumpBoard(
      tester,
      FakeAgentsSource.fromState({
        ...fixture,
        'agents': [
          for (var i = 0; i < 40; i++)
            {
              ...base[i % base.length],
              'id': 'a${i.toString().padLeft(5, '0')}',
              'slot': i,
              'attentionSlot': 0, // given again, as the service would
            },
        ],
      }, attention: true),
    );
    await expectLater(
      find.byType(AgentsApp),
      matchesGoldenFile('goldens/agents-paged.png'),
    );
  });

  for (final (title, golden) in [
    ('checkout-api', 'agents-detail-running'),
    ('infra-network', 'agents-permission'),
    ('auth-flow', 'agents-question'),
  ]) {
    testWidgets('$golden golden', skip: !autoUpdateGoldenFiles, (tester) async {
      await pumpBoard(tester, FakeAgentsSource.demo());
      await tester.tap(find.text(title));
      await tester.pump(const Duration(milliseconds: 100));
      await tester.pump(const Duration(milliseconds: 300));
      await expectLater(
        find.byType(AgentsApp),
        matchesGoldenFile('goldens/$golden.png'),
      );
    });
  }

  // Two Claude accounts: checkout-api runs in "work" (the tile's square,
  // the card's account and "continue on personal").
  FakeAgentsSource twoAccounts() {
    final fixture = FakeAgentsSource.demo().toJson();
    final first = (fixture['agents'] as List).first as Map<String, dynamic>;
    first['account'] = {'name': 'work', 'color': 'yellow'};
    return FakeAgentsSource.fromState(fixture)
      ..accountList.addAll(const [
        AgentAccount(name: 'personal', color: 'teal', isDefault: true),
        AgentAccount(name: 'work', color: 'yellow'),
      ]);
  }

  testWidgets('agents accounts grid golden', skip: !autoUpdateGoldenFiles, (
    tester,
  ) async {
    await pumpBoard(tester, twoAccounts());
    await expectLater(
      find.byType(AgentsApp),
      matchesGoldenFile('goldens/agents-accounts-grid.png'),
    );
  });

  testWidgets('agents accounts detail golden', skip: !autoUpdateGoldenFiles, (
    tester,
  ) async {
    await pumpBoard(tester, twoAccounts());
    await tester.tap(find.text('checkout-api'));
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump(const Duration(milliseconds: 300));
    await expectLater(
      find.byType(AgentsApp),
      matchesGoldenFile('goldens/agents-accounts-detail.png'),
    );
  });

  // The "Needs you" screen, one step left of the first page: the demo
  // fleet's busy agents after api-tests finished and left its spot empty.
  testWidgets('agents attention golden', skip: !autoUpdateGoldenFiles, (
    tester,
  ) async {
    final source = FakeAgentsSource.demo();
    await pumpBoard(tester, source);
    final json = source.toJson();
    for (final a in (json['agents'] as List).cast<Map<String, dynamic>>()) {
      if (a['id'] == 't3st5a') a['status'] = 'idle';
    }
    source.load(json); // the demo, like the service, frees its spot
    await tester.pump(const Duration(seconds: 5));
    await expectLater(
      find.byType(AgentsApp),
      matchesGoldenFile('goldens/agents-deck-count.png'),
    );
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowLeft);
    await tester.pumpAndSettle();
    await expectLater(
      find.byType(AgentsApp),
      matchesGoldenFile('goldens/agents-attention.png'),
    );
  });

  testWidgets('agents attention empty golden', skip: !autoUpdateGoldenFiles, (
    tester,
  ) async {
    final json = FakeAgentsSource.demo().toJson();
    for (final a in (json['agents'] as List).cast<Map<String, dynamic>>()) {
      if (a['status'] != 'error') a['status'] = 'idle';
      a['waiting'] = null;
      a['attentionSlot'] = 0; // given again, as the service would
    }
    await pumpBoard(tester, FakeAgentsSource.fromState(json, attention: true));
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowLeft);
    await tester.pumpAndSettle();
    await expectLater(
      find.byType(AgentsApp),
      matchesGoldenFile('goldens/agents-attention-empty.png'),
    );
  });

  testWidgets('agents attention full golden', skip: !autoUpdateGoldenFiles, (
    tester,
  ) async {
    final fixture = FakeAgentsSource.demo().toJson();
    final base = (fixture['agents'] as List).cast<Map<String, dynamic>>();
    final busy = [
      for (final a in base)
        if (a['status'] == 'running' || a['status'] == 'waiting') a,
    ];
    await pumpBoard(
      tester,
      FakeAgentsSource.fromState({
        ...fixture,
        'agents': [
          for (var i = 0; i < 19; i++)
            {
              ...busy[i % busy.length],
              'id': 'a${i.toString().padLeft(5, '0')}',
              'slot': i,
              'attentionSlot': 0, // given again, as the service would
            },
        ],
      }, attention: true),
    );
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowLeft);
    await tester.pumpAndSettle();
    await expectLater(
      find.byType(AgentsApp),
      matchesGoldenFile('goldens/agents-attention-full.png'),
    );
  });

  testWidgets('agents unpaired golden', skip: !autoUpdateGoldenFiles, (
    tester,
  ) async {
    final source = FakeAgentsSource.demo()
      ..setLink(AgentsLink.unconfigured, keepState: false);
    await pumpBoard(tester, source);
    await expectLater(
      find.byType(AgentsApp),
      matchesGoldenFile('goldens/agents-unpaired.png'),
    );
  });
}
