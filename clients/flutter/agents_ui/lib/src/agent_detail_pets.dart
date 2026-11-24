part of 'agent_detail.dart';

// An agent's page while Agent pets is on: its pet is the only mascot. The
// pet stands on a ground line at the top left (in a dark box when the agent
// needs you, in an outlined box with "tap to hatch" for a ready egg), with
// the status, the name in a serif and the session's title beside it, and
// the pet's hearts, level and bar on the right. Under it: what it does now,
// the last prompt, the context and two actions. Tap the pet for its own
// page. Only the card is rounded; buttons, chips and boxes are square.

const _pRule = Color(0xFF3B3A36);
const _pLine = Color(0xFF262622);
const _pText = Color(0xFFC9C5BC);
const _pMuted = Color(0xFF9A968C);
const _pOrange2 = Color(0xFFFF6A2E);
const _pLevel = Color(0xFFFF8A4C);
const _pTrack = Color(0xFF2E2D2A);
const _pSegOff = Color(0xFF2A2925);
const _pInkOnOrange = Color(0xFF0A0A0A);

/// A small capitalised label ("DOING NOW"), with an optional dimmer tail
/// (" · BASH").
Widget _pLabel(
  String text,
  Color color, {
  String tail = '',
  Color? tailColor,
  double size = 16,
  Key? key,
}) => Text.rich(
  TextSpan(
    children: [
      TextSpan(text: text.toUpperCase()),
      if (tail.isNotEmpty)
        TextSpan(
          text: ' · ${tail.toUpperCase()}',
          style: TextStyle(color: tailColor ?? DeckHud.dim),
        ),
    ],
  ),
  key: key,
  maxLines: 1,
  overflow: TextOverflow.ellipsis,
  style: DeckHud.rm(
    size,
    color,
    weight: 700,
    spacing: size * 0.12,
  ).copyWith(height: 1.25),
);

/// A square status chip.
Widget _pChip(
  String text, {
  required Color fill,
  required Color ink,
  Color? outline,
}) => Container(
  key: const Key('agent-pill'),
  height: 30,
  padding: const EdgeInsets.symmetric(horizontal: 12),
  decoration: BoxDecoration(
    color: fill,
    border: outline == null ? null : Border.all(color: outline, width: 2),
  ),
  child: Center(
    widthFactor: 1,
    child: Text(
      text.toUpperCase(),
      maxLines: 1,
      style: DeckHud.rm(15, ink, weight: 700, spacing: 15 * 0.12),
    ),
  ),
);

/// A slim bar: [value] of it filled.
Widget _pBar(
  double value, {
  required double width,
  required double height,
  required Color fill,
  required Color track,
}) => SizedBox(
  width: width,
  height: height,
  child: ColoredBox(
    color: track,
    child: FractionallySizedBox(
      alignment: Alignment.centerLeft,
      widthFactor: value.clamp(0.0, 1.0),
      child: ColoredBox(color: fill),
    ),
  ),
);

/// "1,040,000".
String _pNum(int n) {
  final s = n.abs().toString();
  final b = StringBuffer(n < 0 ? '-' : '');
  for (var i = 0; i < s.length; i++) {
    if (i > 0 && (s.length - i) % 3 == 0) b.write(',');
    b.write(s[i]);
  }
  return b.toString();
}

/// A prompt as people read it: the wrappers tools put round pasted text
/// and messages (`<pasted_content id="...">`, `<agent-message ...>`: tags
/// whose names have a "-" or "_") are left out, spaces folded.
String _pPrompt(String s) => s
    .replaceAll(RegExp(r'</?[a-zA-Z]+[-_][a-zA-Z_-]*(\s[^>]*)?>'), ' ')
    .replaceAll(RegExp(r'\s+'), ' ')
    .trim();

/// First letter up: "run this command?" -> "Run this command?".
String _pSentence(String s) =>
    s.isEmpty ? s : s[0].toUpperCase() + s.substring(1);

/// The context as one row of square segments, orange from 85%.
class _ContextSegments extends StatelessWidget {
  const _ContextSegments(this.fraction);

  final double fraction;

  @override
  Widget build(BuildContext context) {
    const n = 40;
    final on = (fraction.clamp(0.0, 1.0) * n).round();
    final hot = fraction >= 0.85;
    return SizedBox(
      height: 10,
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          for (var i = 0; i < n; i++) ...[
            if (i > 0) const SizedBox(width: 3),
            Expanded(
              child: ColoredBox(
                color: i < on ? (hot ? DeckHud.accent : DeckHud.ink) : _pSegOff,
              ),
            ),
          ],
        ],
      ),
    );
  }
}

