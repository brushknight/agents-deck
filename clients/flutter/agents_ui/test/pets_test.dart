import 'dart:convert';
import 'dart:io';

import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';

Map<String, dynamic> _agent([Map<String, dynamic>? pet]) => {
  'id': 'p1',
  'slot': 1,
  'title': 'pet-demo',
  'status': 'idle',
  'hat': {'shape': 'cap', 'color': 'green', 'auto': true},
  'pet': ?pet,
};

/// A clock parked at a chosen tick.
class _ParkedClock extends CritterClock {
  _ParkedClock(this.tick) : super(animate: false);
  final int tick;
  @override
  int get value => tick;
}

void main() {
  group('pet JSON', () {
    test('no pet: pets off, or an older service', () {
      expect(Agent.fromJson(_agent()).pet, isNull);
      // The demo fixture has none either.
      final state = AgentsState.fromJson(
        jsonDecode(File('test/fixtures/agents_state.json').readAsStringSync()),
      );
      expect(state.agents.every((a) => a.pet == null), isTrue);
    });

    test('the fields from the first pets service', () {
      final p = Agent.fromJson(
        _agent({
          'name': 'Nib',
          'stage': 'kid',
          'level': 6,
          'ate': 160000,
          'nextAt': 190735,
          'hearts': 3,
          'mood': 'fed',
          'hatchedAt': '2026-10-07T18:20:00Z',
        }),
      ).pet!;
      expect(p.name, 'Nib');
      expect(p.stage, 'kid');
      expect(p.kind, 'Kid');
      expect(p.level, 6);
      expect(p.ate, 160000);
      expect(p.nextAt, 190735);
      expect(p.hearts, 3);
      expect(p.mood, 'fed');
      expect(p.hatchedAt, DateTime.utc(2026, 10, 7, 18, 20));
      // Without them: not ready, no hat of its own, and the level's start
      // worked out the way the service counts it.
      expect(p.ready, isFalse);
      expect(p.hat, isNull);
      expect(p.levelFrom, isNull);
      expect(p.levelSpan, (petLevelAt(6), 190735));
      expect(petLevelAt(6), 152588);
      expect(p.progress, closeTo((160000 - 152588) / (190735 - 152588), 1e-9));
    });

    test('ready, levelFrom and its own hat', () {
      final egg = Agent.fromJson(
        _agent({'stage': 'egg', 'ate': 42000, 'nextAt': 50000, 'ready': true}),
      ).pet!;
      expect(egg.ready, isTrue);
      expect(egg.name, '');
      expect(egg.hatchedAt, isNull);
      final p = Agent.fromJson(
        _agent({
          'name': 'Pim',
          'stage': 'strong',
          'level': 21,
          'ate': 4300000,
          'nextAt': 4500000,
          'levelFrom': 4000000,
          'hearts': 4,
          'mood': 'hungry',
          'hat': {'shape': 'crown', 'color': 'purple'},
        }),
      ).pet!;
      expect(p.kind, 'Strong Builder');
      expect(p.levelSpan, (4000000, 4500000));
      expect(p.progress, closeTo(0.6, 1e-9));
      expect(
        p.hat,
        const AgentHat(shape: 'crown', color: 'purple', auto: false),
      );
    });

    test('odd values degrade, never throw', () {
      final p = Agent.fromJson(
        _agent({
          'stage': 7,
          'hearts': 9,
          'mood': null,
          'levelFrom': 'x',
          'hat': 'cap',
        }),
      ).pet!;
      expect(p.stage, 'egg');
      expect(p.hearts, petMaxHearts);
      expect(p.mood, 'fed');
      expect(p.levelFrom, isNull);
      expect(p.hat, isNull);
      expect(p.progress, 0);
    });
  });

  group('pet look', () {
    const hat = AgentHat(shape: 'cap', color: 'green');
    int count(PetLook l, String k) => l.pix.values.where((v) => v == k).length;

    test('egg cracks at 30k or when ready, and wears no hat', () {
      expect(PetLook.of(const AgentPet(ate: 1000)).art, 'egg');
      expect(PetLook.of(const AgentPet(ate: 1000, ready: true)).art, 'crack');
      final cracked = PetLook.of(const AgentPet(ate: 30000, hat: hat));
      expect(cracked.art, 'crack');
      expect(count(cracked, 'h'), 0);
      expect(count(cracked, 's'), greaterThan(0));
    });

    test('its own hat from kid on, the small cut for the kid', () {
      expect(
        count(PetLook.of(const AgentPet(stage: 'baby', hat: hat)), 'h'),
        0,
      );
      // No hat of its own yet: bare-headed (the agent's hat isn't worn).
      expect(PetLook.of(const AgentPet(stage: 'kid')).hatCells, isEmpty);
      final kid = PetLook.of(const AgentPet(stage: 'kid', hat: hat));
      expect(kid.pix[(6, 2)], 'h'); // the small cap's crown
      final grown = PetLook.of(const AgentPet(stage: 'grown', hat: hat));
      expect(grown.art, 'builder');
      expect(count(grown, 'h'), greaterThan(count(kid, 'h')));
      final horns = PetLook.of(
        const AgentPet(
          stage: 'grown',
          hat: AgentHat(shape: 'devil horns', color: 'red'),
        ),
      );
      expect(horns.hatCells, {
        (2, 1),
        (2, 2),
        (3, 2),
        (13, 1),
        (13, 2),
        (12, 2),
      });
    });

    test('the mystery "?" until the hat shows', () {
      final egg = PetLook.of(const AgentPet(), mystery: true);
      expect(egg.pix[(14, -3)], 'q');
      expect(egg.bottom, 14); // the "?" doesn't count
      expect(
        count(
          PetLook.of(const AgentPet(stage: 'kid', hat: hat), mystery: true),
          'q',
        ),
        0,
      );
    });

    test('moods', () {
      final hungry = PetLook.of(
        const AgentPet(stage: 'kid', mood: 'hungry'),
        idle: true,
      );
      expect(count(hungry, 'm'), 4);
      final idle = PetLook.of(const AgentPet(stage: 'kid'), idle: true);
      expect(count(idle, 'm'), 0);
      expect(idle.pix[(4, 8)], 'e'); // the eyes shut into a line
      expect(idle.pix[(5, 7)], 'b');
      // A pose sets its own eyes.
      expect(
        PetLook.of(const AgentPet(stage: 'kid'), idle: true, posed: true).pix[(
          5,
          7,
        )],
        'e',
      );
      final weak = PetLook.of(const AgentPet(stage: 'mighty', mood: 'weak'));
      expect(weak.art, 'weak');
      expect(weak.aura, '');
      final asleep = PetLook.of(
        const AgentPet(stage: 'legend', mood: 'asleep'),
      );
      expect(asleep.asleep, isTrue);
      expect(asleep.aura, '');
      expect(PetLook.of(const AgentPet(stage: 'legend')).aura, 'legend');
      expect(PetLook.of(const AgentPet(stage: 'grown')).aura, '');
    });
  });

  group('poses (docs/pet-poses.md)', () {
    const builder = AgentPet(stage: 'grown');
    const ink = Color(0xFF141412), body = Color(0xFFFF4D00);
    const white = Color(0xFFFFFFFF), black = petStroke;
    Map<(int, int), Color> at(
      AgentPet pet,
      CritterPose pose,
      int ms, {
      bool still = false,
    }) => petCellsAt(pet, PetDraw(pose: pose), ms, still: still);
    Set<(int, int)> of(Map<(int, int), Color> m, Color c) => {
      for (final e in m.entries)
        if (e.value == c) e.key,
    };

    test('the builder\'s X eyes', () {
      final eyes = of(at(builder, CritterPose.error, 0), ink);
      expect(eyes, {
        (3, 5),
        (5, 5),
        (4, 6),
        (3, 7),
        (5, 7),
        (9, 5),
        (11, 5),
        (10, 6),
        (9, 7),
        (11, 7),
      });
    });

    test('arms: the rig table', () {
      expect(petArmCells(PetLook.of(builder).pix, 2, false), [(0, 2), (0, 3)]);
      expect(petArmCells(PetLook.of(builder).pix, 2, true), [(15, 2), (15, 3)]);
      final kid = PetLook.of(const AgentPet(stage: 'kid')).pix;
      expect(petArmCells(kid, 7, false), [(1, 7), (1, 8)]);
      expect(petArmCells(kid, 4, true), [(13, 4), (13, 5)]);
      final baby = PetLook.of(const AgentPet(stage: 'baby')).pix;
      expect(petArmCells(baby, 3, false), [(4, 3), (4, 4)]);
      final weak = PetLook.of(const AgentPet(stage: 'grown', mood: 'weak')).pix;
      expect(petArmCells(weak, 4, true), [(14, 4), (14, 5)]);
    });

    test('bash: feet step fast, the pet jogs, ">_" with its cursor', () {
      final a = at(builder, CritterPose.bash, 0);
      final b = at(builder, CritterPose.bash, 150);
      // la: feet at 2 4 11 13; lb (from 100 ms): at 3 5 10 12.
      expect(a[(2, 11)], body);
      expect(a[(3, 11)], black);
      expect(b[(3, 11)], body);
      // The whole pet jogs a cell up in the second half of 0.4 s (pjf),
      // feet and all.
      expect(a[(1, 3)], body);
      expect(b[(1, 3)], body);
      final c = at(builder, CritterPose.bash, 250); // la again, lifted
      expect(c[(1, 2)], body);
      expect(c[(2, 10)], body);
      expect(c[(2, 11)], black);
      // The prompt glyph in the bubble box (16, -2), one row down, white.
      expect(a[(16, -1)], white);
      expect(a[(17, 0)], white);
      expect(a[(19, 1)], white); // the cursor
      // The cursor blinks off (from 0.5 s), its stroke with it.
      expect(at(builder, CritterPose.bash, 600)[(19, 1)], isNull);
    });

    test(
      'the stroke: every opaque cell grows by its 4 neighbours, in black',
      () {
        for (final pet in const [
          AgentPet(),
          AgentPet(stage: 'baby'),
          AgentPet(
            stage: 'kid',
            hat: AgentHat(shape: 'wizard', color: 'purple'),
          ),
          AgentPet(
            stage: 'legend',
            hat: AgentHat(shape: 'crown', color: 'yellow'),
          ),
          AgentPet(stage: 'grown', mood: 'weak'),
        ]) {
          for (final pose in CritterPose.values) {
            final m = at(pet, pose, 0);
            final filled = {
              for (final e in m.entries)
                if (e.value != black) e.key,
            };
            for (final (x, y) in filled) {
              for (final n in [
                (x + 1, y),
                (x - 1, y),
                (x, y + 1),
                (x, y - 1),
              ]) {
                expect(
                  m.containsKey(n),
                  isTrue,
                  reason: '${pet.stage} $pose $n',
                );
              }
            }
            // Nothing black that isn't next to the pet.
            for (final (x, y) in of(m, black)) {
              final next = [
                (x, y),
                (x + 1, y),
                (x - 1, y),
                (x, y + 1),
                (x, y - 1),
              ].any(filled.contains);
              expect(next, isTrue, reason: '${pet.stage} $pose ($x, $y)');
            }
          }
        }
      },
    );

    test('the stroke moves with its layer', () {
      // Celebrate: the body hops 2 cells in the second half of 0.4 s.
      final up = at(builder, CritterPose.celebrate, 250);
      final down = at(builder, CritterPose.celebrate, 0);
      expect(down[(1, 2)], black);
      expect(up[(1, 0)], black);
      expect(up[(1, 2)], isNot(black));
    });

    test('an egg rocks while its agent works, and keeps its bubble', () {
      const egg = AgentPet(ate: 1000);
      expect(at(egg, CritterPose.read, 0)[(6, 0)], const Color(0xFFF4F0E6));
      expect(at(egg, CritterPose.read, 300)[(5, 0)], const Color(0xFFF4F0E6));
      // The magnifier at the bubble box (14, -4).
      expect(at(egg, CritterPose.read, 0)[(15, -4)], white);
    });

    test('asleep: frozen, only "?" and "!" show', () {
      const sleepy = AgentPet(stage: 'kid', mood: 'asleep');
      final idle = at(sleepy, CritterPose.idle, 0);
      expect(of(idle, white), isEmpty);
      expect(of(at(sleepy, CritterPose.wait, 0), white), isNotEmpty);
    });

    test('hungry for food: awake while idle, mouth open, no "z z"', () {
      const hungry = AgentPet(stage: 'kid', mood: 'hungry');
      final m = at(hungry, CritterPose.idle, 0);
      expect(of(m, white), isEmpty);
      expect(m[(7, 9)], const Color(0xFFBDB8AD));
    });

    test('reduced motion: the first frame, everything up and in place', () {
      final m = at(builder, CritterPose.bash, 150, still: true);
      expect(m[(2, 11)], body); // la
      expect(m[(3, 11)], black);
      expect(m[(1, 3)], body); // no bob
    });

    test('timings', () {
      expect(petMotion('pp blink', 500), (1.0, 0, 0));
      expect(petMotion('pp blink', 700), (0.15, 0, 0));
      expect(petMotion('glance', 100), (1.0, -1, 0));
      expect(petMotion('glance', 700), (1.0, 1, 0));
      expect(
        [
          for (final t in [0, 300, 600, 900]) petMotion('scan', t).$2,
        ],
        [0, -1, 0, 1],
      );
      expect(
        [
          for (final t in [0, 400, 800]) petMotion('psq', t).$3,
        ],
        [0, 1, 2],
      );
      expect(petMotion('pp zz', 0), (0.9, 0, 0));
      expect(petMotion('pp zz z1', 0), (0.45, 0, -1));
      expect(petMotion('pjog', 300), (1.0, 0, -1));
      expect(petMotion('pjog pjf', 150), (1.0, 0, 0));
      expect(petMotion('pjog pjf', 250), (1.0, 0, -1));
      expect(petMotion('hf-4-120-2', 250).$1, 1);
      expect(petMotion('hf-4-120-2', 130).$1, 0);
      expect(petMotion('pp spark s1', 100).$1, 0.45);
      expect(petMotion('pp spark s1', 250).$1, 1);
      expect(
        [
          for (final t in [0, 1100, 1250, 1350, 1500]) petReadyWobble(t),
        ],
        [0, 0, -1, 1, -1],
      );
    });
  });

  test('every pet, pose and hat fits a tile at 4 px a cell', () {
    // The tile's pet box is 158 x 96 design pixels; at 5 px a cell most
    // fit, the tallest (glows, stars, tall hats with a bubble) at 4.
    final hats = [
      null,
      for (final shape in [...hatShapes, 'devil horns'])
        AgentHat(shape: shape, color: 'yellow'),
    ];
    var tallest = 0.0, widest = 0.0;
    for (final stage in petStages) {
      for (final mood in ['fed', 'hungry', 'weak', 'asleep']) {
        for (final hat in hats) {
          for (final pose in CritterPose.values) {
            final e = petExtent(
              AgentPet(stage: stage, mood: mood, hat: hat, ate: 42000),
              PetDraw(pose: pose),
            );
            if (e.height > tallest) tallest = e.height;
            if (e.width > widest) widest = e.width;
          }
        }
      }
    }
    expect(tallest * 4, lessThanOrEqualTo(96));
    expect(widest * 4, lessThanOrEqualTo(158));
  });

  test('the face shows in every frame of every pose', () {
    // Every hatched stage and mood doing every pose, painted every 50 ms
    // over 2.4 s in paint order: both eyes always show, and never fewer
    // eye cells than at the start (a body layer must never cover them).
    // A hat without near-black cells, so only eyes (and the weak art's
    // mouth) have the eyes' colour.
    const hat = AgentHat(shape: 'beanie', color: 'teal');
    for (final stage in petStages.skip(1)) {
      for (final mood in ['fed', 'hungry', 'weak', 'asleep']) {
        final pet = AgentPet(stage: stage, mood: mood, hat: hat);
        final art = PetLook.of(pet).art;
        final eyeColor = art == 'weak'
            ? const Color(0xFF0A0A0A)
            : const Color(0xFF141412);
        for (final pose in CritterPose.values) {
          if (pose == CritterPose.exited) continue; // eyes become body dots
          int? start;
          for (var ms = 0; ms < 2400; ms += 50) {
            final cells = petCellsAt(pet, PetDraw(pose: pose), ms);
            var eyes = [
              for (final e in cells.entries)
                // (x 15 and up: the flag's dark squares, not a face)
                if (e.value == eyeColor && e.key.$1 < 15) e.key,
            ];
            if (art == 'weak' && eyes.isNotEmpty) {
              // Its mouth shares the eyes' colour and sits rows lower: keep
              // the top two rows of that colour, where the eyes are.
              final top = eyes.map((c) => c.$2).reduce((a, b) => a < b ? a : b);
              eyes = [
                for (final c in eyes)
                  if (c.$2 <= top + 1) c,
              ];
            }
            final why = '$stage $mood $pose at $ms ms';
            expect(eyes.where((c) => c.$1 < 8), isNotEmpty, reason: why);
            expect(eyes.where((c) => c.$1 >= 8), isNotEmpty, reason: why);
            start ??= eyes.length;
            expect(eyes.length, greaterThanOrEqualTo(start), reason: why);
          }
        }
      }
    }
  });

  test('the feet stay on the body in every frame', () {
    // Every hatched stage and mood that walks, painted every 50 ms over
    // 2.4 s: each foot (the body's lowest row) has the body right above it,
    // so the body never jogs off its legs.
    for (final stage in petStages.skip(1)) {
      for (final mood in ['fed', 'hungry', 'weak']) {
        final pet = AgentPet(stage: stage, mood: mood);
        final bodyColor = PetLook.of(pet).weak
            ? const Color(0xFFB0673F)
            : const Color(0xFFFF4D00);
        for (final pose in [
          CritterPose.run,
          CritterPose.bash,
          CritterPose.write,
          CritterPose.delegate,
        ]) {
          for (var ms = 0; ms < 2400; ms += 50) {
            final cells = petCellsAt(pet, PetDraw(pose: pose), ms);
            final own = [
              for (final e in cells.entries)
                // (x 14 and up: the mini pet in tow, its own feet)
                if (e.value == bodyColor && e.key.$1 < 14) e.key,
            ];
            final low = own.map((c) => c.$2).reduce((a, b) => a > b ? a : b);
            for (final (x, y) in own.where((c) => c.$2 == low)) {
              final held =
                  cells[(x, y - 1)] == bodyColor ||
                  cells[(x, y - 2)] == bodyColor;
              expect(
                held,
                isTrue,
                reason: '$stage $mood $pose $ms ms ($x, $y)',
              );
            }
          }
        }
      }
    }
  });

  test('each z of "z z" has its own outline', () {
    final m0 = petCellsAt(
      const AgentPet(stage: 'grown'),
      const PetDraw(pose: CritterPose.idle),
      0,
    );
    final m1 = petCellsAt(
      const AgentPet(stage: 'grown'),
      const PetDraw(pose: CritterPose.idle),
      1200,
    );
    // Half a cycle apart the zs swap heights; nothing black is left where
    // a z was without it.
    expect(m0, isNot(m1));
    for (final m in [m0, m1]) {
      for (final MapEntry(key: (x, y), value: c) in m.entries) {
        if (c != petStroke || y > -1) continue;
        final near = [
          (x + 1, y),
          (x - 1, y),
          (x, y + 1),
          (x, y - 1),
        ].any((n) => m[n] != null && m[n] != petStroke);
        expect(near, isTrue, reason: 'a lone outline at ($x, $y)');
      }
    }
  });

  test('the kid drops one row while compacting, the others two', () {
    expect(petMotion('psq1', 0).$3, 0);
    expect(petMotion('psq1', 500).$3, 1);
    expect(petMotion('psq1', 1000).$3, 1);
    expect(petRig['kid']!.squeeze, 1);
    expect(petRig['builder']!.squeeze, 2);
  });

  group('auras', () {
    test(
      'flames start a cell out (the stroke fills it), never on the body',
      () {
        for (final stage in ['strong', 'mighty', 'legend']) {
          final pet = AgentPet(stage: stage);
          final body = PetLook.of(pet).pix.keys.toSet();
          final flames = petFlameCells(pet);
          expect(flames, isNotEmpty);
          expect(flames.intersection(body), isEmpty);
        }
      },
    );
  });

  group('pet sprite', () {
    Future<void> pump(
      WidgetTester tester,
      Widget child, {
      bool still = false,
    }) => tester.pumpWidget(
      MediaQuery(
        data: MediaQueryData(disableAnimations: still),
        child: Directionality(
          textDirection: TextDirection.ltr,
          child: Center(child: child),
        ),
      ),
    );

    testWidgets('the critter\'s box and size', (tester) async {
      await pump(
        tester,
        const PetSprite(pet: AgentPet(stage: 'kid'), width: 92),
      );
      expect(
        tester.getSize(find.byType(PetSprite)),
        const Size(92, 92 * 13 / 18),
      );
    });

    Future<List<int>> pixels(WidgetTester tester, int tick, bool still) async {
      final key = GlobalKey();
      await pump(
        tester,
        RepaintBoundary(
          key: key,
          child: CritterScope(
            clock: _ParkedClock(tick),
            child: const SizedBox(
              width: 140,
              height: 120,
              child: Center(
                child: PetSprite(pet: AgentPet(stage: 'legend'), width: 92),
              ),
            ),
          ),
        ),
        still: still,
      );
      final boundary =
          key.currentContext!.findRenderObject()! as RenderRepaintBoundary;
      return (await tester.runAsync(() async {
        final image = await boundary.toImage();
        final data = await image.toByteData();
        image.dispose();
        return data!.buffer.asUint8List().toList();
      }))!;
    }

    testWidgets('moves with the clock, and holds still with reduce motion', (
      tester,
    ) async {
      expect(
        await pixels(tester, 0, false),
        isNot(await pixels(tester, 9, false)),
      );
      expect(await pixels(tester, 0, true), await pixels(tester, 9, true));
    });
  });
}
