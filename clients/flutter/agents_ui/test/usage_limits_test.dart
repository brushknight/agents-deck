import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'pets_page_test.dart' show loadPageFonts, petsPageState;

/// Usage limits on the wall panel: the header line, out tiles, the agent
/// page's usage row. Goldens:
///   flutter test --update-goldens test/usage_limits_test.dart
void main() {
  setUpAll(() async {
    TestWidgetsFlutterBinding.ensureInitialized();
    debugAgentsSetupHostname = 'panel';
    await loadPageFonts();
  });

  AgentsState state(String scenario) => AgentsState.fromJson(
    FakeAgentsSource.withLimits(petsPageState(), scenario),
  );

  group('the model', () {
    test('limits, the worst week and who is out', () {
      final s = state('out');
      expect(s.limits.map((l) => l.name), ['personal', 'work', 'codex']);
      final personal = s.limits.first;
      // The 5-hour window is kept but not shown; Fable's week shows once hit.
      expect(personal.windows.length, 3);
      expect(personal.shown.map(personal.labelOf), [
        'all models · week',
        'fable · week',
      ]);
      expect(s.limits.last.labelOf(s.limits.last.shown.single), 'week');
      final (l, w) = s.worstLimit!;
      expect((l.name, w.out), ('work', true));
      final byTitle = {for (final a in s.agents) a.title: a};
      expect(s.outUntil(byTitle['checkout-api']!), isNotNull);
      expect(s.outUntil(byTitle['k8s-nodes']!), isNotNull, reason: 'fable');
      expect(s.outUntil(byTitle['mobile-app']!), isNull);
      expect(s.limitsOf(byTitle['checkout-api']!)?.name, 'work');
      expect(s.limitsOf(byTitle['docs-site']!)?.name, 'codex');
    });

    test('an older service: no limits, nothing out', () {
      final s = AgentsState.fromJson(petsPageState());
      expect(s.limits, isEmpty);
      expect(s.worstLimit, isNull);
      expect(usageStatus(s), isNull);
    });

    test('the status line part', () {
      expect(usageStatus(state('calm')), (
        text: 'work week 41%',
        attention: false,
      ));
      expect(usageStatus(state('near')), (
        text: 'work week 86%',
        attention: true,
      ));
      final out = usageStatus(state('out'))!;
      expect(out.attention, isTrue);
      expect(out.text, startsWith('work out · '));
    });

    test('reset times read short', () {
      final now = DateTime(2026, 11, 24, 14, 12);
      expect(resetText(DateTime(2026, 11, 24, 16, 40), now), '16:40');
      expect(resetText(DateTime(2026, 11, 26, 9, 0), now), 'thu 09:00');
      expect(resetText(DateTime(2026, 12, 8, 9, 0), now), '8 dec');
    });
  });

  Future<void> pumpBoard(WidgetTester tester, String scenario) async {
    final source =
        FakeAgentsSource.fromState(
            FakeAgentsSource.withLimits(petsPageState(), scenario),
          )
          ..accountList.addAll(const [
            AgentAccount(name: 'work', color: 'yellow'),
            AgentAccount(name: 'personal', color: 'sky'),
          ]);
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

  Future<void> open(WidgetTester tester, String title) async {
    await tester.tap(find.text(title));
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump(const Duration(milliseconds: 400));
  }

  Future<void> golden(WidgetTester tester, String name) async {
    if (!autoUpdateGoldenFiles) return;
    await expectLater(
      find.byType(AgentsApp),
      matchesGoldenFile('goldens/usage-$name.png'),
    );
  }

  for (final scenario in ['calm', 'near', 'out']) {
    testWidgets('the panel, $scenario', (tester) async {
      await pumpBoard(tester, scenario);
      // The deck opens on its first page when nobody needs you; go there.
      if (find.byKey(const Key('agents-page-0')).evaluate().isNotEmpty) {
        await tester.tap(find.byKey(const Key('agents-page-0')));
        await tester.pump(const Duration(milliseconds: 400));
      }
      expect(find.byKey(const Key('usage-line')), findsOneWidget);
      expect(find.text('work'), findsOneWidget);
      if (scenario == 'out') {
        expect(find.text('fable out'), findsOneWidget);
        expect(find.textContaining('out · '), findsWidgets);
      } else {
        expect(find.textContaining('out · '), findsNothing);
      }
      await golden(tester, 'panel-$scenario');
    });

    testWidgets('an agent, $scenario', (tester) async {
      await pumpBoard(tester, scenario);
      if (find.byKey(const Key('agents-page-0')).evaluate().isNotEmpty) {
        await tester.tap(find.byKey(const Key('agents-page-0')));
        await tester.pump(const Duration(milliseconds: 400));
      }
      await open(tester, 'checkout-api');
      expect(find.byKey(const Key('agent-usage')), findsOneWidget);
      expect(find.text('all models · week'), findsOneWidget);
      if (scenario == 'out') {
        // The chip says until when, and the line under the rows says the
        // agent waits (the page has no "continue on" row any more).
        expect(find.textContaining('OUT · BACK'), findsOneWidget);
        expect(find.byKey(const Key('agent-usage-out')), findsOneWidget);
        expect(find.byKey(const Key('agent-handoff-note')), findsNothing);
      } else {
        expect(find.textContaining('OUT · BACK'), findsNothing);
      }
      await golden(tester, 'agent-$scenario');
    });
  }

  testWidgets('out with no other account: the line says until when', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(720, 720);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentsApp(
            source: FakeAgentsSource.fromState(
              FakeAgentsSource.withLimits(petsPageState(), 'out'),
            ),
            animate: false,
          ),
        ),
      ),
    );
    await tester.pump();
    if (find.byKey(const Key('agents-page-0')).evaluate().isNotEmpty) {
      await tester.tap(find.byKey(const Key('agents-page-0')));
      await tester.pump(const Duration(milliseconds: 400));
    }
    await open(tester, 'checkout-api');
    expect(find.byKey(const Key('agent-usage-out')), findsOneWidget);
    expect(find.textContaining('it waits until then'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('no limits: no usage line, no usage row', (tester) async {
    tester.view.physicalSize = const Size(720, 720);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentsApp(
            source: FakeAgentsSource.fromState(petsPageState()),
            animate: false,
          ),
        ),
      ),
    );
    await tester.pump();
    expect(find.byKey(const Key('usage-line')), findsNothing);
  });
}