extension _PetsPage on _AgentDetailState {
  /// The colours of a card: orange for a permission, dark otherwise.
  Widget _petCard({
    required Color fill,
    Color? border,
    required Widget child,
  }) => _PetDesignBox(
    child: DecoratedBox(
      key: const Key('agent-pets-card'),
      decoration: BoxDecoration(
        color: fill,
        borderRadius: BorderRadius.circular(_cardRadius),
        border: border == null ? null : Border.all(color: border, width: 3),
      ),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(36, 32, 36, 30),
        child: child,
      ),
    ),
  );

  /// The page while pets are on: the pet's own page, a prompt, or the
  /// overview.
  Widget _petsBuild(AgentsState state, Agent a, AgentPrompt? p) {
    if (_petPage) return _petView(state, a);
    return switch (p?.kind) {
      PromptKind.permission => _petPermission(state, a, p!),
      PromptKind.question => _petQuestion(state, a, p!),
      PromptKind.input => _petInput(state, a, p!),
      null => _petOverview(state, a),
    };
  }

  // ---- the head -------------------------------------------------------------

  Widget _petHead(
    AgentsState state,
    Agent a, {
    bool orange = false,
    bool boxed = false,
  }) {
    final pet = a.pet!;
    final ink = orange ? _pInkOnOrange : DeckHud.ink;
    final t = shortDuration(state.inStatus(a));
    final chip = switch (a.status) {
      AgentStatus.running => _pChip(
        'working · $t',
        fill: DeckHud.ink,
        ink: DeckHud.bg,
      ),
      AgentStatus.waiting when orange => _pChip(
        'needs you · $t',
        fill: _pInkOnOrange,
        ink: DeckHud.accent,
      ),
      AgentStatus.waiting => _pChip(
        'needs you · $t',
        fill: DeckHud.accent,
        ink: _pInkOnOrange,
      ),
      AgentStatus.error => _pChip(
        'error',
        fill: DeckHud.accent,
        ink: _pInkOnOrange,
      ),
      AgentStatus.idle when a.hungry => _pChip(
        'hungry · $t',
        fill: DeckHud.accent,
        ink: _pInkOnOrange,
      ),
      AgentStatus.exited => _pChip(
        a.lost ? 'lost · $t' : 'exited · $t',
        fill: const Color(0x00000000),
        ink: _pMuted,
        outline: _pRule,
      ),
      AgentStatus.starting => _pChip(
        'starting · $t',
        fill: const Color(0x00000000),
        ink: _pMuted,
        outline: _pRule,
      ),
      _ => _pChip(
        'idle · $t',
        fill: const Color(0x00000000),
        ink: _pMuted,
        outline: _pRule,
      ),
    };
    final session = a.aiTitle.isNotEmpty ? a.aiTitle : a.folder;
    return ConstrainedBox(
      constraints: const BoxConstraints(minHeight: 132),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _petZone(state, a, pet, orange: orange, boxed: boxed),
          const SizedBox(width: 22),
          Expanded(
            child: Padding(
              padding: const EdgeInsets.only(top: 2),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  // The status chip, and the pet's level in the corner on
                  // the same row: the name and its session line get the
                  // whole width under them.
                  Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      chip,
                      const Spacer(),
                      const SizedBox(width: 16),
                      _petMeta(pet, orange: orange),
                    ],
                  ),
                  const SizedBox(height: 6),
                  _FitName(
                    a.title,
                    key: const Key('agent-title'),
                    color: ink,
                    size: 56,
                    smallest: 34,
                  ),
                  const SizedBox(height: 6),
                  Text(
                    session.toLowerCase(),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: DeckHud.rm(
                      18,
                      orange ? _pInkOnOrange.withValues(alpha: 0.74) : _pMuted,
                    ),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  /// Hearts, level and its bar (no clock on this page).
  Widget _petMeta(AgentPet pet, {required bool orange}) {
    final ink = orange ? _pInkOnOrange : DeckHud.ink;
    final fill = orange ? _pInkOnOrange : _pLevel;
    final track = orange ? _pInkOnOrange.withValues(alpha: 0.22) : _pTrack;
    if (pet.isEgg) {
      final pct = pet.nextAt > 0 ? (pet.ate / pet.nextAt).clamp(0.0, 1.0) : 0.0;
      return Padding(
        padding: const EdgeInsets.only(top: 2),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            if (pet.ready)
              _pChip('ready to hatch', fill: _pLevel, ink: _pInkOnOrange)
            else ...[
              Text(
                'EGG · ${(pct * 100).floor()}%',
                key: const Key('agent-pet-level'),
                style: DeckHud.rm(20, ink, weight: 700, spacing: 1.2),
              ),
              const SizedBox(height: 7),
              _pBar(pct, width: 150, height: 6, fill: fill, track: track),
            ],
          ],
        ),
      );
    }
    return Padding(
      padding: const EdgeInsets.only(top: 2),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Semantics(
                label: '${pet.hearts} of $petMaxHearts hearts',
                child: PetHearts(hearts: pet.hearts, size: 3, onOrange: orange),
              ),
              const SizedBox(width: 12),
              Text(
                'LV ${pet.level}',
                key: const Key('agent-pet-level'),
                style: DeckHud.rm(20, ink, weight: 700, spacing: 1.2),
              ),
            ],
          ),
          const SizedBox(height: 7),
          _pBar(pet.progress, width: 150, height: 6, fill: fill, track: track),
        ],
      ),
    );
  }

  /// The pet: on its ground line, in a dark box (needs you), or boxed with
  /// "tap to hatch" (a ready egg). Tapping it opens its page; tapping a
  /// ready egg hatches it.
  Widget _petZone(
    AgentsState state,
    Agent a,
    AgentPet pet, {
    required bool orange,
    required bool boxed,
  }) {
    final pose = critterPoseOf(a, inStatus: state.inStatus(a));
    final until = DateTime.now().add(critterCelebration - state.inStatus(a));
    final after = a.unseen ? CritterPose.hungry : CritterPose.idle;
    void open() => _set(() {
      _petPage = true;
      _note = null;
    });
    return PetHatcher(
      pet: pet,
      onHatch: () => widget.source.hatch(a.id),
      onProblem: (m) => _set(() => _note = m),
      builder: (context, pet, hatch) {
        if (hatch != null) {
          return GestureDetector(
            key: const Key('pet-hatch'),
            behavior: HitTestBehavior.opaque,
            onTap: hatch,
            child: Container(
              width: 144,
              height: 132,
              color: DeckHud.panel,
              foregroundDecoration: BoxDecoration(
                border: Border.all(color: _pLevel, width: 3),
              ),
              child: Stack(
                clipBehavior: Clip.none,
                children: [
                  Positioned(
                    left: 0,
                    top: 0,
                    width: 144,
                    height: 100,
                    child: PetSprite(
                      pet: pet,
                      hat: a.hat,
                      pose: pose,
                      // No bubble: the bar under it says what to do.
                      bubble: false,
                      cell: 5,
                      width: 144,
                      height: 100,
                      celebrateUntil: until,
                      afterCelebrate: after,
                    ),
                  ),
                  Positioned(
                    left: 0,
                    right: 0,
                    bottom: 0,
                    height: 26,
                    child: ColoredBox(
                      color: _pLevel,
                      child: Center(
                        child: Text(
                          'TAP TO HATCH',
                          style: DeckHud.rm(
                            _menuBar ? 16 : 13,
                            _pInkOnOrange,
                            weight: 700,
                            spacing: 1.56,
                          ),
                        ),
                      ),
                    ),
                  ),
                ],
              ),
            ),
          );
        }
        if (boxed) {
          // Needs you: the pet in its own colours on a dark box, its "?"
          // white and still (the orange already calls you).
          return GestureDetector(
            key: const Key('agent-pet'),
            behavior: HitTestBehavior.opaque,
            onTap: open,
            child: Container(
              width: 144,
              height: 132,
              // On the orange card a near-black box; on the dark one a
              // step lighter, so it still reads as a box.
              color: orange ? _pInkOnOrange : DeckHud.panel,
              child: PetSprite(
                pet: pet,
                hat: a.hat,
                pose: pose,
                calm: true,
                cell: 6,
                width: 144,
                height: 118,
              ),
            ),
          );
        }
        return GestureDetector(
          key: const Key('agent-pet'),
          behavior: HitTestBehavior.opaque,
          onTap: open,
          child: SizedBox(
            width: 150,
            height: 128,
            child: Stack(
              clipBehavior: Clip.none,
              children: [
                Positioned(
                  left: 0,
                  top: 0,
                  width: 132,
                  height: 122,
                  child: PetSprite(
                    pet: pet,
                    hat: a.hat,
                    pose: pose,
                    cell: 6,
                    width: 132,
                    height: 122,
                    celebrateUntil: until,
                    afterCelebrate: after,
                  ),
                ),
                Positioned(
                  left: 0,
                  right: 6,
                  top: 122,
                  height: 2,
                  child: ColoredBox(
                    color: orange
                        ? _pInkOnOrange.withValues(alpha: 0.45)
                        : _pRule,
                  ),
                ),
              ],
            ),
          ),
        );
      },
    );
  }

  // ---- the overview -----------------------------------------------------------

  Widget _petOverview(AgentsState state, Agent a) {
    final running = a.status == AgentStatus.running;
    final exited = a.status == AgentStatus.exited;
    final focusFlash = _flashColors('focus', onAccent: false);
    final intFlash = _flashColors('interrupt', onAccent: false);
    final rmFlash = _flashColors('dismiss', onAccent: false);
    final resumeFlash = _flashColors('resume', onAccent: false);
    final account = _petAccount(a);
    final tool = switch (a.tool) {
      'claude' => 'claude code',
      '' => '',
      _ => a.tool,
    };
    final where = [
      tool,
      if (a.modelLabel != a.tool) a.modelLabel,
      tildePath(a.cwd),
      a.branch,
    ].where((s) => s.isNotEmpty).join(' · ');
    final ago = state.lastActiveText(a).replaceFirst('last active ', '');
    final subs = running ? a.subagents : const <AgentSubagent>[];
    return _petCard(
      fill: DeckHud.bg,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _petHead(state, a),
          const SizedBox(height: 22),
          if (running)
            ..._petNow(a, subs, account)
          else
            _petStatus(state, a, account),
          const SizedBox(height: 22),
          _pLabel('last prompt', _pMuted, tail: ago),
          const SizedBox(height: 8),
          // Up to five lines of it, as many as fit.
          Expanded(
            child: LayoutBuilder(
              builder: (context, box) {
                final lines = (box.maxHeight / (20 * 1.45)).floor().clamp(
                  0,
                  subs.isNotEmpty ? 3 : 5,
                );
                if (lines == 0) return const SizedBox.shrink();
                return Text(
                  a.lastPrompt.isEmpty ? '-' : _pPrompt(a.lastPrompt),
                  key: const Key('agent-last-prompt'),
                  maxLines: lines,
                  overflow: TextOverflow.ellipsis,
                  style: DeckHud.rm(20, _pText).copyWith(height: 1.45),
                );
              },
            ),
          ),
          const SizedBox(height: 14),
          Row(
            key: const Key('agent-context'),
            children: [
              _pLabel('context', _pMuted),
              const SizedBox(width: 14),
              Expanded(
                child: _ContextSegments(
                  a.contextWindow > 0 ? a.contextFraction : 0,
                ),
              ),
              const SizedBox(width: 14),
              Text(
                a.contextWindow > 0 ? '${a.contextPercent}%' : '--',
                style: DeckHud.rm(18, DeckHud.ink, weight: 700),
              ),
            ],
          ),
          const SizedBox(height: 12),
          Row(
            children: [
              Expanded(
                child: Text(
                  _note ?? where,
                  key: _note == null ? null : const Key('agent-note'),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: DeckHud.rm(
                    16,
                    _note == null ? DeckHud.dim : DeckHud.accent,
                  ),
                ),
              ),
              if (!_menuBar) ...[
                const SizedBox(width: 16),
                Text(state.server.name, style: DeckHud.rm(16, DeckHud.dim)),
              ],
            ],
          ),
          const SizedBox(height: 16),
          Row(
            children: [
              ..._petBack(
                ink: DeckHud.ink,
                outline: _pRule,
                pressedFill: DeckHud.ink,
                pressedInk: DeckHud.bg,
              ),
              if (!exited)
                Expanded(
                  child: AgentButton(
                    key: const Key('agent-focus'),
                    square: true,
                    height: 64,
                    fontSize: 18,
                    label: _flashKey == 'focus' && _flash == _Flash.sent
                        ? 'focused'
                        : a.codexThread
                        ? 'open in codex'
                        : a.inApp
                        ? 'open in ${a.app}'
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
              if (running && (!a.external || a.inApp)) ...[
                const SizedBox(width: 14),
                SizedBox(
                  width: 200,
                  child: AgentButton(
                    key: const Key('agent-interrupt'),
                    square: true,
                    height: 64,
                    fontSize: 18,
                    label: _flashKey == 'interrupt' && _flash == _Flash.sent
                        ? 'sent'
                        : 'interrupt',
                    onTap: _busy ? null : () => _interrupt(a),
                    fill: intFlash?.$1 ?? const Color(0x00000000),
                    ink: intFlash?.$2 ?? DeckHud.ink,
                    outline: intFlash?.$1 ?? _pRule,
                    pressedFill: DeckHud.ink,
                    pressedInk: DeckHud.bg,
                  ),
                ),
              ] else if (exited && a.resumable) ...[
                Expanded(
                  child: AgentButton(
                    key: const Key('agent-resume'),
                    square: true,
                    height: 64,
                    fontSize: 18,
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
                SizedBox(
                  width: 200,
                  child: _removeButton(a, rmFlash, square: true),
                ),
              ] else if (exited)
                Expanded(child: _removeButton(a, rmFlash, square: true))
              else ...[
                const SizedBox(width: 14),
                SizedBox(
                  width: 200,
                  child: _removeButton(a, rmFlash, square: true),
                ),
              ],
            ],
          ),
        ],
      ),
    );
  }

  /// "Doing now · bash", a dark command box with "$ the command", and one
  /// "↳" line per subagent.
  List<Widget> _petNow(Agent a, List<AgentSubagent> subs, Widget? account) {
    var act = a.activity;
    // Only its subagents work (the service says so in the activity): no
    // command to show, just them.
    final subsOnly =
        act != null && RegExp(r'^\d+ subagents? working$').hasMatch(act.detail);
    if (subsOnly) act = null;
    // Two of them at most: the count is on the right, and the last prompt
    // keeps its room.
    final shown = subs.take(2).toList();
    return [
      Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            child: _pLabel(
              'doing now',
              _pMuted,
              tail: subsOnly
                  ? 'subagents'
                  : act == null
                  ? 'thinking'
                  : act.tool,
              key: const Key('agent-doing'),
            ),
          ),
          if (subs.isNotEmpty)
            _pLabel(
              subagentCount(subs.length),
              _pMuted,
              key: const Key('agent-subagent-count'),
            ),
          if (account != null) ...[const SizedBox(width: 28), account],
        ],
      ),
      const SizedBox(height: 10),
      Container(
        key: const Key('agent-command'),
        color: DeckHud.panel,
        padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text.rich(
              TextSpan(
                children: [
                  if (act != null)
                    const TextSpan(
                      text: r'$ ',
                      style: TextStyle(color: _pOrange2),
                    ),
                  TextSpan(
                    text: subsOnly
                        ? a.activity!.detail
                        : act == null
                        ? 'thinking'
                        : (act.detail.isNotEmpty ? act.detail : act.verb),
                  ),
                ],
              ),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: DeckHud.rm(
                21,
                DeckHud.ink,
                weight: 500,
              ).copyWith(height: 28 / 21),
            ),
            for (final s in shown) ...[
              const SizedBox(height: 8),
              Text.rich(
                TextSpan(
                  children: [
                    const WidgetSpan(
                      alignment: PlaceholderAlignment.middle,
                      child: _Arrow(turn: true, color: DeckHud.dim, size: 18),
                    ),
                    const TextSpan(text: ' '),
                    TextSpan(text: s.title.isNotEmpty ? s.title : s.type),
                    if (s.title.isNotEmpty && s.type.isNotEmpty)
                      TextSpan(
                        text: ' · ${s.type.toLowerCase()}',
                        style: const TextStyle(color: DeckHud.dim),
                      ),
                  ],
                ),
                key: Key('agent-subagent-${s.id}'),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: DeckHud.rm(18, _pText).copyWith(height: 24 / 18),
              ),
            ],
          ],
        ),
      ),
    ];
  }

  /// "Status" and how it is: finished, and whether anybody has looked.
  Widget _petStatus(AgentsState state, Agent a, Widget? account) {
    final t = shortDuration(state.inStatus(a));
    final (String main, String rest, Color color) = switch (a.status) {
      AgentStatus.error => (a.errorMessage ?? 'error', '', DeckHud.accent),
      AgentStatus.starting => (
        'starting ${a.tool.isEmpty ? 'the agent' : a.tool}',
        '',
        DeckHud.ink,
      ),
      AgentStatus.exited when a.lost => (
        'lost with tmux $t ago',
        '',
        DeckHud.ink,
      ),
      AgentStatus.exited => (
        'ended $t ago',
        a.resumable ? 'it can resume' : '',
        DeckHud.ink,
      ),
      _ => (
        'finished $t ago',
        a.hungry ? 'nobody has looked yet' : '',
        DeckHud.ink,
      ),
    };
    final status = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _pLabel('status', _pMuted),
        const SizedBox(height: 8),
        Text.rich(
          TextSpan(
            children: [
              TextSpan(text: main),
              if (rest.isNotEmpty)
                TextSpan(
                  text: ' · $rest',
                  style: const TextStyle(color: _pMuted),
                ),
            ],
          ),
          key: const Key('agent-status'),
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
          style: DeckHud.rm(22, color, weight: 500).copyWith(height: 30 / 22),
        ),
      ],
    );
    if (account == null) return status;
    // The status keeps left and gives way first; the account sits right.
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Expanded(child: status),
        const SizedBox(width: 28),
        account,
      ],
    );
  }

  /// Which Claude account it runs in, when there is more than one: a label
  /// and the name after a square in the account's colour.
  Widget? _petAccount(Agent a) {
    final acc = a.account;
    if (acc == null || (_accounts?.length ?? 0) < 2) return null;
    return Column(
      key: const Key('agent-account'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _pLabel('account', _pMuted),
        const SizedBox(height: 8),
        Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Container(width: 10, height: 10, color: hatColor(acc.color).$1),
            const SizedBox(width: 10),
            ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 200),
              child: Text(
                acc.name,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: DeckHud.rm(
                  22,
                  DeckHud.ink,
                  weight: 500,
                ).copyWith(height: 30 / 22),
              ),
            ),
          ],
        ),
      ],
    );
  }

  /// The back button in the actions row (the menu bar; hosts without an
  /// edge swipe).
  List<Widget> _petBack({
    required Color ink,
    required Color outline,
    required Color pressedFill,
    required Color pressedInk,
    double size = 64,
  }) => [
    if (widget.showBack) ...[
      CardBackButton(
        onTap: widget.onClose,
        ink: ink,
        outline: outline,
        pressedFill: pressedFill,
        pressedInk: pressedInk,
        square: true,
        size: size,
      ),
      const SizedBox(width: 14),
    ],
  ];

  // ---- needs you ----------------------------------------------------------------

  /// "It asks · permission", the question in a serif, the command box and
  /// the answers right under it.
  Widget _petPermission(AgentsState state, Agent a, AgentPrompt p) {
    const ink = _pInkOnOrange;
    final bash = p.context.toLowerCase().startsWith('bash');
    return _petCard(
      fill: DeckHud.accent,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _petHead(state, a, orange: true, boxed: true),
          const SizedBox(height: 20),
          _pLabel(
            'it asks',
            ink,
            tail: 'permission',
            tailColor: ink.withValues(alpha: 0.62),
          ),
          const SizedBox(height: 6),
          Text(
            _pSentence(p.title.isEmpty ? 'allow this?' : p.title),
            key: const Key('agent-question'),
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: DeckHud.serif(44, ink).copyWith(height: 48 / 44),
          ),
          if (p.detail.isNotEmpty || p.context.isNotEmpty) ...[
            const SizedBox(height: 10),
            Container(
              key: const Key('agent-command'),
              color: ink,
              padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 14),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (p.detail.isNotEmpty)
                    Text.rich(
                      TextSpan(
                        children: [
                          if (bash)
                            const TextSpan(
                              text: r'$ ',
                              style: TextStyle(color: _pOrange2),
                            ),
                          TextSpan(text: p.detail),
                        ],
                      ),
                      maxLines: 3,
                      overflow: TextOverflow.ellipsis,
                      style: DeckHud.rm(
                        21,
                        DeckHud.ink,
                        weight: 500,
                      ).copyWith(height: 28 / 21),
                    ),
                  if (p.context.isNotEmpty) ...[
                    const SizedBox(height: 8),
                    Text(
                      p.context,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: DeckHud.rm(16, _pMuted),
                    ),
                  ],
                ],
              ),
            ),
          ],
          const SizedBox(height: 18),
          Expanded(
            child: _options([
              for (final o in p.options)
                _permissionButton(a, p, o, square: true),
            ]),
          ),
          const SizedBox(height: 12),
          Row(
            children: [
              ..._petBack(
                ink: ink,
                outline: ink,
                pressedFill: ink,
                pressedInk: DeckHud.accent,
                size: 52,
              ),
              Expanded(
                child: _note != null
                    ? Text(
                        _note!,
                        key: const Key('agent-note'),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: DeckHud.rm(16, ink, weight: 700),
                      )
                    : _pLabel('or answer at the terminal', ink),
              ),
              _focusLink(a, ink: ink, surface: DeckHud.accent, square: true),
            ],
          ),
        ],
      ),
    );
  }

  /// A question: the same head and serif, its answers as square rows.
  Widget _petQuestion(AgentsState state, Agent a, AgentPrompt p) {
    final selected = p.options.where((o) => o.key == _selected).firstOrNull;
    final sendFlash = _flashColors('send', onAccent: false);
    final sent = _flashKey == 'send' && _flash == _Flash.sent;
    return _petCard(
      fill: DeckHud.bg,
      border: DeckHud.accent,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _petHead(state, a, boxed: true),
          const SizedBox(height: 18),
          _pLabel(
            'it asks',
            _pMuted,
            tail: p.context.isNotEmpty ? p.context : 'question',
          ),
          const SizedBox(height: 6),
          Text(
            _pSentence(p.title.isEmpty ? 'question' : p.title),
            key: const Key('agent-question'),
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: DeckHud.serif(40, DeckHud.ink).copyWith(height: 44 / 40),
          ),
          if (p.detail.isNotEmpty) ...[
            const SizedBox(height: 6),
            Text(
              p.detail,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: DeckHud.rm(16, _pMuted).copyWith(height: 1.35),
            ),
          ],
          const SizedBox(height: 14),
          Expanded(
            child: _options([
              for (final o in p.options) _questionRow(o, square: true),
            ]),
          ),
          if (_note != null) ...[
            const SizedBox(height: 10),
            Text(
              _note!,
              key: const Key('agent-note'),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: DeckHud.rm(16, DeckHud.accent),
            ),
          ],
          const SizedBox(height: 14),
          Row(
            children: [
              ..._petBack(
                ink: DeckHud.ink,
                outline: _pRule,
                pressedFill: DeckHud.ink,
                pressedInk: DeckHud.bg,
              ),
              Expanded(
                child: AgentButton(
                  key: const Key('agent-send'),
                  square: true,
                  height: 64,
                  fontSize: 18,
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
                child: _terminalButton(a, 'focus terminal', square: true),
              ),
            ],
          ),
        ],
      ),
    );
  }

  /// A free-text answer: asked here, typed at the terminal.
  Widget _petInput(AgentsState state, Agent a, AgentPrompt p) {
    return _petCard(
      fill: DeckHud.bg,
      border: DeckHud.accent,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _petHead(state, a, boxed: true),
          const SizedBox(height: 18),
          _pLabel('it asks', _pMuted, tail: 'input'),
          const SizedBox(height: 6),
          Text(
            _pSentence(p.title.isEmpty ? 'needs input' : p.title),
            key: const Key('agent-question'),
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: DeckHud.serif(40, DeckHud.ink).copyWith(height: 44 / 40),
          ),
          if (p.detail.isNotEmpty) ...[
            const SizedBox(height: 12),
            Container(
              color: DeckHud.panel,
              padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 14),
              child: Text(
                p.detail,
                maxLines: 6,
                overflow: TextOverflow.ellipsis,
                style: DeckHud.rm(18, _pText).copyWith(height: 1.4),
              ),
            ),
          ],
          if (p.context.isNotEmpty) ...[
            const SizedBox(height: 10),
            Text(
              p.context,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: DeckHud.rm(16, DeckHud.dim),
            ),
          ],
          const Spacer(),
          Text(
            _note ?? 'type the answer at the terminal',
            key: _note == null ? null : const Key('agent-note'),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: DeckHud.rm(16, _note == null ? DeckHud.dim : DeckHud.accent),
          ),
          const SizedBox(height: 16),
          Row(
            children: [
              ..._petBack(
                ink: DeckHud.ink,
                outline: _pRule,
                pressedFill: DeckHud.ink,
                pressedInk: DeckHud.bg,
              ),
              Expanded(
                child: _terminalButton(a, 'focus terminal', square: true),
              ),
            ],
          ),
        ],
      ),
    );
  }

  // ---- the pet's page ---------------------------------------------------------

  Widget _petView(AgentsState state, Agent a) {
    final pet = a.pet!;
    final pose = critterPoseOf(a, inStatus: state.inStatus(a));
    final egg = pet.isEgg;
    final name = egg
        ? (pet.ready ? 'An egg, ready' : 'An egg')
        : (pet.name.isEmpty ? 'Its pet' : pet.name);
    return _petCard(
      fill: DeckHud.bg,
      child: PetHatcher(
        pet: pet,
        onHatch: () => widget.source.hatch(a.id),
        onProblem: (m) => _set(() => _note = m),
        builder: (context, pet, hatch) => Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                Pressable(
                  key: const Key('pet-back'),
                  onTap: () => _set(() {
                    _petPage = false;
                    _note = null;
                  }),
                  builder: (context, pressed) => Container(
                    height: 48,
                    padding: const EdgeInsets.symmetric(horizontal: 14),
                    decoration: BoxDecoration(
                      color: pressed ? DeckHud.ink : null,
                      border: Border.all(
                        color: pressed ? DeckHud.ink : _pRule,
                        width: 2,
                      ),
                    ),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        CustomPaint(
                          size: const Size(16, 16),
                          painter: _ChevronPainter(
                            pressed ? DeckHud.bg : DeckHud.ink,
                          ),
                        ),
                        const SizedBox(width: 8),
                        Text(
                          'BACK',
                          style: DeckHud.rm(
                            16,
                            pressed ? DeckHud.bg : DeckHud.ink,
                            weight: 700,
                            spacing: 1.92,
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
                const SizedBox(width: 16),
                Expanded(
                  child: _pLabel(
                    a.title,
                    _pMuted,
                    tail: egg ? 'its egg' : 'its pet',
                    tailColor: _pMuted,
                  ),
                ),
                if (!_menuBar) HudClock(size: 28),
              ],
            ),
            const SizedBox(height: 18),
            SizedBox(
              height: 284,
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  SizedBox(
                    width: 280,
                    height: 284,
                    child: Stack(
                      clipBehavior: Clip.none,
                      children: [
                        Positioned(
                          left: 0,
                          top: 0,
                          width: 280,
                          height: 262,
                          child: GestureDetector(
                            key: const Key('pet-big'),
                            behavior: HitTestBehavior.opaque,
                            onTap: hatch,
                            child: PetSprite(
                              pet: pet,
                              hat: a.hat,
                              pose: pose,
                              mystery: pet.hat == null && pet.level < 5,
                              cell: 10,
                              width: 280,
                              height: 262,
                            ),
                          ),
                        ),
                        const Positioned(
                          left: 10,
                          right: 10,
                          top: 262,
                          height: 2,
                          child: ColoredBox(color: _pRule),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(width: 24),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Transform.translate(
                          offset: const Offset(0, -6),
                          child: _FitName(
                            name,
                            key: const Key('pet-name'),
                            color: DeckHud.ink,
                            size: 72,
                            smallest: 40,
                          ),
                        ),
                        _pLabel(
                          egg
                              ? 'egg · level 0'
                              : '${pet.kind} · level ${pet.level}',
                          _pOrange2,
                          key: const Key('pet-kind'),
                        ),
                        const SizedBox(height: 6),
                        Text(
                          "${a.title}'s ${egg ? 'egg' : 'pet'}",
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: DeckHud.rm(16, _pMuted),
                        ),
                        const Spacer(),
                        _levelBox(pet, hatch),
                      ],
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 18),
            _petTiles(pet),
            const SizedBox(height: 18),
            ?_petNext(pet),
            const Spacer(),
            if (_note != null)
              Text(
                _note!,
                key: const Key('agent-note'),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: DeckHud.rm(16, DeckHud.accent),
              ),
          ],
        ),
      ),
    );
  }

  /// The level bar with its numbers; for an egg, how far it is to hatching
  /// and, when it is ready, "Hatch it".
  Widget _levelBox(AgentPet pet, VoidCallback? hatch) {
    final egg = pet.isEgg;
    final pct = egg
        ? (pet.nextAt > 0 ? (pet.ate / pet.nextAt).clamp(0.0, 1.0) : 0.0)
        : pet.progress;
    final (_, to) = pet.levelSpan;
    return Container(
      key: const Key('pet-level-box'),
      padding: const EdgeInsets.fromLTRB(16, 14, 16, 14),
      decoration: const BoxDecoration(
        color: DeckHud.panel,
        border: Border(top: BorderSide(color: _pLevel, width: 3)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.baseline,
            textBaseline: TextBaseline.alphabetic,
            children: [
              Expanded(
                child: egg
                    ? _pLabel(
                        pet.ready ? 'ready to hatch' : 'until it hatches',
                        _pLevel,
                      )
                    : Row(
                        children: [
                          _pLabel('level ${pet.level}', _pLevel),
                          const Padding(
                            padding: EdgeInsets.symmetric(horizontal: 8),
                            child: _Arrow(color: _pLevel, size: 16),
                          ),
                          _pLabel('${pet.level + 1}', _pLevel),
                        ],
                      ),
              ),
              Text(
                '${(pct * 100).floor()}%',
                style: DeckHud.rm(30, DeckHud.ink, weight: 700),
              ),
            ],
          ),
          const SizedBox(height: 10),
          _pBar(
            pct,
            width: double.infinity,
            height: 14,
            fill: _pLevel,
            track: _pTrack,
          ),
          if (!egg) ...[
            const SizedBox(height: 10),
            DefaultTextStyle.merge(
              style: DeckHud.rm(15, _pMuted),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Text.rich(
                    TextSpan(
                      children: [
                        TextSpan(
                          text: _pNum(pet.ate),
                          style: DeckHud.rm(15, DeckHud.ink, weight: 700),
                        ),
                        const TextSpan(text: ' now'),
                      ],
                    ),
                  ),
                  Text.rich(
                    TextSpan(
                      children: [
                        TextSpan(
                          text: _pNum(to),
                          style: DeckHud.rm(15, DeckHud.ink, weight: 700),
                        ),
                        TextSpan(text: ' for ${pet.level + 1}'),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ] else if (hatch != null) ...[
            const SizedBox(height: 14),
            Pressable(
              key: const Key('pet-hatch-it'),
              onTap: hatch,
              builder: (context, pressed) => Container(
                height: 52,
                alignment: Alignment.center,
                color: pressed ? DeckHud.ink : _pLevel,
                child: Text(
                  'HATCH IT',
                  style: DeckHud.rm(
                    18,
                    _pInkOnOrange,
                    weight: 700,
                    spacing: 2.16,
                  ),
                ),
              ),
            ),
          ],
        ],
      ),
    );
  }

  /// Strength, what it ate, its hat and when it hatched.
  Widget _petTiles(AgentPet pet) {
    final egg = pet.isEgg;
    final hat = pet.hat;
    final hatched = pet.hatchedAt;
    const months = [
      'Jan',
      'Feb',
      'Mar',
      'Apr',
      'May',
      'Jun',
      'Jul',
      'Aug',
      'Sep',
      'Oct',
      'Nov',
      'Dec',
    ];
    Widget tile(
      String key,
      String label,
      Color color,
      String value,
      String sub, [
      Widget? extra,
    ]) => Expanded(
      child: Container(
        key: Key('pet-tile-$key'),
        padding: const EdgeInsets.fromLTRB(14, 12, 14, 12),
        decoration: BoxDecoration(
          color: DeckHud.panel,
          border: Border(top: BorderSide(color: color, width: 3)),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _pLabel(label, color, size: 14),
            const SizedBox(height: 6),
            Text(
              value,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: DeckHud.rm(
                18,
                DeckHud.ink,
                weight: 700,
              ).copyWith(height: 1.15),
            ),
            if (extra != null) ...[const SizedBox(height: 6), extra],
            const SizedBox(height: 6),
            Text(
              sub,
              maxLines: 3,
              overflow: TextOverflow.ellipsis,
              style: DeckHud.rm(14, _pMuted).copyWith(height: 17 / 14),
            ),
          ],
        ),
      ),
    );
    return IntrinsicHeight(
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          egg
              ? tile(
                  'strength',
                  'strength',
                  const Color(0xFFE8452B),
                  'none yet',
                  'they come when it hatches',
                )
              : tile(
                  'strength',
                  'strength',
                  const Color(0xFFE8452B),
                  '${pet.hearts} of $petMaxHearts',
                  petMoodNames[pet.mood] ?? pet.mood,
                  PetHearts(hearts: pet.hearts, size: 2),
                ),
          const SizedBox(width: 12),
          tile(
            'ate',
            'ate',
            const Color(0xFF5EC4D6),
            pet.ate >= 1000000
                ? '${(pet.ate / 1000000).toStringAsFixed(2)}m'
                : _pNum(pet.ate),
            'tokens, all time',
          ),
          const SizedBox(width: 12),
          hat == null
              ? tile(
                  'hat',
                  'hat',
                  const Color(0xFFB48CE0),
                  '?',
                  'a surprise: it shows at level 5',
                )
              : tile(
                  'hat',
                  'hat',
                  const Color(0xFFB48CE0),
                  '${hat.color} ${hat.shape}',
                  'rolled at hatching, for good',
                ),
          const SizedBox(width: 12),
          hatched == null
              ? tile(
                  'hatched',
                  'hatched',
                  const Color(0xFF8FCB6B),
                  'not yet',
                  pet.ready ? 'ready now' : 'at ${_pNum(pet.nextAt)} tokens',
                )
              : tile(
                  'hatched',
                  'hatched',
                  const Color(0xFF8FCB6B),
                  '${hatched.toLocal().day} ${months[hatched.toLocal().month - 1]} ${hatched.toLocal().year}',
                  pet.name.isEmpty ? '' : 'as ${pet.name}',
                ),
        ],
      ),
    );
  }

  /// The next stage it unlocks, as a dark shape: what it is and when.
  Widget? _petNext(AgentPet pet) {
    final i = petStages.indexOf(pet.stage);
    if (i < 0 || i + 1 >= petStages.length) return null;
    final next = petStages[i + 1];
    final lv = petStageLevel[next]!;
    final what = next == 'baby'
        ? 'It hatches'
        : '${petStageNames[next]}${next == 'kid'
              ? ': its hat shows'
              : lv >= 20
              ? ': a new glow'
              : ''}';
    final at = next == 'baby' ? pet.nextAt : pet.levelAt(lv);
    final left = at - pet.ate;
    return Row(
      key: const Key('pet-next'),
      children: [
        SizedBox(
          width: 88,
          height: 72,
          child: Stack(
            children: [
              Positioned(
                left: 0,
                top: 0,
                width: 88,
                height: 62,
                child: PetSprite(
                  pet: AgentPet(stage: next),
                  sil: const Color(0xFF55524B),
                  cell: 3,
                  width: 88,
                  height: 62,
                ),
              ),
              const Positioned(
                left: 8,
                right: 8,
                bottom: 8,
                height: 2,
                child: ColoredBox(color: _pLine),
              ),
            ],
          ),
        ),
        const SizedBox(width: 16),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              _pLabel('next', _pLevel, tail: 'level $lv', tailColor: _pLevel),
              const SizedBox(height: 4),
              Text(
                what,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: DeckHud.rm(
                  20,
                  DeckHud.ink,
                  weight: 700,
                ).copyWith(height: 1.2),
              ),
              const SizedBox(height: 4),
              Text(
                'at ${_pNum(at)} tokens eaten, ${pet.ready || left <= 0 ? 'ready now' : '${_pNum(left)} to go'}',
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: DeckHud.rm(15, _pMuted).copyWith(height: 1.2),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

/// The fonts have no arrows: "↳" ([turn]) and "→", drawn in the text's
/// colour, one character cell wide.
class _Arrow extends StatelessWidget {
  const _Arrow({this.turn = false, required this.color, required this.size});

  final bool turn;
  final Color color;
  final double size;

  @override
  Widget build(BuildContext context) => CustomPaint(
    size: Size(size * 0.6, size),
    painter: _ArrowPainter(turn, color),
  );
}

class _ArrowPainter extends CustomPainter {
  _ArrowPainter(this.turn, this.color);

  final bool turn;
  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    final w = size.width, h = size.height;
    final y = turn ? h * 0.68 : h * 0.55;
    final path = Path();
    if (turn) {
      path
        ..moveTo(w * 0.15, h * 0.25)
        ..lineTo(w * 0.15, y)
        ..lineTo(w * 0.95, y);
    } else {
      path
        ..moveTo(w * 0.05, y)
        ..lineTo(w * 0.95, y);
    }
    path
      ..moveTo(w * 0.6, y - h * 0.2)
      ..lineTo(w * 0.95, y)
      ..lineTo(w * 0.6, y + h * 0.2);
    canvas.drawPath(
      path,
      Paint()
        ..color = color
        ..style = PaintingStyle.stroke
        ..strokeWidth = h * 0.09
        ..strokeCap = StrokeCap.square
        ..strokeJoin = StrokeJoin.miter,
    );
  }

  @override
  bool shouldRepaint(_ArrowPainter old) =>
      old.turn != turn || old.color != color;
}

/// The page is designed on a 720 x 720 card. A host that gives it less room
/// gets the same page scaled down evenly, never reflowed into cut text.
class _PetDesignBox extends StatelessWidget {
  const _PetDesignBox({required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, box) {
      const design = 720.0;
      if (!box.hasBoundedWidth ||
          !box.hasBoundedHeight ||
          (box.maxWidth >= design && box.maxHeight >= design)) {
        return child;
      }
      return FittedBox(
        child: SizedBox.square(dimension: design, child: child),
      );
    },
  );
}

/// A name in the serif: as big as [size] when it fits on one line, else
/// the largest size down to [smallest] that does, else two lines at
/// [smallest]. It is measured in the font it is drawn in, so it never cuts
/// a name short.
class _FitName extends StatelessWidget {
  const _FitName(
    this.text, {
    super.key,
    required this.color,
    required this.size,
    required this.smallest,
  });

  final String text;
  final Color color;
  final double size;
  final double smallest;

  TextStyle _style(double s) =>
      DeckHud.serif(s, color).copyWith(height: s * 1.04 / s);

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, box) {
      for (var s = size; s >= smallest; s -= 2) {
        final tp = TextPainter(
          text: TextSpan(text: text, style: _style(s)),
          textDirection: TextDirection.ltr,
          maxLines: 1,
        )..layout();
        final fits = tp.width <= box.maxWidth;
        tp.dispose();
        if (fits) {
          return Text(text, maxLines: 1, softWrap: false, style: _style(s));
        }
      }
      return Text(
        text,
        maxLines: 2,
        overflow: TextOverflow.ellipsis,
        style: _style(smallest),
      );
    },
  );
}
