/// Agent pets do the critter's poses: the same bubbles, props and timings,
/// on whole cells of the pet's 16-cell grid. A port of the web's
/// pet-poses.js (backend/internal/web/static/pet-poses.js); docs/pet-poses.md
/// is the spec both follow: edit them together.
library;

import 'critter.dart';

typedef PetCell = (int, int);

/// One layer of a posed pet: cells (x, y, colour key) and the motion classes
/// they share (the web's CSS classes, see [petMotion]).
typedef PetPart = (List<(int, int, String)>, String);

/// Where each pose anchors on an art (docs/pet-poses.md, "The rig").
class PetRig {
  const PetRig({
    this.eyes,
    this.mouth,
    this.arms,
    this.feet,
    required this.bubble,
    required this.tow,
    this.squeeze = 2,
  });

  /// [x, y, w, h], left eye then right.
  final List<(int, int, int, int)>? eyes;
  final PetCell? mouth;

  /// The top row of a 1 x 2 arm hanging down, half up and up.
  final (int, int, int)? arms;

  /// Feet rows, their columns standing, and their columns mid-step.
  final (List<int>, List<int>, List<int>)? feet;

  /// The top-left of the 5 x 5 bubble box.
  final PetCell bubble;

  /// The top-left of the 5 x 3 mini pet in tow.
  final PetCell tow;

  /// How far it drops while compacting: 2 rows, unless its face sits too
  /// low to drop 2 without covering its eyes (the kid's: 1).
  final int squeeze;
}

const petRig = <String, PetRig>{
  'egg': PetRig(bubble: (14, -4), tow: (16, 11)),
  'crack': PetRig(bubble: (14, -4), tow: (16, 11)),
  'baby': PetRig(
    eyes: [(5, 7, 1, 2), (10, 7, 1, 2)],
    mouth: (7, 9),
    arms: (9, 7, 3),
    feet: ([12], [4, 6, 9, 11], [5, 7, 9, 11]),
    bubble: (13, -1),
    tow: (14, 10),
  ),
  'kid': PetRig(
    eyes: [(5, 7, 1, 2), (10, 7, 1, 2)],
    mouth: (7, 9),
    arms: (9, 7, 4),
    feet: ([11, 12], [4, 6, 9, 11], [3, 5, 10, 12]),
    bubble: (14, 0),
    tow: (14, 10),
    squeeze: 1,
  ),
  'builder': PetRig(
    eyes: [(4, 5, 1, 2), (10, 5, 1, 2)],
    mouth: (7, 7),
    arms: (7, 5, 2),
    feet: ([10, 11], [2, 4, 11, 13], [3, 5, 10, 12]),
    bubble: (16, -2),
    tow: (16, 9),
  ),
  'weak': PetRig(
    eyes: [(3, 7, 2, 1), (9, 7, 2, 1)],
    mouth: (6, 10),
    arms: (9, 7, 4),
    feet: ([11], [2, 4, 11, 13], [3, 5, 10, 12]),
    bubble: (15, 0),
    tow: (17, 9),
  ),
};

/// What one pose does (docs/pet-poses.md, "The poses").
class _Pose {
  const _Pose({
    this.eyes = '',
    this.arms,
    this.swing = false,
    this.walk = '',
    this.grp = '',
    this.bubble = '',
    this.tow = false,
    this.flag = false,
    this.chomp = false,
    this.squash = false,
    this.hollow = false,
  });

  final String eyes;
  final (String, String)? arms;
  final bool swing;

  /// "", "yes" or "fast".
  final String walk;
  final String grp;
  final String bubble;
  final bool tow, flag, chomp, squash, hollow;
}

