/// Agent pets: the pixel art and how it is drawn. The grids, hats, auras and
/// the ice cube mirror the web dashboard's pets.js
/// (backend/internal/web/static/pets.js): edit both together.
library;

import 'dart:math' as math;

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';

import 'agents_model.dart';
import 'critter.dart';
import 'hats.dart';

/// The pets on a 16-cell grid, 14 rows. `.` empty, `b` body, `d` body shade,
/// `e` eye, `w` shell, `v` shell shade, `s` spot (hat colour), `k` crack or
/// dark line, `g` ground, `p` prop (the pencil).
const petArt = <String, List<String>>{
  'egg': [
    '......wwww......',
    '....wwwwwwww....',
    '...wwwwwswwwv...',
    '..wwwwwsssswvv..',
    '..wwwwwwswwwwv..',
    '.wwwwwwwwwwwwvv.',
    '.wswwwwwwwwwsvv.',
    '.ssswwwwwwwsssv.',
    '.wswwwwwwwwwsvv.',
    '.wwwwwwwwwwwwvv.',
    '..wwwwwwwwwwwv..',
    '..wwwwwswwwwvv..',
    '...wwwwwwwwvv...',
    '.....vvvvvv.....',
  ],
  'crack': [
    '......wwww......',
    '....wwwwwwww....',
    '...wwwwwswwwv...',
    '..wwwwwsssswvv..',
    '..wwwwwwswwwwv..',
    '.wwkwwwkwwwkwvv.',
    '.wskkwkwkwkkkvv.',
    '.ssswkwwwkwsssv.',
    '.wswwwwwwwwwsvv.',
    '.wwwwwwwwwwwwvv.',
    '..wwwwwwwwwwwv..',
    '..wwwwwswwwwvv..',
    '...wwwwwwwwvv...',
    '.....vvvvvv.....',
  ],
  'baby': [
    '................',
    '................',
    '................',
    '................',
    '.....bbbbbb.....',
    '....bbbbbbbb....',
    '...bbbbbbbbbb...',
    '...bbebbbbebb...',
    '...bbebbbbebb...',
    '...bbbbddbbbb...',
    '...bbbbbbbbbb...',
    '....bbbbbbbb....',
    '....b.b..b.b....',
    '................',
  ],
  'kid': [
    '................',
    '................',
    '................',
    '................',
    '................',
    '...bbbbbbbbbb...',
    '...bbbbbbbbbb...',
    '..bbbebbbbebbb..',
    '..bbbebbbbebbb..',
    '...bbbbbbbbbb...',
    '...bbbbbbbbbb...',
    '....b.b..b.b....',
    '....b.b..b.b....',
    '................',
  ],
  'builder': [
    '................',
    '..............p.',
    '.............pp.',
    '.bbbbbbbbbbbbpb.',
    '.bbbbbbbbbbbpbb.',
    '.bbbebbbbbebbbb.',
    'bbbbebbbbbebbbbb',
    'bbbbbbbbbbbbbbbb',
    'bbbbbbbbbbbbbbbb',
    '.bbbbbbbbbbbbbb.',
    '..b.b......b.b..',
    '..b.b......b.b..',
    '................',
    '................',
  ],
  'weak': [
    '................',
    '................',
    '................',
    '................',
    '..p.............',
    '.ddddddddddddd..',
    '.ddddddddddddd..',
    '.ddkkddddkkdddd.',
    'dddddddddddddddd',
    'ddddddkkkddddddd',
    '.dddddddddddddd.',
    '..d.d......d.d..',
    'gggggggggggggggg',
    '................',
  ],
};

/// Where the hat sits on each art: the body's top row, its centre column,
/// and whether it takes the small cut (the kid).
const _hatAt = <String, (int, double, bool)>{
  'kid': (5, 7.5, true),
  'builder': (3, 7.5, false),
  'weak': (5, 8, false),
};

