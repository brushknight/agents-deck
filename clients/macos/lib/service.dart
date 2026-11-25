import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/foundation.dart';

/// The pages of the Settings window (`<dashboard>/settings#<page>`).
const settingsPages = [
  'general',
  'agents',
  'boards',
  'network',
  'updates',
  'troubleshoot',
  'about',
];

/// What the service says about itself: `GET /admin/v1/system` on its socket.
@immutable
class DeckSystem {
  const DeckSystem({
    this.version = '',
    this.platform = '',
    this.install = '',
    this.boardsOnline = 0,
    this.boardsTotal = 0,
    this.boardsPending = 0,
    this.newPorts = 0,
    this.updateState = 'none',
    this.updateVersion = '',
    this.firmwareUpdates = 0,
    this.cpuBusy = false,
    this.cpuPercent = 0,
  });

  factory DeckSystem.fromJson(Map<String, dynamic> j) {
    Map<String, dynamic> obj(String k) =>
        j[k] is Map<String, dynamic> ? j[k] as Map<String, dynamic> : const {};
    int n(Map<String, dynamic> m, String k) =>
        m[k] is num ? (m[k] as num).toInt() : 0;
    String s(Map<String, dynamic> m, String k) =>
        m[k] is String ? m[k] as String : '';
    final boards = obj('boards');
    final update = obj('update');
    final cpu = obj('cpu');
    return DeckSystem(
      version: s(j, 'version'),
      platform: s(j, 'platform'),
      install: s(j, 'install'),
      boardsOnline: n(boards, 'online'),
      boardsTotal: n(boards, 'total'),
      boardsPending: n(boards, 'pending'),
      newPorts: n(obj('ports'), 'new'),
      updateState: s(update, 'state').isEmpty ? 'none' : s(update, 'state'),
      updateVersion: s(update, 'version'),
      firmwareUpdates: n(j, 'firmware_updates'),
      cpuBusy: cpu['busy'] == true,
      cpuPercent: cpu['percent'] is num ? (cpu['percent'] as num).round() : 0,
    );
  }

  final String version;
  final String platform;

  /// How the app was installed: app, windows, deb, archive, source.
  final String install;
  final int boardsOnline;
  final int boardsTotal;

  /// Boards waiting for you to approve them.
  final int boardsPending;

  /// Boards plugged in over USB that the deck doesn't know yet.
  final int newPorts;

  /// none, available, ready, installing or disabled.
  final String updateState;
  final String updateVersion;
  final int firmwareUpdates;

  /// The CPU guard: the whole computer has been over its CPU limit for a
  /// while (`cpu.busy`), and how busy it is (`cpu.percent`, every core: 100).
  final bool cpuBusy;
  final int cpuPercent;
}

enum ServiceState {
  /// Not asked yet.
  checking,

  /// The socket answers.
  running,

  /// No socket, or nothing answers on it.
  notRunning,
}

@immutable
class ServiceStatus {
  const ServiceStatus(
    this.state, {
    this.system,
    this.canStart = false,
    this.starting = false,
    this.problem,
  });

  final ServiceState state;

  /// Null while not running, and on an older service without
  /// `/admin/v1/system` (it answers 404): then only "running" is known.
  final DeckSystem? system;

  /// An `agentctl` came with the app, so Start can install the service.
  final bool canStart;

  /// `agentctl install` is running, or the app waits for the socket after it.
  final bool starting;

  /// Why the last Start failed.
  final String? problem;

  ServiceStatus copyWith({
    ServiceState? state,
    DeckSystem? system,
    bool clearSystem = false,
    bool? canStart,
    bool? starting,
    String? problem,
    bool clearProblem = false,
  }) => ServiceStatus(
    state ?? this.state,
    system: clearSystem ? null : (system ?? this.system),
    canStart: canStart ?? this.canStart,
    starting: starting ?? this.starting,
    problem: clearProblem ? null : (problem ?? this.problem),
  );
}

/// The service behind the deck, as the status line and the Settings window
/// need it. [LocalDeckService] talks to `agentctl` on this computer.
abstract class DeckService {
  ValueListenable<ServiceStatus> get status;

  /// Asks the service again now.
  Future<void> refresh();

  /// Installs and starts the service with the `agentctl` that came with the
  /// app, then waits for it to answer.
  Future<void> start();

  /// A logged-in address of the dashboard's Settings page on [page], the
  /// way `agentctl web` logs the browser in; null when the service doesn't
  /// answer.
  Future<Uri?> settingsUrl(String page);

