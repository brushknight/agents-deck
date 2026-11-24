import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

import 'theme.dart';

/// A tap target that rebuilds with `pressed` while a finger or the mouse is
/// down, for the deck's ink-invert press feedback.
class Pressable extends StatefulWidget {
  const Pressable({
    super.key,
    required this.onTap,
    required this.builder,
    this.behavior = HitTestBehavior.opaque,
  });

  final VoidCallback? onTap;
  final Widget Function(BuildContext context, bool pressed) builder;
  final HitTestBehavior behavior;

  @override
  State<Pressable> createState() => _PressableState();
}

class _PressableState extends State<Pressable> {
  var _down = false;

  void _press(bool down) {
    if (down != _down) setState(() => _down = down);
  }

  @override
  Widget build(BuildContext context) {
    final tap = widget.onTap;
    if (tap == null) return widget.builder(context, false);
    return GestureDetector(
      behavior: widget.behavior,
      onTapDown: (_) => _press(true),
      onTapUp: (_) => _press(false),
      onTapCancel: () => _press(false),
      onTap: tap,
      child: widget.builder(context, _down),
    );
  }
}

/// A meter drawn as a row of thin ticks with a solid bar over the filled
/// part (context used, and the like).
class TickBar extends StatelessWidget {
  const TickBar({
    super.key,
    required this.value,
    required this.color,
    this.height = 14,
  });

  /// 0..1.
  final double value;
  final Color color;
  final double height;

  @override
  Widget build(BuildContext context) => SizedBox(
    height: height,
    child: CustomPaint(
      size: Size.infinite,
      painter: _TicksPainter(value.clamp(0.0, 1.0), color),
    ),
  );
}

class _TicksPainter extends CustomPainter {
  _TicksPainter(this.value, this.color);

  final double value;
  final Color color;

  static const _pitch = 6.0;

  @override
  void paint(Canvas canvas, Size size) {
    final mid = size.height / 2;
    final thin = Paint()
      ..color = color
      ..strokeWidth = 1.6;
    for (var x = 0.0; x < size.width; x += _pitch) {
      canvas.drawLine(Offset(x, mid - 3.5), Offset(x, mid + 3.5), thin);
    }
    if (value <= 0) return;
    canvas.drawLine(
      Offset(0, mid),
      Offset(size.width * value, mid),
      Paint()
        ..color = color
        ..strokeWidth = 7,
    );
  }

  @override
  bool shouldRepaint(_TicksPainter old) =>
      old.value != value || old.color != color;
}

/// HH:MM, redrawn only when the minute changes.
class HudClock extends StatefulWidget {
  const HudClock({super.key, this.color = DeckHud.ink, this.size = 24});

  final Color color;
  final double size;

  @override
  State<HudClock> createState() => _HudClockState();
}

class _HudClockState extends State<HudClock> {
  late DateTime _shown = DateTime.now();
  late final Timer _timer = Timer.periodic(const Duration(seconds: 1), (_) {
    final now = DateTime.now();
    if (now.minute != _shown.minute || now.hour != _shown.hour) {
      setState(() => _shown = now);
    }
  });

  @override
  void initState() {
    super.initState();
    _timer; // start ticking
  }

  @override
  void dispose() {
    _timer.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    String two(int v) => v.toString().padLeft(2, '0');
    return RepaintBoundary(
      child: Text(
        '${two(_shown.hour)}:${two(_shown.minute)}',
        style: DeckHud.rm(widget.size, widget.color),
      ),
    );
  }
}

/// The deck's 4×4 board: square cells with an 8 px gap, as large as the
/// space allows, centred. With [square] false the cells fill the space
/// instead (a little shorter than wide when room is needed below the grid).
/// How many device pixels one design pixel of the deck takes, when a host
/// scales the deck (the menu bar app shows the 720 deck at 0.61): the grid
/// then puts every tile edge and gap on whole device pixels, so tiles are
/// all one size, gaps even and borders crisp.
class DeckPixels extends InheritedWidget {
  const DeckPixels({
    super.key,
    required this.devicePerDesign,
    required super.child,
  });

  final double devicePerDesign;

  static double? of(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<DeckPixels>()?.devicePerDesign;

  /// [v] design pixels, to the nearest whole device pixel (unchanged
  /// without a [DeckPixels] above).
  static double snap(BuildContext context, double v) {
    final s = of(context);
    if (s == null || s <= 0) return v;
    return (v * s).roundToDouble() / s;
  }

  @override
  bool updateShouldNotify(DeckPixels old) =>
      old.devicePerDesign != devicePerDesign;
}

class DeckGrid extends StatelessWidget {
  const DeckGrid({super.key, required this.cells, this.square = true});

  final List<Widget> cells;
  final bool square;

  static const columns = 4, rows = 4;
  static const gap = 8.0;

