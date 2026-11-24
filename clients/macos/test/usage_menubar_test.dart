import 'dart:io';

import 'package:agents_deck_menubar/main.dart';
import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

import 'status_line_test.dart' show FakeDeckService;

/// Usage limits in the menu bar: the closest account to running out in the
/// status line, and the usage row on an agent's page. Goldens:
///   flutter test --update-goldens test/usage_menubar_test.dart
void main() {
  setUpAll(() async {
    for (final (family, file) in [
      ('RobotoMono', 'RobotoMono.ttf'),
      ('ShareTechMono', 'ShareTechMono-Regular.ttf'),
      ('InstrumentSerif', 'InstrumentSerif-Regular.ttf'),
    ]) {
      final data = File('../flutter/agents_ui/fonts/$file').readAsBytesSync();
      await (FontLoader(
        'packages/agents_deck_ui/$family',
      )..addFont(Future.value(ByteData.view(data.buffer)))).load();
    }
  });

  Future<void> pump(WidgetTester tester, String scenario) async {
    tester.view.physicalSize = const Size(880, 880 + 2 * 32);
    tester.view.devicePixelRatio = 2;
    addTearDown(tester.view.reset);
    final source =
        FakeAgentsSource.fromState(
            FakeAgentsSource.withLimits(
              FakeAgentsSource.demo(pets: true).toJson(),
              scenario,
            ),
          )
          ..accountList.addAll(const [
            AgentAccount(name: 'work', color: 'yellow'),
            AgentAccount(name: 'personal', color: 'sky'),
          ]);
    await tester.pumpWidget(
      MenuBarDeck(
        source: source,
        visible: ValueNotifier(true),
        service: FakeDeckService(
          const ServiceStatus(
            ServiceState.running,
            system: DeckSystem(boardsOnline: 3, boardsTotal: 3),
          ),
        ),
      ),
    );
    await tester.pump();
  }

  Future<void> golden(WidgetTester tester, String name) async {
    if (!autoUpdateGoldenFiles) return;
    await expectLater(
      find.byType(MenuBarDeck),
      matchesGoldenFile('goldens/menubar-usage-$name.png'),
    );
  }

  for (final (scenario, text) in [
    ('calm', 'work week 41%'),
    ('near', 'work week 86%'),
  ]) {
    testWidgets('the status line, $scenario', (tester) async {
      await pump(tester, scenario);
      expect(find.text('· $text'), findsOneWidget);
      // No usage line on the deck itself: the status line says it.
      expect(find.byKey(const Key('usage-line')), findsNothing);
      await golden(tester, scenario);
    });
  }

  testWidgets('the status line and an agent, out', (tester) async {
    await pump(tester, 'out');
    expect(find.textContaining('· work out · '), findsOneWidget);
    await golden(tester, 'out');
    await tester.tap(find.text('checkout-api'));
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump(const Duration(milliseconds: 400));
    expect(find.byKey(const Key('agent-usage')), findsOneWidget);
    expect(find.textContaining('OUT · BACK'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await golden(tester, 'agent-out');
  });
}
