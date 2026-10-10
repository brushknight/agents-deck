import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';

import 'agents_model.dart';

/// The agents app's pixel critter: a 14×7 body on 2-px legs in an 18×13
/// cell box (the top 4 rows hold the "…", "?", "!" or "z z" bubble). One
/// sprite per agent, drawn per status:
///
/// * running — what it is doing shows: thinking (glancing eyes, rising
///   thought dots), reading (eyes scan, magnifier), writing (eyes down, hands
///   typing, pencil), bash (fast legs, ">_" with a blinking cursor), web
///   (globe), planning (checklist ticking), delegating (a mini critter in
///   tow); any other tool walks with "…";
/// * just finished — a short hop with sparkles, then hungry: eyes up at a
///   bobbing cookie, mouth chomping, until its result has been looked at;
/// * waiting — eyes up, blinking "?";
/// * error   — X eyes, "!";
/// * idle    — eyes closed, drifting "z z";
/// * starting — still, "…";
/// * exited  — hollow outline.
enum CritterPose {
  run,
  think,
  read,
  write,
  bash,
  web,
  plan,
  delegate,
  celebrate,
  hungry,
  wait,
  error,
  idle,
  starting,
  exited,

  /// Compacting the conversation: squeezed flat and springing back.
  compact,
}

/// Which mascot draws an agent: Claude's domed critter, or Codex's onigiri
/// (a rice triangle wrapped in nori, on two little feet). Both run the
/// same poses and colours.
enum CritterSpecies { claude, codex }

CritterSpecies critterSpeciesFor(String tool) => tool.toLowerCase() == 'codex'
    ? CritterSpecies.codex
    : CritterSpecies.claude;

/// How long the "just finished" hop lasts after a turn ends.
const critterCelebration = Duration(seconds: 6);

/// The pose for [a]: by status, and for a running agent by the tool it is
/// using. [inStatus] is how long it has been in its status (for the hop).
CritterPose critterPoseOf(Agent a, {Duration inStatus = Duration.zero}) {
  switch (a.status) {
    case AgentStatus.running:
      return critterPoseForTool(a.activity?.tool);
    case AgentStatus.idle:
      if (inStatus < critterCelebration) return CritterPose.celebrate;
      return a.unseen ? CritterPose.hungry : CritterPose.idle;
    default:
      return critterPoseFor(a.status);
  }
}

/// The working pose for a tool (thinking when there is none).
CritterPose critterPoseForTool(String? tool) {
  final t = tool?.toLowerCase();
  if (t == null || t.isEmpty) return CritterPose.think;
  return switch (t) {
    'read' || 'grep' || 'glob' || 'ls' => CritterPose.read,
    'edit' || 'write' || 'multiedit' || 'notebookedit' => CritterPose.write,
    'bash' || 'bashoutput' || 'killshell' || 'killbash' => CritterPose.bash,
    'webfetch' || 'websearch' => CritterPose.web,
    'todowrite' => CritterPose.plan,
    'task' || 'agent' => CritterPose.delegate,
    'compact' => CritterPose.compact,
    _ => CritterPose.run,
  };
}

CritterPose critterPoseFor(AgentStatus s) => switch (s) {
  AgentStatus.running => CritterPose.run,
  AgentStatus.waiting => CritterPose.wait,
  AgentStatus.error => CritterPose.error,
  AgentStatus.starting => CritterPose.starting,
  AgentStatus.exited => CritterPose.exited,
  AgentStatus.idle || AgentStatus.unknown => CritterPose.idle,
};

/// One shared, stepped animation clock for every critter on screen: a
/// 50 ms tick counter driven by a plain timer (no vsync ticker, so idle
/// frames cost nothing on the CM4). Sprites derive their own frame from it
/// and repaint only when that frame changes. A frozen clock (goldens,
/// tests) stays at tick 0.
class CritterClock extends ChangeNotifier implements ValueListenable<int> {
  CritterClock({this.animate = true}) {
    running = true;
  }

  static const step = Duration(milliseconds: 50);

