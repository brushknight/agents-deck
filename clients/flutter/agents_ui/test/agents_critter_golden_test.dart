import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter_test/flutter_test.dart';

/// A clock parked at a chosen tick, to render a pose at a given frame.
class _ParkedClock extends CritterClock {
  _ParkedClock(this.tick) : super(animate: false);
  final int tick;
  @override
  int get value => tick;
}

/// Every critter pose (rows) at successive moments of its animation (columns):
///   flutter test --update-goldens test/agents_critter_golden_test.dart
void main() {
  setUpAll(() async {
    TestWidgetsFlutterBinding.ensureInitialized();
    final data = File('fonts/ShareTechMono-Regular.ttf').readAsBytesSync();
    final loader = FontLoader('packages/agents_deck_ui/ShareTechMono')
      ..addFont(Future.value(ByteData.view(data.buffer)));
    await loader.load();
  });

  testWidgets('critter poses sheet', (tester) async {
    const poses = CritterPose.values;
    final rows = [
      for (final sp in CritterSpecies.values)
        for (final p in poses) (sp, p),
    ];
    const ticks = [0, 3, 6, 10, 12, 16, 22];
    tester.view.physicalSize = Size(
      130.0 + ticks.length * 100,
      30.0 + rows.length * 74,
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
                for (final (sp, p) in rows)
                  SizedBox(
                    height: 74,
                    child: Row(
                      children: [
                        SizedBox(
                          width: 100,
                          child: Text(
                            '${sp == CritterSpecies.codex ? 'codex ' : ''}${p.name}',
                            style: DeckHud.mono(size: 13, color: DeckHud.dim),
                          ),
                        ),
                        for (final t in ticks)
                          SizedBox(
                            width: 100,
                            child: Center(
                              child: CritterScope(
                                clock: _ParkedClock(t),
                                child: CritterSprite(
                                  pose: p,
                                  species: sp,
                                  width: 72,
                                  body:
                                      p == CritterPose.compact ||
                                          p.index <= CritterPose.hungry.index
                                      ? DeckHud.accent
                                      : DeckHud.ink,
                                  cut: DeckHud.bg,
                                ),
                              ),
                            ),
                          ),
                      ],
                    ),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
    await expectLater(
      find.byType(ColoredBox).first,
      matchesGoldenFile('goldens/agents-critters.png'),
    );
  });
}
