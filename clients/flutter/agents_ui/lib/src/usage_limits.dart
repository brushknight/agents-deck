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

/// The wall panel's header line: per account its colour, its name and its
/// week, a model's week once it is out, and "week" at the end. Nothing
/// without limits.
class UsageLine extends StatelessWidget {
  const UsageLine({super.key, required this.state, this.size = 15});

  final AgentsState state;
  final double size;

  @override
  Widget build(BuildContext context) {
    if (state.limits.isEmpty) return const SizedBox.shrink();
    final groups = <Widget>[];
    for (final l in state.limits) {
      final week = l.shown.where((w) => w.model.isEmpty).firstOrNull;
      final models = l.shown.where((w) => w.model.isNotEmpty && w.out);
      groups.add(
        Padding(
          padding: EdgeInsets.only(right: size * 1.1),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Container(
                width: size * 0.55,
                height: size * 0.55,
                color: limitColor(state, l),
              ),
              SizedBox(width: size * 0.4),
              Text(l.name, style: DeckHud.rm(size, _muted)),
              if (week != null) ...[SizedBox(width: size * 0.4), _value(week)],
              for (final m in models) ...[
                SizedBox(width: size * 0.4),
                _block('${m.model} out'),
              ],
            ],
          ),
        ),
      );
    }
    return Row(
      key: const Key('usage-line'),
      children: [
        Flexible(
          child: SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            physics: const NeverScrollableScrollPhysics(),
            child: Row(children: groups),
          ),
        ),
        Text(
          'WEEK',
          style: DeckHud.rm(size * 0.8, DeckHud.dim, spacing: size * 0.1),
        ),
      ],
    );
  }

  Widget _value(LimitWindow w) {
    if (w.out) {
      return _block(
        w.resetsAt == null ? 'out' : 'out ${resetText(w.resetsAt!)}',
      );
    }
    return Text(
      limitValue(w),
      style: DeckHud.rm(
        size,
        w.hot ? _orange2 : (w.reset ? DeckHud.dim : DeckHud.ink),
        weight: 700,
      ),
    );
  }

  Widget _block(String text) => Container(
    padding: EdgeInsets.symmetric(horizontal: size * 0.3, vertical: 1),
    color: DeckHud.accent,
    child: Text(text, style: DeckHud.rm(size, _ink, weight: 700)),
  );
}

/// An agent's page: its account's usage over the context, one row per
/// window shown (label, segments, percent, reset time), and while it is
/// out of usage, a line saying until when.
class UsageRows extends StatelessWidget {
  const UsageRows({
    super.key,
    required this.limits,
    this.outUntil,
    this.moveTo,
    this.size = 16,
  });

  final AgentLimits limits;

  /// The agent is out of usage until then (null: it isn't).
  final DateTime? outUntil;

  /// Another account it could continue on ("" or null: none).
  final String? moveTo;
  final double size;

  @override
  Widget build(BuildContext context) {
    final out = outUntil;
    return Column(
      key: const Key('agent-usage'),
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text.rich(
          TextSpan(
            children: [
              const TextSpan(text: 'USAGE'),
              TextSpan(
                text: ' · ${limits.name.toUpperCase()}',
                style: const TextStyle(color: DeckHud.dim),
              ),
            ],
          ),
          maxLines: 1,
          style: DeckHud.rm(size, _muted, weight: 700, spacing: size * 0.12),
        ),
        for (final w in limits.shown) ...[
          SizedBox(height: size * 0.4),
          Row(
            key: Key('agent-usage-${w.id}'),
            children: [
              SizedBox(
                width: size * 12,
                child: Text(
                  limits.labelOf(w),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: DeckHud.rm(size, _muted),
                ),
              ),
              SizedBox(width: size * 0.8),
              Expanded(child: _Segments(w)),
              SizedBox(width: size * 0.8),
              SizedBox(width: size * 3.4, child: _value(w)),
              SizedBox(
                width: size * 6.6,
                child: Text(
                  w.resetsAt == null ? '' : resetText(w.resetsAt!),
                  textAlign: TextAlign.right,
                  maxLines: 1,
                  style: DeckHud.rm(size, w.reset ? DeckHud.dim : _muted),
                ),
              ),
            ],
          ),
        ],
        if (out != null) ...[
          SizedBox(height: size * 0.5),
          Text(
            'out of usage until ${resetText(out)}'
            '${moveTo == null || moveTo!.isEmpty ? ' · it waits until then' : ' · it can continue on $moveTo'}',
            key: const Key('agent-usage-out'),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: DeckHud.rm(size, _orange2, weight: 700),
          ),
        ],
      ],
    );
  }

  Widget _value(LimitWindow w) {
    final v = limitValue(w);
    if (w.out) {
      return Align(
        alignment: Alignment.centerRight,
        child: Container(
          padding: EdgeInsets.symmetric(horizontal: size * 0.35, vertical: 2),
          color: DeckHud.accent,
          child: Text(
            v.toUpperCase(),
            style: DeckHud.rm(size, _ink, weight: 700),
          ),
        ),
      );
    }
    return Text(
      v,
      textAlign: TextAlign.right,
      style: DeckHud.rm(
        size * 1.1,
        w.hot ? _orange2 : (w.reset ? DeckHud.dim : DeckHud.ink),
        weight: 700,
      ),
    );
  }
}

/// A window as square segments: ink while fine, orange from 80%.
class _Segments extends StatelessWidget {
  const _Segments(this.w);

  final LimitWindow w;

  @override
  Widget build(BuildContext context) {
    const n = 24;
    final on = w.reset ? 0 : (w.used.clamp(0, 100) / 100 * n).round();
    final color = w.hot ? DeckHud.accent : DeckHud.ink;
    return SizedBox(
      height: 10,
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          for (var i = 0; i < n; i++) ...[
            if (i > 0) const SizedBox(width: 3),
            Expanded(child: ColoredBox(color: i < on ? color : _segOff)),
          ],
        ],
      ),
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
  if (w.out) {
    return (
      text:
          '${l.name} out${w.resetsAt == null ? '' : ' · ${resetText(w.resetsAt!)}'}',
      attention: true,
    );
  }
  if (w.reset) return (text: '${l.name} week reset', attention: false);
  return (text: '${l.name} week ${w.used}%', attention: w.hot);
}