  /// Whether the deck is on screen (it polls faster then).
  set visible(bool v);

  void dispose();
}

/// Where `agentctl` keeps its files on this computer: `$AGENTSTERM_HOME`, else
/// `~/.local/share/agents-terminal` (macOS, Linux) or
/// `%LOCALAPPDATA%\agents-deck` (Windows).
String? deckDataDir(Map<String, String> env, {bool? windows}) {
  if (env['AGENTSTERM_HOME'] case final h? when h.isNotEmpty) return h;
  if (windows ?? Platform.isWindows) {
    final local = env['LOCALAPPDATA'];
    return local == null ? null : '$local\\agents-deck';
  }
  final home = env['HOME'];
  return home == null ? null : '$home/.local/share/agents-terminal';
}

/// The `agentctl` that ships next to the app's own executable, if any
/// (macOS: `agents deck.app/Contents/MacOS/agentctl`; Linux: the bundle
/// folder; Windows: the install folder, `agentctl.exe`).
String? bundledAgentctl({String? executable, bool? windows}) {
  final exe = File(executable ?? Platform.resolvedExecutable);
  final name = (windows ?? Platform.isWindows) ? 'agentctl.exe' : 'agentctl';
  final path = '${exe.parent.path}${Platform.pathSeparator}$name';
  try {
    return File(path).statSync().type == FileSystemEntityType.file
        ? path
        : null;
  } catch (_) {
    return null;
  }
}

/// Runs a program and returns its exit code and output (`Process.run`).
typedef RunProcess =
    Future<ProcessResult> Function(String exe, List<String> args);

/// The service on this computer, over its unix socket: `GET
/// /admin/v1/system` for the status line, `POST /local/login-code` for the
/// Settings window. Polls gently: every [pollVisible] while the deck is on
/// screen, [pollHidden] while it isn't, [pollDown] while nothing answers.
class LocalDeckService implements DeckService {
  LocalDeckService({
    Map<String, String>? env,
    String? agentctl,
    bool findAgentctl = true,
    RunProcess? run,
    this.pollVisible = const Duration(seconds: 10),
    this.pollHidden = const Duration(seconds: 60),
    this.pollDown = const Duration(seconds: 5),
    this.startWait = const Duration(seconds: 20),
    this.requestTimeout = const Duration(seconds: 3),
    bool autoStart = true,
  }) : _env = env ?? Platform.environment,
       _run = run ?? Process.run,
       _autoStart = autoStart {
    _agentctl = agentctl ?? (findAgentctl ? bundledAgentctl() : null);
    _status = ValueNotifier(
      ServiceStatus(ServiceState.checking, canStart: _agentctl != null),
    );
    unawaited(refresh());
  }

  final Map<String, String> _env;
  final RunProcess _run;
  final Duration pollVisible;
  final Duration pollHidden;
  final Duration pollDown;
  final Duration startWait;
  final Duration requestTimeout;
  String? _agentctl;
  bool _autoStart;

  late final ValueNotifier<ServiceStatus> _status;
  bool _visible = false;
  bool _closed = false;
  Timer? _timer;
  Future<void>? _inFlight;

  @override
  ValueListenable<ServiceStatus> get status => _status;

  void _set(ServiceStatus s) {
    if (!_closed) _status.value = s;
  }

  @override
  set visible(bool v) {
    if (v == _visible) return;
    _visible = v;
    if (v) {
      unawaited(refresh()); // fresh when the deck opens
    } else {
      _schedule();
    }
  }

  String? get _dataDir => deckDataDir(_env);

  String? get _socket {
    final dir = _dataDir;
    return dir == null ? null : daemonSocketPath(dir, env: _env);
  }

  void _schedule() {
    _timer?.cancel();
    if (_closed) return;
    final down = _status.value.state != ServiceState.running;
    _timer = Timer(
      down ? pollDown : (_visible ? pollVisible : pollHidden),
      () => unawaited(refresh()),
    );
  }

  /// One request on the socket: the status code and the body.
  Future<(int, String)> _request(String method, String path) async {
    final sock = _socket;
    if (sock == null ||
        FileSystemEntity.typeSync(sock) !=
            FileSystemEntityType.unixDomainSock) {
      throw const SocketException('no socket');
    }
    final client = HttpClient()
      ..connectionTimeout = requestTimeout
      ..connectionFactory = (uri, proxyHost, proxyPort) => Socket.startConnect(
        InternetAddress(sock, type: InternetAddressType.unix),
        0,
      );
    try {
      final req = await client
          .openUrl(method, Uri.parse('http://localhost$path'))
          .timeout(requestTimeout);
      if (method == 'POST') req.contentLength = 0;
      final res = await req.close().timeout(requestTimeout);
      final body = await res
          .transform(const Utf8Decoder(allowMalformed: true))
          .join()
          .timeout(requestTimeout);
      return (res.statusCode, body);
    } finally {
      client.close(force: true);
    }
  }

