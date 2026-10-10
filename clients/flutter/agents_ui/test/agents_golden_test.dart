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
