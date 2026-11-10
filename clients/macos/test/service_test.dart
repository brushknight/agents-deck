import 'dart:io';

import 'package:agents_deck_menubar/main.dart';
import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter_test/flutter_test.dart';

// Plain tests (no testWidgets here): the widget test binding replaces
// HttpClient with one that never connects.
void main() {
  group('LocalDeckService', () {
    late Directory dir;
    late Map<String, String> env;
    HttpServer? server;

    setUp(() async {
      // A short path: unix socket paths are capped near 104 bytes.
      dir = await Directory('/tmp').createTemp('deck');
      env = {'AGENTSTERM_HOME': dir.path, 'TMPDIR': '/tmp'};
    });

    tearDown(() async {
      await server?.close(force: true);
      server = null;
      await dir.delete(recursive: true);
    });

    Future<void> serve(void Function(HttpRequest) handle) async {
      server = await HttpServer.bind(
        InternetAddress(
          daemonSocketPath(dir.path, env: env),
          type: InternetAddressType.unix,
        ),
        0,
      );
      server!.listen(handle);
    }

    LocalDeckService client({RunProcess? run, String? agentctl}) =>
        LocalDeckService(
          env: env,
          agentctl: agentctl,
          findAgentctl: false,
          run: run,
          autoStart: run != null,
          startWait: const Duration(seconds: 3),
        );

    Future<ServiceStatus> settled(LocalDeckService s) async {
      for (var i = 0; i < 100; i++) {
        if (s.status.value.state != ServiceState.checking &&
            !s.status.value.starting) {
          break;
        }
        await Future<void>.delayed(const Duration(milliseconds: 20));
      }
      return s.status.value;
    }

    test('reads /admin/v1/system', () async {
      await serve((r) {
        expect(r.uri.path, '/admin/v1/system');
        r.response
          ..headers.contentType = ContentType.json
          ..write('{"boards":{"online":2,"total":2},"update":{"state":"none"}}')
          ..close();
      });
      final s = client();
      addTearDown(s.dispose);
      final st = await settled(s);
      expect(st.state, ServiceState.running);
      expect(st.system?.boardsOnline, 2);
    });

    test('404 is an older service: running, nothing more', () async {
      await serve((r) {
        r.response
          ..statusCode = 404
          ..close();
      });
      final s = client();
      addTearDown(s.dispose);
      final st = await settled(s);
      expect(st.state, ServiceState.running);
      expect(st.system, isNull);
    });

    test('no socket: not running', () async {
      final s = client();
      addTearDown(s.dispose);
      final st = await settled(s);
      expect(st.state, ServiceState.notRunning);
      expect(st.canStart, isFalse);
    });

    test(
      'a bundled agentctl is installed once, then the socket answers',
      () async {
        final calls = <List<String>>[];
        final s = client(
          agentctl: '/opt/deck/agentctl',
          run: (exe, args) async {
            calls.add([exe, ...args]);
            await serve((r) {
              r.response
                ..statusCode = 404
                ..close();
            });
            return ProcessResult(0, 0, '', '');
          },
        );
        addTearDown(s.dispose);
        expect(s.status.value.canStart, isTrue);
        await Future<void>.delayed(const Duration(milliseconds: 50));
        final st = await settled(s);
        expect(calls, [
          ['/opt/deck/agentctl', 'install'],
        ]);
        expect(st.state, ServiceState.running);
        expect(st.problem, isNull);
      },
    );

    test('a failed install says why', () async {
      final s = client(
        agentctl: '/opt/deck/agentctl',
        run: (exe, args) async =>
            ProcessResult(0, 1, '', 'agentctl: launchctl said no\n'),
      );
      addTearDown(s.dispose);
      await Future<void>.delayed(const Duration(milliseconds: 50));
      final st = await settled(s);
      expect(st.state, ServiceState.notRunning);
      expect(st.problem, 'agentctl: launchctl said no');
    });

    test('settingsUrl: a login link to the Settings page', () async {
      File(
        '${dir.path}/config.json',
      ).writeAsStringSync('{"localAddr":"127.0.0.1:7999"}');
      await serve((r) {
        if (r.uri.path != '/local/login-code') {
          r.response
            ..statusCode = 404
            ..close();
          return;
        }
        expect(r.method, 'POST');
        r.response
          ..headers.contentType = ContentType.json
          ..write('{"code":"abc123"}')
          ..close();
      });
      final s = client();
      addTearDown(s.dispose);
      final url = await s.settingsUrl('boards');
      expect(
        url.toString(),
        'http://127.0.0.1:7999/login?code=abc123&next=%2Fsettings#boards',
      );
      expect((await s.settingsUrl('nope'))?.fragment, 'general');
    });

    test('settingsUrl: never off loopback', () async {
      File(
        '${dir.path}/config.json',
      ).writeAsStringSync('{"localAddr":"192.0.2.7:7340"}');
      await serve((r) {
        r.response
          ..write('{"code":"c"}')
          ..close();
      });
      final s = client();
      addTearDown(s.dispose);
      expect((await s.settingsUrl('general'))?.host, '127.0.0.1');
    });

    test('settingsUrl: null when the service is down', () async {
      final s = client();
      addTearDown(s.dispose);
      expect(await s.settingsUrl('general'), isNull);
    });
  });

  test('bundledAgentctl looks next to the executable', () async {
    final dir = await Directory.systemTemp.createTemp('deck');
    addTearDown(() => dir.delete(recursive: true));
    final exe = '${dir.path}/agents deck';
    expect(bundledAgentctl(executable: exe, windows: false), isNull);
    File('${dir.path}/agentctl').writeAsStringSync('');
    expect(
      bundledAgentctl(executable: exe, windows: false),
      '${dir.path}/agentctl',
    );
    expect(bundledAgentctl(executable: exe, windows: true), isNull);
  });

  test('deckDataDir', () {
    expect(deckDataDir({'AGENTSTERM_HOME': '/x'}), '/x');
    expect(
      deckDataDir({'HOME': '/home/someone'}, windows: false),
      '/home/someone/.local/share/agents-terminal',
    );
    expect(
      deckDataDir({
        'LOCALAPPDATA': r'C:\Users\someone\AppData\Local',
      }, windows: true),
      r'C:\Users\someone\AppData\Local\agents-deck',
    );
  });
}
