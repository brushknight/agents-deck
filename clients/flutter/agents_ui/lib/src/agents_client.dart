import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';

import 'agents_config.dart';
import 'agents_model.dart';

/// Where the link to the agents daemon stands.
enum AgentsLink {
  /// No pairing values on this panel yet.
  unconfigured,

  /// First connect in flight (nothing received yet).
  connecting,

  /// Event stream up; [AgentsSnapshot.state] is live.
  connected,

  /// Lost or refused; retrying with backoff. The last state (if any) is
  /// kept so the grid can show it dimmed.
  offline,
}

/// What the UI renders from: the link, the latest fleet state, and a short
/// reason when something is wrong ("certificate mismatch", "token
/// rejected", "connection refused").
@immutable
class AgentsSnapshot {
  const AgentsSnapshot({required this.link, this.state, this.problem});

  final AgentsLink link;
  final AgentsState? state;
  final String? problem;

  AgentsSnapshot copyWith({
    AgentsLink? link,
    AgentsState? state,
    String? problem,
  }) => AgentsSnapshot(
    link: link ?? this.link,
    state: state ?? this.state,
    problem: problem,
  );
}

enum AgentsResultKind {
  ok,

  /// 409: the prompt was answered/replaced meanwhile.
  conflict,

  /// 400: the key isn't one of the prompt's options.
  rejected,

  /// Network, auth, timeout or server error.
  failed,
}

@immutable
class AgentsResult {
  const AgentsResult(this.kind, [this.message]);

  static const ok = AgentsResult(AgentsResultKind.ok);

  final AgentsResultKind kind;
  final String? message;

  bool get isOk => kind == AgentsResultKind.ok;

  @override
  String toString() => 'AgentsResult($kind, $message)';
}

/// The agents app's only dependency: a live fleet state plus the three
/// commands the daemon accepts. [HttpAgentsClient] talks to `agentctl serve`;
/// `FakeAgentsSource` serves the protocol fixture (tests, demo mode).
abstract class AgentsSource {
  ValueListenable<AgentsSnapshot> get snapshot;

  /// `POST /v1/agents/{id}/answer {"promptId", "key"}`.
  Future<AgentsResult> answer(String agentId, String promptId, String key);

  /// `POST /v1/agents/{id}/focus` — brings its terminal to the front.
  Future<AgentsResult> focus(String agentId);

  /// `POST /v1/agents/{id}/interrupt` — Esc (stops the current turn).
  Future<AgentsResult> interrupt(String agentId);

  /// `POST /v1/agents/{id}/dismiss` — stops the agent if still alive and
  /// removes it from the fleet.
  Future<AgentsResult> dismiss(String agentId);

  /// `POST /v1/agents/{id}/resume` — restarts an exited Claude agent on its
  /// previous conversation.
  Future<AgentsResult> resume(String agentId);

  /// `POST /v1/agents/{id}/move` — puts the agent at board position [slot]
  /// (free or not; an agent already there swaps into the old position).
  Future<AgentsResult> move(String agentId, int slot);

  /// `GET /v1/live` — Claude sessions started by hand in other terminals.
  Future<List<LiveSession>> liveSessions();

  /// `POST /v1/live/{session}/add` — puts one on the deck, at [slot] if given.
  Future<AgentsResult> addLive(String sessionId, {int? slot});

  /// `POST /v1/agents/{id}/hat` — picks the hat of the agent's working
  /// folder ([shape] + optional [color]), or back to the automatic one.
  Future<AgentsResult> setHat(
    String agentId, {
    String? shape,
    String? color,
    bool auto = false,
  });

  /// One-off `GET /v1/state` (after a 409, so the new prompt shows at once).
  Future<void> refresh();

  void dispose();
}

// ---- Server-Sent Events ------------------------------------------------------

@immutable
class SseEvent {
  const SseEvent(this.event, this.data, {this.id});

  /// Event type (`message` when the frame had no `event:` field).
  final String event;
  final String data;
  final String? id;
}

/// Incremental `text/event-stream` parser (WHATWG rules): feed decoded
/// chunks as they arrive — split anywhere, even inside a CRLF — and get
/// back the events they complete. Comment lines (`: ping`) are dropped,
/// multi-line `data:` fields join with `\n`, a frame without data is not
/// dispatched.
class SseParser {
  final _line = StringBuffer();
  final _data = StringBuffer();
  bool _hasData = false;
  String _event = '';
  String? _id;
  bool _skipLf = false;