/// Smaller cuts of the deck's hats, for the kid. a = hat colour, b = shade,
/// c = highlight, d = near-black.
const petSmallHats = <String, List<String>>{
  'propeller': ['cc.aa', '..d..', 'ababa'],
  'cap': ['..aaaa.', '.aaaaca', 'bbbbbbbb'],
  'wizard': ['...a..', '..aca.', '.aaaa.', 'bbbbbb'],
  'top hat': ['.aaaa.', '.bbbb.', 'aaaaaa'],
  'crown': ['a.aa.a', 'aaaaaa', 'acaaca'],
  'beret': ['..b...', '.aaaaa.', 'aaaaaaa', '.bbbbb.'],
  'beanie': ['..cc..', '.aaaa.', 'bababa'],
  'hard hat': ['.aaaa.', 'aacaaa', 'bbbbbbb'],
  'cowboy': ['..aa..', 'b.aa.b', 'bbbbbb'],
  'helmet': ['.aaaa.', 'aacaaa', 'bbbbbb'],
  'chef': ['.cccc.', 'cccccc', '.cccc.', '.aaaa.'],
  'headphones': ['.aaaa.', 'a....a', 'b....b'],
  'bandana': ['.aaaaaa.', 'acacacac'],
};

const _colours = <String, Color>{
  'b': Color(0xFFFF4D00),
  'd': Color(0xFFC23A00),
  'e': Color(0xFF141412),
  'w': Color(0xFFF4F0E6),
  'v': Color(0xFFB9B4A8),
  'k': Color(0xFF0A0A0A),
  'g': Color(0xFF55524B),
  'p': Color(0xFFE8E4DC),
  // The open mouth of a hungry pet: light grey, so it reads on a dark tile.
  'm': Color(0xFFBDB8AD),
  'c': Color(0xFFF7F4EC),
};
const _weakBody = Color(0xFFB0673F);

/// A power-up aura: flame tongues around the body (one layer still, one
/// glowing in and out), a light edge hugging it, sparks drifting up.
class _Aura {
  const _Aura(
    this.outer,
    this.inner,
    this.spark,
    this.heights,
    this.sparks,
    this.op,
  );
  final Color outer, inner, spark;
  final List<int> heights;
  final int sparks;
  final double op;
}

const _auras = <String, _Aura>{
  'strong': _Aura(
    Color(0xFFFFE08A),
    Color(0xFFFFF8E6),
    Color(0xFFFFFFFF),
    [1, 3, 1, 2, 2],
    5,
    0.75,
  ),
  'mighty': _Aura(
    Color(0xFF3FA9F5),
    Color(0xFFBDE7FF),
    Color(0xFFFFFFFF),
    [2, 4, 2, 3, 5, 2],
    7,
    0.7,
  ),
  'legend': _Aura(
    Color(0xFFE8A317),
    Color(0xFFFFE58A),
    Color(0xFFFFF6C8),
    [3, 5, 2, 6, 3, 4, 5],
    10,
    0.85,
  ),
};

/// An unhatched egg cracks from this many tokens eaten.
const _crackAt = 30000;

/// The mouth's top-left cell on each art.
const _mouth = <String, (int, int)>{
  'builder': (7, 7),
  'kid': (7, 9),
  'baby': (7, 9),
};

/// Python's round (halves to even), as the art was drawn with it.
int _rnd(double v) {
  final f = v.floorToDouble();
  final d = v - f;
  if (d > 0.5) return f.toInt() + 1;
  if (d < 0.5) return f.toInt();
  return f.toInt().isOdd ? f.toInt() + 1 : f.toInt();
}

typedef _Cell = (int, int);

/// What to draw for a pet: which art, and its cells (x, y) -> colour key.
class PetLook {
  PetLook._(this.art, this.pix, this.weak, this.asleep, this.aura, this.egg);

