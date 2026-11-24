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
const _beanie = AgentHat(shape: 'beanie', color: 'teal');
const _crown = AgentHat(shape: 'crown', color: 'yellow');

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
        (s, AgentPet(stage: s, name: 'Nib', level: 1, hat: _hat)),
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
                                hat: pet.hat,
                                mood: m == 'idle' ? 'fed' : m,
                              ),
                              hat: _hat,
                              idle: m == 'idle',
                              width: 92,
                            ),
                          ),
                        // On a tile that needs you: its own colours, with
                        // its stroke.
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
      ('strong', const AgentPet(stage: 'strong', hat: _hat), false),
      ('mighty', const AgentPet(stage: 'mighty', hat: _hat), false),
      ('legend', const AgentPet(stage: 'legend', hat: _hat), false),
      ('legend, still', const AgentPet(stage: 'legend', hat: _hat), true),
      (
        'asleep',
        const AgentPet(stage: 'kid', mood: 'asleep', hat: _hat),
        false,
      ),
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

  testWidgets('pet poses sheet', (tester) async {
    // Every stage (rows) doing every pose (columns), with the stroke, at
    // two moments (0 and 0.3 s) side by side; the last columns are needs
    // you on an orange tile.
    final rows = <(String, AgentPet)>[
      ('egg', const AgentPet(ate: 12000)),
      ('egg cracked', const AgentPet(ate: 42000)),
      ('baby', const AgentPet(stage: 'baby')),
      ('kid', const AgentPet(stage: 'kid', hat: _hat)),
      ('grown-up', const AgentPet(stage: 'grown', hat: _beanie)),
      ('strong', const AgentPet(stage: 'strong', hat: _beanie)),
      ('mighty', const AgentPet(stage: 'mighty', hat: _hat)),
      ('legend', const AgentPet(stage: 'legend', hat: _crown)),
      ('weak', const AgentPet(stage: 'grown', mood: 'weak', hat: _beanie)),
      ('hungry', const AgentPet(stage: 'kid', mood: 'hungry', hat: _hat)),
    ];
    const poses = CritterPose.values;
    const w = 96.0, h = 86.0;
    tester.view.physicalSize = Size(
      110 + (poses.length + 1) * w,
      40 + rows.length * h,
    );
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    Widget at(
      AgentPet pet,
      CritterPose pose,
      int tick, {
      Color fill = DeckHud.bg,
    }) => Container(
      width: w / 2,
      height: h,
      color: fill,
      alignment: Alignment.center,
      child: OverflowBox(
        maxWidth: 200,
        maxHeight: 200,
        child: CritterScope(
          clock: _ParkedClock(tick),
          child: PetSprite(pet: pet, pose: pose, width: 36),
        ),
      ),
    );
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
                Row(
                  children: [
                    const SizedBox(width: 90),
                    for (final p in [...poses.map((p) => p.name), 'on orange'])
                      SizedBox(
                        width: w,
                        child: Text(
                          p,
                          style: DeckHud.mono(size: 11, color: DeckHud.dim),
                        ),
                      ),
                  ],
                ),
                for (final (name, pet) in rows)
                  Row(
                    children: [
                      SizedBox(
                        width: 90,
                        child: Text(
                          name,
                          style: DeckHud.mono(size: 11, color: DeckHud.dim),
                        ),
                      ),
                      for (final pose in poses) ...[
                        at(pet, pose, 0),
                        at(pet, pose, 6),
                      ],
                      at(pet, CritterPose.wait, 0, fill: DeckHud.accent),
                      at(pet, CritterPose.bash, 0, fill: DeckHud.accent),
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
      matchesGoldenFile('goldens/pets-poses.png'),
    );
  });
}
