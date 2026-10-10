import 'dart:async';

import 'package:flutter/material.dart';

import 'theme.dart';
import 'widgets.dart';
import 'agents_client.dart';
import 'agents_model.dart';
import 'agents_style.dart';
import 'critter.dart';

/// Matches the morph card's landed corner radius (the morph lerps to 26).
const _cardRadius = 26.0;

/// How long the "sent" invert holds before the card closes itself.
const _sentHold = Duration(milliseconds: 600);

/// Feedback on the button that was pressed last.
enum _Flash { sent, error }

/// Full-screen card for one agent. Follows the live state: a prompt that
/// appears, changes or gets answered at the terminal swaps the layout in
/// place; an agent that disappears shows "gone" and the card closes.
///
/// * running / idle / error / exited / starting — dark overview.
/// * waiting · permission — accent card, one big button per option; a tap
///   answers at once.
/// * waiting · question — dark card with an accent frame; pick a row, send.
/// * waiting · input — free text can't be typed here: focus the terminal.
class AgentDetail extends StatefulWidget {
  const AgentDetail({
    super.key,
    required this.source,
    required this.agentId,
    required this.onClose,
    this.onVisible,
    this.showBack = false,
  });

  final AgentsSource source;
  final String agentId;
  final VoidCallback onClose;

  /// Mounted (true) / unmounted (false) — the grid pauses its critters
  /// while the card covers it.
  final ValueChanged<bool>? onVisible;

  /// A back button left of the mascot, for hosts without an edge swipe
  /// (the menu bar app). The desk panel closes cards with its swipe.
  final bool showBack;

  @override
  State<AgentDetail> createState() => _AgentDetailState();
}

class _AgentDetailState extends State<AgentDetail> {
  /// Prompt the per-prompt UI state (selection, notes) belongs to.
  String? _promptId;
  String? _selected;

  /// A command is in flight (or succeeded and the card is about to close).
  bool _busy = false;

  /// "stop" on a live agent needs a second tap; this arms it for a few seconds.
  bool _confirmStop = false;
  Timer? _confirmTimer;
  bool _closing = false;

  /// Which control flashes, and how (key = option key, 'focus', 'interrupt',
  /// 'send').
  String? _flashKey;
  _Flash? _flash;

  /// One-line feedback ("prompt changed", "timed out").
  String? _note;
  Timer? _timer;
  bool _goneScheduled = false;

  /// What the last build showed (frozen while closing).
  (AgentsState, Agent, AgentPrompt?)? _shown;

  @override
  void initState() {
    super.initState();
    widget.onVisible?.call(true);
  }

  @override
  void dispose() {
    _timer?.cancel();
    _confirmTimer?.cancel();
    widget.onVisible?.call(false);
    super.dispose();
  }

  void _syncPrompt(AgentPrompt? p) {
    if (p?.id == _promptId) return;
    _promptId = p?.id;
    _selected = p == null
        ? null
        : (p.options.where((o) => o.primary).firstOrNull ??
                  p.options.firstOrNull)
              ?.key;
    if (!_closing) {
      _note = null;
      _flash = null;
      _flashKey = null;
    }
  }

  Future<void> _run(
    String key,
    Future<AgentsResult> Function() action, {
    bool closeOnOk = false,
  }) async {
    if (_busy || _closing) return;
    setState(() {
      _busy = true;
      _flashKey = key;
      _flash = null;
      _note = null;
    });
    final r = await action();
    if (!mounted) return;
    _timer?.cancel();
    switch (r.kind) {
      case AgentsResultKind.ok:
        setState(() {
          _flash = _Flash.sent;
          _closing = closeOnOk;
          _busy = closeOnOk;
        });
        _timer = Timer(
          closeOnOk ? _sentHold : const Duration(milliseconds: 450),
          () {
            if (!mounted) return;
            if (closeOnOk) {
              widget.onClose();
            } else {
              setState(() => _flash = null);
            }
          },
        );
      case AgentsResultKind.conflict:
        setState(() {
          _busy = false;
          _flash = null;
          _note = 'prompt changed';
        });
        unawaited(widget.source.refresh());
      case AgentsResultKind.rejected:
      case AgentsResultKind.failed:
        setState(() {
          _busy = false;
          _flash = _Flash.error;
          _note = r.message ?? 'failed';
        });
        _timer = Timer(const Duration(milliseconds: 450), () {
          if (mounted) setState(() => _flash = null);
        });
    }
  }