  /// [hat]: the hat it wears (its own, else its agent's); its colour also
  /// spots the egg. [idle]: eyes shut.
  factory PetLook.of(AgentPet pet, {AgentHat? hat, bool idle = false}) {
    final stage = pet.stage;
    final mood = pet.mood;
    final egg = stage == 'egg';
    var art = switch (stage) {
      'egg' => pet.ate >= _crackAt ? 'crack' : 'egg',
      'baby' || 'kid' => stage,
      _ => 'builder',
    };
    final weak = mood == 'weak' && !egg;
    final asleep = mood == 'asleep';
    if (weak && art == 'builder') art = 'weak';
    final pix = <_Cell, String>{};
    for (final (y, row) in petArt[art]!.indexed) {
      for (var x = 0; x < row.length; x++) {
        if (row[x] != '.') pix[(x, y)] = row[x];
      }
    }
    if (!egg && art != 'weak') {
      // Hungry shows its open mouth even while its agent is idle.
      if (mood == 'hungry' && !asleep) {
        final (mx, my) = _mouth[art]!;
        for (final (dx, dy) in const [(0, 0), (1, 0), (0, 1), (1, 1)]) {
          pix[(mx + dx, my + dy)] = 'm';
        }
      } else if (asleep || weak || idle) {
        _shutEyes(pix);
      }
    }
    if (art != 'baby' && !egg && hat != null) _putHat(pix, art, hat);
    final aura = !weak && !asleep && _auras.containsKey(stage) ? stage : '';
    return PetLook._(art, pix, weak, asleep, aura, egg);
  }

  /// egg, crack, baby, kid, builder or weak.
  final String art;

  /// Its cells: (x, y) on the 16-cell grid -> colour key (see [petArt]; h,
  /// H are the hat's colour and shade, m the open mouth).
  final Map<(int, int), String> pix;
  final bool weak;
  final bool asleep;

  /// strong, mighty, legend, or "" for none.
  final String aura;
  final bool egg;

  /// Eyes shut: each eye becomes a short line at its lower row, turned out.
  static void _shutEyes(Map<_Cell, String> pix) {
    final eyes = [
      for (final e in pix.entries)
        if (e.value == 'e') e.key,
    ];
    if (eyes.isEmpty) return;
    final xs = {for (final (x, _) in eyes) x}.toList()..sort();
    final low = eyes.map((c) => c.$2).reduce((a, b) => a > b ? a : b);
    for (final c in eyes) {
      pix[c] = 'b';
    }
    final l = xs.first, r = xs.last;
    for (final x in [l - 1, l, r, r + 1]) {
      pix[(x, low)] = 'e';
    }
  }

  /// The deck's hat on an art: its still frame, moved from the critter's
  /// spot to the pet's (the kid takes a smaller cut). a/b/c/d become h (hat
  /// colour), H (shade), c (highlight), k (near-black).
  static void _putHat(Map<_Cell, String> pix, String art, AgentHat hat) {
    const keys = {'a': 'h', 'b': 'H', 'c': 'c', 'd': 'k'};
    final hatArtFor = hatArt[hat.shape];
    final place = _hatAt[art];
    if (hatArtFor == null || place == null) return;
    final (top, cx, small) = place;
    final cut = petSmallHats[hat.shape];
    if (small && cut != null) {
      final x0 = _rnd(cx - cut[0].length / 2 + 0.5);
      final y0 = top - cut.length;
      for (final (i, r) in cut.indexed) {
        for (var j = 0; j < r.length; j++) {
          if (keys[r[j]] case final k?) pix[(x0 + j, y0 + i)] = k;
        }
      }
      return;
    }
    final f = hatArtFor.critterStill;
    final ox = 1 + _rnd(cx - 7.5);
    for (final (i, r) in f.rows.indexed) {
      for (var j = 0; j < r.length; j++) {
        if (keys[r[j]] case final k?) pix[(f.x + ox + j, f.top + top + i)] = k;
      }
    }
    for (final (x, y, ch) in f.extras) {
      if (keys[ch] case final k?) pix[(x + ox, y + top)] = k;
    }
    if (hat.shape == 'headphones') {
      // The ear cups, beside the head.
      for (final dy in const [0, 1]) {
        pix[(ox - 1, top + dy)] = 'H';
        pix[(ox + 14, top + dy)] = 'H';
      }
    }
  }
}