  /// False: a frozen clock that never ticks, whatever [running] says.
  final bool animate;

  Timer? _timer;
  int _tick = 0;

  bool get running => _timer != null;

  /// Pause while nobody can see the sprites (e.g. the grid under an open
  /// detail card); the tick count carries on where it stopped.
  set running(bool on) {
    if (!animate || on == running) return;
    if (on) {
      _timer = Timer.periodic(step, (_) {
        _tick++;
        notifyListeners();
      });
    } else {
      _timer?.cancel();
      _timer = null;
    }
  }

  @override
  int get value => _tick;

  @override
  void dispose() {
    _timer?.cancel();
    _timer = null;
    super.dispose();
  }
}

/// Hands the shared [CritterClock] to every sprite below it.
class CritterScope extends InheritedWidget {
  const CritterScope({super.key, required this.clock, required super.child});

  final CritterClock clock;

  static CritterClock? of(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<CritterScope>()?.clock;

  @override
  bool updateShouldNotify(CritterScope old) => clock != old.clock;
}

/// Animation frame for [pose] at clock [tick] — an int that changes only
/// when the drawing does.
int critterFrame(CritterPose pose, int tick) => switch (pose) {
  // Legs swap every 250 ms (2 Hz walk) and the body bobs with them;
  // the dots grow 1-2-3 every 400 ms (first frame shows all three).
  CritterPose.run => ((tick ~/ 5) % 2) * 10 + ((tick ~/ 8) + 2) % 3 + 1,
  CritterPose.starting => ((tick ~/ 8) + 2) % 3 + 1,
  // "?" on for 600 ms, faded for 400 ms.
  CritterPose.wait => (tick % 20) < 12 ? 1 : 0,
  // z z drift in four 600 ms steps.
  CritterPose.idle => (tick ~/ 12) % 4,
  CritterPose.error || CritterPose.exited => 0,
  // Eyes glance every 600 ms; the thought dots rise in 300 ms steps.
  CritterPose.think => ((tick ~/ 12) % 2) * 10 + (tick ~/ 6) % 4,
  // Eyes scan left/centre/right/centre, 300 ms each.
  CritterPose.read => (tick ~/ 6) % 4,
  // Squeeze, squeeze harder, ease off, let go: 300 ms each.
  CritterPose.compact => (tick ~/ 6) % 4,
  // Hands tap every 150 ms.
  CritterPose.write => (tick ~/ 3) % 2,
  // Legs every 100 ms (in a hurry); cursor blinks at 1 Hz.
  CritterPose.bash => ((tick ~/ 2) % 2) * 10 + (tick ~/ 10) % 2,
  // The globe turns every 400 ms.
  CritterPose.web => (tick ~/ 8) % 2,
  // One more box ticked every 500 ms.
  CritterPose.plan => (tick ~/ 10) % 4,
  // Both critters walk at 2 Hz.
  CritterPose.delegate => (tick ~/ 5) % 2,
  // Chomp and bob the cookie every 300 ms.
  CritterPose.hungry => (tick ~/ 6) % 2,
  // Hop every 200 ms; sparkles twinkle in turn.
  CritterPose.celebrate => ((tick ~/ 4) % 2) * 10 + (tick ~/ 4) % 3,
};

/// A critter sprite [width] wide (height = width × 13/18). Cells snap to
/// whole pixels so the edges stay crisp. [cut] is the colour of the eyes
/// (the surface behind the sprite). Animates when a [CritterScope] is
/// above it.
class CritterSprite extends StatefulWidget {
  const CritterSprite({
    super.key,
    required this.pose,
    required this.width,
    required this.body,
    required this.cut,
    this.bubble,
    this.celebrateUntil,
    this.afterCelebrate = CritterPose.idle,
    this.species = CritterSpecies.claude,
    this.centerBody = false,
  });

  final CritterSpecies species;

  /// Centre the body itself (not body + bubble) in [width]; the bubble then
  /// pokes out past the right edge. For sprites centred on a tile.
  final bool centerBody;