  List<SseEvent> add(String chunk) {
    final out = <SseEvent>[];
    for (var i = 0; i < chunk.length; i++) {
      final c = chunk.codeUnitAt(i);
      if (_skipLf) {
        _skipLf = false;
        if (c == 0x0A) continue; // second half of a CRLF split across chunks
      }
      if (c == 0x0D || c == 0x0A) {
        _skipLf = c == 0x0D;
        final ev = _endLine(_line.toString());
        _line.clear();
        if (ev != null) out.add(ev);
      } else {
        _line.writeCharCode(c);
      }
    }
    return out;
  }

  SseEvent? _endLine(String line) {
    if (line.isEmpty) return _dispatch();
    if (line.startsWith(':')) return null; // comment / keep-alive
    final colon = line.indexOf(':');
    final field = colon < 0 ? line : line.substring(0, colon);
    var value = colon < 0 ? '' : line.substring(colon + 1);
    if (value.startsWith(' ')) value = value.substring(1);
    switch (field) {
      case 'event':
        _event = value;
      case 'data':
        if (_hasData) _data.write('\n');
        _data.write(value);
        _hasData = true;
      case 'id':
        if (!value.contains('\u0000')) _id = value;
      default:
        break; // retry / unknown fields: ignored
    }
    return null;
  }

  SseEvent? _dispatch() {
    final ev = _hasData
        ? SseEvent(
            _event.isEmpty ? 'message' : _event,
            _data.toString(),
            id: _id,
          )
        : null;
    _data.clear();
    _hasData = false;
    _event = '';
    return ev;
  }
}

/// Reconnect delays: 1 s, 2 s, 4 s, 8 s, then 10 s flat; [reset] after a
/// connection actually delivered state.
class Backoff {
  Backoff({
    this.initial = const Duration(seconds: 1),
    this.max = const Duration(seconds: 10),
  });

  final Duration initial;
  final Duration max;
  int _attempt = 0;

  Duration next() {
    final ms = initial.inMilliseconds * (1 << _attempt.clamp(0, 20));
    if (ms < max.inMilliseconds) _attempt++;
    return Duration(
      milliseconds: ms < max.inMilliseconds ? ms : max.inMilliseconds,
    );
  }

  void reset() => _attempt = 0;
}

// ---- real client -----------------------------------------------------------------

class _Problem implements Exception {
  const _Problem(this.message);
  final String message;
}

/// Live link to `agentctl serve` over HTTPS on the LAN.
///
/// * Server identity: the daemon's self-signed certificate is accepted iff
///   the SHA-256 of its DER bytes equals the paired fingerprint. The client
///   trusts NO certificate authorities (so every certificate reaches the pin
///   check — a CA-valid one included) and verification is never disabled.
/// * `GET /v1/events` (SSE) with the bearer token; every `state` event
///   replaces the snapshot. More than 40 s without a byte (the daemon pings
///   every 15 s) counts as a dead link.
/// * Reconnects with [Backoff] 1 s → 10 s. The pairing file is re-read on
///   every attempt, so values saved from the setup page apply without a
///   restart.
class HttpAgentsClient implements AgentsSource {
  HttpAgentsClient({
    (AgentsConfig?, String?) Function()? loadConfig,
    this.silenceTimeout = const Duration(seconds: 40),
    this.requestTimeout = const Duration(seconds: 5),
    this.unconfiguredPoll = const Duration(seconds: 5),
    Backoff? backoff,
  }) : _loadConfig = loadConfig ?? AgentsConfig.load,
       _backoff = backoff ?? Backoff() {
    unawaited(_run());
  }

  final (AgentsConfig?, String?) Function() _loadConfig;
  final Duration silenceTimeout;
  final Duration requestTimeout;
  final Duration unconfiguredPoll;
  final Backoff _backoff;

  final _snapshot = ValueNotifier(
    const AgentsSnapshot(link: AgentsLink.connecting),
  );

  @override
  ValueListenable<AgentsSnapshot> get snapshot => _snapshot;

  bool _closed = false;
  AgentsConfig? _config;
  String? _configKey;
  HttpClient? _client;

  /// Set by the certificate callback when the server presented a
  /// certificate whose fingerprint isn't the paired one.
  bool _pinMismatch = false;

  StreamSubscription<String>? _sub;
  Completer<void>? _streamDone;
  Timer? _sleepTimer;
  Completer<void>? _sleeping;

  void _set(AgentsSnapshot s) {
    if (!_closed) _snapshot.value = s;
  }

  Future<void> _sleep(Duration d) {
    final c = _sleeping = Completer<void>();
    _sleepTimer = Timer(d, () {
      if (!c.isCompleted) c.complete();
    });
    return c.future;
  }