/// One colour layer: every cell of one colour as one path, in grid cells.
class _Layer {
  _Layer(this.color, Iterable<_Cell> cells)
    : path = (Path()..fillType = PathFillType.nonZero) {
    for (final (x, y) in cells) {
      path.addRect(Rect.fromLTWH(x.toDouble(), y.toDouble(), 1, 1));
    }
  }
  _Layer.rect(this.color, Rect r) : path = Path()..addRect(r);

  final Color color;
  final Path path;
}

List<_Layer> _layers(Iterable<(int, int, Color)> cells) {
  final by = <Color, List<_Cell>>{};
  for (final (x, y, c) in cells) {
    (by[c] ??= []).add((x, y));
  }
  return [for (final e in by.entries) _Layer(e.key, e.value)];
}

/// Everything drawn for one look, built once and kept: the still layers,
/// and the few moving ones (each moves as a whole, never a pixel at a time).
class _PetScene {
  _PetScene(PetLook look, AgentHat? hat) {
    final (main, shade) = hatColor(
      hatColors.contains(hat?.color) ? hat!.color : 'teal',
    );
    final col = {..._colours, 's': main, 'h': main, 'H': shade};
    if (look.weak) col['b'] = col['d'] = _weakBody;
    final filled = look.pix.keys.toSet();
    final cells = [
      for (final e in look.pix.entries)
        (e.key.$1, e.key.$2, col[e.value] ?? col['b']!),
    ];
    body = _layers(cells);
    final xs = filled.map((c) => c.$1), ys = filled.map((c) => c.$2);
    bounds = Rect.fromLTRB(
      xs.reduce(math.min).toDouble(),
      ys.reduce(math.min).toDouble(),
      xs.reduce(math.max) + 1.0,
      ys.reduce(math.max) + 1.0,
    );
    if (look.asleep) _ice(filled);
    if (_auras[look.aura] case final a?) {
      _aura(filled, a, look.aura == 'legend');
    }
  }

  /// The box the body (and its ice) takes, in cells.
  late Rect bounds;

  List<_Layer> behind = const [];
  List<_Layer> glow = const [];
  List<_Layer> under = const [];
  late final List<_Layer> body;
  List<_Layer> over = const [];
  _Layer? glint;
  List<(int, int)> sparks = const [];
  Color spark = const Color(0x00000000);
  List<_Layer> stars = const [];
  List<(int, int)> snow = const [];

  /// Has parts that move (a ready egg's wobble is up to its pet).
  bool get moves => glow.isNotEmpty || sparks.isNotEmpty || snow.isNotEmpty;