  void _answer(Agent a, AgentPrompt p, String key) =>
      _run(key, () => widget.source.answer(a.id, p.id, key), closeOnOk: true);

  void _focus(Agent a) => _run('focus', () => widget.source.focus(a.id));

  void _interrupt(Agent a) =>
      _run('interrupt', () => widget.source.interrupt(a.id));

  void _resume(Agent a) => _run('resume', () => widget.source.resume(a.id));

  /// Exited agents go at once; a live agent is stopped only on a second tap.
  void _dismiss(Agent a) {
    if (a.status != AgentStatus.exited && !a.external && !_confirmStop) {
      _confirmTimer?.cancel();
      setState(() => _confirmStop = true);
      _confirmTimer = Timer(const Duration(seconds: 3), () {
        if (mounted) setState(() => _confirmStop = false);
      });
      return;
    }
    _confirmTimer?.cancel();
    _confirmStop = false;
    _run('dismiss', () => widget.source.dismiss(a.id), closeOnOk: true);
  }

  Widget _removeButton(Agent a, (Color, Color)? flash) {
    final exited = a.status == AgentStatus.exited;
    final armed = _confirmStop && !exited;
    return AgentButton(
      key: const Key('agent-remove'),
      label: _flashKey == 'dismiss' && _flash == _Flash.sent
          ? 'removed'
          : a.external
          ? 'hide'
          : exited
          ? 'remove'
          : armed
          ? 'tap again to stop'
          : 'stop',
      onTap: _busy ? null : () => _dismiss(a),
      fill: flash?.$1 ?? (armed ? DeckHud.accent : const Color(0x00000000)),
      ink: flash?.$2 ?? (armed ? DeckHud.bg : DeckHud.ink),
      outline: flash?.$1 ?? (armed ? DeckHud.accent : DeckHud.dim),
      pressedFill: DeckHud.accent,
      pressedInk: DeckHud.bg,
    );
  }

