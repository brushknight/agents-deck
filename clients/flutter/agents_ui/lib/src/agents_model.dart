/// Data model for the agents app: the `agentctl serve` protocol v1 types
/// (agents-terminal docs/protocol.md) plus the display helpers the tiles and
/// detail screens share. Parsing is defensive — a missing or mistyped field
/// degrades to an empty value, it never throws past [AgentsState.fromJson].
library;

/// Agent lifecycle as reported by the daemon. [unknown] covers statuses a
/// newer daemon may add; it renders like [idle].
enum AgentStatus { starting, running, waiting, idle, error, exited, unknown }

AgentStatus _status(Object? v) => switch (v) {
  'starting' => AgentStatus.starting,
  'running' => AgentStatus.running,
  'waiting' => AgentStatus.waiting,
  'idle' => AgentStatus.idle,
  'error' => AgentStatus.error,
  'exited' => AgentStatus.exited,
  _ => AgentStatus.unknown,
};

/// What a waiting agent is blocked on.
enum PromptKind { permission, question, input }

String _str(Object? v) => v is String ? v : '';
int _int(Object? v) => v is num ? v.toInt() : 0;
double _dbl(Object? v) => v is num ? v.toDouble() : 0;
DateTime? _time(Object? v) => v is String ? DateTime.tryParse(v) : null;
Map<String, dynamic>? _map(Object? v) =>
    v is Map ? v.cast<String, dynamic>() : null;

class AgentOption {
  const AgentOption({
    required this.key,
    required this.label,
    this.primary = false,
  });

  /// Sent back verbatim as the answer's `key`.
  final String key;
  final String label;
  final bool primary;

  static AgentOption fromJson(Map<String, dynamic> j) => AgentOption(
    key: _str(j['key']),
    label: _str(j['label']),
    primary: j['primary'] == true,
  );

  /// Question options arrive as "title — explanation"; the rows show the
  /// two parts on separate lines.
  (String, String) get titleAndSub {
    final i = label.indexOf(' — ');
    if (i < 0) return (label, '');
    return (label.substring(0, i), label.substring(i + 3));
  }
}

class AgentPrompt {
  const AgentPrompt({
    required this.id,
    required this.kind,
    required this.title,
    required this.detail,
    required this.context,
    required this.options,
  });

  /// Changes with every new prompt; echoed back with the answer so the
  /// daemon can refuse a stale one (409).
  final String id;
  final PromptKind kind;
  final String title;
  final String detail;
  final String context;
  final List<AgentOption> options;

  static AgentPrompt fromJson(Map<String, dynamic> j) => AgentPrompt(
    id: _str(j['id']),
    kind: switch (j['kind']) {
      'permission' => PromptKind.permission,
      'question' => PromptKind.question,
      _ => PromptKind.input,
    },
    title: _str(j['title']),
    detail: _str(j['detail']),
    context: _str(j['context']),
    options: [
      for (final o in (j['options'] is List ? j['options'] as List : const []))
        if (_map(o) case final m?) AgentOption.fromJson(m),
    ],
  );
}

class AgentActivity {
  const AgentActivity({required this.tool, required this.detail});

  final String tool;
  final String detail;

  /// Short lowercase verb for the tile meta line ("edit", "bash", "plan").
  String get verb {
    final t = tool.toLowerCase();
    return switch (t) {
      'todowrite' => 'plan',
      'multiedit' || 'notebookedit' => 'edit',
      'webfetch' => 'fetch',
      'websearch' => 'search',
      'compact' => 'compacting',
      _ => t,
    };
  }
}

/// One of an agent's subagents that is still working.
class AgentSubagent {
  const AgentSubagent({
    required this.id,
    required this.title,
    this.type = '',
    this.tool = '',
  });

  final String id;

  /// The task description it was given.
  final String title;

  /// Agent type (Explore, general-purpose, fork…).
  final String type;