  void _aura(Set<_Cell> filled, _Aura a, bool legend) {
    final xs = {for (final (x, _) in filled) x}.toList()..sort();
    final ys = {for (final (_, y) in filled) y}.toList()..sort();
    final topOf = <int, int>{};
    final span = <int, (int, int)>{};
    for (final (x, y) in filled) {
      topOf[x] = topOf[x] == null || y < topOf[x]! ? y : topOf[x]!;
      final s = span[y];
      span[y] = s == null ? (x, x) : (x < s.$1 ? x : s.$1, x > s.$2 ? x : s.$2);
    }
    final mid = (ys.first + ys.last) / 2;
    final inner = <_Cell>{};
    for (final MapEntry(key: y, value: (l, r)) in span.entries) {
      inner
        ..add((l - 1, y))
        ..add((r + 1, y));
    }
    for (var x = xs.first; x <= xs.last; x++) {
      if (topOf[x] case final t?) inner.add((x, t - 1));
    }
    final frames = <List<_Layer>>[];
    for (final f in const [0, 1]) {
      final fl = <_Cell>{};
      for (final MapEntry(key: y, value: (l, r)) in span.entries) {
        final w = y <= mid ? 2 : 1;
        for (var k = 2; k < 2 + w; k++) {
          if ((y + f) % 4 != 0 || k == 2) {
            fl
              ..add((l - k, y))
              ..add((r + k, y));
          }
        }
      }
      var i = 0;
      for (var x = xs.first - 2; x <= xs.last + 2; x++, i++) {
        final h = a.heights[(i + f * 3) % a.heights.length];
        final t = (topOf[x] ?? topOf[x - 1] ?? topOf[x + 1] ?? ys.first) - 2;
        for (var k = 0; k < h; k++) {
          fl.add((x, t - k));
        }
      }
      fl.removeWhere((c) => filled.contains(c) || inner.contains(c));
      frames.add([_Layer(a.outer.withValues(alpha: f == 0 ? a.op : 1), fl)]);
    }
    behind = [...frames[0], _Layer(a.inner.withValues(alpha: 0.9), inner)];
    glow = frames[1];
    final x0 = xs.first, x1 = xs.last, cxm = ((x0 + x1) / 2).floor();
    final sx = [
      x0 - 2,
      x1 + 2,
      x0 + 2,
      x1 - 2,
      cxm,
      x0 - 1,
      x1 + 1,
      x0 + 4,
      x1 - 4,
      cxm + 2,
    ];
    sparks = [
      for (var i = 0; i < a.sparks; i++) (sx[i % 10], ys.last - (i % 3)),
    ];
    spark = a.spark;
    if (legend) {
      stars = [
        for (final (x, y) in [
          (-2, ys.first - 2),
          (17, ys.first),
          (-3, 7),
          (18, 8),
          // Nudged down from the web's 7 rows, so it stays on the tile.
          (8, ys.first - 4),
        ])
          _Layer(const Color(0xFFFFF6C8), [
            for (final (dx, dy) in const [
              (0, 0),
              (-1, 0),
              (1, 0),
              (0, -1),
              (0, 1),
            ])
              (x + dx, y + dy),
          ]),
      ];
    }
  }

  /// The ice cube of a hibernating pet: still, with a glint now and then
  /// and snow falling past.
  void _ice(Set<_Cell> filled) {
    final xs = filled.map((c) => c.$1), ys = filled.map((c) => c.$2);
    final x0 = xs.reduce((a, b) => a < b ? a : b) - 1;
    final x1 = xs.reduce((a, b) => a > b ? a : b) + 1;
    final y0 = ys.reduce((a, b) => a < b ? a : b) - 1;
    final y1 = ys.reduce((a, b) => a > b ? a : b) + 1;
    const edgeC = Color(0xFFDFF4FF);
    final edge = <_Cell>[
      for (var y = y0; y <= y1; y++)
        for (var x = x0; x <= x1; x++)
          if (x == x0 || x == x1 || y == y0 || y == y1) (x, y),
    ];
    final g = (y1 - y0 - 1) < (x1 - x0 - 4) ? y1 - y0 - 1 : x1 - x0 - 4;
    // The cube and the frost under it.
    bounds = Rect.fromLTRB(x0 - 1.0, y0.toDouble(), x1 + 2.0, y1 + 2.0);
    under = [
      _Layer(edgeC.withValues(alpha: 0.45), [
        for (var x = x0 - 1; x <= x1 + 1; x++) (x, y1 + 1),
      ]),
    ];
    over = [
      _Layer.rect(
        const Color(0xFF8FD0F2).withValues(alpha: 0.4),
        Rect.fromLTRB(x0.toDouble(), y0.toDouble(), x1 + 1.0, y1 + 1.0),
      ),
      _Layer(edgeC.withValues(alpha: 0.9), edge),
      _Layer(const Color(0xCCFFFFFF), [
        for (var i = 0; i < 3; i++) (x0 + 1 + i, y0 + 1 + i),
      ]),
    ];
    glint = _Layer(const Color(0xFFFFFFFF), [
      for (var i = 0; i < g; i++) (x0 + 3 + i, y0 + 1 + i),
    ]);
    snow = [
      (x0 - 2, y0 - 3),
      (x1 + 2, y0 - 3),
      (x0 + 3, y0 - 3),
      (x1 - 3, y0 - 3),
    ];
  }

