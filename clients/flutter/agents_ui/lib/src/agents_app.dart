import 'dart:math' as math;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'theme.dart';
import 'widgets.dart';
import 'agent_detail.dart';
import 'agents_client.dart';
import 'agents_model.dart';
import 'agents_style.dart';
import 'critter.dart';
import 'live_picker.dart';

/// Agents app: a Stream-Deck-style board of the Mac's coding agents
/// (`agentctl serve`). Chrome-less 4×4 grid, one tile per agent by slot,
/// each tile morphing into that agent's card. The host decides how cards
/// open ([openCard] + [cardHost]); by default the built-in [CardMorph].
///
/// Tiles sit at their slot, so the board can have gaps: arrange it freely.
/// While every agent is in slots 0..15 it's one page; beyond that pages hold
/// 15 slots each plus a pager tile in the last cell (tap = next page).
/// Tile-based paging, because edge swipes may belong to the host (the desk
/// panel's shell uses left = back, top = settings).
class AgentsApp extends StatefulWidget {
  const AgentsApp({
    super.key,
    required this.source,
    this.animate = true,
    this.openCard,
    this.cardHost,
    this.pairingCard,
    this.visible,
    this.backButton = false,
  }) : assert(
         (openCard == null) == (cardHost == null),
         'openCard and cardHost come together',
       );

  final AgentsSource source;

  /// Opens an agent's card from its tile; null = [CardMorph.open].
  final AgentCardOpener? openCard;

  /// Wraps the board so [openCard] can find its host; null = [CardMorph].
  final Widget Function(Widget board)? cardHost;

  /// A back button on agent cards, for hosts without an edge swipe.
  final bool backButton;

  /// Whether anyone can see the board (a menu bar window that hides): the
  /// critters stop animating while false. Null = always visible.
  final ValueListenable<bool>? visible;

  /// Shown while there are no pairing values (with why saved ones failed);
  /// null = [AgentsPairingCard], the desk panel's setup steps.
  final Widget Function(String? problem)? pairingCard;

  /// False freezes every critter on its first frame (goldens, tests).
  final bool animate;

  @override
  State<AgentsApp> createState() => _AgentsAppState();
}

class _AgentsAppState extends State<AgentsApp> {
  late final _gridClock = CritterClock(animate: widget.animate);
  late final _cardClock = CritterClock(animate: widget.animate)
    ..running = false;
  int _page = 0;
  final _pager = PageController();

  static const _cells = 16;

  @override
  void dispose() {
    widget.visible?.removeListener(_syncClocks);
    _pager.dispose();
    _gridClock.dispose();
    _cardClock.dispose();
    super.dispose();
  }

  bool _cardShown = false;

  @override
  void initState() {
    super.initState();
    widget.visible?.addListener(_syncClocks);
    _syncClocks();
  }

  /// The card covers the grid: only one set of critters runs at a time, and
  /// none while the board is hidden.
  void _cardVisible(bool shown) {
    _cardShown = shown;
    _syncClocks();
  }

  void _syncClocks() {
    final seen = widget.visible?.value ?? true;
    _gridClock.running = seen && !_cardShown;
    _cardClock.running = seen && _cardShown;
  }

  void _open(BuildContext tileContext, GlobalKey tileKey, Agent a) {
    final p = a.status == AgentStatus.waiting ? a.waiting : null;
    (widget.openCard ?? CardMorph.open)(
      tileContext,
      tileKey,
      id: 'agent/${a.id}',
      // The flight's colour = the card it lands as.
      fill: p?.kind == PromptKind.permission ? DeckHud.accent : DeckHud.bg,
      builder: (close) => CritterScope(
        clock: _cardClock,
        child: AgentDetail(
          source: widget.source,
          agentId: a.id,
          onClose: close,
          onVisible: _cardVisible,
          showBack: widget.backButton,
        ),
      ),
    );
  }

