import 'dart:io';

import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

/// The demo fleet with pets, set up like the page's design: checkout-api
/// runs bash with two subagents, infra-network asks for a permission,
/// search-index finished an hour ago and nobody has looked, api-tests is an
/// egg reading a file, docs-site an egg ready to hatch.
Map<String, dynamic> petsPageState({bool readyEgg = true}) {
  final json = FakeAgentsSource.demo(pets: true).toJson();
  final now = DateTime.now().toUtc();
  String ago(Duration d) => now.subtract(d).toIso8601String();
  final byId = {
    for (final a in (json['agents'] as List).cast<Map<String, dynamic>>())
      a['id'] as String: a,
  };
  byId['k3f9a2']!
    ..['aiTitle'] = 'fix refresh token race'
    ..['statusSince'] = ago(const Duration(minutes: 38))
    ..['lastActive'] = ago(const Duration(minutes: 38))
    ..['activity'] = {'tool': 'Bash', 'detail': 'go test ./auth/...'}
    ..['subagents'] = [
      {
        'id': 'a1f3c9',
        'title': 'map the payment retry paths',
        'type': 'explore',
        'tool': 'Grep',
      },
      {
        'id': 'b72e01',
        'title': 'write tests for refunds',
        'type': 'agent',
        'tool': 'Edit',
      },
    ];
  final infra = byId['h0m3l4']!
    ..['aiTitle'] = 'move staging to the new subnet'
    ..['statusSince'] = ago(const Duration(minutes: 2));
  (((infra['waiting'] as Map)['options'] as List)[2] as Map)['label'] =
      'no, tell it what to do instead';
  byId['h4ptic']!
    ..['status'] = 'idle'
    ..['unseen'] = true
    ..['aiTitle'] = 'reindex product search'
    ..['statusSince'] = ago(const Duration(hours: 1))
    ..['lastActive'] = ago(const Duration(hours: 1))
    ..['modelLabel'] = 'opus 5.5'
    ..['cwd'] = '/Users/sam/dev/search-index'
    ..['branch'] = 'main'
    ..['context'] = {'used': 140000, 'window': 1000000}
    ..['lastPrompt'] =
        'rebuild the product search index with typo tolerance, then run the '
        "relevance checks against last week's queries and tell me what got "
        'worse';
  byId['t3st5a']!
    ..['status'] = 'running'
    ..['aiTitle'] = 'flaky login tests'
    ..['statusSince'] = ago(const Duration(minutes: 6))
    ..['lastActive'] = ago(const Duration(minutes: 6))
    ..['activity'] = {'tool': 'Read', 'detail': 'src/auth/session_test.go'}
    ..['subagents'] = null
    ..['modelLabel'] = 'sonnet 5'
    ..['branch'] = 'fix-auth'
    ..['context'] = {'used': 96000, 'window': 200000}
    ..['lastPrompt'] = 'find why the login tests flake on CI and fix the race';
  byId['d0cs1t']!
    ..['unseen'] = false
    ..['aiTitle'] = 'rewrite install guide'
    ..['statusSince'] = ago(const Duration(minutes: 12))
    ..['lastActive'] = ago(const Duration(minutes: 12))
    ..['context'] = {'used': 52000, 'window': 200000}
    ..['lastPrompt'] = 'rewrite the install guide for the new cli'
    ..['pet'] = {
      'stage': 'egg',
      'ate': readyEgg ? 52000 : 46000,
      'nextAt': 50000,
      'ready': readyEgg,
    };
  return json;
}

Future<void> loadPageFonts() async {
  for (final (family, file) in [
    ('RobotoMono', 'RobotoMono.ttf'),
    ('ShareTechMono', 'ShareTechMono-Regular.ttf'),
    ('InstrumentSerif', 'InstrumentSerif-Regular.ttf'),
  ]) {
    final data = File('fonts/$file').readAsBytesSync();
    await (FontLoader(
      'packages/agents_deck_ui/$family',
    )..addFont(Future.value(ByteData.view(data.buffer)))).load();
  }
}