  @override
  Widget build(BuildContext context) {
    final s = DeckPixels.of(context);
    return LayoutBuilder(
      builder: (context, box) {
        var g = gap;
        var w = math.max(0.0, (box.maxWidth - gap * (columns - 1)) / columns);
        var h = math.max(0.0, (box.maxHeight - gap * (rows - 1)) / rows);
        if (s != null && s > 0) {
          // Whole device pixels: one tile size and one gap everywhere.
          final gd = (gap * s).roundToDouble();
          g = gd / s;
          w =
              math.max(
                0.0,
                ((box.maxWidth * s - gd * (columns - 1)) / columns)
                    .floorToDouble(),
              ) /
              s;
          h =
              math.max(
                0.0,
                ((box.maxHeight * s - gd * (rows - 1)) / rows).floorToDouble(),
              ) /
              s;
        }
        final side = math.min(w, h);
        final cellW = square ? side : w, cellH = square ? side : h;
        final grid = Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            for (var r = 0; r * columns < cells.length; r++) ...[
              if (r > 0) SizedBox(height: g),
              Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  for (var c = 0; c < columns; c++) ...[
                    if (c > 0) SizedBox(width: g),
                    SizedBox(
                      width: cellW,
                      height: cellH,
                      child: r * columns + c < cells.length
                          ? cells[r * columns + c]
                          : null,
                    ),
                  ],
                ],
              ),
            ],
          ],
        );
        if (s == null || s <= 0) return Center(child: grid);
        // Centred on a whole device pixel too.
        final rowsShown = (cells.length / columns).ceil();
        final totalW = cellW * columns + g * (columns - 1);
        final totalH = cellH * rowsShown + g * math.max(0, rowsShown - 1);
        final left = (((box.maxWidth - totalW) / 2) * s).roundToDouble() / s;
        final top = (((box.maxHeight - totalH) / 2) * s).roundToDouble() / s;
        return Padding(
          padding: EdgeInsets.only(
            left: math.max(0, left),
            top: math.max(0, top),
          ),
          child: Align(alignment: Alignment.topLeft, child: grid),
        );
      },
    );
  }
}

/// Opens an agent's card from its tile. The desk panel passes its own
/// tile-to-card morph (with the shell's back gesture); [CardMorph] is the
/// built-in one.
typedef AgentCardOpener =
    void Function(
      BuildContext tileContext,
      GlobalKey tileKey, {
      required String id,
      required Color fill,
      required Widget Function(VoidCallback close) builder,
    });

/// The built-in card opener: the card grows out of its tile to fill this
/// widget, and shrinks back into it on close.
class CardMorph extends StatefulWidget {
  const CardMorph({super.key, required this.child});

  final Widget child;

  static CardMorphState? of(BuildContext context) =>
      context.findAncestorStateOfType<CardMorphState>();

  /// An [AgentCardOpener] that uses the nearest [CardMorph].
  static void open(
    BuildContext tileContext,
    GlobalKey tileKey, {
    required String id,
    required Color fill,
    required Widget Function(VoidCallback close) builder,
  }) => of(tileContext)?.open(tileKey, fill: fill, builder: builder);

  @override
  State<CardMorph> createState() => CardMorphState();
}

class CardMorphState extends State<CardMorph>
    with SingleTickerProviderStateMixin {
  // Created in initState, not lazily: a lazy controller would be built for
  // the first time inside dispose() when no card was ever opened.
  late final AnimationController _t;
  final _area = GlobalKey();
  // The open card takes keyboard focus (Esc closes it); whatever had it
  // before (the deck, for its arrow keys) gets it back on close.
  final _cardFocus = FocusNode(debugLabel: 'card');
  FocusNode? _focusBefore;
  Rect? _from;
  Color _fill = DeckHud.bg;
  Widget Function(VoidCallback close)? _card;

  bool get isOpen => _card != null;

  @override
  void initState() {
    super.initState();
    _t = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 260),
    );
  }

  void open(
    GlobalKey tileKey, {
    required Color fill,
    required Widget Function(VoidCallback close) builder,
  }) {
    if (isOpen) return;
    final tile = tileKey.currentContext?.findRenderObject() as RenderBox?;
    final area = _area.currentContext?.findRenderObject() as RenderBox?;
    if (tile != null && area != null && tile.hasSize) {
      final topLeft = tile.localToGlobal(Offset.zero, ancestor: area);
      _from = topLeft & tile.size;
    } else {
      _from = null;
    }
    setState(() {
      _fill = fill;
      _card = builder;
    });
    _focusBefore = FocusManager.instance.primaryFocus;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && isOpen) _cardFocus.requestFocus();
    });
    _t.forward(from: 0);
  }

  void close() {
    final before = _focusBefore;
    _focusBefore = null;
    if (before != null && before.context != null) before.requestFocus();
    _t.reverse().whenComplete(() {
      if (mounted) setState(() => _card = null);
    });
  }

  @override
  void dispose() {
    _t.dispose();
    _cardFocus.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final card = _card;
    return LayoutBuilder(
      key: _area,
      builder: (context, box) {
        final full = Offset.zero & Size(box.maxWidth, box.maxHeight);
        return Stack(
          fit: StackFit.expand,
          children: [
            widget.child,
            if (card != null)
              AnimatedBuilder(
                animation: _t,
                builder: (context, child) {
                  if (_t.isDismissed) return const SizedBox.shrink(); // closed
                  final v = Curves.easeOutCubic.transform(_t.value);
                  final r = Rect.lerp(_from ?? full.deflate(40), full, v)!;
                  return Positioned.fromRect(
                    rect: r,
                    child: ClipRRect(
                      borderRadius: BorderRadius.circular(6 + 20 * v),
                      child: ColoredBox(
                        color: _fill,
                        child: Opacity(
                          opacity: ((v - 0.35) / 0.65).clamp(0.0, 1.0),
                          child: child,
                        ),
                      ),
                    ),
                  );
                },
                // Esc closes the card (keyboard hosts: the menu bar app).
                child: Focus(
                  focusNode: _cardFocus,
                  onKeyEvent: (_, e) {
                    if (e is KeyDownEvent &&
                        e.logicalKey == LogicalKeyboardKey.escape) {
                      close();
                      return KeyEventResult.handled;
                    }
                    return KeyEventResult.ignored;
                  },
                  child: SizedBox.fromSize(
                    size: full.size,
                    child: FittedBox(
                      fit: BoxFit.cover,
                      alignment: Alignment.topLeft,
                      child: SizedBox.fromSize(
                        size: full.size,
                        child: card(close),
                      ),
                    ),
                  ),
                ),
              ),
          ],
        );
      },
    );
  }
}