  /// Colours for a control given the shared flash state.
  (Color, Color)? _flashColors(String key, {required bool onAccent}) {
    if (_flashKey != key || _flash == null) return null;
    return switch (_flash!) {
      _Flash.sent => (DeckHud.ink, DeckHud.bg),
      // Error = accent invert; on the accent card that would vanish, so it
      // inverts to ink-on-accent text instead.
      _Flash.error =>
        onAccent ? (DeckHud.bg, DeckHud.ink) : (DeckHud.accent, DeckHud.bg),
    };
  }

  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<AgentsSnapshot>(
      valueListenable: widget.source.snapshot,
      builder: (context, snap, _) {
        // While an answer's "sent" holds, keep showing the prompt it
        // answered — the live state has usually moved on already.
        final frozen = _closing ? _shown : null;
        final state = frozen?.$1 ?? snap.state;
        final a = frozen?.$2 ?? state?.byId(widget.agentId);
        if (state == null || a == null) return _gone();
        final p = frozen != null
            ? frozen.$3
            : (a.status == AgentStatus.waiting ? a.waiting : null);
        if (frozen == null) {
          _syncPrompt(p);
          _shown = (state, a, p);
        }
        return switch (p?.kind) {
          PromptKind.permission => _permission(state, a, p!),
          PromptKind.question => _question(state, a, p!),
          PromptKind.input => _input(state, a, p!),
          null => _overview(state, a),
        };
      },
    );
  }

  // ---- shared pieces ---------------------------------------------------------

  Widget _card({required Color fill, Color? border, required Widget child}) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: fill,
        borderRadius: BorderRadius.circular(_cardRadius),
        border: border == null ? null : Border.all(color: border, width: 3),
      ),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(36, 34, 36, 30),
        child: child,
      ),
    );
  }

  Widget _topBar(
    AgentsState state,
    Agent a, {
    required Color ink,
    required Color pillFill,
    required Color pillInk,
    required Color body,
    required Color cut,
  }) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  CritterSprite(
                    pose: critterPoseOf(a, inStatus: state.inStatus(a)),
                    celebrateUntil: DateTime.now().add(
                      critterCelebration - state.inStatus(a),
                    ),
                    afterCelebrate: a.unseen
                        ? CritterPose.hungry
                        : CritterPose.idle,
                    species: critterSpeciesFor(a.tool),
                    width: 54,
                    body: body,
                    cut: cut,
                  ),
                  if (a.subagents.isNotEmpty) ...[
                    const SizedBox(width: 16),
                    SubagentCrew(subagents: a.subagents, body: body, cut: cut),
                  ],
                ],
              ),
              const SizedBox(height: 10),
              Text(
                a.title,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: DeckHud.rm(30, ink, weight: 700, spacing: -0.3),
              ),
            ],
          ),
        ),
        const SizedBox(width: 16),
        Column(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            HudClock(color: ink, size: 22),
            const SizedBox(height: 10),
            Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Container(width: 8, height: 8, color: agentToolColor(a.tool)),
                const SizedBox(width: 8),
                ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 300),
                  child: Container(
                    key: const Key('agent-pill'),
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 4,
                    ),
                    decoration: BoxDecoration(
                      color: pillFill,
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: Text(
                      agentPillLabel(a, state),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: DeckHud.mono(size: 15, color: pillInk),
                    ),
                  ),
                ),
              ],
            ),
          ],
        ),
      ],
    );
  }

  /// The back button at the start of a card's bottom row (hosts without an
  /// edge swipe): a square in the house button style, with a chevron.
  List<Widget> _back({
    required Color ink,
    required Color outline,
    required Color pressedFill,
    required Color pressedInk,
  }) => [
    if (widget.showBack) ...[
      _BackButton(
        onTap: widget.onClose,
        ink: ink,
        outline: outline,
        pressedFill: pressedFill,
        pressedInk: pressedInk,
      ),
      const SizedBox(width: 14),
    ],
  ];

  Widget _label(String text, Color color) =>
      Text(text, style: DeckHud.mono(size: 14, color: color));

  /// Text link variant of "focus terminal" (prompt cards' footers).
  Widget _focusLink(Agent a, {required Color ink, required Color surface}) {
    final flash = _flashColors('focus', onAccent: surface == DeckHud.accent);
    return Pressable(
      key: const Key('agent-focus'),
      onTap: _closing ? null : () => _focus(a),
      builder: (context, pressed) {
        final fill = flash?.$1 ?? (pressed ? ink : null);
        final color = flash?.$2 ?? (pressed ? surface : ink);
        return Container(
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
          decoration: fill == null
              ? null
              : BoxDecoration(
                  color: fill,
                  borderRadius: BorderRadius.circular(6),
                ),
          child: Text(
            _flashKey == 'focus' && _flash == _Flash.sent
                ? 'focused'
                : 'focus terminal',
            style: DeckHud.mono(size: 15, color: color).copyWith(
              decoration: TextDecoration.underline,
              decorationColor: color,
            ),
          ),
        );
      },
    );
  }

  // ---- overview (not blocked) ----------------------------------------------

  Widget _overview(AgentsState state, Agent a) {
    final (pillFill, pillInk) = switch (a.status) {
      AgentStatus.running => (DeckHud.ink, DeckHud.bg),
      AgentStatus.error => (DeckHud.accent, DeckHud.bg),
      _ => (DeckHud.panel, DeckHud.ink),
    };
    final body = AgentLook.of(a).body;
    final (nowLabel, nowValue) = _doingNow(a);
    final footer = [
      a.modelLabel,
      tildePath(a.cwd),
      a.branch,
    ].where((s) => s.isNotEmpty).join(' · ');
    final running = a.status == AgentStatus.running;
    final exited = a.status == AgentStatus.exited;
    final focusFlash = _flashColors('focus', onAccent: false);
    final intFlash = _flashColors('interrupt', onAccent: false);
    final rmFlash = _flashColors('dismiss', onAccent: false);
    final resumeFlash = _flashColors('resume', onAccent: false);
    return _card(
      fill: DeckHud.bg,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _topBar(
            state,
            a,
            ink: DeckHud.ink,
            pillFill: pillFill,
            pillInk: pillInk,
            body: body,
            cut: DeckHud.bg,
          ),
          const SizedBox(height: 26),
          _label(nowLabel, DeckHud.dim),
          const SizedBox(height: 8),
          nowValue,
          const SizedBox(height: 26),
          Row(
            crossAxisAlignment: CrossAxisAlignment.baseline,
            textBaseline: TextBaseline.alphabetic,
            children: [
              Text('context', style: DeckHud.rm(22, DeckHud.ink, weight: 700)),
              const SizedBox(width: 12),
              Text(
                a.contextWindow > 0
                    ? '${compactCount(a.contextUsed)} / ${compactCount(a.contextWindow)}'
                    : '--',
                style: DeckHud.mono(size: 14, color: DeckHud.dim),
              ),
              const Spacer(),
              Text(
                a.contextWindow > 0 ? '${a.contextPercent}%' : '--',
                style: DeckHud.rm(22, DeckHud.ink, weight: 700),
              ),
            ],
          ),
          const SizedBox(height: 10),
          TickBar(value: a.contextFraction, color: DeckHud.ink),
          const SizedBox(height: 26),
          Row(
            children: [
              for (final (label, value) in [
                ('tokens out', compactCount(a.tokensOutput)),
                ('cache read', compactCount(a.cacheRead)),
                (
                  'cost · est',
                  a.external ? '—' : '\$${a.costUsd.toStringAsFixed(2)}',
                ),
                ('turns', '${a.turns}'),
              ])
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      _label(label, DeckHud.dim),
                      const SizedBox(height: 6),
                      Text(
                        value,
                        style: DeckHud.rm(22, DeckHud.ink, weight: 600),
                      ),
                    ],
                  ),
                ),
            ],
          ),
          const SizedBox(height: 26),
          _label('last prompt', DeckHud.dim),
          const SizedBox(height: 8),
          Expanded(
            child: Text(
              a.lastPrompt.isEmpty ? '—' : a.lastPrompt,
              maxLines: 4,
              overflow: TextOverflow.ellipsis,
              style: DeckHud.rm(17, DeckHud.ink).copyWith(height: 1.45),
            ),
          ),
          Row(
            children: [
              Expanded(
                child: Text(
                  _note ?? footer,
                  key: _note == null ? null : const Key('agent-note'),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: DeckHud.mono(
                    size: 14,
                    color: _note == null ? DeckHud.dim : DeckHud.accent,
                  ),
                ),
              ),
              const SizedBox(width: 16),
              Text(
                state.server.name,
                style: DeckHud.mono(size: 14, color: DeckHud.dim),
              ),
            ],
          ),
          const SizedBox(height: 22),
          Row(
            children: [
              ..._back(
                ink: DeckHud.ink,
                outline: DeckHud.dim,
                pressedFill: DeckHud.ink,
                pressedInk: DeckHud.bg,
              ),
              if (!exited)
                Expanded(
                  child: AgentButton(
                    key: const Key('agent-focus'),
                    label: _flashKey == 'focus' && _flash == _Flash.sent
                        ? 'focused'
                        : a.external
                        ? 'open in codex'
                        : a.hungry
                        ? 'review in terminal'
                        : 'focus terminal',
                    onTap: _busy ? null : () => _focus(a),
                    fill: focusFlash?.$1 ?? DeckHud.ink,
                    ink: focusFlash?.$2 ?? DeckHud.bg,
                    outline: focusFlash?.$1 ?? DeckHud.ink,
                    pressedFill: DeckHud.accent,
                    pressedInk: DeckHud.bg,
                  ),
                ),
              if (running && !a.external) ...[
                const SizedBox(width: 14),
                SizedBox(
                  width: 200,
                  child: AgentButton(
                    key: const Key('agent-interrupt'),
                    label: _flashKey == 'interrupt' && _flash == _Flash.sent
                        ? 'sent'
                        : 'interrupt',
                    onTap: _busy ? null : () => _interrupt(a),
                    fill: intFlash?.$1 ?? const Color(0x00000000),
                    ink: intFlash?.$2 ?? DeckHud.ink,
                    outline: intFlash?.$1 ?? DeckHud.dim,
                    pressedFill: DeckHud.ink,
                    pressedInk: DeckHud.bg,
                  ),
                ),
              ] else if (exited && a.resumable) ...[
                Expanded(
                  child: AgentButton(
                    key: const Key('agent-resume'),
                    label: _flashKey == 'resume' && _flash == _Flash.sent
                        ? 'resuming'
                        : 'resume',
                    onTap: _busy ? null : () => _resume(a),
                    fill: resumeFlash?.$1 ?? DeckHud.ink,
                    ink: resumeFlash?.$2 ?? DeckHud.bg,
                    outline: resumeFlash?.$1 ?? DeckHud.ink,
                    pressedFill: DeckHud.accent,
                    pressedInk: DeckHud.bg,
                  ),
                ),
                const SizedBox(width: 14),
                SizedBox(width: 200, child: _removeButton(a, rmFlash)),
              ] else if (exited)
                Expanded(child: _removeButton(a, rmFlash))
              else ...[
                const SizedBox(width: 14),
                SizedBox(width: 200, child: _removeButton(a, rmFlash)),
              ],
            ],
          ),
        ],
      ),
    );
  }

  (String, Widget) _doingNow(Agent a) {
    Widget line(String text, {Color color = DeckHud.ink}) => Text(
      text,
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
      style: DeckHud.rm(20, color),
    );
    switch (a.status) {
      case AgentStatus.running:
        final act = a.activity;
        if (act == null) return ('doing now', line('thinking'));
        return (
          'doing now',
          Text.rich(
            TextSpan(
              children: [
                TextSpan(text: act.verb),
                if (act.detail.isNotEmpty) ...[
                  const TextSpan(
                    text: ' · ',
                    style: TextStyle(color: DeckHud.dim),
                  ),
                  TextSpan(text: act.detail),
                ],
              ],
            ),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: DeckHud.rm(20, DeckHud.ink),
          ),
        );
      case AgentStatus.error:
        return (
          'error',
          line(a.errorMessage ?? 'error', color: DeckHud.accent),
        );
      case AgentStatus.starting:
        return (
          'starting',
          line('launching ${a.tool.isEmpty ? 'agent' : a.tool}…'),
        );
      case AgentStatus.idle:
      case AgentStatus.exited:
      case AgentStatus.unknown:
      case AgentStatus.waiting:
        final t = a.aiTitle.isNotEmpty ? a.aiTitle : a.folder;
        return ('session', line(t.isEmpty ? '—' : t.toLowerCase()));
    }
  }

  // ---- waiting: permission ---------------------------------------------------

  Widget _permission(AgentsState state, Agent a, AgentPrompt p) {
    return _card(
      fill: DeckHud.accent,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _topBar(
            state,
            a,
            ink: DeckHud.bg,
            pillFill: DeckHud.bg,
            pillInk: DeckHud.accent,
            body: DeckHud.bg,
            cut: DeckHud.accent,
          ),
          const SizedBox(height: 22),
          Text(
            p.title.isEmpty ? 'allow this?' : p.title,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: DeckHud.rm(
              24,
              DeckHud.bg,
              weight: 700,
            ).copyWith(height: 1.25),
          ),
          if (p.detail.isNotEmpty) ...[
            const SizedBox(height: 10),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 16),
              decoration: BoxDecoration(
                color: DeckHud.bg,
                borderRadius: BorderRadius.circular(12),
              ),
              child: Text(
                p.detail,
                maxLines: 4,
                overflow: TextOverflow.ellipsis,
                style: DeckHud.rm(17, DeckHud.ink).copyWith(height: 1.45),
              ),
            ),
          ],
          if (p.context.isNotEmpty) ...[
            const SizedBox(height: 10),
            Text(
              p.context,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: DeckHud.mono(size: 14, color: DeckHud.bg),
            ),
          ],
          const SizedBox(height: 22),
          Expanded(
            child: _options([
              for (final o in p.options) _permissionButton(a, p, o),
            ], bottom: true),
          ),
          const SizedBox(height: 22),
          Row(
            children: [
              ..._back(
                ink: DeckHud.bg,
                outline: DeckHud.bg,
                pressedFill: DeckHud.bg,
                pressedInk: DeckHud.accent,
              ),
              Expanded(
                child: Text(
                  _note ?? 'answer here or at the terminal',
                  key: _note == null ? null : const Key('agent-note'),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: DeckHud.mono(size: 15, color: DeckHud.bg),
                ),
              ),
              _focusLink(a, ink: DeckHud.bg, surface: DeckHud.accent),
            ],
          ),
        ],
      ),
    );
  }

  Widget _permissionButton(Agent a, AgentPrompt p, AgentOption o) {
    final flash = _flashColors(o.key, onAccent: true);
    final sent = _flashKey == o.key && _flash == _Flash.sent;
    final fill =
        flash?.$1 ?? (o.primary ? DeckHud.bg : const Color(0x00000000));
    final ink = flash?.$2 ?? (o.primary ? DeckHud.accent : DeckHud.bg);
    final button = AgentButton(
      key: Key('agent-option-${o.key}'),
      label: '${o.key}  ${sent ? 'sent' : o.label}',
      onTap: _busy ? null : () => _answer(a, p, o.key),
      fill: fill,
      ink: ink,
      outline: flash?.$1 ?? DeckHud.bg,
      pressedFill: o.primary ? DeckHud.ink : DeckHud.bg,
      pressedInk: o.primary ? DeckHud.bg : DeckHud.accent,
      height: 64,
      fontSize: o.primary ? 20 : 18,
      alignLeft: true,
    );
    // Once one answer is out, the others step back.
    return Opacity(opacity: _closing && !sent ? 0.4 : 1, child: button);
  }

  /// Option stack: natural height, scrolls only if a prompt brings more
  /// options than fit.
  Widget _options(List<Widget> children, {bool bottom = false}) {
    final column = Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (final (i, c) in children.indexed) ...[
          if (i > 0) const SizedBox(height: 10),
          c,
        ],
      ],
    );
    return LayoutBuilder(
      builder: (context, constraints) => SingleChildScrollView(
        physics: const ClampingScrollPhysics(),
        child: ConstrainedBox(
          constraints: BoxConstraints(minHeight: constraints.maxHeight),
          child: Align(
            alignment: bottom ? Alignment.bottomCenter : Alignment.topCenter,
            child: column,
          ),
        ),
      ),
    );
  }

  // ---- waiting: question ----------------------------------------------------

  Widget _question(AgentsState state, Agent a, AgentPrompt p) {
    final selected = p.options.where((o) => o.key == _selected).firstOrNull;
    final sendFlash = _flashColors('send', onAccent: false);
    final sent = _flashKey == 'send' && _flash == _Flash.sent;
    return _card(
      fill: DeckHud.bg,
      border: DeckHud.accent,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _topBar(
            state,
            a,
            ink: DeckHud.ink,
            pillFill: DeckHud.accent,
            pillInk: DeckHud.bg,
            body: DeckHud.accent,
            cut: DeckHud.bg,
          ),
          const SizedBox(height: 20),
          Text(
            p.title.isEmpty ? 'question' : p.title,
            maxLines: 3,
            overflow: TextOverflow.ellipsis,
            style: DeckHud.rm(
              23,
              DeckHud.ink,
              weight: 700,
            ).copyWith(height: 1.35),
          ),
          if (p.detail.isNotEmpty) ...[
            const SizedBox(height: 8),
            Text(
              p.detail,
              maxLines: 3,
              overflow: TextOverflow.ellipsis,
              style: DeckHud.rm(16, DeckHud.dim).copyWith(height: 1.4),
            ),
          ],
          const SizedBox(height: 20),
          Expanded(
            child: _options([for (final o in p.options) _questionRow(o)]),
          ),
          if (_note != null) ...[
            const SizedBox(height: 10),
            Text(
              _note!,
              key: const Key('agent-note'),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: DeckHud.mono(size: 15, color: DeckHud.accent),
            ),
          ],
          const SizedBox(height: 20),
          Row(
            children: [
              ..._back(
                ink: DeckHud.ink,
                outline: DeckHud.dim,
                pressedFill: DeckHud.ink,
                pressedInk: DeckHud.bg,
              ),
              Expanded(
                child: AgentButton(
                  key: const Key('agent-send'),
                  label: sent
                      ? 'sent'
                      : (selected == null ? 'send' : 'send ${selected.key}'),
                  onTap: _busy || selected == null
                      ? null
                      : () => _run(
                          'send',
                          () => widget.source.answer(a.id, p.id, selected.key),
                          closeOnOk: true,
                        ),
                  fill:
                      sendFlash?.$1 ??
                      (selected == null ? DeckHud.panel : DeckHud.accent),
                  ink:
                      sendFlash?.$2 ??
                      (selected == null ? DeckHud.dim : DeckHud.bg),
                  outline:
                      sendFlash?.$1 ??
                      (selected == null ? DeckHud.panel : DeckHud.accent),
                  pressedFill: DeckHud.ink,
                  pressedInk: DeckHud.bg,
                ),
              ),
              const SizedBox(width: 14),
              SizedBox(
                width: 260,
                child: _terminalButton(a, 'type in terminal'),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _questionRow(AgentOption o) {
    final on = o.key == _selected;
    final (title, sub) = o.titleAndSub;
    return Pressable(
      key: Key('agent-option-${o.key}'),
      onTap: _busy
          ? null
          : () => setState(() {
              _selected = o.key;
              _note = null;
            }),
      builder: (context, pressed) {
        final ink = on || pressed;
        final fg = ink ? DeckHud.bg : DeckHud.ink;
        return Container(
          constraints: const BoxConstraints(minHeight: 74),
          padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
          decoration: BoxDecoration(
            color: ink ? DeckHud.ink : DeckHud.panel,
            borderRadius: BorderRadius.circular(14),
            border: Border.all(color: ink ? DeckHud.ink : agentsRowOutline),
          ),
          child: Row(
            children: [
              SizedBox(
                width: 22,
                child: Text(o.key, style: DeckHud.rm(22, fg, weight: 700)),
              ),
              const SizedBox(width: 18),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: DeckHud.mono(size: 18, color: fg),
                    ),
                    if (sub.isNotEmpty) ...[
                      const SizedBox(height: 4),
                      Text(
                        sub,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: DeckHud.mono(
                          size: 13.5,
                          color: ink ? agentsRowSubOnInk : DeckHud.dim,
                        ),
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
        );
      },
    );
  }

  /// Outlined "focus the terminal" button for the dark prompt cards.
  Widget _terminalButton(Agent a, String label) {
    final flash = _flashColors('focus', onAccent: false);
    return AgentButton(
      key: const Key('agent-focus'),
      label: _flashKey == 'focus' && _flash == _Flash.sent ? 'focused' : label,
      onTap: _busy ? null : () => _focus(a),
      fill: flash?.$1 ?? const Color(0x00000000),
      ink: flash?.$2 ?? DeckHud.ink,
      outline: flash?.$1 ?? DeckHud.dim,
      pressedFill: DeckHud.ink,
      pressedInk: DeckHud.bg,
    );
  }

  // ---- waiting: free-text input ------------------------------------------------

  Widget _input(AgentsState state, Agent a, AgentPrompt p) {
    return _card(
      fill: DeckHud.bg,
      border: DeckHud.accent,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _topBar(
            state,
            a,
            ink: DeckHud.ink,
            pillFill: DeckHud.accent,
            pillInk: DeckHud.bg,
            body: DeckHud.accent,
            cut: DeckHud.bg,
          ),
          const SizedBox(height: 24),
          Text(
            p.title.isEmpty ? 'needs input' : p.title,
            maxLines: 3,
            overflow: TextOverflow.ellipsis,
            style: DeckHud.rm(
              23,
              DeckHud.ink,
              weight: 700,
            ).copyWith(height: 1.35),
          ),
          if (p.detail.isNotEmpty) ...[
            const SizedBox(height: 14),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 16),
              decoration: BoxDecoration(
                color: DeckHud.panel,
                borderRadius: BorderRadius.circular(12),
              ),
              child: Text(
                p.detail,
                maxLines: 6,
                overflow: TextOverflow.ellipsis,
                style: DeckHud.rm(17, DeckHud.ink).copyWith(height: 1.45),
              ),
            ),
          ],
          if (p.context.isNotEmpty) ...[
            const SizedBox(height: 10),
            Text(
              p.context,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: DeckHud.mono(size: 14, color: DeckHud.dim),
            ),
          ],
          const Spacer(),
          Text(
            _note ?? 'free-text answer · type it at the terminal',
            key: _note == null ? null : const Key('agent-note'),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: DeckHud.mono(
              size: 15,
              color: _note == null ? DeckHud.dim : DeckHud.accent,
            ),
          ),
          const SizedBox(height: 20),
          Row(
            children: [
              ..._back(
                ink: DeckHud.ink,
                outline: DeckHud.dim,
                pressedFill: DeckHud.ink,
                pressedInk: DeckHud.bg,
              ),
              Expanded(child: _terminalButton(a, 'focus terminal')),
            ],
          ),
        ],
      ),
    );
  }

  // ---- agent gone ------------------------------------------------------------

  Widget _gone() {
    if (!_goneScheduled) {
      _goneScheduled = true;
      _timer?.cancel();
      _timer = Timer(const Duration(milliseconds: 1500), () {
        if (mounted) widget.onClose();
      });
    }
    return _card(
      fill: DeckHud.bg,
      child: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const CritterSprite(
              pose: CritterPose.exited,
              width: 92,
              body: DeckHud.dim,
              cut: DeckHud.bg,
            ),
            const SizedBox(height: 22),
            Text('agent gone', style: DeckHud.rm(26, DeckHud.ink, weight: 700)),
            const SizedBox(height: 10),
            Text(
              'it left the fleet',
              style: DeckHud.mono(size: 15, color: DeckHud.dim),
            ),
          ],
        ),
      ),
    );
  }
}