  final CritterPose pose;

  /// For [CritterPose.celebrate]: when the hop ends and the critter dozes off
  /// (checked on every clock tick, so it ends without a rebuild from above).
  final DateTime? celebrateUntil;

  /// What a finished critter turns into after the hop (idle, or hungry).
  final CritterPose afterCelebrate;
  final double width;
  final Color body;
  final Color cut;

  /// Colour of the "…/?/!/z" bubble; defaults to [body].
  final Color? bubble;

  @override
  State<CritterSprite> createState() => _CritterSpriteState();
}

class _CritterSpriteState extends State<CritterSprite> {
  final _frame = ValueNotifier<int>(0);
  CritterClock? _clock;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final c = CritterScope.of(context);
    if (c != _clock) {
      _clock?.removeListener(_onTick);
      _clock = c?..addListener(_onTick);
    }
    _onTick();
  }

  @override
  void didUpdateWidget(CritterSprite old) {
    super.didUpdateWidget(old);
    _onTick();
  }

  late CritterPose _pose = widget.pose;

  void _onTick() {
    var pose = widget.pose;
    final until = widget.celebrateUntil;
    if (pose == CritterPose.celebrate &&
        until != null &&
        DateTime.now().isAfter(until)) {
      pose = widget.afterCelebrate;
    }
    if (pose != _pose) {
      if (mounted) setState(() => _pose = pose);
    }
    _frame.value = critterFrame(pose, _clock?.value ?? 0);
  }

  @override
  void dispose() {
    _clock?.removeListener(_onTick);
    _frame.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return RepaintBoundary(
      child: CustomPaint(
        size: Size(widget.width, widget.width * 13 / 18),
        painter: _CritterPainter(
          pose: _pose,
          body: widget.body,
          cut: widget.cut,
          bubble: widget.bubble ?? widget.body,
          species: widget.species,
          centerBody: widget.centerBody,
          frame: _frame,
        ),
      ),
    );
  }
}

class _CritterPainter extends CustomPainter {
  _CritterPainter({
    required this.pose,
    required this.body,
    required this.cut,
    required this.bubble,
    required this.frame,
    this.species = CritterSpecies.claude,
    this.centerBody = false,
  }) : super(repaint: frame);

  final CritterSpecies species;
  final bool centerBody;
  final CritterPose pose;
  final Color body;
  final Color cut;
  final Color bubble;
  final ValueListenable<int> frame;

  /// Body rows (y 0..6) as [x0, x1) spans — the mockup's BODY path.
  static const _rows = [
    (2, 12),
    (1, 13),
    (0, 14),
    (0, 14),
    (1, 13),
    (1, 13),
    (2, 12),
  ];

  /// Codex onigiri: a rounded rice triangle (rows y -3..6), wrapped in nori.
  static const _onigiriTop = -3;
  static const _onigiriRows = [
    (6, 8),
    (5, 9),
    (4, 10),
    (3, 11),
    (2, 12),
    (2, 12),
    (1, 13),
    (1, 13),
    (0, 14),
    (1, 13),
  ];

  /// The hollow left inside an exited onigiri (rows y -1..5).
  static const _onigiriInner = [
    (6, 8),
    (5, 9),
    (4, 10),
    (4, 10),
    (3, 11),
    (3, 11),
    (2, 12),
  ];
  static const _legsA = [2, 4, 9, 11];
  static const _legsB = [3, 5, 8, 10];

  static const _question = ['###', '..#', '.#.', '...', '.#.'];
  static const _bang = ['#', '#', '#', '.', '#'];
  static const _z = ['###', '.#.', '###'];