const _poses = <CritterPose, _Pose>{
  CritterPose.think: _Pose(
    eyes: 'glance',
    arms: ('down', 'mid'),
    bubble: 'think',
  ),
  CritterPose.read: _Pose(eyes: 'scan', bubble: 'read'),
  CritterPose.bash: _Pose(walk: 'fast', grp: 'pjog pjf', bubble: 'bash'),
  CritterPose.write: _Pose(walk: 'fast', grp: 'pjog pjf', bubble: 'bash'),
  CritterPose.web: _Pose(eyes: 'up', bubble: 'web'),
  CritterPose.plan: _Pose(bubble: 'plan'),
  CritterPose.delegate: _Pose(walk: 'yes', grp: 'pjog', swing: true, tow: true),
  CritterPose.run: _Pose(walk: 'yes', grp: 'pjog', swing: true, bubble: 'dots'),
  CritterPose.compact: _Pose(eyes: 'strain', squash: true, bubble: 'press'),
  CritterPose.celebrate: _Pose(
    eyes: 'happy',
    grp: 'hop',
    arms: ('up', 'up'),
    flag: true,
    bubble: 'sparks',
  ),
  CritterPose.hungry: _Pose(eyes: 'up', chomp: true, bubble: 'cookie'),
  CritterPose.wait: _Pose(eyes: 'up', bubble: 'ask'),
  CritterPose.error: _Pose(eyes: 'x', arms: ('up', 'up'), bubble: 'bang'),
  CritterPose.starting: _Pose(bubble: 'dots'),
  CritterPose.exited: _Pose(hollow: true),
  CritterPose.idle: _Pose(eyes: 'shut', bubble: 'zz'),
};

/// Poses of a working agent: an egg rocks through them (faster for bash).
const _rock = {
  CritterPose.run,
  CritterPose.think,
  CritterPose.read,
  CritterPose.write,
  CritterPose.bash,
  CritterPose.web,
  CritterPose.plan,
  CritterPose.delegate,
};

/// The critter's bubble bitmaps ('#' = a cell), shared with the web
/// (deck-core.js GLYPH).
const petGlyph = <String, List<String>>{
  'mag': ['.##..', '#..#.', '#..#.', '.##..', '....#'],
  'prompt': ['#..', '.#.', '#..'],
  'globe': ['.###.', '#.#.#', '#####', '#.#.#', '.###.'],
  'globe2': ['.###.', '##.##', '#####', '##.##', '.###.'],
  'cookie': ['.###.', '##.##', '#####', '#.###', '.###.'],
  'ask': ['###', '..#', '.#.', '...', '.#.'],
  'bang': ['#', '#', '#', '.', '#'],
};

const _z4 = ['####', '..#.', '.#..', '####'];
const _z3 = ['###', '.#.', '###'];
const _plus = ['.#.', '###', '.#.'];

/// The bubbles, in cells of the bubble box: rows, x, y, motion, colour key
/// (P the prop colour, B the body's).
final _bubbles = <String, List<(List<String>, int, int, String, String)>>{
  'think': [
    (['#'], 0, 4, 't1', 'P'),
    (['#'], 1, 2, 't2', 'P'),
    (['##', '##'], 3, 0, 't3', 'P'),
  ],
  'read': [(petGlyph['mag']!, 0, 0, '', 'P')],
  'bash': [
    (petGlyph['prompt']!, 0, 1, '', 'P'),
    (['##'], 3, 3, 'cursor', 'P'),
  ],
  'web': [
    (petGlyph['globe']!, 0, 0, 'la slow', 'P'),
    (petGlyph['globe2']!, 0, 0, 'lb slow', 'P'),
  ],
  'plan': [
    for (var i = 0; i < 3; i++) ...[
      (['#.###'], 0, i * 2, '', 'P'),
      (['#'], 0, i * 2, 'tick$i', 'B'),
    ],
  ],
  'dots': [
    (['#.#.#'], 0, 4, 'dots', 'P'),
  ],
  'press': [
    (['####'], 0, 0, 'press2', 'P'),
    (['####'], 0, 2, 'press1', 'P'),
    (['####'], 0, 4, '', 'P'),
  ],
  'cookie': [(petGlyph['cookie']!, 0, 0, 'bobc', 'P')],
  'ask': [(petGlyph['ask']!, 1, 0, 'blink', 'P')],
  'bang': [(petGlyph['bang']!, 2, 0, 'bang', 'P')],
  // Each z its own class, so each has its own outline.
  'zz': [(_z4, 0, 1, 'zz z0', 'P'), (_z3, 4, -2, 'zz z1', 'P')],
  'sparks': [(_plus, 0, 2, 'spark s0', 'P'), (_plus, 2, 0, 'spark s1', 'P')],
};

