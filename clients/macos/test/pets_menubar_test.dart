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

/// A box's rect on screen, in device pixels (2 per point here).
Rect _corners(RenderBox b) {
  final a = b.localToGlobal(Offset.zero) * 2;
  final z = b.localToGlobal(b.size.bottomRight(Offset.zero)) * 2;
  return Rect.fromPoints(a, z);
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

  testWidgets('the grid: tiles on whole pixels, one size, even gaps', (
    tester,
  ) async {
    await pumpDeck(tester);
    // Every tile of the page, in device pixels (0.61 x 2 here).
    final rects = [
      for (final f in [find.byType(AgentTile), find.byType(AgentsEmptyTile)])
        for (final e in f.evaluate()) _corners(e.renderObject! as RenderBox),
    ];
    expect(rects, isNotEmpty);
    for (final r in rects) {
      for (final v in [r.left, r.top, r.right, r.bottom]) {
        expect(v, closeTo(v.roundToDouble(), 0.01), reason: '$r');
      }
    }
    expect({for (final r in rects) r.width.round()}, hasLength(1));
    expect({for (final r in rects) r.height.round()}, hasLength(1));
    final xs = {for (final r in rects) r.left.round()}.toList()..sort();
    final w = rects.first.width.round();
    final gaps = {for (var i = 1; i < xs.length; i++) xs[i] - xs[i - 1] - w};
    expect(gaps, hasLength(1));
    expect(gaps.first, greaterThan(0)); // never overlapping
  });

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