  @override
  void paint(Canvas canvas, Size size) {
    final f = frame.value;
    // Whole-pixel cells; the sprite is centred in whatever is left over.
    final u = (size.width / 18).floorToDouble().clamp(1.0, double.infinity);
    // The body spans x 0..14 of the 18-cell grid; centring it shifts by 2.
    final ox =
        ((size.width - u * 18) / 2).roundToDouble() + (centerBody ? 2 * u : 0);
    final oy = ((size.height - u * 13) / 2).roundToDouble() + 4 * u;
    final fill = Paint()
      ..color = body
      ..isAntiAlias = false;
    final hole = Paint()
      ..color = cut
      ..isAntiAlias = false;
    Rect cell(double x, double y, double w, double h) =>
        Rect.fromLTWH(ox + x * u, oy + y * u, w * u, h * u);

    final legsB = switch (pose) {
      CritterPose.run || CritterPose.bash => f >= 10,
      CritterPose.delegate => f == 1,
      _ => false,
    };
    final hop = pose == CritterPose.celebrate && f >= 10;
    final bot = species == CritterSpecies.codex;
    final bob = hop ? -1.5 : (legsB ? -0.5 : 0.0);

    // Compacting squashes body and eyes around the feet (x 7, y 7).
    const squashY = [1.0, 0.8, 0.62, 0.8];
    final squash = pose == CritterPose.compact;
    if (squash) {
      final sy = squashY[f], sx = 1 + (1 - sy) / 2;
      final pivot = cell(7, 7, 0, 0).topLeft;
      canvas
        ..save()
        ..translate(pivot.dx, pivot.dy)
        ..scale(sx, sy)
        ..translate(-pivot.dx, -pivot.dy);
    }

    // Body (+ eyes, which bob with it).
    if (pose == CritterPose.exited) {
      final outline = Paint()
        ..color = body
        ..style = PaintingStyle.stroke
        ..strokeWidth = (u * 0.5).clamp(1.0, 4.0)
        ..isAntiAlias = false;
      if (bot) {
        _onigiri(canvas, 0, fill, cell);
        for (final (y, (x0, x1)) in _onigiriInner.indexed) {
          canvas.drawRect(
            cell(x0.toDouble(), y - 1.0, (x1 - x0).toDouble(), 1),
            hole,
          );
        }
      } else {
        canvas.drawPath(_bodyPath(cell), outline);
      }
    } else if (bot) {
      _onigiri(canvas, bob, fill, cell);
      // The nori wrap across the bottom.
      canvas.drawRect(cell(4, 5.4 + bob, 6, 1.6), hole);
    } else {
      for (final (y, (x0, x1)) in _rows.indexed) {
        canvas.drawRect(
          cell(x0.toDouble(), y + bob, (x1 - x0).toDouble(), 1),
          fill,
        );
      }
    }
    switch (pose) {
      case CritterPose.run:
      case CritterPose.bash:
      case CritterPose.plan:
      case CritterPose.delegate:
        canvas.drawRect(cell(4, 2 + bob, 1, 2), hole);
        canvas.drawRect(cell(9, 2 + bob, 1, 2), hole);
      case CritterPose.think:
        // Glancing up, left then right.
        final dx = f >= 10 ? 1.0 : -1.0;
        canvas.drawRect(cell(4 + dx, 1, 1, 1.5), hole);
        canvas.drawRect(cell(9 + dx, 1, 1, 1.5), hole);
      case CritterPose.read:
        const scan = [0.0, -1.0, 0.0, 1.0];
        canvas.drawRect(cell(4 + scan[f], 2, 1, 2), hole);
        canvas.drawRect(cell(9 + scan[f], 2, 1, 2), hole);
      case CritterPose.write:
        // Looking down at the keys.
        canvas.drawRect(cell(4, 3.2, 1, 1.2), hole);
        canvas.drawRect(cell(9, 3.2, 1, 1.2), hole);
      case CritterPose.web:
        canvas.drawRect(cell(4.4, 1, 1, 2), hole);
        canvas.drawRect(cell(9.4, 1, 1, 2), hole);
      case CritterPose.hungry:
        // Eyes up at the cookie; the mouth chomps open and shut.
        canvas.drawRect(cell(4.6, 1, 1, 2), hole);
        canvas.drawRect(cell(9.6, 1, 1, 2), hole);
        canvas.drawRect(
          bot
              ? (f == 0 ? cell(6.2, 3.4, 1.6, 1.4) : cell(6.2, 4.0, 1.6, 0.5))
              : (f == 0 ? cell(6.2, 4.2, 2.2, 1.8) : cell(6.2, 5.0, 2.2, 0.5)),
          hole,
        );
      case CritterPose.celebrate:
        // Happy "^ ^" eyes.
        final happy = Paint()
          ..color = cut
          ..strokeWidth = u * 0.55
          ..strokeCap = StrokeCap.square
          ..style = PaintingStyle.stroke;
        for (final ex in [4.5, 9.5]) {
          canvas.drawPath(
            Path()
              ..moveTo(
                cell(ex - 1, 3.3 + bob, 0, 0).left,
                cell(ex - 1, 3.3 + bob, 0, 0).top,
              )
              ..lineTo(
                cell(ex, 2.2 + bob, 0, 0).left,
                cell(ex, 2.2 + bob, 0, 0).top,
              )
              ..lineTo(
                cell(ex + 1, 3.3 + bob, 0, 0).left,
                cell(ex + 1, 3.3 + bob, 0, 0).top,
              ),
            happy,
          );
        }
      case CritterPose.wait:
        canvas.drawRect(cell(4, 1, 1, 2), hole);
        canvas.drawRect(cell(9, 1, 1, 2), hole);
      case CritterPose.error:
        final x = Paint()
          ..color = cut
          ..strokeWidth = u * 0.55
          ..strokeCap = StrokeCap.butt;
        for (final ex in [3.8, 8.8]) {
          canvas.drawLine(
            cell(ex - 0.4, 1.8, 0, 0).topLeft,
            cell(ex + 1.4, 4.0, 0, 0).topLeft,
            x,
          );
          canvas.drawLine(
            cell(ex + 1.4, 1.8, 0, 0).topLeft,
            cell(ex - 0.4, 4.0, 0, 0).topLeft,
            x,
          );
        }
      case CritterPose.idle:
      case CritterPose.starting:
        canvas.drawRect(cell(3.5, 3.2, 2, 0.55), hole);
        canvas.drawRect(cell(8.5, 3.2, 2, 0.55), hole);
      case CritterPose.compact:
        // Strained "> <".
        final strain = Paint()
          ..color = cut
          ..strokeWidth = u * 0.55
          ..style = PaintingStyle.stroke;
        Offset at(double x, double y) => cell(x, y, 0, 0).topLeft;
        canvas.drawPath(
          Path()
            ..moveTo(at(3.6, 1.9).dx, at(3.6, 1.9).dy)
            ..lineTo(at(4.8, 2.7).dx, at(4.8, 2.7).dy)
            ..lineTo(at(3.6, 3.5).dx, at(3.6, 3.5).dy)
            ..moveTo(at(10.4, 1.9).dx, at(10.4, 1.9).dy)
            ..lineTo(at(9.2, 2.7).dx, at(9.2, 2.7).dy)
            ..lineTo(at(10.4, 3.5).dx, at(10.4, 3.5).dy),
          strain,
        );
      case CritterPose.exited:
        final shut = Paint()
          ..color = body
          ..isAntiAlias = false;
        canvas.drawRect(cell(3.5, 3.2, 2, 0.55), shut);
        canvas.drawRect(cell(8.5, 3.2, 2, 0.55), shut);
    }

    if (squash) canvas.restore();

    // Legs (they leave the ground on a hop).
    final legY = hop ? 7 + bob : 7.0;
    if (bot) {
      // Two little feet that step in turn.
      canvas.drawRect(cell(3.5, legY - (legsB ? 0.6 : 0), 2, 1.4), fill);
      canvas.drawRect(cell(8.5, legY, 2, 1.4), fill);
    } else {
      for (final lx in legsB ? _legsB : _legsA) {
        canvas.drawRect(cell(lx.toDouble(), legY, 1, 2), fill);
      }
    }
    if (pose == CritterPose.write) {
      // Two little hands tapping between the legs.
      final up = f == 0;
      canvas.drawRect(cell(5.9, up ? 7.0 : 7.6, 0.9, 0.8), fill);
      canvas.drawRect(cell(7.3, up ? 7.6 : 7.0, 0.9, 0.8), fill);
    }
    if (pose == CritterPose.delegate) {
      // A mini critter in tow, walking in step.
      _mini(canvas, 14.4, 3.2, 0.26, f == 1, fill, hole, cell);
    }

    // Bubble.
    switch (pose) {
      case CritterPose.run:
      case CritterPose.starting:
        final dots = f % 10;
        for (var i = 0; i < dots; i++) {
          canvas.drawRect(cell(14.6 + i * 1.3, -2.6, 0.9, 0.9), fill);
        }
      case CritterPose.wait:
        final q = Paint()
          ..color = body.withValues(alpha: f == 1 ? 1 : 0.15)
          ..isAntiAlias = false;
        _bitmap(canvas, _question, 14.5, -4, 1.1, 0.9, q, cell);
      case CritterPose.error:
        final bang = Paint()
          ..color = bubble
          ..isAntiAlias = false;
        _bitmap(canvas, _bang, 15.6, -4, 1.2, 0.9, bang, cell);
      case CritterPose.idle:
        // Two z's drifting up and fading in four steps.
        const lift = [0.0, -0.3, -0.6, -0.3];
        const alpha = [0.9, 0.65, 0.4, 0.65];
        final z = Paint()
          ..color = body.withValues(alpha: alpha[f])
          ..isAntiAlias = false;
        _bitmap(canvas, _z, 14.8, -2.4 + lift[f], 0.8, 0.7, z, cell);
        _bitmap(canvas, _z, 16.7, -3.9 + lift[f], 0.5, 0.5, z, cell);
      case CritterPose.think:
        // Thought dots rising: small, medium, large.
        final n = (f % 10) + 1;
        const dots = [(14.3, -0.9, 0.6), (15.3, -2.1, 0.9), (16.5, -3.8, 1.3)];
        for (final (i, (x, y, d)) in dots.indexed) {
          if (i < n) canvas.drawRect(cell(x, y, d, d), fill);
        }
      case CritterPose.read:
        _bitmap(canvas, _lens, 14.6, -4, 0.75, 0.75, fill, cell);
      case CritterPose.write:
        _bitmap(canvas, _pencil, 14.7, -4, 0.7, 0.7, fill, cell);
      case CritterPose.bash:
        _bitmap(canvas, _prompt, 14.5, -3.7, 0.7, 0.8, fill, cell);
        if (f % 10 == 0) canvas.drawRect(cell(16.5, -1.7, 1.2, 0.5), fill);
      case CritterPose.web:
        _bitmap(
          canvas,
          f == 0 ? _globeA : _globeB,
          14.4,
          -4.2,
          0.72,
          0.72,
          fill,
          cell,
        );
      case CritterPose.plan:
        for (var row = 0; row < 3; row++) {
          final y = -3.9 + row * 1.25;
          canvas.drawRect(cell(14.4, y, 0.9, 0.9), fill);
          if (row >= f) canvas.drawRect(cell(14.6, y + 0.2, 0.5, 0.5), hole);
          canvas.drawRect(cell(15.6, y + 0.25, 2.2, 0.45), fill);
        }
      case CritterPose.celebrate:
        final lit = f % 10;
        const spots = [(15.0, -3.4), (17.2, -1.9), (14.5, -0.8)];
        for (final (i, (x, y)) in spots.indexed) {
          final big = i == lit;
          final a = big ? 1.0 : 0.45;
          final d = big ? 0.6 : 0.4;
          final sp = Paint()
            ..color = body.withValues(alpha: a)
            ..isAntiAlias = false;
          canvas.drawRect(cell(x - d, y, d * 3, d), sp); // plus sign
          canvas.drawRect(cell(x, y - d, d, d * 3), sp);
        }
      case CritterPose.hungry:
        _bitmap(
          canvas,
          _cookie,
          14.3,
          f == 0 ? -4.2 : -3.6,
          0.75,
          0.75,
          fill,
          cell,
        );
      case CritterPose.compact:
        // A stack of pages pressed into one.
        const gap = [1.1, 0.7, 0.0, 0.7];
        for (var i = 0; i < 3; i++) {
          canvas.drawRect(cell(14.6, -1.6 - i * gap[f], 3.2, 0.6), fill);
        }
      case CritterPose.delegate:
      case CritterPose.exited:
        break;
    }
  }

