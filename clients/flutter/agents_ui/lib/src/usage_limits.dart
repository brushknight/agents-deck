import 'package:flutter/widgets.dart';

import 'agents_model.dart';
import 'agents_style.dart';
import 'critter.dart';
import 'theme.dart';

// Usage limits on the deck: how much of each account's week is gone, as
// Claude Code and Codex report it themselves (direct use only). Grey and
// quiet under 80%, orange from 80%, an orange block at 100% ("out").

const _muted = Color(0xFF9A968C);
const _segOff = Color(0xFF2A2925);
const _orange2 = Color(0xFFFF6A2E);
const _ink = Color(0xFF0A0A0A);

/// The colour that marks an account: its hat colour (from an agent that
/// runs in it), Codex's tool colour, else ink.
Color limitColor(AgentsState state, AgentLimits l) {
  if (l.tool != 'claude' || l.account.isEmpty) return agentToolColor(l.tool);
  for (final a in state.agents) {
    if (a.account?.name == l.account) return hatColor(a.account!.color).$1;
  }
  return agentToolColor(l.tool);
}

/// "86%", "out", "–" (reset since the last report).
String limitValue(LimitWindow w) => w.reset
    ? '–'
    : w.out
    ? 'out'
    : '${w.used}%';

/// The week of a set of limits: what the corner and the agent's row show.
LimitWindow? weekOf(AgentLimits l) =>
    l.shown.where((w) => w.model.isEmpty).firstOrNull ?? l.shown.firstOrNull;

/// The two sources the deck's corner shows: the Claude account in use (the
/// one most agents run in, busy ones first), then the other source closest
/// to its limit. Pending Grigorii's answer on which two to show: change it
/// here only.
List<AgentLimits> cornerSources(AgentsState state) {
  if (state.limits.isEmpty) return const [];
  int score(AgentLimits l) {
    var n = 0;
    for (final a in state.agents) {
      if (a.tool != l.tool || (a.account?.name ?? '') != l.account) continue;
      n += a.status == AgentStatus.running || a.needsYou ? 1000 : 1;
    }
    return n;
  }

  final claude = state.limits.where((l) => l.tool == 'claude').toList();
  final pool = claude.isEmpty ? state.limits : claude;
  var inUse = pool.first;
  for (final l in pool) {
    if (score(l) > score(inUse)) inUse = l;
  }
  const rank = {'ok': 0, 'near': 1, 'low': 2, 'out': 3};
  int nearness(AgentLimits l) {
    final w = weekOf(l);
    if (w == null || w.reset) return -1;
    return (rank[w.level] ?? 0) * 1000 + w.used;
  }

  AgentLimits? next;
  for (final l in state.limits) {
    if (identical(l, inUse)) continue;
    if (next == null || nearness(l) > nearness(next)) next = l;
  }
  return [inUse, ?next];
}

/// A source's value in the corner: "86%", "out · thu 06:34", "–".
String cornerValue(LimitWindow w) {
  if (w.out) {
    return w.resetsAt == null ? 'out' : 'out · ${resetText(w.resetsAt!)}';
  }
  return limitValue(w);
}

/// The deck's bottom-left corner: the account in use as a tiny critter in
/// its colour and its week, then the second source as a dot and its week.
/// Orange past 80% and when out. Nothing without limits.
class UsageCorner extends StatelessWidget {
  const UsageCorner({super.key, required this.state, this.size = 15});

  final AgentsState state;
  final double size;

  @override
  Widget build(BuildContext context) {
    final sources = cornerSources(state);
    if (sources.isEmpty) return const SizedBox.shrink();
    Widget value(AgentLimits l, String key) {
      final w = weekOf(l);
      return Text(
        w == null ? '–' : cornerValue(w),
        key: Key('usage-corner-$key'),
        maxLines: 1,
        softWrap: false,
        style: DeckHud.rm(
          size,
          w == null || w.reset
              ? DeckHud.dim
              : (w.hot ? DeckHud.accent : DeckHud.ink),
          weight: 700,
          spacing: size * 0.02,
        ),
      );
    }

    final gap = size * 7 / 15;
    return Row(
      key: const Key('usage-corner'),
      mainAxisSize: MainAxisSize.min,
      children: [
        CustomPaint(
          size: Size(size * 14 / 15, size * 8 / 15),
          painter: _CritterIcon(limitColor(state, sources.first)),
        ),
        SizedBox(width: gap),
        value(sources.first, 'first'),
        if (sources.length > 1) ...[
          SizedBox(width: gap + size * 6 / 15),
          Container(
            width: size * 8 / 15,
            height: size * 8 / 15,
            decoration: BoxDecoration(
              color: limitColor(state, sources[1]),
              shape: BoxShape.circle,
            ),
          ),
          SizedBox(width: gap),
          value(sources[1], 'second'),
        ],
      ],
    );
  }
}

/// The corner's critter: 7 × 4 pixels, as on the boards.
class _CritterIcon extends CustomPainter {
  const _CritterIcon(this.color);

  final Color color;

  static const _rows = ['.xxxxx.', 'xx.x.xx', 'xxxxxxx', '.x.x.x.'];

