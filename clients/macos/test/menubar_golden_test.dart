import 'dart:io';

import 'package:agents_deck_menubar/main.dart';
import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  setUpAll(() async {
    for (final (family, file) in [
      ('RobotoMono', 'RobotoMono.ttf'),
      ('ShareTechMono', 'ShareTechMono-Regular.ttf'),
    ]) {
      final data = File('../flutter/agents_ui/fonts/$file').readAsBytesSync();
      await (FontLoader(
        'packages/agents_deck_ui/$family',
      )..addFont(Future.value(ByteData.view(data.buffer)))).load();
    }
  });

  testWidgets('the deck at menu bar size', skip: !autoUpdateGoldenFiles, (
    tester,
  ) async {
    tester.view.physicalSize = const Size(
      880,
      880,
    ); // 440 pt on a Retina screen
    tester.view.devicePixelRatio = 2;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MenuBarDeck(
        source: FakeAgentsSource.demo(),
        visible: ValueNotifier(true),
      ),
    );
    await tester.pump();
    await expectLater(
      find.byType(MenuBarDeck),
      matchesGoldenFile('goldens/menubar.png'),
    );
  });

  testWidgets('an agent card at menu bar size', skip: !autoUpdateGoldenFiles, (
    tester,
  ) async {
    tester.view.physicalSize = const Size(880, 880);
    tester.view.devicePixelRatio = 2;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MenuBarDeck(
        source: FakeAgentsSource.demo(),
        visible: ValueNotifier(true),
      ),
    );
    await tester.pump();
    await tester.tap(find.text('checkout-api'));
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump(const Duration(milliseconds: 400));
    await expectLater(
      find.byType(MenuBarDeck),
      matchesGoldenFile('goldens/menubar-card.png'),
    );
  });
}
