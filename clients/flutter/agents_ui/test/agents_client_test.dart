import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter_test/flutter_test.dart';

/// Protocol fixture (agents-terminal docs/fixtures/state.json, verbatim).
final _fixture = File('test/fixtures/agents_state.json').readAsStringSync();

void main() {
  test('an agent lost with tmux says so on its tile and card', () {
    final fixture = jsonDecode(_fixture) as Map<String, dynamic>;
    final first = (fixture['agents'] as List).first as Map<String, dynamic>;
    first.addAll({'status': 'exited', 'lost': true, 'resumable': true});
    final s = AgentsState.fromJson(fixture);
    final a = s.agents.first;
    expect(a.lost, isTrue);
    expect(agentMetaLine(a, s), 'lost with tmux');
    expect(agentPillLabel(a, s), startsWith('lost with tmux · '));
  });

  group('SseParser', () {
    test('parses a state frame and skips ping comments', () {
      final p = SseParser();
      final evs = p.add(': ping\n\nevent: state\ndata: {"a":1}\n\n: ping\n\n');
      expect(evs, hasLength(1));
      expect(evs.single.event, 'state');
      expect(evs.single.data, '{"a":1}');
    });

    test('joins multi-line data with newlines; strips one leading space', () {
      final p = SseParser();
      final evs = p.add('event: state\ndata: {\ndata:  "x": 1\ndata: }\n\n');
      expect(evs.single.data, '{\n "x": 1\n}');
    });

    test('frames split at arbitrary points, CRLF split across chunks', () {
      const wire =
          'event: state\r\ndata: {"agents":[]}\r\n\r\n'
          'data: second\r\n\r\n';
      for (var cut = 1; cut < wire.length; cut++) {
        final p = SseParser();
        final evs = [
          ...p.add(wire.substring(0, cut)),
          ...p.add(wire.substring(cut)),
        ];
        expect(evs.map((e) => '${e.event}|${e.data}'), [
          'state|{"agents":[]}',
          'message|second',
        ], reason: 'cut at $cut');
      }
    });

    test('one byte at a time', () {
      final p = SseParser();
      final evs = <SseEvent>[];
      for (final ch in 'id: 7\nevent: state\ndata: hi\n\n'.split('')) {
        evs.addAll(p.add(ch));
      }
      expect(evs.single.data, 'hi');
      expect(evs.single.id, '7');
    });

    test('a frame without data is not dispatched; event type resets', () {
      final p = SseParser();
      expect(p.add('event: state\n\n'), isEmpty);
      expect(p.add('data: x\n\n').single.event, 'message');
    });

    test('lone CR line endings', () {
      final p = SseParser();
      expect(p.add('event: state\rdata: y\r\r').single.data, 'y');
    });
  });

  group('Backoff', () {
    test('1s -> 2s -> 4s -> 8s -> 10s cap, reset to 1s', () {
      final b = Backoff();
      expect(
        [for (var i = 0; i < 7; i++) b.next().inSeconds],
        [1, 2, 4, 8, 10, 10, 10],
      );
      b.reset();
      expect(b.next(), const Duration(seconds: 1));
    });
  });

  group('fingerprint', () {
    final digest = sha256.convert(utf8.encode('cert')).bytes;
    final hex = digest.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
    final colon = [
      for (var i = 0; i < hex.length; i += 2)
        hex.substring(i, i + 2).toUpperCase(),
    ].join(':');

    test('normalizes prefix, case, colons and spaces', () {
      for (final form in [
        'sha256:$colon',
        'SHA256:${colon.toLowerCase()}',
        hex,
        hex.toUpperCase(),
        '  sha256:${colon.replaceAll(':', ' ')}  ',
        'SHA256 Fingerprint=$colon'.replaceFirst(
          'SHA256 Fingerprint=',
          'sha256:',
        ),
      ]) {
        expect(parseFingerprint(form), digest, reason: form);
      }
      expect(formatFingerprint(digest), 'sha256:$colon');
    });

    test('rejects malformed values', () {
      for (final bad in [
        '',
        'sha256:',
        'sha256:AB:CD',
        '${hex}00',
        'zz${hex.substring(2)}',
      ]) {
        expect(parseFingerprint(bad), isNull, reason: bad);
      }
    });

    test('matches only the exact certificate', () {
      expect(fingerprintMatches(utf8.encode('cert'), digest), isTrue);
      expect(fingerprintMatches(utf8.encode('cert2'), digest), isFalse);
      expect(
        fingerprintMatches(utf8.encode('cert'), digest.sublist(1)),
        isFalse,
      );
    });
  });

  group('AgentsConfig', () {
    final fp = 'sha256:${List.filled(32, 'AB').join(':')}';

    test('parse normalizes the url and validates', () {
      final c = AgentsConfig.parse(
        url: 'https://192.0.2.10:7341/',
        token: ' t0k ',
        fingerprint: fp,
      );
      expect(c.url.toString(), 'https://192.0.2.10:7341');
      expect(c.token, 't0k');
      expect(
        c.endpoint(['v1', 'agents', 'a/b', 'answer']).toString(),
        'https://192.0.2.10:7341/v1/agents/a%2Fb/answer',
      );
      expect(
        () =>
            AgentsConfig.parse(url: 'http://x:1', token: 't', fingerprint: fp),
        throwsFormatException,
      );
      expect(
        () =>
            AgentsConfig.parse(url: 'https://x:1', token: '', fingerprint: fp),
        throwsFormatException,
      );
      expect(
        () => AgentsConfig.parse(
          url: 'https://x:1',
          token: 't',
          fingerprint: 'sha256:AB',
        ),
        throwsFormatException,
      );
    });

    test('load: env triple wins, then file, else unconfigured', () async {
      final tmp = await Directory.systemTemp.createTemp('agentscfg');
      addTearDown(() => tmp.delete(recursive: true));
      final file = File('${tmp.path}/agents.json')
        ..writeAsStringSync(
          jsonEncode({
            'url': 'https://mac:7341',
            'token': 'f',
            'fingerprint': fp,
          }),
        );

      var (cfg, why) = AgentsConfig.load(
        env: const {},
        paths: ['${tmp.path}/none.json'],
      );
      expect(cfg, isNull);
      expect(why, isNull);

      (cfg, why) = AgentsConfig.load(env: const {}, paths: [file.path]);
      expect(cfg!.url.host, 'mac');

      (cfg, why) = AgentsConfig.load(
        env: {
          'AGENTS_URL': 'https://env:1',
          'AGENTS_TOKEN': 'e',
          'AGENTS_FINGERPRINT': fp,
        },
        paths: [file.path],
      );
      expect(cfg!.url.host, 'env');

      (cfg, why) = AgentsConfig.load(
        env: {'AGENTS_CONFIG': file.path, 'HOME': '/nope'},
      );
      expect(cfg!.token, 'f');

      file.writeAsStringSync('{"url": "https://mac:7341"}');
      (cfg, why) = AgentsConfig.load(env: const {}, paths: [file.path]);
      expect(cfg, isNull);
      expect(why, contains('token'));
    });
  });

  group('model', () {
    test('the embedded demo fleet is the protocol fixture', () {
      expect(jsonDecode(agentsDemoStateJson), jsonDecode(_fixture));
    });

    test('fixture parses: 11 agents by slot, prompts, errors', () {
      final s = AgentsState.fromJson(jsonDecode(_fixture));
      expect(s.agents, hasLength(11));
      // checkout-api has three subagents at work; the rest none.
      final subs = s.agents.first.subagents;
      expect(subs.map((x) => x.tool), ['Grep', 'Edit', 'WebFetch']);
      expect(subs.first.title, 'map the payment retry paths');
      expect(subs.first.type, 'Explore');
      expect(s.agents.skip(1).every((a) => a.subagents.isEmpty), isTrue);
      // Slots can leave gaps: scratch sits alone in the bottom-right corner.
      expect(s.agents.map((a) => a.slot), [
        ...List.generate(9, (i) => i),
        10,
        15,
      ]);
      expect(s.server.name, 'workstation');
      expect(s.waitingCount, 2);
      final network = s.byId('h0m3l4')!;
      expect(network.waiting!.kind, PromptKind.permission);
      expect(network.waiting!.options.first.primary, isTrue);
      expect(s.byId('1nfr4t')!.errorMessage, 'api 529 overloaded · retrying');
      expect(s.byId('t3st5a')!.focused, isTrue);
    });

    test('meta lines per status', () {
      final s = AgentsState.fromJson(jsonDecode(_fixture));
      final now = s.receivedAt;
      String meta(String id) => agentMetaLine(s.byId(id)!, s, now);
      expect(meta('k3f9a2'), 'edit · 41% ctx');
      expect(meta('fw0t4x'), 'plan · 27% ctx'); // TodoWrite
      expect(meta('h0m3l4'), 'allow bash?');
      expect(meta('4uthfl'), 'pick 1 of 3');
      expect(meta('d0cs1t'), 'hungry · feed me'); // finished, not yet reviewed
      expect(meta('h4ptic'), 'done · 0s ago');
      expect(meta('1nfr4t'), 'api 529 overloaded · retrying');
    });

    test('durations follow the daemon clock, not ours', () {
      final s = AgentsState.fromJson(
        jsonDecode(_fixture),
        receivedAt: DateTime.utc(2030),
      ); // panel clock far off
      final a = s.byId('k3f9a2')!; // statusSince == server.time
      expect(
        s.inStatus(a, DateTime.utc(2030).add(const Duration(minutes: 38))),
        const Duration(minutes: 38),
      );
      expect(
        agentPillLabel(a, s, DateTime.utc(2030, 1, 1, 0, 38)),
        'running · 38m',
      );
    });

    test('compact counts and paths', () {
      expect([900, 1500, 84000, 410000, 1000000, 3100000].map(compactCount), [
        '900',
        '1.5k',
        '84k',
        '410k',
        '1m',
        '3.1m',
      ]);
      expect(tildePath('/Users/x/dev/checkout-api'), '~/dev/checkout-api');
      expect(tildePath('/home/rpi/a'), '~/a');
      expect(tildePath('/srv/a'), '/srv/a');
      expect(shortDuration(const Duration(minutes: 65)), '1h');
    });

    test('garbage is a FormatException, missing fields degrade', () {
      expect(() => AgentsState.fromJson('nope'), throwsFormatException);
      final s = AgentsState.fromJson({
        'agents': [
          {'id': 'x', 'status': 'brand-new'},
        ],
      });
      expect(s.agents.single.status, AgentStatus.unknown);
      expect(s.agents.single.contextFraction, 0);
    });
  });

  group('HttpAgentsClient over TLS', () {
    late Directory tmp;
    late HttpServer server;
    late String goodFp;
    final answers = <Map<String, dynamic>>[];
    var answerStatus = 204;
    var sendState = true;
    final streams = <HttpResponse>[];

    setUpAll(() async {
      tmp = await Directory.systemTemp.createTemp('agentstls');
      final r = await Process.run('openssl', [
        'req',
        '-x509',
        '-newkey',
        'rsa:2048',
        '-nodes',
        '-days',
        '1',
        '-keyout',
        '${tmp.path}/key.pem',
        '-out',
        '${tmp.path}/cert.pem',
        '-subj',
        '/CN=localhost',
        '-addext',
        'subjectAltName=IP:127.0.0.1',
      ]);
      expect(r.exitCode, 0, reason: '${r.stderr}');
      final fp = await Process.run('openssl', [
        'x509',
        '-in',
        '${tmp.path}/cert.pem',
        '-noout',
        '-fingerprint',
        '-sha256',
      ]);
      // "sha256 Fingerprint=AB:CD:…" — the same text a user might paste.
      goodFp = 'sha256:${(fp.stdout as String).split('=').last.trim()}';
      final ctx = SecurityContext()
        ..useCertificateChain('${tmp.path}/cert.pem')
        ..usePrivateKey('${tmp.path}/key.pem');
      server = await HttpServer.bindSecure(
        InternetAddress.loopbackIPv4,
        0,
        ctx,
      );
      server.listen((req) async {
        final res = req.response;
        if (req.headers.value('authorization') != 'Bearer tok') {
          res.statusCode = 401;
          res.write('{"error":"bad token"}');
          return res.close();
        }
        if (req.uri.path == '/v1/events') {
          res.headers.contentType = ContentType(
            'text',
            'event-stream',
            charset: 'utf-8',
          );
          res.bufferOutput = false;
          streams.add(res);
          if (sendState) {
            res.write(
              ': ping\n\nevent: state\ndata: ${_fixture.replaceAll('\n', '\ndata: ')}\n\n',
            );
            await res.flush();
          }
          return; // held open
        }
        if (req.uri.path == '/v1/agents/h0m3l4/answer' &&
            req.method == 'POST') {
          answers.add(
            jsonDecode(await utf8.decoder.bind(req).join())
                as Map<String, dynamic>,
          );
          res.statusCode = answerStatus;
          if (answerStatus == 409) res.write('{"error":"prompt changed"}');
          return res.close();
        }
        res.statusCode = 404;
        await res.close();
      });
    });

    tearDownAll(() async {
      for (final s in streams) {
        try {
          await s.close();
        } catch (_) {}
      }
      await server.close(force: true);
      await tmp.delete(recursive: true);
    });

    AgentsConfig cfg({String? fingerprint, String token = 'tok'}) =>
        AgentsConfig.parse(
          url: 'https://127.0.0.1:${server.port}',
          token: token,
          fingerprint: fingerprint ?? goodFp,
        );

    Future<AgentsSnapshot> until(
      HttpAgentsClient c,
      bool Function(AgentsSnapshot) ok,
    ) async {
      final end = DateTime.now().add(const Duration(seconds: 10));
      while (!ok(c.snapshot.value)) {
        if (DateTime.now().isAfter(end)) {
          fail(
            'timed out at ${c.snapshot.value.link} ${c.snapshot.value.problem}',
          );
        }
        await Future<void>.delayed(const Duration(milliseconds: 20));
      }
      return c.snapshot.value;
    }

    test('pinned cert: streams state, posts answers, maps 409', () async {
      final c = HttpAgentsClient(loadConfig: () => (cfg(), null));
      addTearDown(c.dispose);
      final s = await until(c, (s) => s.link == AgentsLink.connected);
      expect(s.state!.agents, hasLength(11));

      answerStatus = 204;
      expect((await c.answer('h0m3l4', 'p17', '1')).isOk, isTrue);
      expect(answers.last, {'promptId': 'p17', 'key': '1'});

      answerStatus = 409;
      final r = await c.answer('h0m3l4', 'p16', '1');
      expect(r.kind, AgentsResultKind.conflict);
      answerStatus = 204;
    });

    test('a different certificate is refused (no fallback to CAs)', () async {
      final wrong = 'sha256:${List.filled(32, '00').join(':')}';
      final c = HttpAgentsClient(
        loadConfig: () => (cfg(fingerprint: wrong), null),
        backoff: Backoff(initial: const Duration(milliseconds: 50)),
      );
      addTearDown(c.dispose);
      final s = await until(c, (s) => s.link == AgentsLink.offline);
      expect(s.problem, 'certificate mismatch');
      expect(s.state, isNull);
      final r = await c.focus('h0m3l4');
      expect(r.kind, AgentsResultKind.failed);
    });

    test('a rejected token reads as such', () async {
      final c = HttpAgentsClient(
        loadConfig: () => (cfg(token: 'nope'), null),
        backoff: Backoff(initial: const Duration(milliseconds: 50)),
      );
      addTearDown(c.dispose);
      final s = await until(c, (s) => s.link == AgentsLink.offline);
      expect(s.problem, 'token rejected');
    });

    test(
      'silence past the watchdog drops the link, keeping the last state',
      () async {
        final c = HttpAgentsClient(
          loadConfig: () => (cfg(), null),
          silenceTimeout: const Duration(milliseconds: 300),
          backoff: Backoff(initial: const Duration(seconds: 5)),
        );
        addTearDown(c.dispose);
        await until(c, (s) => s.link == AgentsLink.connected);
        final s = await until(c, (s) => s.link == AgentsLink.offline);
        expect(s.problem, 'stream went silent');
        expect(
          s.state!.agents,
          hasLength(11),
          reason: 'grid keeps the last fleet, dimmed',
        );
      },
    );

    test('no pairing values: unconfigured, then picks them up', () async {
      AgentsConfig? current;
      final c = HttpAgentsClient(
        loadConfig: () => (current, null),
        unconfiguredPoll: const Duration(milliseconds: 50),
      );
      addTearDown(c.dispose);
      await until(c, (s) => s.link == AgentsLink.unconfigured);
      current = cfg();
      await until(c, (s) => s.link == AgentsLink.connected);
    });
  });
}