  static void _onigiri(
    Canvas canvas,
    double bob,
    Paint fill,
    Rect Function(double, double, double, double) cell,
  ) {
    for (final (i, (x0, x1)) in _onigiriRows.indexed) {
      canvas.drawRect(
        cell(x0.toDouble(), _onigiriTop + i + bob, (x1 - x0).toDouble(), 1),
        fill,
      );
    }
  }

  static const _cookie = ['.###.', '##.##', '#####', '#.###', '.###.'];
  static const _lens = ['.##..', '#..#.', '#..#.', '.##..', '....#'];
  static const _pencil = ['...##', '..##.', '.##..', '##...', '#....'];
  static const _prompt = ['#..', '.#.', '#..'];
  static const _globeA = ['.###.', '#.#.#', '#####', '#.#.#', '.###.'];
  static const _globeB = ['.###.', '##.##', '#####', '##.##', '.###.'];

  /// A tiny critter at (x, y) scaled by s, legs swapped on [step].
  static void _mini(
    Canvas canvas,
    double x,
    double y,
    double s,
    bool step,
    Paint fill,
    Paint hole,
    Rect Function(double, double, double, double) cell,
  ) {
    final bob = step ? -0.3 : 0.0;
    for (final (row, (x0, x1)) in _rows.indexed) {
      canvas.drawRect(
        cell(x + x0 * s, y + bob + row * s, (x1 - x0) * s, s),
        fill,
      );
    }
    canvas.drawRect(cell(x + 4 * s, y + bob + 2 * s, s, 2 * s), hole);
    canvas.drawRect(cell(x + 9 * s, y + bob + 2 * s, s, 2 * s), hole);
    for (final lx in step ? _legsB : _legsA) {
      canvas.drawRect(cell(x + lx * s, y + 7 * s, s, 2 * s), fill);
    }
  }