  HttpClient _clientFor(AgentsConfig cfg) {
    final key = jsonEncode(cfg.toJson());
    if (_client != null && key == _configKey) return _client!;
    _client?.close(force: true);
    _configKey = key;
    // No trusted roots: every chain "fails" verification and lands in the
    // callback, which accepts exactly the pinned certificate.
    final client = HttpClient(context: SecurityContext(withTrustedRoots: false))
      ..connectionTimeout = requestTimeout
      ..idleTimeout = const Duration(seconds: 30)
      ..badCertificateCallback = (cert, host, port) {
        final ok = fingerprintMatches(cert.der, cfg.fingerprint);
        if (!ok) _pinMismatch = true;
        return ok;
      };
    return _client = client;
  }

  void _checkPin(X509Certificate? cert, AgentsConfig cfg) {
    if (cert != null && !fingerprintMatches(cert.der, cfg.fingerprint)) {
      throw const _Problem('certificate mismatch');
    }
  }

  Future<void> _run() async {
    while (!_closed) {
      final (cfg, reason) = _loadConfig();
      _config = cfg;
      if (cfg == null) {
        _client?.close(force: true);
        _client = null;
        _configKey = null;
        _set(AgentsSnapshot(link: AgentsLink.unconfigured, problem: reason));
        await _sleep(unconfiguredPoll);
        continue;
      }
      if (_snapshot.value.link == AgentsLink.unconfigured) {
        _set(const AgentsSnapshot(link: AgentsLink.connecting));
      }
      String problem;
      try {
        await _stream(cfg);
        problem = 'connection closed';
      } catch (e) {
        problem = _describe(e);
      }
      if (_closed) break;
      // Keep the last fleet (the grid dims it) and say why we're retrying.
      _set(
        AgentsSnapshot(
          link: AgentsLink.offline,
          state: _snapshot.value.state,
          problem: problem,
        ),
      );
      await _sleep(_backoff.next());
    }
  }

  Future<void> _stream(AgentsConfig cfg) async {
    _pinMismatch = false;
    final client = _clientFor(cfg);
    final req = await client
        .getUrl(cfg.endpoint(const ['v1', 'events']))
        .timeout(requestTimeout);
    req.headers
      ..set(HttpHeaders.authorizationHeader, 'Bearer ${cfg.token}')
      ..set(HttpHeaders.acceptHeader, 'text/event-stream')
      ..set(HttpHeaders.cacheControlHeader, 'no-cache');
    final res = await req.close().timeout(requestTimeout);
    _checkPin(res.certificate, cfg);
    if (res.statusCode != 200) {
      final body = await _readBody(res);
      throw _Problem(_httpProblem(res.statusCode, body));
    }
    final parser = SseParser();
    final done = _streamDone = Completer<void>();
    Timer? watchdog;
    void arm() {
      watchdog?.cancel();
      watchdog = Timer(silenceTimeout, () {
        if (!done.isCompleted) {
          done.completeError(const _Problem('stream went silent'));
        }
      });
    }

    arm();
    _sub = res
        .transform(const Utf8Decoder(allowMalformed: true))
        .listen(
          (chunk) {
            arm();
            for (final ev in parser.add(chunk)) {
              if (ev.event != 'state') continue;
              try {
                final state = AgentsState.fromJson(jsonDecode(ev.data));
                _backoff.reset();
                _set(AgentsSnapshot(link: AgentsLink.connected, state: state));
              } catch (_) {
                // A malformed frame is skipped; the next one replaces it anyway.
              }
            }
          },
          onError: (Object e) {
            if (!done.isCompleted) done.completeError(e);
          },
          onDone: () {
            if (!done.isCompleted) done.complete();
          },
          cancelOnError: true,
        );
    try {
      await done.future;
    } finally {
      watchdog?.cancel();
      await _sub?.cancel();
      _sub = null;
    }
  }

  Future<String> _readBody(HttpClientResponse res) async {
    try {
      return await res
          .transform(const Utf8Decoder(allowMalformed: true))
          .join()
          .timeout(requestTimeout);
    } catch (_) {
      return '';
    }
  }

  static String? _errorField(String body) {
    try {
      final j = jsonDecode(body);
      if (j is Map && j['error'] is String) return j['error'] as String;
    } catch (_) {}
    return null;
  }

  static String _httpProblem(int code, String body) {
    if (code == 401 || code == 403) return 'token rejected';
    return _errorField(body) ?? 'http $code';
  }

  String _describe(Object e) {
    if (e is _Problem) return e.message;
    if (_pinMismatch) return 'certificate mismatch';
    if (e is TimeoutException) return 'timed out';
    if (e is HandshakeException || e is TlsException) {
      return 'tls handshake failed';
    }
    if (e is SocketException) {
      final m = e.osError?.message ?? e.message;
      return m.isEmpty ? 'unreachable' : m.toLowerCase();
    }
    if (e is HttpException) return e.message;
    return '$e';
  }