  @override
  void paint(Canvas canvas, Size size) {
    final px = size.width / 7, py = size.height / 4;
    final paint = Paint()
      ..color = color
      ..isAntiAlias = false;
    for (var y = 0; y < 4; y++) {
      for (var x = 0; x < 7; x++) {
        if (_rows[y][x] == 'x') {
          canvas.drawRect(Rect.fromLTWH(x * px, y * py, px, py), paint);
        }
      }
    }
  }

  @override
  bool shouldRepaint(_CritterIcon old) => old.color != color;
}

/// An agent's page, one row: its account's week (label, segments, percent,
/// reset time) and its context at the end, "ctx 41%".
class UsageContextRow extends StatelessWidget {
  const UsageContextRow({
    super.key,
    required this.limits,
    required this.context,
    this.size = 15,
  });

  final AgentLimits limits;

  /// "41%", "--" (not known).
  final String context;
  final double size;

  @override
  Widget build(BuildContext ctx) {
    final w = weekOf(limits);
    final model = w == null || w.model.isEmpty ? '' : ' ${w.model}';
    return Row(
      key: const Key('agent-usage'),
      children: [
        // A long account name is cut; the bar keeps its place.
        ConstrainedBox(
          constraints: BoxConstraints(maxWidth: size * 11),
          child: Text(
            'USAGE · ${limits.name}$model'.toUpperCase(),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            softWrap: false,
            style: DeckHud.rm(size, _muted, weight: 700, spacing: size * 0.1),
          ),
        ),
        if (w != null) ...[
          SizedBox(width: size * 0.8),
          _Segments(w, n: 26, width: size * 0.6, height: size * 0.8),
          SizedBox(width: size * 0.8),
          _value(w),
          if (w.resetsAt != null) ...[
            SizedBox(width: size * 0.8),
            Text(
              resetText(w.resetsAt!),
              maxLines: 1,
              softWrap: false,
              style: DeckHud.rm(size, _muted),
            ),
          ],
        ],
        const Spacer(),
        SizedBox(width: size * 0.8),
        Text(
          'ctx $context',
          key: const Key('agent-context'),
          maxLines: 1,
          softWrap: false,
          style: DeckHud.rm(size, _muted),
        ),
      ],
    );
  }

  Widget _value(LimitWindow w) {
    if (w.out) {
      return Container(
        key: Key('agent-usage-${w.id}'),
        padding: EdgeInsets.symmetric(horizontal: size * 0.3, vertical: 1),
        color: DeckHud.accent,
        child: Text('OUT', style: DeckHud.rm(size, _ink, weight: 700)),
      );
    }
    return Text(
      limitValue(w),
      key: Key('agent-usage-${w.id}'),
      style: DeckHud.rm(
        size,
        w.hot ? DeckHud.accent : (w.reset ? DeckHud.dim : DeckHud.ink),
        weight: 700,
      ),
    );
  }
}

/// While an agent is out of usage: until when, and what it does meanwhile.
class UsageOutLine extends StatelessWidget {
  const UsageOutLine({
    super.key,
    required this.until,
    this.moveTo,
    this.size = 15,
  });

  final DateTime until;

  /// Another account it could continue on ("" or null: none).
  final String? moveTo;
  final double size;

  @override
  Widget build(BuildContext context) => Text(
    'out of usage until ${resetText(until)}'
    '${moveTo == null || moveTo!.isEmpty ? ' · it waits until then' : ' · it can continue on $moveTo'}',
    key: const Key('agent-usage-out'),
    maxLines: 1,
    overflow: TextOverflow.ellipsis,
    style: DeckHud.rm(size, _orange2, weight: 700),
  );
}

/// A window as square segments: ink while fine, orange from 80%.
class _Segments extends StatelessWidget {
  const _Segments(
    this.w, {
    required this.n,
    required this.width,
    required this.height,
  });

  final LimitWindow w;
  final int n;
  final double width, height;

  @override
  Widget build(BuildContext context) {
    final on = w.reset ? 0 : (w.used.clamp(0, 100) / 100 * n).round();
    final color = w.hot ? DeckHud.accent : DeckHud.ink;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        for (var i = 0; i < n; i++) ...[
          if (i > 0) SizedBox(width: width / 4.5),
          SizedBox(
            width: width,
            height: height,
            child: ColoredBox(color: i < on ? color : _segOff),
          ),
        ],
      ],
    );
  }
}

/// The menu bar's status line part: the account closest to running out,
/// "work week 41%", "work week 86%" (attention), "work out · thu 09:00".
/// Null without limits.
({String text, bool attention})? usageStatus(AgentsState? state) {
  final worst = state?.worstLimit;
  if (worst == null) return null;
  final (l, w) = worst;
  // The line is short: a long account name is cut.
  final name = l.name.length > 14 ? '${l.name.substring(0, 13)}…' : l.name;
  if (w.out) {
    return (
      text:
          '$name out${w.resetsAt == null ? '' : ' · ${resetText(w.resetsAt!)}'}',
      attention: true,
    );
  }
  if (w.reset) return (text: '$name week reset', attention: false);
  return (text: '$name week ${w.used}%', attention: w.hot);
}