  static final _cache = <String, _PetScene>{};

  static _PetScene of(AgentPet pet, AgentHat? hat, bool idle) {
    final key = [
      pet.stage,
      pet.mood,
      pet.isEgg && pet.ate >= _crackAt,
      hat?.shape,
      hat?.color,
      idle,
    ].join('|');
    final hit = _cache[key];
    if (hit != null) return hit;
    if (_cache.length > 64) _cache.clear();
    return _cache[key] = _PetScene(PetLook.of(pet, hat: hat, idle: idle), hat);
  }
}

/// The hat a pet wears: its own, or its agent's (an older service sends
/// none); [hat] also colours the egg's spots.
AgentHat? petHatFor(AgentPet pet, AgentHat? agentHat) => pet.hat ?? agentHat;

/// One motion step, in [CritterClock] ticks: every moving part changes at
/// most this often (150 ms), so a tile repaints a few times a second.
const _stepTicks = 3;

/// An agent's pet [width] wide, in the critter's box (height = width ×
/// 13/18) and at the critter's scale: the body is drawn in whole pixels,
/// centred where the critter's would be. Auras, the ice and hats reach
/// past the box. Moves (glow, sparks, snow, a ready egg wobbling) when a
/// [CritterScope] is above it and the platform allows motion.
class PetSprite extends StatefulWidget {
  const PetSprite({
    super.key,
    required this.pet,
    required this.width,
    this.hat,
    this.idle = false,
    this.plate = false,
  });

  final AgentPet pet;
  final double width;

  /// Its agent's hat: worn from kid on when the pet has none of its own.
  final AgentHat? hat;

  /// Its agent is idle: eyes shut.
  final bool idle;

  /// On a dark square plate, a cell wider than its body all round: for the
  /// solid orange tiles, where its own orange would vanish.
  final bool plate;

  @override
  State<PetSprite> createState() => _PetSpriteState();
}

class _PetSpriteState extends State<PetSprite> {
  final _step = ValueNotifier<int>(0);
  CritterClock? _clock;
  bool _still = false;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _still = MediaQuery.maybeDisableAnimationsOf(context) ?? false;
    final c = CritterScope.of(context);
    if (c != _clock) {
      _clock?.removeListener(_onTick);
      _clock = c?..addListener(_onTick);
    }
    _onTick();
  }

  void _onTick() {
    _step.value = (_clock?.value ?? 0) ~/ _stepTicks;
  }

  @override
  void dispose() {
    _clock?.removeListener(_onTick);
    _step.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final p = widget.pet;
    final hat = petHatFor(p, widget.hat);
    final scene = _PetScene.of(p, hat, widget.idle);
    final wobbles = p.isEgg && p.ready;
    return RepaintBoundary(
      child: CustomPaint(
        size: Size(widget.width, widget.width * 13 / 18),
        painter: _PetPainter(
          scene: scene,
          still: _still,
          rocks: wobbles,
          plate: widget.plate,
          step: _still || !(scene.moves || wobbles) ? null : _step,
        ),
      ),
    );
  }
}

/// Paints one pet scene; see [PetSprite].
class _PetPainter extends CustomPainter {
  _PetPainter({
    required _PetScene scene,
    required this.still,
    this.plate = false,
    required this.rocks,
    this.step,
  }) : _scene = scene,
       super(repaint: step);

  final _PetScene _scene;

  /// Reduce motion: no glow, sparks, snow or glint; stars stay lit.
  final bool still;
  final bool plate;

  /// A ready egg wobbles.
  final bool rocks;
  final ValueListenable<int>? step;

  static final _paint = Paint()..isAntiAlias = false;