List<(int, int, String)> _glyph(List<String> rows, int x0, int y0, String ch) =>
    [
      for (final (y, r) in rows.indexed)
        for (var x = 0; x < r.length; x++)
          if (r[x] == '#') (x0 + x, y0 + y, ch),
    ];

const _bodyKeys = {'b', 'd', 'e', 'k', 'w', 'v', 's', 'm'};

(int, int) _span(Map<PetCell, String> pix, int y) {
  var l = 1 << 20, r = -(1 << 20);
  for (final MapEntry(key: (x, yy), value: ch) in pix.entries) {
    if (yy == y && _bodyKeys.contains(ch)) {
      if (x < l) l = x;
      if (x > r) r = x;
    }
  }
  return (l, r);
}

/// A 1 x 2 arm whose top row is [y], beside the body ([right] or left).
List<PetCell> petArmCells(Map<PetCell, String> pix, int y, bool right) {
  final a = _span(pix, y), b = _span(pix, y + 1);
  final x = right
      ? (a.$2 > b.$2 ? a.$2 : b.$2) + 1
      : (a.$1 < b.$1 ? a.$1 : b.$1) - 1;
  return [(x, y), (x, y + 1)];
}

/// One eye [x, y, w, h] drawn as [kind]; [right] is the right eye.
List<PetCell> petEyeCells((int, int, int, int) e, String kind, bool right) {
  final (x, y, w, h) = e;
  final t = h >= 2 ? y : y - 1; // the top of a 3-row mark
  List<PetCell> row(int yy, [int? x0, int? n]) => [
    for (var i = 0; i < (n ?? w); i++) ((x0 ?? x) + i, yy),
  ];
  List<PetCell> rect(int dy) => [
    for (var j = 0; j < (h < 2 ? h : 2); j++) ...row(y + j + dy),
  ];
  return switch (kind) {
    'up' || 'glance' => rect(-1),
    'shut' => row(y + h - 1, right ? x : x - 1, w + 1),
    'happy' => [(x - 1, t + 1), ...row(t), (x + w, t + 1)],
    'x' => [
      (x - 1, t),
      (x + w, t),
      ...row(t + 1),
      (x - 1, t + 2),
      (x + w, t + 2),
    ],
    'strain' =>
      right
          ? [(x + w, t), ...row(t + 1), (x + w, t + 2)]
          : [(x - 1, t), ...row(t + 1), (x - 1, t + 2)],
    'dot' => [(right ? x : x + w - 1, y + h - 1)],
    _ => rect(0),
  };
}

/// A pet's look as [posePet] needs it.
class PetPoseInput {
  const PetPoseInput({
    required this.art,
    required this.pix,
    required this.egg,
    required this.asleep,
    required this.hungry,
    required this.hatCells,
  });

  final String art;
  final Map<PetCell, String> pix;
  final bool egg, asleep, hungry;
  final Set<PetCell> hatCells;
}

/// What a pose changes on a pet: its cells, the motion of the whole body
/// ([grp]), layers inside the body's motion ([inner]) and outside it
/// ([outer]: stepping feet, the bubble, the mini pet), the cells that drop
/// when it is squeezed ([top]), and whether it is drawn hollow with its hat
/// in shade.
class PosedPet {
  PosedPet(this.pix);

  final Map<PetCell, String> pix;
  String grp = '';
  final inner = <PetPart>[];
  final outer = <PetPart>[];
  Set<PetCell>? top;
  bool hollow = false;
  bool shade = false;
  PetCell? flagAt;

  /// How far it drops while compacting (see [PetRig.squeeze]).
  int squeeze = 2;
}