  Future<AgentsResult> _post(
    String agentId,
    String verb, [
    Map<String, dynamic>? body,
  ]) => _postPath(['v1', 'agents', agentId, verb], body);

  Future<AgentsResult> _postPath(
    List<String> path, [
    Map<String, dynamic>? body,
  ]) async {
    final cfg = _config;
    if (cfg == null) {
      return const AgentsResult(AgentsResultKind.failed, 'not paired');
    }
    try {
      final req = await _clientFor(
        cfg,
      ).postUrl(cfg.endpoint(path)).timeout(requestTimeout);
      req.headers.set(HttpHeaders.authorizationHeader, 'Bearer ${cfg.token}');
      if (body != null) {
        final bytes = utf8.encode(jsonEncode(body));
        req.headers.contentType = ContentType.json;
        req.contentLength = bytes.length;
        req.add(bytes);
      } else {
        req.contentLength = 0;
      }
      final res = await req.close().timeout(requestTimeout);
      _checkPin(res.certificate, cfg);
      final text = await _readBody(res);
      final code = res.statusCode;
      if (code >= 200 && code < 300) return AgentsResult.ok;
      if (code == 409) {
        return AgentsResult(
          AgentsResultKind.conflict,
          _errorField(text) ?? 'prompt changed',
        );
      }
      if (code == 400) {
        return AgentsResult(
          AgentsResultKind.rejected,
          _errorField(text) ?? 'not an option',
        );
      }
      return AgentsResult(AgentsResultKind.failed, _httpProblem(code, text));
    } catch (e) {
      return AgentsResult(AgentsResultKind.failed, _describe(e));
    }
  }

  @override
  Future<AgentsResult> answer(String agentId, String promptId, String key) =>
      _post(agentId, 'answer', {'promptId': promptId, 'key': key});

  @override
  Future<AgentsResult> focus(String agentId) => _post(agentId, 'focus');

  @override
  Future<AgentsResult> interrupt(String agentId) => _post(agentId, 'interrupt');

  @override
  Future<AgentsResult> dismiss(String agentId) => _post(agentId, 'dismiss');

  @override
  Future<AgentsResult> resume(String agentId) => _post(agentId, 'resume');

  @override
  Future<AgentsResult> move(String agentId, int slot) =>
      _postPath(['v1', 'agents', agentId, 'move'], {'slot': slot});

  @override
  Future<List<LiveSession>> liveSessions() async {
    final cfg = _config;
    if (cfg == null) return const [];
    try {
      final req = await _clientFor(
        cfg,
      ).getUrl(cfg.endpoint(const ['v1', 'live'])).timeout(requestTimeout);
      req.headers.set(HttpHeaders.authorizationHeader, 'Bearer ${cfg.token}');
      final res = await req.close().timeout(requestTimeout);
      _checkPin(res.certificate, cfg);
      final text = await _readBody(res);
      if (res.statusCode != 200) return const [];
      final j = jsonDecode(text);
      final list = j is Map<String, dynamic> ? j['sessions'] : null;
      return [
        if (list is List)
          for (final x in list)
            if (x is Map<String, dynamic>) LiveSession.fromJson(x),
      ];
    } catch (_) {
      return const [];
    }
  }

  @override
  Future<AgentsResult> setHat(
    String agentId, {
    String? shape,
    String? color,
    bool auto = false,
  }) => _postPath([
    'v1',
    'agents',
    agentId,
    'hat',
  ], auto ? {'auto': true} : {'shape': shape, 'color': ?color});

  @override
  Future<AgentsResult> addLive(String sessionId, {int? slot}) => _postPath([
    'v1',
    'live',
    sessionId,
    'add',
  ], slot == null ? null : {'slot': slot});

  @override
  Future<void> refresh() async {
    final cfg = _config;
    if (cfg == null) return;
    try {
      final req = await _clientFor(
        cfg,
      ).getUrl(cfg.endpoint(const ['v1', 'state'])).timeout(requestTimeout);
      req.headers.set(HttpHeaders.authorizationHeader, 'Bearer ${cfg.token}');
      final res = await req.close().timeout(requestTimeout);
      _checkPin(res.certificate, cfg);
      final text = await _readBody(res);
      if (res.statusCode != 200) return;
      final state = AgentsState.fromJson(jsonDecode(text));
      _set(
        _snapshot.value.copyWith(
          state: state,
          problem: _snapshot.value.problem,
        ),
      );
    } catch (_) {
      // The event stream will catch up on its own.
    }
  }

  @override
  void dispose() {
    _closed = true;
    _sleepTimer?.cancel();
    if (_sleeping case final c? when !c.isCompleted) c.complete();
    if (_streamDone case final d? when !d.isCompleted) d.complete();
    unawaited(_sub?.cancel());
    _client?.close(force: true);
    _client = null;
  }
}
