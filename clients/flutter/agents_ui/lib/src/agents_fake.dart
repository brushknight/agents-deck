import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';

import 'agents_client.dart';
import 'agents_demo_state.dart';
import 'agents_model.dart';
import 'attention.dart';

/// In-memory [AgentsSource]: the protocol fixture, with commands applied the
/// way `agentctl serve --demo` applies them (an answer resumes the agent,
/// interrupt idles it, focus moves the focus flag). Tests construct it
/// still; the desktop demo (`AGENTS_DEMO=1`) passes `animate: true` so the
/// fleet works, finishes and asks for things on its own.
///
/// The demo also gives agents their "Needs you" spots ([Agent.attentionSlot])
/// the way the service does, without the 5 s stay and with the lowest free
/// spot where the service picks a random one. [FakeAgentsSource.fromState]
/// sends states as they are, like a service would (pass `attention: true`
/// to have spots given there too).
class FakeAgentsSource implements AgentsSource {
  FakeAgentsSource._(
    this._json, {
    required bool animate,
    AgentsLink link = AgentsLink.connected,
    bool liveClock = false,
    bool attention = false,
  }) : _liveClock = liveClock,
       _attention = attention,
       _snapshot = ValueNotifier(AgentsSnapshot(link: link)) {
    _emit();
    if (animate) {
      _timer = Timer.periodic(const Duration(seconds: 3), (_) => _step());
    }
  }

  /// The fixture fleet. Status ages are spread out (38m, 2m, 12m, …) so
  /// the tiles and pills read like a real afternoon instead of all "0s".
  factory FakeAgentsSource.demo({bool animate = false}) {
    final json = jsonDecode(agentsDemoStateJson) as Map<String, dynamic>;
    final now = DateTime.now().toUtc();
    (json['server'] as Map)['time'] = now.toIso8601String();
    for (final a in (json['agents'] as List).cast<Map<String, dynamic>>()) {
      final slot = (a['slot'] as num).toInt();
      final age = _demoAges[slot % _demoAges.length];
      a['statusSince'] = now.subtract(age).toIso8601String();
    }
    return FakeAgentsSource._(
      json,
      animate: animate,
      liveClock: true,
      attention: true,
    );
  }

  /// Any state (tests: empty fleets, more than 16 agents, …).
  factory FakeAgentsSource.fromState(
    Map<String, dynamic> json, {
    AgentsLink link = AgentsLink.connected,
    bool attention = false,
  }) => FakeAgentsSource._(
    jsonDecode(jsonEncode(json)) as Map<String, dynamic>,
    animate: false,
    link: link,
    attention: attention,
  );

  static const _demoAges = [
    Duration(minutes: 38), Duration(minutes: 2), Duration(minutes: 12),
    Duration(minutes: 6), Duration(minutes: 1), Duration(minutes: 21),
    Duration(minutes: 4), Duration(minutes: 65), Duration(minutes: 3),
    Duration(hours: 3), Duration(minutes: 9), //
  ];

  final Map<String, dynamic> _json;
  final ValueNotifier<AgentsSnapshot> _snapshot;

  /// Demo: the daemon clock follows ours. Fixed states keep their own.
  final bool _liveClock;

  /// Gives "Needs you" spots like the service ([_arrangeAttention]).
  final bool _attention;

  /// Agent id to the "Needs you" spot it had last.
  final _attentionHome = <String, int>{};
  Timer? _timer;
  int _tick = 0;

  /// Every command received, in order: (verb, agentId, body).
  final calls = <(String, String, Map<String, dynamic>?)>[];

  /// Test hook: when set, decides each command's result instead of the
  /// built-in demo behaviour (and the state is left untouched).
  AgentsResult Function(String verb, String agentId)? respond;

  /// Simulated round-trip for commands.
  Duration latency = Duration.zero;

  int refreshes = 0;

  @override
  ValueListenable<AgentsSnapshot> get snapshot => _snapshot;

  /// Test hook: force the link (offline, unconfigured, …) keeping the state
  /// unless [keepState] is false.
  void setLink(AgentsLink link, {String? problem, bool keepState = true}) {
    _snapshot.value = AgentsSnapshot(
      link: link,
      state: keepState ? _snapshot.value.state : null,
      problem: problem,
    );
  }

  /// Test hook: replace the whole fleet (agents appear, vanish, change).
  void load(Map<String, dynamic> json) {
    _json
      ..clear()
      ..addAll(jsonDecode(jsonEncode(json)) as Map<String, dynamic>);
    _emit();
  }

  /// A deep copy of the current fleet JSON, for tests to edit and [load].
  Map<String, dynamic> toJson() =>
      jsonDecode(jsonEncode(_json)) as Map<String, dynamic>;

