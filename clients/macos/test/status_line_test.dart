import 'dart:convert';
import 'dart:io';

import 'package:agents_deck_menubar/main.dart';
import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

class FakeDeckService implements DeckService {
  FakeDeckService(ServiceStatus s) : _status = ValueNotifier(s);

  final ValueNotifier<ServiceStatus> _status;
  int starts = 0;
  int refreshes = 0;

  @override
  ValueListenable<ServiceStatus> get status => _status;

  set value(ServiceStatus s) => _status.value = s;

  @override
  Future<void> refresh() async => refreshes++;

  @override
  Future<void> start() async => starts++;

  @override
  Future<Uri?> settingsUrl(String page) async => null;

  @override
  set visible(bool v) {}

  @override
  void dispose() {}
}

const _system = DeckSystem(
  version: '0.2.0',
  boardsOnline: 3,
  boardsTotal: 3,
  updateState: 'ready',
  updateVersion: '0.2.1',
);

void main() {
  // The deck's own fonts, so the status line measures its parts as in the app.
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

  group('statusParts', () {
    List<String> texts(ServiceStatus s) => [
      for (final p in statusParts(s)) p.text,
    ];

    test('an older service says only running', () {
      expect(texts(const ServiceStatus(ServiceState.running)), ['running']);
    });

    test('boards, then what wants attention, in order', () {
      const sys = DeckSystem(
        boardsOnline: 2,
        boardsTotal: 3,
        boardsPending: 1,
        newPorts: 1,
        updateState: 'available',
        updateVersion: '0.2.1',
        firmwareUpdates: 2,
      );
      final parts = statusParts(
        const ServiceStatus(ServiceState.running, system: sys),
      );
      expect(
        [for (final p in parts) (p.text, p.page)],
        [
          ('running', 'troubleshoot'),
          ('2 of 3 boards', 'boards'),
          ('update available', 'updates'),
          ('new board on USB', 'boards'),
          ('1 to approve', 'boards'),
          ('2 firmware updates', 'updates'),
        ],
      );
    });

    test('no boards, one board', () {
      expect(
        texts(const ServiceStatus(ServiceState.running, system: DeckSystem())),
        ['running', 'no boards'],
      );
      expect(
        texts(
          const ServiceStatus(
            ServiceState.running,
            system: DeckSystem(boardsOnline: 1, boardsTotal: 1),
          ),
        ),
        ['running', '1 board'],
      );
    });

    test('not running, starting', () {
      expect(texts(const ServiceStatus(ServiceState.notRunning)), [
        'not running',
      ]);
      expect(
        texts(const ServiceStatus(ServiceState.notRunning, starting: true)),
        ['starting'],
      );
    });

    test('a busy Mac, from the CPU guard', () {
      final s = DeckSystem.fromJson(
        jsonDecode(
              '{"platform":"darwin/arm64","boards":{"online":1,"total":1},'
              '"cpu":{"available":true,"percent":91.6,"busy":true,"limit":80}}',
            )
            as Map<String, dynamic>,
      );
      expect((s.cpuBusy, s.cpuPercent), (true, 92));
      final parts = statusParts(ServiceStatus(ServiceState.running, system: s));
      expect(
        [for (final p in parts) (p.text, p.page, p.attention)],
        [
          ('running', 'troubleshoot', false),
          ('1 board', 'boards', false),
          ('Mac busy (92% CPU)', 'agents', true),
        ],
      );
      expect(
        texts(
          const ServiceStatus(
            ServiceState.running,
            system: DeckSystem(
              platform: 'linux/amd64',
              cpuBusy: true,
              cpuPercent: 85,
            ),
          ),
        ),
        ['running', 'no boards', 'computer busy (85% CPU)'],
      );
    });

    test('the system schema', () {
      final s = DeckSystem.fromJson(
        jsonDecode(
              '{"version":"0.2.0","platform":"darwin/arm64","install":"app",'
              '"started":"2026-11-10T10:00:00Z",'
              '"boards":{"online":2,"total":3,"pending":0},"ports":{"new":1},'
              '"update":{"state":"ready","version":"0.2.1"},'
              '"firmware_updates":1}',
            )
            as Map<String, dynamic>,
      );
      expect(s.version, '0.2.0');
      expect(s.install, 'app');
      expect((s.boardsOnline, s.boardsTotal, s.boardsPending), (2, 3, 0));
      expect(s.newPorts, 1);
      expect((s.updateState, s.updateVersion), ('ready', '0.2.1'));
      expect(s.firmwareUpdates, 1);
    });
  });

  group('the popover', () {
    late List<String> opened;

    Future<FakeDeckService> pump(WidgetTester tester, ServiceStatus s) async {
      tester.view.physicalSize = const Size(880, 880 + 2 * statusLineHeight);
      tester.view.devicePixelRatio = 2;
      addTearDown(tester.view.reset);
      opened = [];
      final service = FakeDeckService(s);
      await tester.pumpWidget(
        MenuBarDeck(
          source: FakeAgentsSource.demo(),
          visible: ValueNotifier(true),
          service: service,
          onSettings: opened.add,
        ),
      );
      await tester.pump();
      return service;
    }

    testWidgets('running, older service: running and the gear', (tester) async {
      await pump(tester, const ServiceStatus(ServiceState.running));
      expect(find.text('running'), findsOneWidget);
      expect(find.textContaining('board'), findsNothing);
      expect(find.byType(AgentsApp), findsOneWidget);
      await tester.tap(find.byKey(const Key('status-gear')));
      expect(opened, ['general']);
      await tester.tap(find.text('running'));
      expect(opened, ['general', 'troubleshoot']);
    });

    testWidgets('update ready opens Updates', (tester) async {
      await pump(
        tester,
        const ServiceStatus(ServiceState.running, system: _system),
      );
      expect(find.text('· 3 boards'), findsOneWidget);
      await tester.tap(find.text('· update ready'));
      expect(opened, ['updates']);
    });

    testWidgets('a new board opens Boards', (tester) async {
      await pump(
        tester,
        const ServiceStatus(
          ServiceState.running,
          system: DeckSystem(boardsOnline: 1, boardsTotal: 1, newPorts: 1),
        ),
      );
      await tester.tap(find.text('· new board on USB'));
      await tester.tap(find.text('· 1 board'));
      expect(opened, ['boards', 'boards']);
    });

    testWidgets('no Pin button where the shell can\'t pin', (tester) async {
      await pump(tester, const ServiceStatus(ServiceState.running));
      expect(find.byKey(const Key('status-pin')), findsNothing);
      expect(find.byKey(const Key('status-drag')), findsNothing);
    });

    testWidgets('Pin keeps the deck on top; pinned, the line moves it', (
      tester,
    ) async {
      tester.view.physicalSize = const Size(880, 880 + 2 * statusLineHeight);
      tester.view.devicePixelRatio = 2;
      addTearDown(tester.view.reset);
      final pinned = ValueNotifier(false);
      final asked = <bool>[];
      var drags = 0;
      await tester.pumpWidget(
        MenuBarDeck(
          source: FakeAgentsSource.demo(),
          visible: ValueNotifier(true),
          service: FakeDeckService(
            const ServiceStatus(ServiceState.running, system: _system),
          ),
          pinned: pinned,
          onPin: (on) {
            asked.add(on);
            pinned.value = on;
          },
          onDrag: () => drags++,
        ),
      );
      await tester.pump();
      expect(find.text('Pin'), findsOneWidget);
      expect(find.byKey(const Key('status-drag')), findsNothing);

      await tester.tap(find.byKey(const Key('status-pin')));
      await tester.pump();
      expect(asked, [true]);
      expect(find.text('Pinned'), findsOneWidget);
      expect(find.byKey(const Key('status-gear')), findsOneWidget);

      await tester.drag(find.text('running'), const Offset(-60, 20));
      expect(drags, 1);

      await tester.tap(find.byKey(const Key('status-pin')));
      await tester.pump();
      expect(asked, [true, false]);
      expect(find.text('Pin'), findsOneWidget);
      expect(find.byKey(const Key('status-drag')), findsNothing);
    });

    testWidgets('what does not fit becomes +N', (tester) async {
      await pump(
        tester,
        const ServiceStatus(
          ServiceState.running,
          system: DeckSystem(
            boardsOnline: 2,
            boardsTotal: 3,
            boardsPending: 2,
            newPorts: 3,
            updateState: 'available',
            firmwareUpdates: 2,
          ),
        ),
      );
      expect(find.text('· 2 of 3 boards'), findsOneWidget);
      final more = find.textContaining(RegExp(r'^· \+\d$'));
      expect(more, findsOneWidget);
      await tester.tap(more);
      expect(opened, hasLength(1));
    });

    testWidgets('not running, with a bundled agentctl: Start', (tester) async {
      final service = await pump(
        tester,
        const ServiceStatus(ServiceState.notRunning, canStart: true),
      );
      expect(find.text('not running'), findsOneWidget);
      expect(find.byKey(const Key('status-gear')), findsNothing);
      expect(find.byType(AgentsApp), findsNothing);
      expect(find.text('the service isn\'t running'), findsOneWidget);
      await tester.tap(find.byKey(const Key('start-service')));
      expect(service.starts, 1);
      await tester.tap(find.text('not running'));
      expect(opened, isEmpty); // no Settings without the service

      service.value = const ServiceStatus(
        ServiceState.notRunning,
        canStart: true,
        starting: true,
      );
      await tester.pump();
      expect(find.text('starting…'), findsOneWidget);
      await tester.tap(find.byKey(const Key('start-service')));
      expect(service.starts, 1);

      service.value = const ServiceStatus(ServiceState.running);
      await tester.pump();
      expect(find.byType(AgentsApp), findsOneWidget);
    });

    testWidgets('not running, nothing bundled: how to start it', (
      tester,
    ) async {
      await pump(tester, const ServiceStatus(ServiceState.notRunning));
      expect(find.byKey(const Key('start-service')), findsNothing);
      expect(find.textContaining('install it and start it'), findsOneWidget);
    });

    testWidgets(
      'the status line at menu bar size',
      skip: !autoUpdateGoldenFiles,
      (tester) async {
        await pump(
          tester,
          const ServiceStatus(
            ServiceState.running,
            system: DeckSystem(
              boardsOnline: 3,
              boardsTotal: 3,
              newPorts: 1,
              updateState: 'ready',
              updateVersion: '0.2.1',
            ),
          ),
        );
        await expectLater(
          find.byType(MenuBarDeck),
          matchesGoldenFile('goldens/menubar-status.png'),
        );
      },
    );
  });
}