  static void _bitmap(
    Canvas canvas,
    List<String> rows,
    double x,
    double y,
    double cw,
    double ch,
    Paint p,
    Rect Function(double, double, double, double) cell,
  ) {
    for (final (r, row) in rows.indexed) {
      for (var c = 0; c < row.length; c++) {
        if (row[c] == '#') {
          canvas.drawRect(cell(x + c * cw, y + r * ch, cw, ch), p);
        }
      }
    }
  }

  /// Outline of the body (exited critter), cell-aligned.
  static Path _bodyPath(Rect Function(double, double, double, double) cell) {
    Offset p(double x, double y) => cell(x, y, 0, 0).topLeft;
    const pts = [
      (2, 0), (12, 0), (12, 1), (13, 1), (13, 2), (14, 2), (14, 4), (13, 4),
      (13, 6), (12, 6), (12, 7), (2, 7), (2, 6), (1, 6), (1, 4), (0, 4),
      (0, 2), (1, 2), (1, 1), (2, 1), //
    ];
    final path = Path()..moveTo(p(2, 0).dx, p(2, 0).dy);
    for (final (x, y) in pts.skip(1)) {
      final o = p(x.toDouble(), y.toDouble());
      path.lineTo(o.dx, o.dy);
    }
    return path..close();
  }

  @override
  bool shouldRepaint(_CritterPainter old) =>
      old.pose != pose ||
      old.body != body ||
      old.cut != cut ||
      old.bubble != bubble ||
      old.species != species ||
      old.centerBody != centerBody ||
      old.frame != frame;
}