  List<Map<String, dynamic>> get _agents =>
      (_json['agents'] as List).cast<Map<String, dynamic>>();

  Map<String, dynamic>? _agent(String id) {
    for (final a in _agents) {
      if (a['id'] == id) return a;
    }
    return null;
  }

  /// The service's "Needs you" rules, simplified: an agent that needs you
  /// keeps its spot; a newcomer goes back to its old spot when that is free,
  /// else takes the lowest free one; one that stops needing you leaves.
  void _arrangeAttention() {
    bool wants(Map<String, dynamic> a) =>
        const {'starting', 'running', 'waiting'}.contains(a['status']) ||
        (a['status'] == 'idle' && a['unseen'] == true);
    final agents = [..._agents]
      ..sort((x, y) => (x['slot'] as num).compareTo(y['slot'] as num));
    final taken = <int>{};
    final newcomers = <Map<String, dynamic>>[];
    for (final a in agents) {
      final s = (a['attentionSlot'] as num?)?.toInt() ?? 0;
      if (!wants(a)) {
        a['attentionSlot'] = 0;
      } else if (s >= 1 && s <= needsYouSpots && taken.add(s)) {
        _attentionHome[a['id'] as String] = s;
      } else {
        a['attentionSlot'] = 0;
        newcomers.add(a);
      }
    }
    for (final a in newcomers) {
      final home = _attentionHome[a['id']];
      var spot = home != null && !taken.contains(home) ? home : 0;
      for (var s = 1; spot == 0 && s <= needsYouSpots; s++) {
        if (!taken.contains(s)) spot = s;
      }
      if (spot == 0) break; // full: the rest wait
      taken.add(spot);
      a['attentionSlot'] = spot;
      _attentionHome[a['id'] as String] = spot;
    }
  }

  void _emit() {
    final link = _snapshot.value.link;
    if (link == AgentsLink.unconfigured) return;
    if (_attention) _arrangeAttention();
    final now = DateTime.now().toUtc();
    final server = _json['server'];
    final prev = server is Map ? DateTime.tryParse('${server['time']}') : null;
    // Keep the daemon clock moving with ours (the demo ages stay intact).
    if (_liveClock && server is Map && prev != null && now.isAfter(prev)) {
      server['time'] = now.toIso8601String();
    }
    _snapshot.value = AgentsSnapshot(
      link: link,
      state: AgentsState.fromJson(_json),
      problem: _snapshot.value.problem,
    );
  }

  void _setStatus(Map<String, dynamic> a, String status) {
    a['status'] = status;
    a['statusSince'] = DateTime.now().toUtc().toIso8601String();
    if (status != 'waiting') a['waiting'] = null;
    if (status != 'error') a['error'] = null;
    if (status != 'running') a['activity'] = null;
  }