/// The pet doing [pose] (docs/pet-poses.md); [bubble] false leaves its
/// bubble out, [keepOut]: cells the bubble must keep a cell clear of (the
/// mystery hat's "?").
PosedPet posePet(
  PetPoseInput look,
  CritterPose pose, {
  bool bubble = true,
  Set<PetCell> keepOut = const {},
}) {
  final spec = _poses[pose] ?? _poses[CritterPose.idle]!;
  final rig = petRig[look.art];
  final pix = Map.of(look.pix);
  final out = PosedPet(pix);
  if (rig == null) return out;
  final egg = look.egg;
  final body = look.art == 'weak' ? 'd' : 'b';
  final eyeCh = look.art == 'weak' ? 'k' : 'e';
  // In the ice nothing moves: only "?" and "!" show beside the cube.
  final bubbleOnly =
      look.asleep && (pose == CritterPose.wait || pose == CritterPose.error);
  if (look.asleep && !bubbleOnly) return out;
  // A pet hungry for food stays awake: an idle one keeps its eyes open and
  // its mouth wide, no "z z".
  final awake = pose == CritterPose.idle && look.hungry;
  void layer(List<(int, int, String)> cells, String cls, [bool outer = false]) {
    if (cells.isNotEmpty) (outer ? out.outer : out.inner).add((cells, cls));
  }

  if (!egg && !bubbleOnly) {
    final kind = awake ? '' : spec.eyes;
    if (kind.isNotEmpty && rig.eyes != null) {
      final cells = <(int, int, String)>[];
      for (final (side, e) in rig.eyes!.indexed) {
        for (final c in petEyeCells(e, 'open', side == 1)) {
          pix[c] = body;
        }
        cells.addAll([
          for (final (x, y) in petEyeCells(e, kind, side == 1)) (x, y, eyeCh),
        ]);
      }
      if (kind == 'glance' || kind == 'scan') {
        layer(cells, kind);
      } else {
        for (final (x, y, ch) in cells) {
          pix[(x, y)] = ch;
        }
      }
    }
    void arm(String pos, bool right, [String cls = '']) {
      final (down, mid, up) = rig.arms!;
      final y = switch (pos) {
        'down' => down,
        'mid' => mid,
        _ => up,
      };
      layer([
        for (final (x, yy) in petArmCells(look.pix, y, right)) (x, yy, 'b'),
      ], cls.isEmpty ? 'pp' : 'pp $cls');
    }

    if (spec.swing) {
      arm('down', false, 'la');
      arm('mid', true, 'la');
      arm('mid', false, 'lb');
      arm('down', true, 'lb');
    } else if (spec.arms case (final l, final r)) {
      arm(l, false);
      arm(r, true);
    }
    if (spec.walk.isNotEmpty && rig.feet != null) {
      final (rows, xs, stepped) = rig.feet!;
      for (final y in rows) {
        for (final x in xs) {
          pix.remove((x, y));
        }
      }
      final fast = spec.walk == 'fast' ? ' fast' : '';
      // Inside the body's motion: the feet never come apart from the body.
      layer([
        for (final y in rows)
          for (final x in xs) (x, y, body),
      ], 'la$fast');
      layer([
        for (final y in rows)
          for (final x in stepped) (x, y, body),
      ], 'lb$fast');
    }
    if (spec.chomp) {
      final (mx, my) = rig.mouth!;
      if (look.art == 'weak') {
        layer([(mx, my, 'k'), (mx + 1, my, 'k'), (mx + 2, my, 'k')], 'la slow');
      } else {
        for (final (dx, dy) in const [(0, 0), (1, 0), (0, 1), (1, 1)]) {
          pix[(mx + dx, my + dy)] = body;
        }
        layer([
          (mx, my, 'm'),
          (mx + 1, my, 'm'),
          (mx, my + 1, 'm'),
          (mx + 1, my + 1, 'm'),
        ], 'la slow');
        layer([(mx, my + 1, 'm'), (mx + 1, my + 1, 'm')], 'lb slow');
      }
    }
    if (spec.flag) {
      // The finish flag in the raised right hand: a pole and a 3 x 2
      // checker that waves by swapping its squares.
      final up = rig.arms!.$3;
      final px = petArmCells(look.pix, up, true)[0].$1 + 1;
      List<(int, int, String)> checker(int phase) => [
        for (var cx = 0; cx < 3; cx++)
          for (var cy = 0; cy < 2; cy++)
            (px + 1 + cx, up - 4 + cy, (cx + cy + phase).isOdd ? 'k' : 'w'),
      ];
      layer([for (var i = 0; i < 6; i++) (px, up - 4 + i, 'P')], 'pp');
      layer(checker(0), 'pp la');
      layer(checker(1), 'pp lb');
      out.flagAt = (px, up);
    }
    if (spec.hollow) {
      // Only the outline stays, with two dots for eyes; the hat dims.
      bool solid(PetCell c) =>
          look.pix.containsKey(c) && !look.hatCells.contains(c);
      for (final c in pix.keys.toList()) {
        if (look.hatCells.contains(c)) continue;
        final (x, y) = c;
        if (solid((x + 1, y)) &&
            solid((x - 1, y)) &&
            solid((x, y + 1)) &&
            solid((x, y - 1))) {
          pix.remove(c);
        }
      }
      for (final (side, e) in (rig.eyes ?? const []).indexed) {
        for (final c in petEyeCells(e, 'dot', side == 1)) {
          pix[c] = body;
        }
      }
      out.hollow = out.shade = true;
    }
  } else if (egg && !bubbleOnly && spec.hollow) {
    for (final c in pix.keys.toList()) {
      final (x, y) = c;
      if (look.pix.containsKey((x + 1, y)) &&
          look.pix.containsKey((x - 1, y)) &&
          look.pix.containsKey((x, y + 1)) &&
          look.pix.containsKey((x, y - 1))) {
        pix.remove(c);
      }
    }
    out.hollow = true;
  }
  if (!bubbleOnly) {
    // The whole body moves: an egg rocks while its agent works.
    if (egg) {
      out.grp = _rock.contains(pose)
          ? 'prock${spec.walk == 'fast' ? ' prk2' : ''}'
          : pose == CritterPose.celebrate
          ? 'hop'
          : '';
    } else {
      out.grp = spec.grp;
    }
    // Squeezed: all but the rows it drops over (above the feet) drop, and
    // are drawn over them, face and all.
    if (spec.squash) {
      final feetTop =
          rig.feet?.$1.first ??
          pix.keys.map((c) => c.$2).reduce((a, b) => a > b ? a : b) + 1;
      out.squeeze = rig.squeeze;
      final split = feetTop - rig.squeeze;
      out.top = {
        for (final c in pix.keys)
          if (c.$2 < split) c,
      };
    }
  }
  if (spec.tow && !bubbleOnly) {
    final (tx, ty) = rig.tow;
    // It doesn't bob: its feet would come apart from it.
    layer(_glyph(['#####', '#.#.#'], tx, ty, body), 'pp', true);
    layer(_glyph(['#...#'], tx, ty + 2, body), 'pp la', true);
    layer(_glyph(['.#.#.'], tx, ty + 2, body), 'pp lb', true);
  }
  final name = awake ? '' : spec.bubble;
  if (name.isNotEmpty && bubble) {
    var (bx, by) = rig.bubble;
    var list = _bubbles[name]!;
    // Beside the flag: one spark right of it, one above.
    if (name == 'sparks' && out.flagAt != null) {
      (bx, by) = out.flagAt!;
      list = [(_plus, 5, -4, 'spark s0', 'P'), (_plus, 3, -8, 'spark s1', 'P')];
    }
    final parts = [
      for (final (rows, x, y, cls, ch) in list)
        (_glyph(rows, bx + x, by + y, ch), cls),
    ];
    bool near(int x, int y) {
      for (var dx = -1; dx <= 1; dx++) {
        for (var dy = -1; dy <= 1; dy++) {
          if (keepOut.contains((x + dx, y + dy))) return true;
        }
      }
      return false;
    }

    final clash = parts.any((p) => p.$1.any((c) => near(c.$1, c.$2)));
    if (!clash) {
      for (final (cells, cls) in parts) {
        // The sparks by the flag hop with it.
        layer(
          cells,
          cls.isEmpty ? 'pp' : 'pp $cls',
          !(name == 'sparks' && out.flagAt != null),
        );
      }
    }
  }
  return out;
}

