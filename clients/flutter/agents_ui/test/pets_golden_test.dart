import 'dart:io';

import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

/// A clock parked at a chosen tick, to render a moment of the motion.
class _ParkedClock extends CritterClock {
  _ParkedClock(this.tick) : super(animate: false);
  final int tick;
  @override
  int get value => tick;
}

const _hat = AgentHat(shape: 'wizard', color: 'purple');

/// Every stage (rows) in every mood (columns), at the critter's size on a
/// tile, next to the critter for scale; then the motion of a legend, a
/// sleeping pet and a ready egg at successive moments:
///   flutter test --update-goldens test/pets_golden_test.dart
void main() {
  setUpAll(() async {
    TestWidgetsFlutterBinding.ensureInitialized();
    final data = File('fonts/ShareTechMono-Regular.ttf').readAsBytesSync();
    final loader = FontLoader('packages/agents_deck_ui/ShareTechMono')
      ..addFont(Future.value(ByteData.view(data.buffer)));
    await loader.load();
  });

  Widget label(String s) => SizedBox(
    width: 110,
    child: Text(s, style: DeckHud.mono(size: 13, color: DeckHud.dim)),
  );
  Widget cell(Widget child, {Color fill = DeckHud.bg}) => Container(
    width: 120,
    height: 110,
    color: fill,
    alignment: Alignment.center,
    child: child,
  );

  testWidgets('pet stages and moods sheet', (tester) async {
    final rows = <(String, AgentPet)>[
      ('egg', const AgentPet(ate: 12000)),
      ('egg cracked', const AgentPet(ate: 42000)),
      for (final s in petStages.skip(1))
        (s, AgentPet(stage: s, name: 'Nib', level: 1)),
    ];
    const moods = ['fed', 'idle', 'hungry', 'weak', 'asleep'];
    tester.view.physicalSize = Size(
      130.0 + (moods.length + 2) * 120,
      40.0 + (rows.length + 1) * 110,
    );
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(
        debugShowCheckedModeBanner: false,
        home: ColoredBox(
          color: DeckHud.bg,
          child: Padding(
            padding: const EdgeInsets.all(10),
            child: CritterScope(
              clock: _ParkedClock(0),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      label(''),
                      for (final m in [...moods, 'needs you', 'critter'])
                        SizedBox(width: 120, child: label(m)),
                    ],
                  ),
                  for (final (name, pet) in rows)
                    Row(
                      children: [
                        label(name),
                        for (final m in moods)
                          cell(
                            PetSprite(
                              pet: AgentPet(
                                stage: pet.stage,
                                ate: pet.ate,
                                level: pet.level,
                                mood: m == 'idle' ? 'fed' : m,
                              ),
                              hat: _hat,
                              idle: m == 'idle',
                              width: 92,
                            ),
                          ),
                        // On a tile that needs you: its own colours still.
                        cell(
                          PetSprite(pet: pet, hat: _hat, width: 92),
                          fill: DeckHud.accent,
                        ),
                        cell(
                          const CritterSprite(
                            pose: CritterPose.idle,
                            hat: _hat,
                            centerBody: true,
                            showBubble: false,
                            width: 92,
                            body: DeckHud.ink,
                            cut: DeckHud.bg,
                          ),
                        ),
                      ],
                    ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
    await expectLater(
      find.byType(ColoredBox).first,
      matchesGoldenFile('goldens/pets-stages.png'),
    );
  });

  testWidgets('pet motion sheet', (tester) async {
    final rows = <(String, AgentPet, bool)>[
      ('strong', const AgentPet(stage: 'strong'), false),
      ('mighty', const AgentPet(stage: 'mighty'), false),
      ('legend', const AgentPet(stage: 'legend'), false),
      ('legend, still', const AgentPet(stage: 'legend'), true),
      ('asleep', const AgentPet(stage: 'kid', mood: 'asleep'), false),
      ('ready egg', const AgentPet(ate: 45000, ready: true), false),
    ];
    // 150 ms steps: 0, 1, 2, 3, 5, 8, 13, 29.
    const ticks = [0, 3, 6, 9, 15, 24, 39, 87];
    tester.view.physicalSize = Size(
      130.0 + ticks.length * 120,
      20.0 + rows.length * 110,
    );
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(
        debugShowCheckedModeBanner: false,
        home: ColoredBox(
          color: DeckHud.bg,
          child: Padding(
            padding: const EdgeInsets.all(10),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                for (final (name, pet, still) in rows)
                  Row(
                    children: [
                      label(name),
                      for (final t in ticks)
                        cell(
                          MediaQuery(
                            data: MediaQueryData(disableAnimations: still),
                            child: CritterScope(
                              clock: _ParkedClock(t),
                              child: PetSprite(pet: pet, hat: _hat, width: 92),
                            ),
                          ),
                        ),
                    ],
                  ),
              ],
            ),
          ),
        ),
      ),
    );
    await expectLater(
      find.byType(ColoredBox).first,
      matchesGoldenFile('goldens/pets-motion.png'),
    );
  });
}