  Future<AgentsResult> _command(
    String verb,
    String agentId, [
    Map<String, dynamic>? body,
  ]) async {
    calls.add((verb, agentId, body));
    if (latency > Duration.zero) await Future<void>.delayed(latency);
    final hook = respond;
    if (hook != null) return hook(verb, agentId);
    final a = _agent(agentId);
    if (a == null) {
      return const AgentsResult(AgentsResultKind.failed, 'no such agent');
    }
    switch (verb) {
      case 'answer':
        final w = a['waiting'];
        if (w is! Map || w['id'] != body?['promptId']) {
          return const AgentsResult(
            AgentsResultKind.conflict,
            'prompt changed',
          );
        }
        final keys = [
          for (final o in (w['options'] as List? ?? const []))
            (o as Map)['key'],
        ];
        if (!keys.contains(body?['key'])) {
          return const AgentsResult(AgentsResultKind.rejected, 'not an option');
        }
        // Fixtures can chain prompts ("next": the following step of a
        // question form), the way the daemon walks one step at a time.
        if (w['next'] case final Map<String, dynamic> next) {
          a['waiting'] = next;
          _emit();
          return AgentsResult.ok;
        }
        _setStatus(a, 'running');
        a['activity'] = {'tool': 'Bash', 'detail': w['detail'] ?? ''};
      case 'interrupt':
        if (a['status'] == 'running') _setStatus(a, 'idle');
      case 'focus':
        for (final other in _agents) {
          other['focused'] = identical(other, a);
        }
      case 'dismiss':
        _agents.remove(a);
      case 'resume':
        if (a['status'] != 'exited') {
          return const AgentsResult(
            AgentsResultKind.failed,
            'agent is still running',
          );
        }
        _setStatus(a, 'idle');
        a['resumable'] = false;
      case 'hatch':
        // As the service does: only an egg that is ready hatches, into a
        // baby with a name (made up here from the agent's id).
        final pet = a['pet'];
        if (pet is! Map || pet['stage'] != 'egg' || pet['ready'] != true) {
          return const AgentsResult(
            AgentsResultKind.conflict,
            "this pet isn't an egg ready to hatch",
          );
        }
        const names = ['Nib', 'Pip', 'Bix', 'Tup', 'Mox', 'Zib', 'Fen', 'Kip'];
        a['pet'] = {
          ...pet,
          'name':
              names[agentId.codeUnits.fold(0, (s, c) => s + c) % names.length],
          'stage': 'baby',
          'level': 1,
          'levelFrom': 50000,
          'nextAt': 62500,
          'hearts': petMaxHearts,
          'ready': false,
          'hatchedAt': DateTime.now().toUtc().toIso8601String(),
        };
      case 'handoff':
        final name = body?['account'];
        final to = accountList.where((x) => x.name == name).firstOrNull;
        if (to == null) {
          return const AgentsResult(AgentsResultKind.failed, 'no such account');
        }
        final account = {'name': to.name, 'color': to.color};
        if (a['external'] != true) {
          // The same agent stops and comes back on its conversation.
          _setStatus(a, 'starting');
          a['account'] = account;
          _emit();
          return AgentsResult(AgentsResultKind.ok, agentId);
        }
        // A watched session keeps running in its terminal; a new agent
        // continues the conversation in the other account.
        final used = {for (final x in _agents) x['slot']};
        var free = 0;
        while (used.contains(free)) {
          free++;
        }
        final id = 'h${_agents.length}$agentId';
        _agents.add({
          ...a,
          'id': id,
          'slot': free,
          'external': false,
          'account': account,
          'status': 'starting',
          'statusSince': DateTime.now().toUtc().toIso8601String(),
          'activity': null,
          'waiting': null,
          'subagents': null,
        });
        _emit();
        return AgentsResult(AgentsResultKind.ok, id);
    }
    _emit();
    return AgentsResult.ok;
  }

  @override
  Future<AgentsResult> answer(String agentId, String promptId, String key) =>
      _command('answer', agentId, {'promptId': promptId, 'key': key});

  @override
  Future<AgentsResult> focus(String agentId) => _command('focus', agentId);

  @override
  Future<AgentsResult> interrupt(String agentId) =>
      _command('interrupt', agentId);

  @override
  Future<AgentsResult> dismiss(String agentId) => _command('dismiss', agentId);

  @override
  Future<AgentsResult> resume(String agentId) => _command('resume', agentId);

  @override
  Future<AgentsResult> hatch(String agentId) => _command('hatch', agentId);

  /// Claude accounts for `GET /v1/accounts` (none set up by default, like a
  /// Mac with a single account).
  final accountList = <AgentAccount>[];

  @override
  Future<List<AgentAccount>> accounts() async => List.of(accountList);

  @override
  Future<AgentsResult> handoff(String agentId, String account) =>
      _command('handoff', agentId, {'account': account});

  /// Hand-started sessions the "add a session" picker offers.
  final live = <LiveSession>[
    const LiveSession(
      sessionId: '7a1c9e20-5d3b-4f6a-9b2e-1c4d5e6f7a8b',
      title: 'notes cleanup',
      folder: 'notes',
    ),
    const LiveSession(
      sessionId: '3e8f0b14-2a6c-4d7e-8f9a-0b1c2d3e4f5a',
      title: 'release checklist',
      folder: 'checkout-api',
    ),
  ];

  @override
  Future<List<LiveSession>> liveSessions() async => List.of(live);

  @override
  Future<AgentsResult> setHat(
    String agentId, {
    String? shape,
    String? color,
    bool auto = false,
  }) async {
    calls.add((
      'hat',
      agentId,
      auto ? {'auto': true} : {'shape': shape, 'color': color},
    ));
    final a = _agent(agentId);
    if (a == null) return AgentsResult.ok;
    // Hats belong to the folder: every agent working there changes.
    for (final x in _agents.where((x) => x['cwd'] == a['cwd'])) {
      final cur = (x['hat'] as Map?) ?? const {};
      x['hat'] = auto
          ? (() {
              final h = autoHatFor('${x['cwd']}');
              return {'shape': h.shape, 'color': h.color, 'auto': true};
            })()
          : {
              'shape': shape,
              'color': color ?? cur['color'] ?? 'teal',
              'auto': false,
            };
    }
    _emit();
    return AgentsResult.ok;
  }