  /// Tool of its latest call, for the mini critter's pose.
  final String tool;

  static AgentSubagent fromJson(Map<String, dynamic> j) => AgentSubagent(
    id: _str(j['id']),
    title: _str(j['title']),
    type: _str(j['type']),
    tool: _str(j['tool']),
  );
}

class Agent {
  const Agent({
    required this.id,
    required this.slot,
    required this.title,
    this.aiTitle = '',
    this.tool = 'claude',
    required this.status,
    this.statusSince,
    this.cwd = '',
    this.folder = '',
    this.branch = '',
    this.model = '',
    this.modelLabel = '',
    this.activity,
    this.lastPrompt = '',
    this.waiting,
    this.errorMessage,
    this.tokensInput = 0,
    this.tokensOutput = 0,
    this.cacheRead = 0,
    this.cacheWrite = 0,
    this.contextUsed = 0,
    this.contextWindow = 0,
    this.costUsd = 0,
    this.turns = 0,
    this.focused = false,
    this.attached = false,
    this.resumable = false,
    this.lost = false,
    this.unseen = false,
    this.external = false,
    this.subagents = const [],
    this.startedAt,
    this.updatedAt,
  });

  final String id;
  final int slot;
  final String title;
  final String aiTitle;

  /// claude | codex | gemini | shell
  final String tool;
  final AgentStatus status;
  final DateTime? statusSince;
  final String cwd;
  final String folder;
  final String branch;
  final String model;
  final String modelLabel;
  final AgentActivity? activity;
  final String lastPrompt;

  /// Set only while [status] is waiting.
  final AgentPrompt? waiting;

  /// Set only while [status] is error.
  final String? errorMessage;
  final int tokensInput;
  final int tokensOutput;
  final int cacheRead;
  final int cacheWrite;
  final int contextUsed;
  final int contextWindow;
  final double costUsd;
  final int turns;

  /// Its terminal is the front iTerm tab on the Mac.
  final bool focused;
  final bool attached;

  /// Exited Claude agent the daemon can bring back on its conversation.
  final bool resumable;

  /// Ended with the tmux server (crash, reboot), not by itself: the daemon
  /// can bring it back (`agentctl restore`, or resume on its card).
  final bool lost;

  /// Finished a turn you haven't looked at yet: "hungry" until you focus its
  /// terminal (review the result) or give it a new prompt.
  final bool unseen;

  /// Mirrored from another app (a Codex app thread): view and open only.
  final bool external;

  /// Subagents still working, oldest first.
  final List<AgentSubagent> subagents;

  /// A Codex app thread (view and open in the app only).
  bool get codexThread => external && tool == 'codex';

  /// A Claude session started by hand in another terminal and added to the
  /// deck: watched (status, context, cost), answered in its own terminal.
  bool get watched => external && tool == 'claude';

  /// Idle with an unreviewed result.
  bool get hungry => unseen && status == AgentStatus.idle;
  final DateTime? startedAt;
  final DateTime? updatedAt;