void main() {
  setUpAll(() async {
    TestWidgetsFlutterBinding.ensureInitialized();
    debugAgentsSetupHostname = 'panel';
    await loadPageFonts();
  });

  Future<FakeAgentsSource> pumpBoard(
    WidgetTester tester, {
    bool readyEgg = true,
  }) async {
    final source = FakeAgentsSource.fromState(
      petsPageState(readyEgg: readyEgg),
    );
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
    return source;
  }

  Future<void> open(WidgetTester tester, String title) async {
    await tester.tap(find.text(title));
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump(const Duration(milliseconds: 400));
  }

  Future<void> golden(WidgetTester tester, String name) async {
    if (!autoUpdateGoldenFiles) return;
    await expectLater(
      find.byType(AgentsApp),
      matchesGoldenFile('goldens/pets-page-$name.png'),
    );
  }

  testWidgets('running: the pet on its ground line, bash and subagents', (
    tester,
  ) async {
    await pumpBoard(tester);
    await open(tester, 'checkout-api');
    expect(find.byKey(const Key('agent-pets-card')), findsOneWidget);
    // Only one mascot: the pet.
    expect(
      find.descendant(
        of: find.byKey(const Key('agent-pets-card')),
        matching: find.byType(CritterSprite),
      ),
      findsNothing,
    );
    expect(
      tester
          .widget<PetSprite>(
            find.descendant(
              of: find.byKey(const Key('agent-pet')),
              matching: find.byType(PetSprite),
            ),
          )
          .pose,
      CritterPose.bash,
    );
    // No hat picker: the pet wears its own hat.
    expect(find.byKey(const Key('agent-hat')), findsNothing);
    expect(find.text('WORKING · 38M'), findsOneWidget);
    expect(find.text('LV 14'), findsOneWidget);
    expect(find.text(r'$ go test ./auth/...'), findsOneWidget);
    expect(
      find.textContaining('map the payment retry paths · explore'),
      findsOneWidget,
    );
    expect(find.text('2 SUBAGENTS'), findsOneWidget);
    expect(find.text('41%'), findsOneWidget);
    // Gone: token counts, cost, turns, cache.
    for (final gone in ['tokens out', 'cache read', 'cost · est', 'turns']) {
      expect(find.text(gone), findsNothing);
    }
    expect(find.text('FOCUS TERMINAL'), findsOneWidget);
    expect(find.text('INTERRUPT'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await golden(tester, 'running');
  });

  testWidgets('needs you: the pet boxed, the question in a serif', (
    tester,
  ) async {
    await pumpBoard(tester);
    await open(tester, 'infra-network');
    expect(find.text('Run this command?'), findsOneWidget);
    expect(find.text('NEEDS YOU · 2M'), findsOneWidget);
    final pet = tester.widget<PetSprite>(
      find.descendant(
        of: find.byKey(const Key('agent-pet')),
        matching: find.byType(PetSprite),
      ),
    );
    expect((pet.pose, pet.calm), (CritterPose.wait, true));
    expect(find.byKey(const Key('agent-option-1')), findsOneWidget);
    expect(find.text('FOCUS TERMINAL'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await golden(tester, 'waiting');
  });

  testWidgets('idle and unseen: status and the whole last prompt', (
    tester,
  ) async {
    await pumpBoard(tester);
    await open(tester, 'search-index');
    expect(find.text('HUNGRY · 1H'), findsOneWidget);
    expect(
      find.text('finished 1h ago · nobody has looked yet'),
      findsOneWidget,
    );
    expect(find.text('REVIEW IN TERMINAL'), findsOneWidget);
    expect(find.text('STOP'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await golden(tester, 'idle');
  });

  // The pets page with an answer from an hour ago on search-index (idle)
  // and checkout-api (working).
  Future<void> pumpAnswers(WidgetTester tester) async {
    final state = petsPageState();
    final ago = DateTime.now().toUtc().subtract(const Duration(hours: 1));
    for (final a in (state['agents'] as List).cast<Map<String, dynamic>>()) {
      if (a['id'] == 'h4ptic' || a['id'] == 'k3f9a2') {
        a['lastAnswer'] =
            'Rebuilt the product index with typo tolerance on names and brands.';
        a['lastAnswerAt'] = ago.toIso8601String();
      }
    }
    tester.view.physicalSize = const Size(720, 720);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(
        debugShowCheckedModeBanner: false,
        theme: ThemeData.dark().copyWith(scaffoldBackgroundColor: DeckHud.bg),
        home: Scaffold(
          body: AgentsApp(
            source: FakeAgentsSource.fromState(state),
            animate: false,
          ),
        ),
      ),
    );
    await tester.pump();
  }

  testWidgets('idle with an answer: the answer in place of the prompt', (
    tester,
  ) async {
    await pumpAnswers(tester);
    await open(tester, 'search-index');
    expect(find.text('LAST ANSWER · 1H AGO'), findsOneWidget);
    expect(
      find.text(
        'Rebuilt the product index with typo tolerance on names and brands.',
      ),
      findsOneWidget,
    );
    expect(find.byKey(const Key('agent-last-prompt')), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('working: the prompt, not the answer from before', (
    tester,
  ) async {
    await pumpAnswers(tester);
    await open(tester, 'checkout-api');
    expect(find.byKey(const Key('agent-last-prompt')), findsOneWidget);
    expect(find.byKey(const Key('agent-last-answer')), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('an egg: how far to hatching', (tester) async {
    await pumpBoard(tester);
    await open(tester, 'api-tests');
    expect(find.text('EGG · 84%'), findsOneWidget);
    expect(find.text(r'$ src/auth/session_test.go'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await golden(tester, 'egg');
  });

  testWidgets('a ready egg: boxed, tap to hatch', (tester) async {
    final source = await pumpBoard(tester);
    await open(tester, 'docs-site');
    expect(find.text('READY TO HATCH'), findsOneWidget);
    expect(find.text('TAP TO HATCH'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await golden(tester, 'ready');
    await tester.tap(find.byKey(const Key('pet-hatch')));
    await tester.pump();
    expect(source.calls.last, ('hatch', 'd0cs1t', null));
    await tester.pump(const Duration(milliseconds: 1500));
    await tester.pump(const Duration(milliseconds: 700));
    expect(find.text('TAP TO HATCH'), findsNothing);
    expect(find.text('LV 1'), findsOneWidget);
  });

  testWidgets('a question: the pet boxed, the answers as square rows', (
    tester,
  ) async {
    final source = await pumpBoard(tester);
    await open(tester, 'auth-flow');
    expect(
      find.text('Which session strategy should the login flow use?'),
      findsOneWidget,
    );
    expect(find.text('IT ASKS · QUESTION · 1 OF 1'), findsOneWidget);
    expect(
      tester
          .widget<PetSprite>(
            find.descendant(
              of: find.byKey(const Key('agent-pet')),
              matching: find.byType(PetSprite),
            ),
          )
          .calm,
      isTrue,
    );
    await golden(tester, 'question');
    await tester.tap(find.byKey(const Key('agent-option-2')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('agent-send')));
    await tester.pump();
    expect(source.calls.last.$1, 'answer');
    expect(tester.takeException(), isNull);
  });

  testWidgets('tap the pet: its page, and back', (tester) async {
    await pumpBoard(tester);
    await open(tester, 'checkout-api');
    await tester.tap(find.byKey(const Key('agent-pet')));
    await tester.pump();
    // It goes by its agent's name; its own name isn't shown.
    expect(find.text('Juno'), findsNothing);
    expect(
      tester
          .widget<Text>(
            find.descendant(
              of: find.byKey(const Key('pet-name')),
              matching: find.byType(Text),
            ),
          )
          .data,
      'checkout-api',
    );
    expect(find.text('BUILDER · LEVEL 14'), findsOneWidget);
    expect(find.text('LEVEL 14'), findsOneWidget);
    expect(find.text('its pet'), findsOneWidget);
    // The whole growth path: reached stages in their real look, the ones
    // ahead as dark shapes.
    final path = tester
        .widgetList<PetSprite>(
          find.descendant(
            of: find.byKey(const Key('pet-path')),
            matching: find.byType(PetSprite),
          ),
        )
        .toList();
    expect(path, hasLength(petStages.length));
    // Juno is a grown-up (index 3): egg to grown-up real, strong on dark.
    expect(
      [for (final p in path) p.sil != null],
      [
        false, false, false, false, true, true, true, //
      ],
    );
    expect(find.text('as a baby'), findsOneWidget);
    expect(find.byType(HudClock), findsNothing);
    expect(find.text('57%'), findsOneWidget);
    expect(find.text('teal beanie'), findsOneWidget);
    expect(find.text('rolled at hatching, for good'), findsOneWidget);
    expect(find.text('Strong: a new glow'), findsOneWidget);
    expect(
      find.text('at 3,469,434 tokens eaten, 2,429,434 to go'),
      findsOneWidget,
    );
    expect(tester.takeException(), isNull);
    await golden(tester, 'pet');
    await tester.tap(find.byKey(const Key('pet-back')));
    await tester.pump();
    expect(find.byKey(const Key('pet-name')), findsNothing);
    expect(find.text('FOCUS TERMINAL'), findsOneWidget);
  });

  testWidgets('an egg that becomes ready on its page: hatch it there', (
    tester,
  ) async {
    final source = await pumpBoard(tester, readyEgg: false);
    await open(tester, 'docs-site');
    await tester.tap(find.byKey(const Key('agent-pet')));
    await tester.pump();
    expect(find.text('its egg'), findsOneWidget);
    expect(find.text('?'), findsOneWidget); // its hat: a surprise
    source.load(petsPageState());
    await tester.pump();
    expect(find.text('its egg, ready to hatch'), findsOneWidget);
    expect(find.text('It hatches'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await golden(tester, 'petegg');
    await tester.tap(find.byKey(const Key('pet-hatch-it')));
    await tester.pump();
    expect(source.calls.last, ('hatch', 'd0cs1t', null));
  });

  testWidgets('real-world text: long names fit, wrappers left out', (
    tester,
  ) async {
    final json = petsPageState();
    final a = (json['agents'] as List).cast<Map<String, dynamic>>().first;
    a['title'] = 'a made-up agent with a rather long descriptive name';
    a['lastPrompt'] =
        '<pasted_content id="ab12"> please tidy the export tables</pasted_content>';
    a['account'] = {'name': 'personal', 'color': 'sky'};
    final source = FakeAgentsSource.fromState(json)
      ..accountList.addAll(const [
        AgentAccount(name: 'personal', color: 'sky', isDefault: true),
        AgentAccount(name: 'work', color: 'yellow'),
      ]);
    tester.view.physicalSize = const Size(720, 720);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(home: AgentsApp(source: source, animate: false)),
    );
    await tester.pump();
    await tester.tap(find.byKey(const ValueKey('agent-tile-k3f9a2')));
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump(const Duration(milliseconds: 400));
    // The whole name, smaller (two lines at the smallest), never cut short.
    final name = tester.widget<Text>(
      find.descendant(
        of: find.byKey(const Key('agent-title')),
        matching: find.byType(Text),
      ),
    );
    expect(name.data, a['title']);
    expect(name.style!.fontSize, lessThan(56));
    expect(find.text('please tidy the export tables'), findsOneWidget);
    // With two accounts: which one it runs in, beside the status; moving
    // it to the other is for the browser.
    // A working agent: at the end of the last prompt's row, the square and
    // the name only.
    expect(find.byKey(const Key('agent-account')), findsNothing);
    expect(find.byKey(const Key('agent-account-name')), findsOneWidget);
    expect(find.text('personal'), findsOneWidget);
    expect(find.text('CONTINUE ON WORK'), findsNothing);
    // No clock on this page.
    expect(find.byType(HudClock), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('pets off: the critter page as before', (tester) async {
    tester.view.physicalSize = const Size(720, 720);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentsApp(source: FakeAgentsSource.demo(), animate: false),
        ),
      ),
    );
    await tester.pump();
    await open(tester, 'checkout-api');
    expect(find.byKey(const Key('agent-pets-card')), findsNothing);
    expect(find.byKey(const Key('agent-hat')), findsOneWidget);
    expect(find.text('tokens out'), findsOneWidget);
  });
}