/// An agent's working subagents: small critters next to its mascot, each
/// posed by its latest tool, and a count.
class SubagentCrew extends StatelessWidget {
  const SubagentCrew({
    super.key,
    required this.subagents,
    required this.body,
    required this.cut,
  });

  static const max = 6;

  final List<AgentSubagent> subagents;
  final Color body;
  final Color cut;

  @override
  Widget build(BuildContext context) {
    final shown = subagents.take(max).toList();
    final more = subagents.length - shown.length;
    final n = subagents.length;
    return Semantics(
      label:
          '$n subagent${n == 1 ? '' : 's'}: '
          '${subagents.map((s) => s.title).join(', ')}',
      child: Row(
        key: const Key('agent-subagents'),
        crossAxisAlignment: CrossAxisAlignment.end,
        mainAxisSize: MainAxisSize.min,
        children: [
          for (final s in shown)
            Padding(
              padding: const EdgeInsets.only(right: 9),
              child: CritterSprite(
                pose: critterPoseForTool(s.tool),
                width: 36,
                body: body,
                cut: cut,
              ),
            ),
          const SizedBox(width: 4),
          Text(
            '$n subagent${n == 1 ? '' : 's'}${more > 0 ? ' · +$more' : ''}',
            style: DeckHud.rm(13, body),
          ),
        ],
      ),
    );
  }
}