  static Agent fromJson(Map<String, dynamic> j) {
    final act = _map(j['activity']);
    final wait = _map(j['waiting']);
    final err = _map(j['error']);
    final tokens = _map(j['tokens']) ?? const {};
    final ctx = _map(j['context']) ?? const {};
    return Agent(
      id: _str(j['id']),
      slot: _int(j['slot']),
      title: _str(j['title']),
      aiTitle: _str(j['aiTitle']),
      tool: _str(j['tool']),
      status: _status(j['status']),
      statusSince: _time(j['statusSince']),
      cwd: _str(j['cwd']),
      folder: _str(j['folder']),
      branch: _str(j['branch']),
      model: _str(j['model']),
      modelLabel: _str(j['modelLabel']),
      activity: act == null
          ? null
          : AgentActivity(tool: _str(act['tool']), detail: _str(act['detail'])),
      lastPrompt: _str(j['lastPrompt']),
      waiting: wait == null ? null : AgentPrompt.fromJson(wait),
      errorMessage: err == null ? null : _str(err['message']),
      tokensInput: _int(tokens['input']),
      tokensOutput: _int(tokens['output']),
      cacheRead: _int(tokens['cacheRead']),
      cacheWrite: _int(tokens['cacheWrite']),
      contextUsed: _int(ctx['used']),
      contextWindow: _int(ctx['window']),
      costUsd: _dbl(j['costUsd']),
      turns: _int(j['turns']),
      focused: j['focused'] == true,
      attached: j['attached'] == true,
      resumable: j['resumable'] == true,
      lost: j['lost'] == true,
      unseen: j['unseen'] == true,
      external: j['external'] == true,
      subagents: [
        if (j['subagents'] case final List l)
          for (final x in l)
            if (x is Map<String, dynamic>) AgentSubagent.fromJson(x),
      ],
      startedAt: _time(j['startedAt']),
      updatedAt: _time(j['updatedAt']),
    );
  }

  /// Blocked on the user with a prompt to show.
  bool get needsYou => status == AgentStatus.waiting;

  /// Context fill, 0..1 (0 when the window is unknown).
  double get contextFraction =>
      contextWindow <= 0 ? 0 : (contextUsed / contextWindow).clamp(0.0, 1.0);

  int get contextPercent => (contextFraction * 100).round();
}

class AgentsServer {
  const AgentsServer({this.name = '', this.version = '', this.time});

  final String name;
  final String version;
  final DateTime? time;
}

/// One full `state` event. The daemon sends the whole fleet every time
/// (no deltas), so this is also the unit of UI updates.
class AgentsState {
  AgentsState({
    required this.server,
    required List<Agent> agents,
    DateTime? receivedAt,
  }) : agents = List.unmodifiable(
         [...agents]..sort((a, b) => a.slot.compareTo(b.slot)),
       ),
       receivedAt = receivedAt ?? DateTime.now();

  final AgentsServer server;

  /// Ordered by slot.
  final List<Agent> agents;

  /// Local clock when this state arrived; with [AgentsServer.time] it maps
  /// the daemon's timestamps onto the panel clock (the two may disagree).
  final DateTime receivedAt;

  /// Throws [FormatException] when the payload isn't a state object at all.
  static AgentsState fromJson(Object? json, {DateTime? receivedAt}) {
    final j = _map(json);
    if (j == null || j['agents'] is! List) {
      throw const FormatException('not an agents state');
    }
    final s = _map(j['server']) ?? const {};
    return AgentsState(
      server: AgentsServer(
        name: _str(s['name']),
        version: _str(s['version']),
        time: _time(s['time']),
      ),
      agents: [
        for (final a in j['agents'] as List)
          if (_map(a) case final m?) Agent.fromJson(m),
      ],
      receivedAt: receivedAt,
    );
  }

  Agent? byId(String id) {
    for (final a in agents) {
      if (a.id == id) return a;
    }
    return null;
  }

  int get waitingCount => agents.where((a) => a.needsYou).length;

  /// "Now" on the daemon's clock: its timestamp at send time plus however
  /// long ago that was here. Falls back to the local clock.
  DateTime serverNow([DateTime? localNow]) {
    final now = localNow ?? DateTime.now();
    final t = server.time;
    if (t == null) return now;
    return t.add(now.difference(receivedAt));
  }

  /// How long [a] has been in its current status (zero when unknown).
  Duration inStatus(Agent a, [DateTime? localNow]) {
    final since = a.statusSince;
    if (since == null) return Duration.zero;
    final d = serverNow(localNow).difference(since);
    return d.isNegative ? Duration.zero : d;
  }
}

// ---- display helpers -------------------------------------------------------

