import 'dart:io';

import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

/// Made-up pets, one of each look, for the demo fleet.
const _pets = <String, Map<String, dynamic>>{
  'k3f9a2': {
    'name': 'Nib',
    'stage': 'legend',
    'level': 41,
    'ate': 380000000,
    'nextAt': 400000000,
    'levelFrom': 360000000,
    'hearts': 4,
    'mood': 'fed',
    'hat': {'shape': 'crown', 'color': 'yellow'},
  },
  'h0m3l4': {
    'name': 'Pim',
    'stage': 'kid',
    'level': 6,
    'ate': 160000,
    'nextAt': 190735,
    'hearts': 2,
    'mood': 'hungry',
    'hat': {'shape': 'wizard', 'color': 'purple'},
  },
  'd0cs1t': {'stage': 'egg', 'ate': 42000, 'nextAt': 50000, 'ready': true},
  't3st5a': {
    'name': 'Tuft',
    'stage': 'strong',
    'level': 22,
    'ate': 3200000,
    'nextAt': 3600000,
    'hearts': 3,
    'mood': 'fed',
    'hat': {'shape': 'propeller', 'color': 'blue'},
  },
  '1nfr4t': {
    'name': 'Moss',
    'stage': 'grown',
    'level': 12,
    'ate': 700000,
    'nextAt': 800000,
    'hearts': 0,
    'mood': 'weak',
    'hat': {'shape': 'hard hat', 'color': 'yellow'},
  },
  'k8sn0d': {
    'name': 'Bree',
    'stage': 'mighty',
    'level': 33,
    'ate': 60000000,
    'nextAt': 70000000,
    'hearts': 4,
    'mood': 'fed',
    'hat': {'shape': 'top hat', 'color': 'sky'},
  },
  'h4ptic': {
    'name': 'Oat',
    'stage': 'baby',
    'level': 2,
    'ate': 70000,
    'nextAt': 78125,
    'hearts': 1,
    'mood': 'asleep',
  },
};

FakeAgentsSource _withPets() {
  final fixture = FakeAgentsSource.demo().toJson();
  for (final a in (fixture['agents'] as List).cast<Map<String, dynamic>>()) {
    if (_pets[a['id']] case final p?) a['pet'] = p;
  }
  return FakeAgentsSource.fromState(fixture);
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

  Future<void> open(WidgetTester tester, String title) async {
    await tester.tap(find.text(title));
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump(const Duration(milliseconds: 400));
  }

  Finder inTile(String id, Type t) => find.descendant(
    of: find.byKey(ValueKey('agent-tile-$id')),
    matching: find.byType(t),
  );

  testWidgets('pets off: critters everywhere, no pet on the card', (
    tester,
  ) async {
    await pumpBoard(tester, FakeAgentsSource.demo());
    expect(find.byType(PetSprite), findsNothing);
    expect(find.byType(CritterSprite), findsWidgets);
    await open(tester, 'checkout-api');
    expect(find.byKey(const Key('agent-pet')), findsNothing);
  });

  testWidgets('pets on: the pet takes the critter\'s place, at its size', (
    tester,
  ) async {
    await pumpBoard(tester, _withPets());
    expect(find.byType(PetSprite), findsNWidgets(_pets.length));
    for (final id in _pets.keys) {
      expect(inTile(id, PetSprite), findsOneWidget, reason: id);
    }
    // With pets on, a tile's subagents are a count in its corner, not the
    // old mini critters: the pet itself shows a mini pet in tow when its
    // agent delegates.
    expect(inTile('k3f9a2', CritterSprite), findsNothing);
    expect(
      tester
          .widget<Text>(
            find.descendant(
              of: find.byKey(const ValueKey('agent-tile-k3f9a2')),
              matching: find.byKey(const Key('agent-tile-subagents')),
            ),
          )
          .data,
      '3 SUB',
    );
    expect(inTile('h0m3l4', CritterSprite), findsNothing);
    // Agents without a pet keep their critter.
    expect(inTile('scr4tc', CritterSprite), findsOneWidget);
    expect(inTile('scr4tc', PetSprite), findsNothing);
    // The pet stands on a ground line and keeps all of it in the tile.
    expect(tester.widget<PetSprite>(inTile('h0m3l4', PetSprite)).fit, isTrue);
    // Each pet does its agent's pose: infra-network needs you, in its own
    // colours on the orange tile (no plate: the stroke keeps it apart).
    expect(
      tester.widget<PetSprite>(inTile('h0m3l4', PetSprite)).pose,
      CritterPose.wait,
    );
    expect(
      tester.widget<PetSprite>(inTile('k3f9a2', PetSprite)).pose,
      isNot(CritterPose.wait),
    );
    // No level or hearts on tiles.
    expect(find.byType(PetHearts), findsNothing);
    expect(tester.takeException(), isNull);
  });

  test('hatching moves whole cells: shake, white, gone; then the pop-in', () {
    expect(
      [
        for (final t in [0.05, 0.15, 0.25, 0.55, 0.65])
          PetHatchStep.hatch(t).dx,
      ],
      [0, -1, 1, -2, 2],
    );
    expect(PetHatchStep.hatch(0.75), const PetHatchStep(white: true));
    expect(PetHatchStep.hatch(0.9).shown, isFalse);
    expect(PetHatchStep.born(0.1), const PetHatchStep(dy: 2, shown: false));
    expect(PetHatchStep.born(0.3), const PetHatchStep(dy: 2));
    expect(PetHatchStep.born(0.6), const PetHatchStep(dy: -1));
    expect(PetHatchStep.born(0.9), const PetHatchStep());
  });

  testWidgets('pets grid golden', skip: !autoUpdateGoldenFiles, (tester) async {
    await pumpBoard(tester, _withPets());
    await expectLater(
      find.byType(AgentsApp),
      matchesGoldenFile('goldens/agents-pets-grid.png'),
    );
  });
}