  /// A free cell opens "add a session" (hand-started Claude sessions); the
  /// one you pick lands in that cell.
  void _openPicker(BuildContext cellContext, GlobalKey cellKey, int slot) {
    (widget.openCard ?? CardMorph.open)(
      cellContext,
      cellKey,
      id: 'add/$slot',
      fill: DeckHud.bg,
      builder: (close) => LivePicker(
        source: widget.source,
        slot: slot,
        onClose: close,
        showBack: widget.backButton,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return CritterScope(
      clock: _gridClock,
      child: (widget.cardHost ?? (board) => CardMorph(child: board))(
        ValueListenableBuilder<AgentsSnapshot>(
          valueListenable: widget.source.snapshot,
          builder: (context, snap, _) => _body(snap),
        ),
      ),
    );
  }

  Widget _body(AgentsSnapshot snap) {
    final state = snap.state;
    if (snap.link == AgentsLink.unconfigured) {
      return widget.pairingCard?.call(snap.problem) ??
          AgentsPairingCard(problem: snap.problem);
    }
    if (state == null) {
      return _Notice(
        pose: CritterPose.starting,
        title: snap.link == AgentsLink.offline
            ? 'offline · retrying'
            : 'connecting',
        lines: [if (snap.problem != null) snap.problem!],
      );
    }
    final offline = snap.link != AgentsLink.connected;
    final Widget content;
    if (state.agents.isEmpty) {
      content = const _Notice(
        pose: CritterPose.idle,
        title: 'no agents running',
        lines: ['start one with', 'agentctl new'],
        codeLine: 1,
      );
    } else {
      content = _deck(state);
    }
    if (!offline) return content;
    return Stack(
      fit: StackFit.expand,
      children: [
        Opacity(opacity: 0.35, child: content),
        Positioned(
          left: 0,
          right: 0,
          bottom: 22,
          child: Center(child: _OfflineChip(problem: snap.problem)),
        ),
      ],
    );
  }

  /// Long-press a tile and drop it on another to swap the two agents, or on
  /// a free cell to move it there. Positions are stored by the daemon, so the
  /// web board follows.
  Widget _swapCell(
    AgentsState state, {
    required int slot,
    String? id,
    String title = '',
    required Widget child,
  }) {
    return LayoutBuilder(
      builder: (context, box) {
        final target = DragTarget<String>(
          onWillAcceptWithDetails: (d) => d.data != id,
          onAcceptWithDetails: (d) => widget.source.move(d.data, slot),
          builder: (context, candidates, _) => Stack(
            fit: StackFit.expand,
            children: [
              child,
              if (candidates.isNotEmpty)
                IgnorePointer(
                  child: DecoratedBox(
                    decoration: BoxDecoration(
                      border: Border.all(color: DeckHud.ink, width: 3),
                    ),
                  ),
                ),
            ],
          ),
        );
        if (id == null) return target;
        return LongPressDraggable<String>(
          data: id,
          delay: const Duration(milliseconds: 350),
          hapticFeedbackOnStart: false,
          feedback: SizedBox(
            width: box.maxWidth,
            height: box.maxHeight,
            child: _DragGhost(title: title),
          ),
          childWhenDragging: Opacity(opacity: 0.35, child: child),
          child: target,
        );
      },
    );
  }

  /// Pages of 16 slots (page = slot ~/ 16), side by side: swipe between
  /// them, use the arrow keys or click a bar. The bar row underneath is always
  /// there, so the grid doesn't jump when a 17th agent arrives.
  Widget _deck(AgentsState state) {
    final last = state.agents.map((a) => a.slot).fold(0, math.max);
    final pages = last ~/ _cells + 1;
    if (_page >= pages) _page = pages - 1;
    final hot = {
      for (final a in state.agents)
        if (a.needsYou) a.slot ~/ _cells,
    };
    return Focus(
      autofocus: true,
      onKeyEvent: (_, e) {
        if (e is! KeyDownEvent) return KeyEventResult.ignored;
        final step = switch (e.logicalKey) {
          LogicalKeyboardKey.arrowLeft => -1,
          LogicalKeyboardKey.arrowRight => 1,
          _ => 0,
        };
        if (step == 0) return KeyEventResult.ignored;
        _goTo((_page + step).clamp(0, pages - 1));
        return KeyEventResult.handled;
      },
      child: Padding(
        padding: const EdgeInsets.fromLTRB(8, 8, 8, 0),
        child: Column(
          children: [
            Expanded(
              child: PageView.builder(
                key: const Key('agents-pages'),
                controller: _pager,
                itemCount: pages,
                onPageChanged: (p) => setState(() => _page = p),
                itemBuilder: (context, p) =>
                    DeckGrid(cells: _cellsFor(state, p)),
              ),
            ),
            SizedBox(
              height: _barsHeight,
              child: pages < 2
                  ? null
                  : _PageBars(
                      pages: pages,
                      current: _page,
                      hot: hot,
                      onTap: _goTo,
                    ),
            ),
          ],
        ),
      ),
    );
  }

  static const _barsHeight = 34.0;

  void _goTo(int page) {
    if (!_pager.hasClients || page == _page) return;
    _pager.animateToPage(
      page,
      duration: const Duration(milliseconds: 260),
      curve: Curves.easeOutCubic,
    );
  }

  List<Widget> _cellsFor(AgentsState state, int page) {
    final bySlot = {for (final a in state.agents) a.slot: a};
    final start = page * _cells;
    return [
      for (var slot = start; slot < start + _cells; slot++)
        if (bySlot[slot] case final a?)
          _swapCell(
            state,
            slot: slot,
            id: a.id,
            title: a.title,
            child: AgentTile(
              key: ValueKey('agent-tile-${a.id}'),
              agent: a,
              state: state,
              onOpen: _open,
            ),
          )
        else
          _swapCell(
            state,
            slot: slot,
            child: _FreeCell(onOpen: (ctx, key) => _openPicker(ctx, key, slot)),
          ),
    ];
  }
}

/// One agent on the board: tool chip, critter, title, status line.
class AgentTile extends StatefulWidget {
  const AgentTile({
    super.key,
    required this.agent,
    required this.state,
    required this.onOpen,
  });

  final Agent agent;
  final AgentsState state;
  final void Function(BuildContext tileContext, GlobalKey tileKey, Agent a)
  onOpen;

  @override
  State<AgentTile> createState() => _AgentTileState();
}

class _AgentTileState extends State<AgentTile> {
  static const _tileDesign = 170.0;
  final _tileKey = GlobalKey();

  @override
  Widget build(BuildContext context) {
    final a = widget.agent;
    final look = AgentLook.of(a);
    return Pressable(
      key: _tileKey,
      onTap: () => widget.onOpen(context, _tileKey, a),
      builder: (context, pressed) {
        // Pressed = the deck's ink invert.
        final fill = pressed ? DeckHud.ink : look.fill;
        Color c(Color v) => pressed ? DeckHud.bg : v;
        return RepaintBoundary(
          child: CustomPaint(
            // Over the content so the ring never shifts the layout.
            foregroundPainter: pressed
                ? null
                : ContextRingPainter(
                    fraction: a.contextFraction,
                    color: look.ring,
                    track: look.ringTrack,
                  ),
            child: Container(
              color: fill,
              child: Stack(
                children: [
                  if (look.stripes && !pressed)
                    const Positioned.fill(child: HungryStripes()),
                  if (!pressed)
                    Positioned(
                      top: 10,
                      right: 10,
                      child: Container(width: 8, height: 8, color: look.chip),
                    ),
                  // Subagents at work: a small critter in the top-left
                  // corner (body only), with a count when there are more.
                  if (a.subagents.isNotEmpty)
                    Positioned(
                      top: 8,
                      left: 9,
                      child: Row(
                        key: const Key('agent-tile-subagents'),
                        crossAxisAlignment: CrossAxisAlignment.end,
                        children: [
                          ClipRect(
                            child: Align(
                              alignment: Alignment.centerLeft,
                              widthFactor: 14 / 18,
                              child: CritterSprite(
                                pose: critterPoseForTool(
                                  a.subagents.first.tool,
                                ),
                                width: 36,
                                body: c(look.body),
                                cut: fill,
                              ),
                            ),
                          ),
                          if (a.subagents.length > 1)
                            Padding(
                              padding: const EdgeInsets.only(left: 3),
                              child: Text(
                                '×${a.subagents.length}',
                                style: DeckHud.mono(
                                  size: 11,
                                  color: c(look.meta),
                                ),
                              ),
                            ),
                        ],
                      ),
                    ),
                  // Laid out on the deck's 170 px cell and scaled to the
                  // real one, so any grid size keeps the same composition.
                  Positioned.fill(
                    child: FittedBox(
                      child: SizedBox.square(
                        dimension: _tileDesign,
                        child: Column(
                          children: [
                            const SizedBox(height: 30),
                            CritterSprite(
                              pose: critterPoseOf(
                                a,
                                inStatus: widget.state.inStatus(a),
                              ),
                              celebrateUntil: DateTime.now().add(
                                critterCelebration - widget.state.inStatus(a),
                              ),
                              afterCelebrate: a.unseen
                                  ? CritterPose.hungry
                                  : CritterPose.idle,
                              species: critterSpeciesFor(a.tool),
                              centerBody: true,
                              width: 92,
                              body: c(look.body),
                              bubble: look.bubble == null
                                  ? null
                                  : c(look.bubble!),
                              cut: fill,
                            ),
                            const SizedBox(height: 14),
                            _line(
                              a.title,
                              DeckHud.mono(size: 16, color: c(look.label)),
                            ),
                            const SizedBox(height: 5),
                            _line(
                              agentMetaLine(a, widget.state),
                              DeckHud.mono(
                                size: 12.5,
                                color: c(look.meta),
                              ).copyWith(letterSpacing: 0.4),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
        );
      },
    );
  }

  static Widget _line(String text, TextStyle style) => Padding(
    padding: const EdgeInsets.symmetric(horizontal: 10),
    child: Text(
      text,
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
      textAlign: TextAlign.center,
      style: style,
    ),
  );
}

/// Diagonal accent stripes sliding across a hungry tile (a finished agent
/// whose result nobody has looked at yet), driven by the shared critter clock.
class HungryStripes extends StatefulWidget {
  const HungryStripes({super.key});

  @override
  State<HungryStripes> createState() => _HungryStripesState();
}

class _HungryStripesState extends State<HungryStripes> {
  final _step = ValueNotifier<int>(0);
  CritterClock? _clock;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final c = CritterScope.of(context);
    if (c != _clock) {
      _clock?.removeListener(_tick);
      _clock = c?..addListener(_tick);
    }
    _tick();
  }

  // One step every 100 ms: a calm, stepped slide.
  void _tick() => _step.value = ((_clock?.value ?? 0) ~/ 2) % 6;

  @override
  void dispose() {
    _clock?.removeListener(_tick);
    _step.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) =>
      RepaintBoundary(child: CustomPaint(painter: _StripesPainter(_step)));
}

class _StripesPainter extends CustomPainter {
  _StripesPainter(this.step) : super(repaint: step);
  final ValueListenable<int> step;

  static const period = 24.0; // stripe + gap, along x

  @override
  void paint(Canvas canvas, Size size) {
    canvas.clipRect(Offset.zero & size);
    final p = Paint()..color = DeckHud.accent.withValues(alpha: 0.16);
    final shift = step.value * period / 6;
    for (var x = -size.height - period + shift; x < size.width; x += period) {
      canvas.drawPath(
        Path()
          ..moveTo(x, size.height)
          ..lineTo(x + period / 2, size.height)
          ..lineTo(x + period / 2 + size.height, 0)
          ..lineTo(x + size.height, 0)
          ..close(),
        p,
      );
    }
  }

  @override
  bool shouldRepaint(_StripesPainter old) => false;
}

/// What follows the finger while a tile is dragged.
class _DragGhost extends StatelessWidget {
  const _DragGhost({required this.title});

  final String title;

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: DeckHud.panel.withValues(alpha: 0.9),
        border: Border.all(color: DeckHud.ink, width: 2),
      ),
      alignment: Alignment.center,
      padding: const EdgeInsets.all(10),
      child: Text(
        title,
        maxLines: 2,
        overflow: TextOverflow.ellipsis,
        textAlign: TextAlign.center,
        style: DeckHud.mono(size: 16, color: DeckHud.ink),
      ),
    );
  }
}

/// The tile's border as a context gauge: [fraction] of the perimeter, drawn
/// clockwise from the top centre over a faint full-length track.
class ContextRingPainter extends CustomPainter {
  ContextRingPainter({
    required this.fraction,
    required this.color,
    required this.track,
  });

  final double fraction;
  final Color color;
  final Color track;

  static const stroke = 4.0;

  @override
  void paint(Canvas canvas, Size size) {
    const h = stroke / 2;
    final w = size.width, ht = size.height;
    final path = Path()
      ..moveTo(w / 2, h)
      ..lineTo(w - h, h)
      ..lineTo(w - h, ht - h)
      ..lineTo(h, ht - h)
      ..lineTo(h, h)
      ..close();
    final paint = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = stroke
      ..strokeJoin = StrokeJoin.miter
      ..isAntiAlias = false;
    canvas.drawPath(path, paint..color = track);
    final f = fraction.clamp(0.0, 1.0);
    if (f > 0) {
      final metric = path.computeMetrics().first;
      canvas.drawPath(
        metric.extractPath(0, metric.length * f),
        paint..color = color,
      );
    }
  }

  @override
  bool shouldRepaint(ContextRingPainter old) =>
      old.fraction != fraction || old.color != color || old.track != track;
}

/// Unused slot: near-black block with a faint frame.
/// A free cell: tap it to add a session there.
class _FreeCell extends StatefulWidget {
  const _FreeCell({required this.onOpen});

  final void Function(BuildContext context, GlobalKey key) onOpen;

  @override
  State<_FreeCell> createState() => _FreeCellState();
}

class _FreeCellState extends State<_FreeCell> {
  final _key = GlobalKey();

  @override
  Widget build(BuildContext context) => GestureDetector(
    key: _key,
    behavior: HitTestBehavior.opaque,
    onTap: () => widget.onOpen(context, _key),
    child: const AgentsEmptyTile(),
  );
}

class AgentsEmptyTile extends StatelessWidget {
  const AgentsEmptyTile({super.key});

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: agentsEmptyFill,
        border: Border.all(color: agentsEmptyOutline),
      ),
      child: const SizedBox.expand(),
    );
  }
}

/// One rounded bar per page under the deck: the current one wide and bright,
/// another page's orange when an agent there needs you. Tap one to go there.
class _PageBars extends StatelessWidget {
  const _PageBars({
    required this.pages,
    required this.current,
    required this.hot,
    required this.onTap,
  });

  final int pages;
  final int current;
  final Set<int> hot;
  final ValueChanged<int> onTap;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: 'page ${current + 1} of $pages',
      child: Row(
        key: const Key('agents-pager'),
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          for (var p = 0; p < pages; p++)
            GestureDetector(
              key: Key('agents-page-$p'),
              behavior: HitTestBehavior.opaque,
              onTap: () => onTap(p),
              child: Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 6,
                  vertical: 12,
                ),
                child: AnimatedContainer(
                  duration: const Duration(milliseconds: 200),
                  width: p == current ? 56 : 30,
                  height: 8,
                  decoration: BoxDecoration(
                    // Where you are is always bright; other pages turn
                    // orange when an agent there needs you.
                    color: p == current
                        ? DeckHud.ink
                        : (hot.contains(p)
                              ? DeckHud.accent
                              : const Color(0xFF2A2925)),
                    borderRadius: BorderRadius.circular(4),
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class _OfflineChip extends StatelessWidget {
  const _OfflineChip({this.problem});

  final String? problem;

  @override
  Widget build(BuildContext context) {
    return Container(
      key: const Key('agents-offline'),
      constraints: const BoxConstraints(maxWidth: 640),
      padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 9),
      decoration: BoxDecoration(
        color: DeckHud.panel,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: DeckHud.accent, width: 1.5),
      ),
      child: Text(
        'offline · retrying${problem == null ? '' : ' · $problem'}',
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: DeckHud.rm(14, DeckHud.accent),
      ),
    );
  }
}

/// Centred critter + a few lines (connecting, empty fleet).
class _Notice extends StatelessWidget {
  const _Notice({
    required this.pose,
    required this.title,
    this.lines = const [],
    this.codeLine,
  });

  final CritterPose pose;
  final String title;
  final List<String> lines;

  /// Index into [lines] rendered as a command (ink, Roboto Mono).
  final int? codeLine;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 48),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            CritterSprite(
              pose: pose,
              width: 126,
              body: DeckHud.dim,
              cut: DeckHud.bg,
              centerBody: true,
            ),
            const SizedBox(height: 28),
            Text(
              title,
              textAlign: TextAlign.center,
              style: DeckHud.rm(28, DeckHud.ink, weight: 700),
            ),
            for (final (i, l) in lines.indexed) ...[
              const SizedBox(height: 12),
              Text(
                l,
                textAlign: TextAlign.center,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: i == codeLine
                    ? DeckHud.rm(20, DeckHud.accent)
                    : DeckHud.mono(size: 16, color: DeckHud.dim),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// Not paired yet: how to get the three values onto the panel.
class AgentsPairingCard extends StatelessWidget {
  const AgentsPairingCard({super.key, this.problem});

  /// Why saved values were not usable (e.g. a malformed agents.json).
  final String? problem;

  @override
  Widget build(BuildContext context) {
    Widget step(String n, List<InlineSpan> text) => Padding(
      padding: const EdgeInsets.only(bottom: 18),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 34,
            child: Text(n, style: DeckHud.rm(20, DeckHud.accent, weight: 700)),
          ),
          Expanded(
            child: Text.rich(
              TextSpan(children: text),
              style: DeckHud.mono(
                size: 17,
                color: DeckHud.ink,
              ).copyWith(height: 1.4),
            ),
          ),
        ],
      ),
    );
    TextSpan code(String s) =>
        TextSpan(text: s, style: DeckHud.rm(17, DeckHud.accent));
    return Padding(
      padding: const EdgeInsets.all(8),
      child: Container(
        key: const Key('agents-pairing'),
        color: DeckHud.panel,
        padding: const EdgeInsets.fromLTRB(40, 44, 40, 36),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const CritterSprite(
              pose: CritterPose.wait,
              width: 90,
              body: DeckHud.ink,
              cut: DeckHud.panel,
            ),
            const SizedBox(height: 26),
            Text(
              'pair with your mac',
              style: DeckHud.rm(30, DeckHud.ink, weight: 700),
            ),
            const SizedBox(height: 10),
            Text(
              'the agents board needs the daemon\'s address, token and '
              'certificate fingerprint.',
              style: DeckHud.mono(
                size: 16,
                color: DeckHud.dim,
              ).copyWith(height: 1.4),
            ),
            const SizedBox(height: 34),
            step('1', [
              const TextSpan(text: 'on the mac, run '),
              code('agentctl pair'),
            ]),
            step('2', [
              const TextSpan(text: 'open the panel setup page '),
              code(agentsSetupUrl()),
            ]),
            step('3', [
              const TextSpan(
                text: 'paste the url, token and fingerprint into ',
              ),
              const TextSpan(
                text: 'agents',
                style: TextStyle(color: DeckHud.accent),
              ),
              const TextSpan(text: ' and save'),
            ]),
            const Spacer(),
            Text(
              problem ??
                  'the board connects by itself once the values are saved.',
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: DeckHud.mono(
                size: 14,
                color: problem == null ? DeckHud.dim : DeckHud.accent,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