  @override
  Future<void> refresh() => _inFlight ??= _refresh().whenComplete(() {
    _inFlight = null;
  });

  Future<void> _refresh() async {
    try {
      final (code, body) = await _request('GET', '/admin/v1/system');
      DeckSystem? system;
      if (code == 200) {
        try {
          final j = jsonDecode(body);
          if (j is Map<String, dynamic>) system = DeckSystem.fromJson(j);
        } catch (_) {}
      }
      // 404: a service from before /admin/v1/system; it runs, that's all
      // we know.
      _set(
        _status.value.copyWith(
          state: ServiceState.running,
          system: system,
          clearSystem: system == null,
          clearProblem: true,
        ),
      );
    } catch (_) {
      _set(
        _status.value.copyWith(
          state: ServiceState.notRunning,
          clearSystem: true,
        ),
      );
      if (_autoStart && _agentctl != null && !_status.value.starting) {
        // The app came with the service and it isn't answering: install it,
        // once per start of the app. After that, Start does it again.
        _autoStart = false;
        unawaited(start());
      }
    }
    _schedule();
  }

  @override
  Future<void> start() async {
    final exe = _agentctl;
    if (exe == null || _status.value.starting || _closed) return;
    _autoStart = false;
    _set(_status.value.copyWith(starting: true, clearProblem: true));
    String? problem;
    try {
      final r = await _run(exe, const ['install']);
      if (r.exitCode != 0) {
        problem = _lastLine('${r.stderr}') ?? _lastLine('${r.stdout}');
        problem ??= 'agentctl install failed (exit ${r.exitCode})';
      } else {
        final until = DateTime.now().add(startWait);
        while (!_closed && DateTime.now().isBefore(until)) {
          await refresh();
          if (_status.value.state == ServiceState.running) break;
          await Future<void>.delayed(const Duration(milliseconds: 500));
        }
        if (_status.value.state != ServiceState.running) {
          problem = 'the service was installed but does not answer yet';
        }
      }
    } catch (e) {
      problem = 'could not run agentctl: $e';
    }
    _set(_status.value.copyWith(starting: false, problem: problem));
  }

  static String? _lastLine(String s) {
    final lines = s
        .split('\n')
        .map((l) => l.trim())
        .where((l) => l.isNotEmpty)
        .toList();
    return lines.isEmpty ? null : lines.last;
  }

  /// The dashboard's address from `config.json` (`localAddr`), loopback
  /// only, as the service itself insists.
  String _localAddr() {
    final dir = _dataDir;
    if (dir != null) {
      try {
        final j = jsonDecode(
          File('$dir${Platform.pathSeparator}config.json').readAsStringSync(),
        );
        if (j is Map && j['localAddr'] is String) {
          final a = j['localAddr'] as String;
          if (RegExp(r'^127\.0\.0\.1:\d{1,5}$').hasMatch(a)) return a;
        }
      } catch (_) {}
    }
    return '127.0.0.1:7340';
  }

  @override
  Future<Uri?> settingsUrl(String page) async {
    if (!settingsPages.contains(page)) page = 'general';
    try {
      final (code, body) = await _request('POST', '/local/login-code');
      if (code != 200) return null;
      final j = jsonDecode(body);
      final login = j is Map && j['code'] is String ? j['code'] as String : '';
      if (login.isEmpty) return null;
      return settingsLoginUrl(_localAddr(), login, page);
    } catch (_) {
      unawaited(refresh());
      return null;
    }
  }

  @override
  void dispose() {
    _closed = true;
    _timer?.cancel();
  }
}

/// `http://<addr>/login?code=<code>&next=/settings#<page>`: the one-time login
/// link `agentctl web` opens, asking to land on the Settings page. A service
/// that ignores `next` lands on the dashboard's front page; the app's
/// Settings window then goes on to `/settings#<page>` itself.
Uri settingsLoginUrl(String addr, String code, String page) {
  final colon = addr.lastIndexOf(':');
  return Uri(
    scheme: 'http',
    host: addr.substring(0, colon),
    port: int.parse(addr.substring(colon + 1)),
    path: '/login',
    queryParameters: {'code': code, 'next': '/settings'},
    fragment: page,
  );
}