/// A square "‹" button that closes the card (hosts without an edge swipe),
/// the same height and outline as the [AgentButton]s beside it.
class _BackButton extends StatelessWidget {
  const _BackButton({
    required this.onTap,
    required this.ink,
    required this.outline,
    required this.pressedFill,
    required this.pressedInk,
  });

  final VoidCallback onTap;
  final Color ink;
  final Color outline;
  final Color pressedFill;
  final Color pressedInk;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      button: true,
      label: 'back',
      child: Pressable(
        key: const Key('agent-back'),
        onTap: onTap,
        builder: (context, pressed) => Container(
          width: 68,
          height: 68,
          decoration: BoxDecoration(
            color: pressed ? pressedFill : null,
            borderRadius: BorderRadius.circular(14),
            border: Border.all(
              color: pressed ? pressedFill : outline,
              width: 2,
            ),
          ),
          child: CustomPaint(
            painter: _ChevronPainter(pressed ? pressedInk : ink),
          ),
        ),
      ),
    );
  }
}

/// A square-capped "<", the deck's pixel style.
class _ChevronPainter extends CustomPainter {
  _ChevronPainter(this.color);

  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    final c = size.center(Offset.zero);
    canvas.drawPath(
      Path()
        ..moveTo(c.dx + 4, c.dy - 8)
        ..lineTo(c.dx - 4, c.dy)
        ..lineTo(c.dx + 4, c.dy + 8),
      Paint()
        ..color = color
        ..style = PaintingStyle.stroke
        ..strokeWidth = 3
        ..strokeCap = StrokeCap.square
        ..strokeJoin = StrokeJoin.miter,
    );
  }

  @override
  bool shouldRepaint(_ChevronPainter old) => old.color != color;
}