  @override
  Future<AgentsResult> addLive(String sessionId, {int? slot}) async {
    calls.add(('addLive', sessionId, slot == null ? null : {'slot': slot}));
    final i = live.indexWhere((s) => s.sessionId == sessionId);
    if (i < 0) {
      return const AgentsResult(AgentsResultKind.failed, 'no such session');
    }
    final s = live[i];
    live[i] = LiveSession(
      sessionId: s.sessionId,
      title: s.title,
      folder: s.folder,
      onBoard: true,
    );
    final used = {for (final a in _agents) a['slot']};
    var free = slot ?? 0;
    while (slot == null && used.contains(free)) {
      free++;
    }
    _agents.add({
      ...(_agents.first),
      'id': 'l${s.sessionId.substring(0, 5)}',
      'title': s.title,
      'folder': s.folder,
      'slot': free,
      'tool': 'claude',
      'external': true,
      'status': 'idle',
      'unseen': false,
      'activity': null,
      'waiting': null,
      'subagents': null,
    });
    _emit();
    return AgentsResult.ok;
  }

  @override
  Future<AgentsResult> magicView(List<String> ids) async {
    calls.add(('magicView', '', {'ids': ids}));
    return AgentsResult.ok;
  }

  @override
  Future<AgentsResult> move(String agentId, int slot) async {
    calls.add(('move', agentId, {'slot': slot}));
    final a = _agent(agentId);
    if (a == null) return AgentsResult.ok;
    for (final x in _agents) {
      if (x['slot'] == slot) x['slot'] = a['slot'];
    }
    a['slot'] = slot;
    _emit();
    return AgentsResult.ok;
  }

  @override
  Future<void> refresh() async {
    refreshes++;
    _emit();
  }

  // ---- demo animation --------------------------------------------------------

  static const _work = [
    ('Read', 'src/auth/session.ts'),
    ('Edit', 'src/auth/session.ts'),
    ('Bash', 'go test ./...'),
    ('Grep', 'refreshToken'),
    ('TodoWrite', 'planning 4 steps'),
    ('Write', 'docs/notes.md'),
  ];

  /// Prompts the demo raises again once an agent has been answered, so the
  /// "needs you" path never runs dry.
  static final _prompts = <Map<String, dynamic>>[
    {
      'kind': 'permission',
      'title': 'run this command?',
      'detail': 'npm run build && npm test',
      'context': 'bash · ~/dev/demo',
      'options': [
        {'key': '1', 'label': 'yes', 'primary': true},
        {'key': '2', 'label': "yes, and don't ask again for npm"},
        {'key': '3', 'label': 'no'},
      ],
    },
    {
      'kind': 'question',
      'title': 'which database should the cache use?',
      'detail': '',
      'context': 'question · 1 of 1',
      'options': [
        {'key': '1', 'label': 'sqlite — one file, zero ops', 'primary': true},
        {'key': '2', 'label': 'redis — shared across hosts'},
      ],
    },
  ];

  void _step() {
    _tick++;
    final agents = _agents;
    for (final (i, a) in agents.indexed) {
      if (a['status'] != 'running') continue;
      final w = _work[(_tick + i) % _work.length];
      a['activity'] = {'tool': w.$1, 'detail': w.$2};
      final tokens = a['tokens'] as Map;
      tokens['output'] = (tokens['output'] as num) + 900 + 137 * i;
      final ctx = a['context'] as Map;
      final window = (ctx['window'] as num).toInt();
      ctx['used'] = ((ctx['used'] as num).toInt() + window ~/ 200).clamp(
        0,
        window,
      );
      a['costUsd'] = ((a['costUsd'] as num) + 0.03);
    }
    // Every ~12 s one agent changes state: finish a turn, pick an idle one
    // back up, or (every fourth change) block on a prompt.
    if (_tick % 4 == 0) {
      final n = agents.length;
      final a = agents[(_tick ~/ 4) % n];
      switch (a['status']) {
        case 'running' when (_tick ~/ 4) % 4 == 0:
          _setStatus(a, 'waiting');
          a['waiting'] = {
            ..._prompts[(_tick ~/ 16) % _prompts.length],
            'id': 'd$_tick',
          };
        case 'running':
          _setStatus(a, 'idle');
          a['turns'] = (a['turns'] as num) + 1;
        case 'idle':
          _setStatus(a, 'running');
          a['activity'] = {'tool': 'Read', 'detail': 'README.md'};
        case 'error':
          _setStatus(a, 'running');
          a['activity'] = {'tool': 'Bash', 'detail': 'retrying'};
      }
    }
    _emit();
  }

  @override
  void dispose() {
    _timer?.cancel();
    _timer = null;
  }
}