/// Compact counts: 900, 1.5k, 84k, 410k, 1m, 3.1m.
String compactCount(int n) {
  String fmt(double v, String unit) {
    final s = v < 10 ? v.toStringAsFixed(1) : v.round().toString();
    return '${s.endsWith('.0') ? s.substring(0, s.length - 2) : s}$unit';
  }

  if (n < 1000) return '$n';
  if (n < 999500) return fmt(n / 1000, 'k');
  return fmt(n / 1000000, 'm');
}

/// Short elapsed time: 45s, 12m, 3h, 2d.
String shortDuration(Duration d) {
  if (d.inMinutes < 1) return '${d.inSeconds}s';
  if (d.inHours < 1) return '${d.inMinutes}m';
  if (d.inDays < 1) return '${d.inHours}h';
  return '${d.inDays}d';
}

/// Home directory folded to `~` (macOS and Linux homes).
String tildePath(String path) {
  final m = RegExp(r'^/(?:Users|home)/[^/]+').firstMatch(path);
  return m == null ? path : '~${path.substring(m.end)}';
}

/// The tile's second line, by status.
String agentMetaLine(Agent a, AgentsState state, [DateTime? localNow]) {
  switch (a.status) {
    case AgentStatus.running:
      final verb = a.activity?.verb;
      return '${verb == null || verb.isEmpty ? 'working' : verb}'
          ' · ${a.contextPercent}% ctx';
    case AgentStatus.waiting:
      final p = a.waiting;
      if (p == null) return 'needs you';
      switch (p.kind) {
        case PromptKind.permission:
          final word = p.context.trim().split(RegExp(r'[\s·]+')).first;
          return word.isEmpty ? 'allow?' : 'allow ${word.toLowerCase()}?';
        case PromptKind.question:
          return p.options.isEmpty
              ? 'question'
              : 'pick 1 of ${p.options.length}';
        case PromptKind.input:
          return 'needs input';
      }
    case AgentStatus.idle when a.hungry:
      return 'hungry · feed me';
    case AgentStatus.idle:
    case AgentStatus.unknown:
      return 'done · ${shortDuration(state.inStatus(a, localNow))} ago';
    case AgentStatus.error:
      final m = a.errorMessage ?? '';
      return m.isEmpty ? 'error' : m;
    case AgentStatus.starting:
      return 'starting';
    case AgentStatus.exited:
      if (a.lost) return 'lost with tmux';
      return a.resumable ? 'ended · resumable' : 'exited';
  }
}

/// Detail-screen status pill text ("running · 38m", "needs you · 2m").
String agentPillLabel(Agent a, AgentsState state, [DateTime? localNow]) {
  final t = shortDuration(state.inStatus(a, localNow));
  return switch (a.status) {
    AgentStatus.running => 'running · $t',
    AgentStatus.waiting =>
      a.waiting?.kind == PromptKind.question &&
              (a.waiting?.context.isNotEmpty ?? false)
          ? a.waiting!.context
          : 'needs you · $t',
    AgentStatus.idle when a.hungry => 'hungry · $t',
    AgentStatus.idle || AgentStatus.unknown => 'idle · $t',
    AgentStatus.error => 'error',
    AgentStatus.starting => 'starting · $t',
    AgentStatus.exited => a.lost ? 'lost with tmux · $t' : 'exited · $t',
  };
}

/// A Claude session started by hand in another terminal (`GET /v1/live`),
/// which can be added to the deck (watched).
class LiveSession {
  const LiveSession({
    required this.sessionId,
    required this.title,
    required this.folder,
    this.started,
    this.onBoard = false,
  });

  final String sessionId;
  final String title;
  final String folder;
  final DateTime? started;
  final bool onBoard;

  static LiveSession fromJson(Map<String, dynamic> j) => LiveSession(
    sessionId: _str(j['sessionId']),
    title: _str(j['title']),
    folder: _str(j['folder']),
    started: _time(j['started']),
    onBoard: j['onBoard'] == true,
  );
}