  @override
  void paint(Canvas canvas, Size size) {
    final s = _scene;
    final n = step?.value ?? 0;
    // Whole device pixels per cell, as many as fit the critter's 18 cells.
    final m = canvas.getTransform();
    final scale = m[0] > 0 ? m[0] : 1.0;
    final ud = (size.width * scale / 18).floorToDouble().clamp(
      1.0,
      double.infinity,
    );
    final u = ud / scale;
    // The 16-cell grid sits one cell in from the critter box's left, and a
    // row higher (the pets' feet end where the critter's do).
    double snap(double v, double t) =>
        ((v + t) * scale).roundToDouble() / scale - t;
    final ox = snap((size.width - u * 18) / 2 + u, m[12] / scale);
    final oy = snap((size.height - u * 13) / 2 - u, m[13] / scale);
    canvas.save();
    canvas.translate(ox, oy);
    canvas.scale(u);

    void draw(
      List<_Layer> layers, [
      double alpha = 1,
      double dx = 0,
      double dy = 0,
    ]) {
      if (alpha <= 0) return;
      for (final l in layers) {
        _paint.color = l.color.withValues(alpha: l.color.a * alpha);
        if (dx == 0 && dy == 0) {
          canvas.drawPath(l.path, _paint);
        } else {
          canvas.drawPath(l.path.shift(Offset(dx, dy)), _paint);
        }
      }
    }

    if (plate) {
      _paint.color = const Color(0xFF0A0A0A);
      canvas.drawRect(s.bounds.inflate(1), _paint);
    }
    draw(s.behind);
    if (!still) {
      // The glow layer fades in and out over 1.8 s (12 steps).
      const glowSteps = 12;
      final g = (n % glowSteps) / glowSteps;
      draw(s.glow, 0.75 * _wave(g));
    }
    draw(s.under);
    // A ready egg wobbles a cell left and right, then rests (2.4 s).
    const wobble = [0, -1, 0, 1, 0, -1, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0];
    draw(s.body, 1, rocks && !still ? wobble[n % wobble.length].toDouble() : 0);
    draw(s.over);
    if (!still && s.glint != null && n % 32 >= 28 && n % 32 <= 30) {
      draw([s.glint!]);
    }
    if (!still) {
      // Sparks rise 9 cells over 2.4 s (16 steps), each a little later.
      for (final (i, (x, y)) in s.sparks.indexed) {
        final k = (n + (i * 7) ~/ 2) % 16;
        final t = (k + 0.5) / 16;
        final a = t < 0.15 ? t / 0.15 : 1 - (t - 0.15) / 0.85;
        _paint.color = s.spark.withValues(alpha: a);
        canvas.drawRect(
          Rect.fromLTWH(x.toDouble(), y - (9 * k ~/ 16).toDouble(), 1, 1),
          _paint,
        );
      }
      // Snow falls 12 cells over 2.7 s (18 steps).
      for (final (i, (x, y)) in s.snow.indexed) {
        final k = (n + i * 4) % 18;
        final t = (k + 0.5) / 18;
        final a = t < 0.2 ? t / 0.2 : 1 - (t - 0.2) / 0.8;
        _paint.color = const Color(0xFFE8F6FF).withValues(alpha: a);
        canvas.drawRect(
          Rect.fromLTWH(x.toDouble(), y - 3 + (12 * k ~/ 18).toDouble(), 1, 1),
          _paint,
        );
      }
    }
    // Legend's stars twinkle in turn (1.2 s); lit and still with no motion.
    for (final (i, star) in s.stars.indexed) {
      final k = (n + i * 2) % 8;
      draw([star], still ? 1 : 0.15 + 0.85 * _wave(k / 8));
    }
    canvas.restore();
  }

  /// 0 → 1 → 0 over t in 0..1, eased.
  static double _wave(double t) => 0.5 - 0.5 * math.cos(2 * math.pi * t);

  @override
  bool shouldRepaint(_PetPainter old) =>
      old._scene != _scene ||
      old.still != still ||
      old.rocks != rocks ||
      old.plate != plate ||
      old.step != step;
}