/// How a layer with motion classes [cls] shows at [ms] on the shared
/// clock: its opacity and its offset in whole cells. The classes and their
/// timings are the web's (settings.css): see docs/pet-poses.md, "Timings".
/// [still]: reduced motion, the first frame of each pair, everything else
/// up and in place.
(double, int, int) petMotion(String cls, int ms, {bool still = false}) {
  if (cls.isEmpty) return (1, 0, 0);
  final c = cls.split(' ').toSet();
  if (still) {
    return (c.contains('lb') || c.contains('hfo') ? 0 : 1, 0, 0);
  }
  double f(int period, [int delay = 0]) =>
      ((ms - delay) % period + period) % period / period;
  final pace = c.contains('fast')
      ? 200
      : c.contains('slow')
      ? 800
      : 500;
  var a = 1.0;
  var dx = 0, dy = 0;
  for (final k in c) {
    switch (k) {
      case 'la':
        if (f(pace) >= 0.5) a = 0;
      case 'lb':
        if (f(pace) < 0.5) a = 0;
      case 'bob':
        if (f(pace) >= 0.5) dy -= 1;
      case 'pjog':
        // The whole pet lifts a cell, feet and all (0.4 s for bash: pjf).
        if (f(c.contains('pjf') ? 400 : 500) >= 0.5) dy -= 1;
      case 'pbob':
        if (f(1000) >= 0.5) dy -= 1;
      case 'hop':
        if (f(400) >= 0.5) dy -= 2;
      case 'glance':
        dx += f(1200) < 0.5 ? -1 : 1;
      case 'scan':
      case 'prock':
        final q = (f(k == 'prock' && c.contains('prk2') ? 600 : 1200) * 4)
            .floor();
        dx += const [0, -1, 0, 1][q.clamp(0, 3)];
      case 't2':
        if (f(1200) < 0.25) a = 0;
      case 't3':
        if (f(1200) < 0.5) a = 0;
      case 'cursor':
        if (f(1000) >= 0.5) a = 0;
      case 'tick0':
        if (f(2000) >= 0.25) a = 0;
      case 'tick1':
        if (f(2000) >= 0.5) a = 0;
      case 'tick2':
        if (f(2000) >= 0.75) a = 0;
      case 'dots':
        // 0.2 → 1 → 0.2, three steps each way (steps(3) per keyframe).
        const levels = [
          0.2,
          0.2 + 0.8 / 3,
          0.2 + 1.6 / 3,
          1,
          1 - 0.8 / 3,
          1 - 1.6 / 3,
        ];
        a *= levels[(f(1200) * 6).floor().clamp(0, 5)];
      case 'blink':
        if (f(1000) >= 0.6) a *= 0.15;
      case 'spark':
        if (f(600, c.contains('s1') ? 200 : 0) >= 0.335) a *= 0.45;
      case 'bobc':
        if (f(600) >= 0.5) dy += 1;
      case 'zz':
        if (f(2400, c.contains('z1') ? 1200 : 0) >= 0.5) {
          dy -= 1;
          a *= 0.45;
        } else {
          a *= 0.9;
        }
      case 'psq1':
        // The kid's squeeze: one row, from a third of the way.
        if (f(1200) >= 0.33) dy += 1;
      case 'psq':
        final t = f(1200);
        dy += t < 0.33
            ? 0
            : t < 0.66
            ? 1
            : 2;
      case 'press2':
        if (f(1200) >= 0.33) a = 0;
      case 'press1':
        if (f(1200) >= 0.66) a = 0;
      default:
        if (k.startsWith('hf-')) {
          final p = k.split('-');
          final n = int.parse(p[1]), each = int.parse(p[2]);
          final i = int.parse(p[3]);
          if ((ms % (n * each)) ~/ each != i) a = 0;
        }
    }
  }
  return (a, dx, dy);
}

/// A ready egg's wobble at [ms] (1.6 s: still, then left, right, left).
int petReadyWobble(int ms) {
  final t = ms % 1600 / 1600;
  if (t < 0.7) return 0;
  if (t < 0.8) return -1;
  if (t < 0.9) return 1;
  return -1;
}
