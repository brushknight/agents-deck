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
      expect(petHatFor(p, const AgentHat(shape: 'cap', color: 'green')), p.hat);
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

    test('egg cracks at 30k and wears no hat', () {
      expect(PetLook.of(const AgentPet(ate: 1000), hat: hat).art, 'egg');
      final cracked = PetLook.of(const AgentPet(ate: 30000), hat: hat);
      expect(cracked.art, 'crack');
      expect(count(cracked, 'h'), 0);
      expect(count(cracked, 's'), greaterThan(0));
    });

    test('hat from kid on, the small cut for the kid', () {
      expect(
        count(PetLook.of(const AgentPet(stage: 'baby'), hat: hat), 'h'),
        0,
      );
      final kid = PetLook.of(const AgentPet(stage: 'kid'), hat: hat);
      expect(kid.pix[(6, 2)], 'h'); // the small cap's crown
      final grown = PetLook.of(const AgentPet(stage: 'grown'), hat: hat);
      expect(grown.art, 'builder');
      expect(count(grown, 'h'), greaterThan(count(kid, 'h')));
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
