import 'dart:io';

import 'package:agents_deck_menubar/main.dart';
import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

/// The smallest font size in [span], each span inheriting its parent's.
double _smallest(InlineSpan span, [double inherited = 14]) {
  var min = double.infinity;
  void walk(InlineSpan s, double size) {
    final here = s.style?.fontSize ?? size;
    if (s is TextSpan) {
      if ((s.text ?? '').trim().isNotEmpty && here < min) min = here;
      for (final c in s.children ?? const <InlineSpan>[]) {
        walk(c, here);
      }
    }
  }

  walk(span, inherited);
  return min;
}

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

  Future<void> pumpDeck(WidgetTester tester) async {
    tester.view.physicalSize = const Size(880, 880); // 440 pt, Retina
    tester.view.devicePixelRatio = 2;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MenuBarDeck(
        source: FakeAgentsSource.demo(pets: true),
        visible: ValueNotifier(true),
      ),
    );
    await tester.pump();
  }

  Future<void> open(WidgetTester tester, String title) async {
    await tester.tap(find.text(title));
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump(const Duration(milliseconds: 400));
  }

  final card = find.byKey(const Key('agent-pets-card'));

  void checkMenuBarRules(WidgetTester tester, String what) {
    expect(card, findsOneWidget, reason: what);
    // No clock in the menu bar.
    expect(
      find.descendant(of: card, matching: find.byType(HudClock)),
      findsNothing,
      reason: what,
    );
    // No text under 16 design pixels (the card is shown at 0.61).
    for (final t in tester.widgetList<RichText>(
      find.descendant(of: card, matching: find.byType(RichText)),
    )) {
      expect(
        _smallest(t.text),
        greaterThanOrEqualTo(16),
        reason: '$what: "${t.text.toPlainText()}"',
      );
    }
  }

  for (final (title, name) in [
    ('checkout-api', 'running'),
    ('infra-network', 'waiting'),
    ('search-index', 'idle'),
    ('api-tests', 'egg'),
    ('docs-site', 'ready'),
    ('auth-flow', 'question'),
  ]) {
    testWidgets('$name: the card at 0.61, a back button, no clock', (
      tester,
    ) async {
      await pumpDeck(tester);
      await open(tester, title);
      checkMenuBarRules(tester, name);
      expect(find.byKey(const Key('agent-back')), findsOneWidget);
      expect(tester.takeException(), isNull);
      if (autoUpdateGoldenFiles) {
        await expectLater(
          find.byType(MenuBarDeck),
          matchesGoldenFile('goldens/menubar-pets-$name.png'),
        );
      }
    });
  }

  testWidgets('the pet\'s page in the menu bar', (tester) async {
    await pumpDeck(tester);
    await open(tester, 'checkout-api');
    await tester.tap(find.byKey(const Key('agent-pet')));
    await tester.pump();
    // Under its agent's name, with its growth path.
    expect(find.byKey(const Key('pet-name')), findsOneWidget);
    expect(find.byKey(const Key('pet-path')), findsOneWidget);
    checkMenuBarRules(tester, 'pet');
    expect(tester.takeException(), isNull);
    if (autoUpdateGoldenFiles) {
      await expectLater(
        find.byType(MenuBarDeck),
        matchesGoldenFile('goldens/menubar-pets-pet.png'),
      );
    }
  });
}
