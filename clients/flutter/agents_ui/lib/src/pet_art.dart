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
import 'pet_poses.dart';

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

/// The mystery hat before it shows: a "?" where the hat will be (the egg's
/// floats beside it), from its top-left cell.
const _mystery = ['.qq.', 'q..q', '..q.', '....', '..q.'];
const _mysteryAt = <String, (int, int)>{
  'egg': (13, -3),
  'crack': (13, -3),
  'baby': (6, -1),
};

/// Devil horns, the hat only pets find: two small horns on the head.
const _horns = <String, List<(int, int)>>{
  'kid': [(3, 3), (3, 4), (4, 4), (12, 3), (12, 4), (11, 4)],
  'builder': [(2, 1), (2, 2), (3, 2), (13, 1), (13, 2), (12, 2)],
  'weak': [(2, 3), (2, 4), (3, 4), (13, 3), (13, 4), (12, 4)],
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
  // A hat's near-black.
  'K': Color(0xFF0A0A0A),
  // The mystery hat's "?".
  'q': Color(0xFF8A867C),
  // Props and bubbles: white, the stroke keeps them apart from anything.
  'P': Color(0xFFFFFFFF),
};
const _weakBody = Color(0xFFB0673F);

/// The stroke: a black outline one cell wide round every layer of the pet.
const petStroke = Color(0xFF000000);

/// A power-up aura: flame tongues around the body (one layer still, one
/// glowing in and out), sparks drifting up.
class _Aura {
  const _Aura(this.outer, this.spark, this.heights, this.sparks);
  final Color outer, spark;
  final List<int> heights;
  final int sparks;
}

const _auras = <String, _Aura>{
  'strong': _Aura(Color(0xFFFFE08A), Color(0xFFFFFFFF), [1, 3, 1, 2, 2], 5),
  'mighty': _Aura(Color(0xFF3FA9F5), Color(0xFFFFFFFF), [2, 4, 2, 3, 5, 2], 7),
  'legend': _Aura(Color(0xFFE8A317), Color(0xFFFFF6C8), [
    3,
    5,
    2,
    6,
    3,
    4,
    5,
  ], 10),
};

/// An unhatched egg cracks from this many tokens eaten (or once it is ready).
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
  PetLook._(
    this.art,
    this.pix,
    this.base,
    this.weak,
    this.asleep,
    this.hungry,
    this.aura,
    this.egg,
    this.hatCells,
  );

  /// The pet wears its own hat ([AgentPet.hat], from kid on). [idle]: eyes
  /// shut (only without a [posed] pose: a pose sets its own eyes).
  /// [mystery]: a "?" where the hat will be, until it shows.
  factory PetLook.of(
    AgentPet pet, {
    bool idle = false,
    bool posed = false,
    bool mystery = false,
  }) {
    final stage = pet.stage;
    final mood = pet.mood;
    final egg = stage == 'egg';
    var art = switch (stage) {
      'egg' => pet.ate >= _crackAt || pet.ready ? 'crack' : 'egg',
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
      } else if (asleep || (!posed && (weak || idle))) {
        _shutEyes(pix);
      }
    }
    final base = Map.of(pix);
    final hatPix = <_Cell, String>{};
    if (art != 'baby' && !egg && pet.hat != null) {
      _putHat(hatPix, art, pet.hat!);
    }
    pix.addAll(hatPix);
    if (hatPix.isEmpty && mystery) {
      if (_mysteryAt[art] case (final x0, final y0)) {
        for (final (y, r) in _mystery.indexed) {
          for (var x = 0; x < r.length; x++) {
            if (r[x] == 'q') pix[(x0 + x, y0 + y)] = 'q';
          }
        }
      }
    }
    final aura = !weak && !asleep && _auras.containsKey(stage) ? stage : '';
    return PetLook._(
      art,
      pix,
      base,
      weak,
      asleep,
      mood == 'hungry' && !asleep && !egg,
      aura,
      egg,
      hatPix.keys.toSet(),
    );
  }

  /// egg, crack, baby, kid, builder or weak.
  final String art;

  /// Its cells: (x, y) on the 16-cell grid -> colour key (see [petArt]; h,
  /// H are the hat's colour and shade, K its near-black, m the open mouth,
  /// q the mystery hat's "?").
  final Map<(int, int), String> pix;

  /// [pix] without the hat.
  final Map<(int, int), String> base;
  final bool weak;
  final bool asleep;

  /// Hungry for food (not asleep): it keeps its mouth open.
  final bool hungry;

  /// The cells its hat takes.
  final Set<(int, int)> hatCells;

  /// strong, mighty, legend, or "" for none.
  final String aura;
  final bool egg;

  /// The row under its lowest cell (the "?" doesn't count): where it stands.
  int get bottom {
    var max = 0;
    for (final MapEntry(key: (_, y), value: ch) in pix.entries) {
      if (ch != 'q' && y > max) max = y;
    }
    return max + 1;
  }

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

  /// The deck's hat on an art: its still frame (or [frame], one of its
  /// working ones), moved from the critter's spot to the pet's (the kid
  /// takes a smaller cut). a/b/c/d become h (hat colour), H (shade), c
  /// (highlight), K (near-black).
  static void _putHat(
    Map<_Cell, String> pix,
    String art,
    AgentHat hat, [
    HatFrame? frame,
  ]) {
    const keys = {'a': 'h', 'b': 'H', 'c': 'c', 'd': 'K'};
    if (hat.shape == 'devil horns' && _horns[art] != null) {
      for (final (i, c) in _horns[art]!.indexed) {
        pix[c] = i % 3 == 0 ? 'H' : 'h';
      }
      return;
    }
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
    final f = frame ?? hatArtFor.critterStill;
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

  /// The hat's working frames on an art (each a cell map) and how long each
  /// shows; null when it has none (the kid's small cuts and the devil horns
  /// hold still).
  static (List<Map<_Cell, String>>, int)? _hatFrames(
    String art,
    AgentHat? hat,
  ) {
    final a = hatArt[hat?.shape];
    final place = _hatAt[art];
    if (a == null || place == null) return null;
    if (place.$3 && petSmallHats.containsKey(hat!.shape)) return null;
    return (
      [
        for (final f in a.critter)
          <_Cell, String>{}..also((m) => _putHat(m, art, hat!, f)),
      ],
      a.ms,
    );
  }
}

extension<T> on T {
  T also(void Function(T) f) {
    f(this);
    return this;
  }
}

/// One colour layer: every cell of one colour as one path, in grid cells.
/// [cls] is its motion (see [petMotion]); [group] 1 moves with the body, 2
/// also drops with the squeeze.
class _Layer {
  _Layer(this.color, Iterable<_Cell> cells, {this.cls = '', this.group = 0})
    : path = (Path()..fillType = PathFillType.nonZero) {
    for (final (x, y) in _rows(cells)) {
      path.addRect(
        Rect.fromLTWH(x.$1.toDouble(), y.toDouble(), x.$2.toDouble(), 1),
      );
    }
  }
  _Layer.rect(this.color, Rect r)
    : path = Path()..addRect(r),
      cls = '',
      group = 0;

  final Color color;
  final Path path;
  final String cls;
  final int group;

  /// Cells joined into runs along each row: ((x, n), y).
  static List<((int, int), int)> _rows(Iterable<_Cell> cells) {
    final pts = cells.toSet().toList()
      ..sort((p, q) => p.$2 != q.$2 ? p.$2 - q.$2 : p.$1 - q.$1);
    final out = <((int, int), int)>[];
    for (var i = 0; i < pts.length;) {
      final (x, y) = pts[i];
      var n = 1;
      while (i + n < pts.length &&
          pts[i + n].$2 == y &&
          pts[i + n].$1 == x + n) {
        n++;
      }
      out.add(((x, n), y));
      i += n;
    }
    return out;
  }
}

List<_Layer> _layers(
  Iterable<(int, int, Color)> cells, {
  String cls = '',
  int group = 0,
}) {
  final by = <Color, List<_Cell>>{};
  for (final (x, y, c) in cells) {
    (by[c] ??= []).add((x, y));
  }
  return [
    for (final e in by.entries) _Layer(e.key, e.value, cls: cls, group: group),
  ];
}

/// How a pet is drawn beyond its look: the pose it does, without its bubble,
/// with the mystery "?", as a silhouette, or with its "?" held still.
class PetDraw {
  const PetDraw({
    this.pose,
    this.bubble = true,
    this.mystery = false,
    this.sil,
    this.idle = false,
    this.calm = false,
  });

  /// The critter's pose ([critterPoseOf]); null: a drawing that holds still
  /// (stage previews), no pose.
  final CritterPose? pose;

  /// False: the pose without its bubble.
  final bool bubble;

  /// A "?" where the hat will be, until it shows.
  final bool mystery;

  /// One colour, no aura, stroke or motion: a stage not reached yet.
  final Color? sil;

  /// Eyes shut, without a pose.
  final bool idle;

  /// The bubble holds still (a "?" that doesn't blink: where the page
  /// already calls you).
  final bool calm;

  @override
  bool operator ==(Object other) =>
      other is PetDraw &&
      other.pose == pose &&
      other.bubble == bubble &&
      other.mystery == mystery &&
      other.sil == sil &&
      other.idle == idle &&
      other.calm == calm;

  @override
  int get hashCode => Object.hash(pose, bubble, mystery, sil, idle, calm);
}

/// Everything drawn for one look, built once and kept: the layers back to
/// front (flames, strokes, the pet, its bubble and props, sparks and stars,
/// the ice), each with its motion class.
class _PetScene {
  _PetScene(AgentPet pet, AgentHat? spots, PetDraw d) {
    final pose = d.sil != null ? null : d.pose;
    look = PetLook.of(
      pet,
      idle: d.idle,
      posed: pose != null,
      mystery: d.mystery,
    );
    final hat = pet.hat;
    final (main, shade) = hatColor(
      hatColors.contains(hat?.color) ? hat!.color : 'teal',
    );
    final spot = hatColors.contains(spots?.color)
        ? spots!.color
        : hatColors.contains(hat?.color)
        ? hat!.color
        : 'teal';
    final col = {..._colours, 's': hatColor(spot).$1, 'h': main, 'H': shade};
    if (look.weak) col['b'] = col['d'] = _weakBody;
    final glow =
        look.aura.isNotEmpty && d.sil == null && pose != CritterPose.exited;
    final keepOut = {
      for (final e in look.pix.entries)
        if (e.value == 'q') e.key,
    };
    final p = pose == null
        ? null
        : posePet(
            PetPoseInput(
              art: look.art,
              pix: look.pix,
              egg: look.egg,
              asleep: look.asleep,
              hungry: look.hungry,
              hatCells: look.hatCells,
            ),
            pose,
            bubble: d.bubble,
            keepOut: keepOut,
          );
    final pix = p?.pix ?? Map.of(look.pix);
    // A working hat cycles through its frames: what they share is drawn
    // with the body, the rest one layer per frame.
    final hatParts = <PetPart>[];
    final hf = p != null && critterPoseWorking(pose!) && !look.asleep
        ? PetLook._hatFrames(look.art, hat)
        : null;
    if (hf != null) {
      final (frames, ms) = hf;
      for (final c in look.hatCells) {
        if (look.base[c] case final ch?) {
          pix[c] = ch;
        } else {
          pix.remove(c);
        }
      }
      final keys = {for (final m in frames) ...m.keys};
      for (final c in keys) {
        final ch = frames[0][c];
        if (ch != null && frames.every((m) => m[c] == ch)) pix[c] = ch;
      }
      for (final (i, m) in frames.indexed) {
        hatParts.add((
          [
            for (final MapEntry(key: (x, y), value: ch) in m.entries)
              if (!frames.every((o) => o[(x, y)] == ch)) (x, y, ch),
          ],
          'hf-${frames.length}-$ms-$i${i > 0 ? ' hfo' : ''}',
        ));
      }
    }
    // The hat of a pet whose agent has exited dims to its shade.
    if (p?.shade ?? false) {
      col['h'] = col['H']!;
      col['H'] = Color.from(
        alpha: 1,
        red: (col['H']!.r * 0.72 * 255).roundToDouble() / 255,
        green: (col['H']!.g * 0.72 * 255).roundToDouble() / 255,
        blue: (col['H']!.b * 0.72 * 255).roundToDouble() / 255,
      );
    }
    if (d.sil case final sil?) {
      for (final k in col.keys.toList()) {
        col[k] = sil;
      }
    }
    Color cell(String ch) => col[ch] ?? col['b']!;
    grp = p?.grp ?? '';
    calm = d.calm;
    // The body and what moves with it, in the web's paint order: the body
    // cells that don't drop (all of them when nothing is squeezed), then
    // the ones that drop when it is squeezed (group 2, over the rows they
    // drop onto), then the layers that move on their own (glancing and
    // scanning eyes, the chomping mouth, arms, the flag, hat frames), so
    // nothing of the body covers the face.
    final top = p?.top;
    squeeze = p != null && p.squeeze == 1 ? 'psq1' : 'psq';
    final bodyParts = <(List<(int, int, String)>, String, int)>[
      (
        [
          for (final MapEntry(key: (x, y), value: ch) in pix.entries)
            if (top == null || !top.contains((x, y))) (x, y, ch),
        ],
        '',
        1,
      ),
      if (top != null)
        (
          [
            for (final MapEntry(key: (x, y), value: ch) in pix.entries)
              if (top.contains((x, y))) (x, y, ch),
          ],
          '',
          2,
        ),
      for (final (cells, cls) in [...?p?.inner, ...hatParts])
        (cells, cls, top != null ? 2 : 1),
    ];
    final outerParts = [
      for (final (c, cls) in [...?p?.outer]) (c, cls, 0),
    ];
    filled = look.pix.keys.toSet();
    if (d.sil == null) {
      // The stroke: each layer's cells grown by one (4 neighbours), in
      // black, one path per motion class, under the whole pet.
      for (final parts in [bodyParts, outerParts]) {
        final grown = <(String, int), Set<_Cell>>{};
        for (final (cells, cls, g) in parts) {
          final set = grown[(cls, g)] ??= {};
          for (final (x, y, _) in cells) {
            set
              ..add((x, y))
              ..add((x + 1, y))
              ..add((x - 1, y))
              ..add((x, y + 1))
              ..add((x, y - 1));
          }
        }
        for (final MapEntry(key: (cls, g), value: set) in grown.entries) {
          if (set.isNotEmpty) {
            strokes.add(_Layer(petStroke, set, cls: cls, group: g));
          }
        }
      }
    }
    body = [
      for (final (cells, cls, g) in bodyParts)
        ..._layers(
          [for (final (x, y, ch) in cells) (x, y, cell(ch))],
          cls: cls,
          group: g,
        ),
    ];
    outer = [
      for (final (cells, cls, _) in outerParts)
        ..._layers([
          for (final (x, y, ch) in cells) (x, y, cell(ch)),
        ], cls: cls),
    ];
    classes = {
      for (final l in [...body, ...outer, ...strokes]) l.cls,
      grp,
      if (top != null) squeeze,
    }.toList();
    final xs = filled.map((c) => c.$1), ys = filled.map((c) => c.$2);
    bounds = Rect.fromLTRB(
      xs.reduce(math.min).toDouble(),
      ys.reduce(math.min).toDouble(),
      xs.reduce(math.max) + 1.0,
      ys.reduce(math.max) + 1.0,
    );
    ready = pet.isEgg && pet.ready && d.sil == null;
    if (look.asleep && d.sil == null) _ice(filled);
    if (glow) _aura(filled, _auras[look.aura]!, look.aura == 'legend');
  }

  late final PetLook look;
  late final Set<_Cell> filled;

  /// The box the body (and its ice) takes, in cells.
  late Rect bounds;

  /// The flame cells, both layers (for tests).
  Set<_Cell> flames = {};

  /// The body's motion, and the distinct motion classes of every layer.
  late final String grp;
  late final List<String> classes;
  late final bool calm;

  /// The squeeze's motion class: psq, or psq1 for a pet that drops 1 row.
  late final String squeeze;

  /// A ready egg: it wobbles.
  late final bool ready;

  List<_Layer> behind = const [];
  List<_Layer> glow = const [];
  List<_Layer> under = const [];
  final strokes = <_Layer>[];
  late final List<_Layer> body;
  late final List<_Layer> outer;
  List<_Layer> over = const [];
  _Layer? glint;
  List<(int, int)> sparks = const [];
  Color spark = const Color(0x00000000);

  /// A legend's stars: a dot, and its 4 arms (on for half of each beat).
  List<(_Layer, _Layer)> stars = const [];
  List<(int, int)> snow = const [];

  /// Has parts that move on the 150 ms aura and ice steps.
  bool get glows => glow.isNotEmpty || sparks.isNotEmpty || snow.isNotEmpty;

  /// Has parts that move at all.
  bool get moves => glows || ready || classes.any((c) => c.isNotEmpty);

  /// Every cell anything of it can reach, at any moment of its motion: the
  /// stroke, flames, sparks rising, stars, bubbles, props, the ice, a hat's
  /// frames, hops and wobbles. Fitting this box keeps all of it inside.
  late final Rect extent = _extent();

  Rect _extent() {
    var r = Rect.zero;
    var first = true;
    void add(Rect b) {
      if (b.isEmpty) return;
      r = first ? b : r.expandToInclude(b);
      first = false;
    }

    for (final l in [...under, ...over]) {
      add(l.path.getBounds());
    }
    // The aura moves with the pet.
    final aura = [
      for (final l in [...behind, ...glow]) l.path.getBounds(),
      for (final (d, arms) in stars) ...[
        d.path.getBounds(),
        arms.path.getBounds(),
      ],
      for (final (x, y) in sparks)
        Rect.fromLTRB(x.toDouble(), y - 8.0, x + 1.0, y + 1.0),
    ];
    for (final o in {
      for (var t = 0; t < 4800; t += 50) motion(grp, t, false),
    }) {
      for (final b in aura) {
        add(b.shift(Offset(o.$2.toDouble(), o.$3.toDouble())));
      }
    }
    for (final (x, y) in snow) {
      add(Rect.fromLTRB(x.toDouble(), y - 3.0, x + 1.0, y + 9.0));
    }
    final posed = [...strokes, ...body, ...outer];
    final squeezed = classes.contains(squeeze);
    for (var t = 0; t < 4800; t += 50) {
      final (_, gx, gy) = motion(grp, t, false);
      final (_, qx, qy) = motion(squeeze, t, false);
      for (final l in posed) {
        final (a, dx, dy) = motion(l.cls, t, false);
        if (a <= 0) continue;
        final x =
            dx + (l.group > 0 ? gx : 0) + (l.group == 2 && squeezed ? qx : 0);
        final y =
            dy + (l.group > 0 ? gy : 0) + (l.group == 2 && squeezed ? qy : 0);
        add(l.path.getBounds().shift(Offset(x.toDouble(), y.toDouble())));
      }
    }
    if (ready) r = Rect.fromLTRB(r.left - 1, r.top, r.right + 1, r.bottom);
    return r;
  }

  /// The motion of [cls] at [ms].
  (double, int, int) motion(String cls, int ms, bool still) {
    if (calm && cls.contains('blink')) return (1, 0, 0);
    return petMotion(cls, ms, still: still);
  }

  /// A number that changes whenever what is drawn at [ms] changes.
  int signature(int ms, bool still) => Object.hashAll([
    for (final c in classes) motion(c, ms, still),
    if (ready && !still) petReadyWobble(ms),
    if (glows && !still) ...[
      petGlowOn(ms),
      for (var i = 0; i < sparks.length; i++) petSparkAt(i, ms),
      for (var i = 0; i < stars.length; i++) petStarArms(i, ms),
      for (var i = 0; i < snow.length; i++) petSnowAt(i, ms),
      (ms ~/ 150) % 32,
    ],
  ]);

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
    // The flames start a cell out: the stroke fills the cell next to the
    // body.
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
      flames.addAll(fl);
      // Opaque cells only: nothing see-through next to the body.
      frames.add([_Layer(a.outer, fl)]);
    }
    behind = frames[0];
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
          (
            _Layer(const Color(0xFFFFF6C8), [(x, y)]),
            _Layer(const Color(0xFFFFF6C8), [
              for (final (dx, dy) in const [(-1, 0), (1, 0), (0, -1), (0, 1)])
                (x + dx, y + dy),
            ]),
          ),
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

  static final _cache = <Object, _PetScene>{};

  static _PetScene of(AgentPet pet, AgentHat? spots, PetDraw d) {
    final key = (
      pet.stage,
      pet.mood,
      pet.isEgg && (pet.ate >= _crackAt || pet.ready),
      pet.ready,
      pet.hat?.shape,
      pet.hat?.color,
      pet.isEgg ? spots?.color : null,
      d,
    );
    final hit = _cache[key];
    if (hit != null) return hit;
    if (_cache.length > 96) _cache.clear();
    return _cache[key] = _PetScene(pet, spots, d);
  }
}

/// The flame cells of [pet]'s aura (empty without an aura).
@visibleForTesting
Set<(int, int)> petFlameCells(AgentPet pet) =>
    _PetScene.of(pet, null, const PetDraw()).flames;

/// The cells drawn for [pet] doing [pose] at [ms] on the shared clock, by
/// colour: what a frame of the web's SVG shows (for tests). The stroke is
/// [petStroke]; see-through layers (the ice) are left out.
@visibleForTesting
Map<(int, int), Color> petCellsAt(
  AgentPet pet,
  PetDraw draw,
  int ms, {
  AgentHat? spots,
  bool still = false,
}) {
  final s = _PetScene.of(pet, spots, draw);
  final out = <(int, int), Color>{};
  final (_, gx, gy) = s.motion(s.grp, ms, still);
  final (_, qx, qy) = s.motion(s.squeeze, ms, still);
  for (final l in [...s.strokes, ...s.body, ...s.outer]) {
    final (a, dx, dy) = s.motion(l.cls, ms, still);
    if (a <= 0) continue;
    final ox =
        dx +
        (l.group > 0 ? gx : 0) +
        (l.group == 2 && s.classes.contains(s.squeeze) ? qx : 0);
    final oy =
        dy +
        (l.group > 0 ? gy : 0) +
        (l.group == 2 && s.classes.contains(s.squeeze) ? qy : 0);
    final b = l.path.getBounds();
    for (var y = b.top.round(); y < b.bottom.round(); y++) {
      for (var x = b.left.round(); x < b.right.round(); x++) {
        if (l.path.contains(Offset(x + 0.5, y + 0.5))) {
          out[(x + ox, y + oy)] = l.color;
        }
      }
    }
  }
  return out;
}

/// Every cell [pet] can reach drawn as [draw], over all of its motion
/// (for tests: what has to fit a tile).
@visibleForTesting
Rect petExtent(AgentPet pet, PetDraw draw) =>
    _PetScene.of(pet, null, draw).extent;

/// One motion step, in [CritterClock] ticks (50 ms): the pose classes are
/// checked every tick, but a pet repaints only when what it shows changes.
const _msPerTick = 50;

/// An agent's pet. In the critter's box by default ([width] wide, height =
/// width × 13/18, at the critter's scale: centred where the critter's body
/// would be); with [cell], standing on the bottom edge of a [width] ×
/// [height] box, centred, at [cell] logical pixels per pet pixel. Either way
/// the cells snap to whole device pixels, and auras, the stroke, bubbles,
/// props and the ice reach past the box. Moves when a [CritterScope] is
/// above it and the platform allows motion.
class PetSprite extends StatefulWidget {
  const PetSprite({
    super.key,
    required this.pet,
    required this.width,
    this.height,
    this.cell,
    this.hat,
    this.pose,
    this.bubble = true,
    this.mystery = false,
    this.sil,
    this.idle = false,
    this.calm = false,
    this.celebrateUntil,
    this.afterCelebrate = CritterPose.idle,
    this.fit = false,
  });

  final AgentPet pet;
  final double width;

  /// Keep all of it inside the [width] × [height] box: at most [cell] per
  /// pet pixel, smaller (whole device pixels) when its glow, hat or bubble
  /// would leave the box, standing on the box's bottom edge.
  final bool fit;

  /// With [cell]: the box's height (the ground line is its bottom edge).
  final double? height;

  /// Logical pixels per pet pixel; null: fit the critter's box.
  final double? cell;

  /// Its agent's hat: only an egg's spots take its colour (the pet wears
  /// its own, [AgentPet.hat]).
  final AgentHat? hat;

  /// The critter's pose for its agent ([critterPoseOf]); null holds still.
  final CritterPose? pose;

  /// False: the pose without its bubble.
  final bool bubble;

  /// A "?" where its hat will show, until it does.
  final bool mystery;

  /// One colour, no aura, stroke or motion: a stage not reached yet.
  final Color? sil;

  /// Without a pose: eyes shut.
  final bool idle;

  /// The "?" holds still.
  final bool calm;

  /// For [CritterPose.celebrate]: when the hop ends and [afterCelebrate]
  /// takes over (checked on every clock tick).
  final DateTime? celebrateUntil;
  final CritterPose afterCelebrate;

  @override
  State<PetSprite> createState() => _PetSpriteState();
}

class _PetSpriteState extends State<PetSprite> {
  final _ms = ValueNotifier<int>(0);
  CritterClock? _clock;
  bool _still = false;
  int _sig = 0;
  CritterPose? _pose;

  @override
  void initState() {
    super.initState();
    _pose = widget.pose;
  }

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

  @override
  void didUpdateWidget(PetSprite old) {
    super.didUpdateWidget(old);
    _pose = widget.pose;
    _onTick();
  }

  PetDraw get _draw => PetDraw(
    pose: _pose,
    bubble: widget.bubble,
    mystery: widget.mystery,
    sil: widget.sil,
    idle: widget.idle,
    calm: widget.calm,
  );

  void _onTick() {
    var pose = widget.pose;
    final until = widget.celebrateUntil;
    if (pose == CritterPose.celebrate &&
        until != null &&
        DateTime.now().isAfter(until)) {
      pose = widget.afterCelebrate;
    }
    if (pose != _pose) {
      _pose = pose;
      if (mounted) setState(() {});
    }
    final ms = (_clock?.value ?? 0) * _msPerTick;
    final sig = _PetScene.of(
      widget.pet,
      widget.hat,
      _draw,
    ).signature(ms, _still);
    if (sig != _sig) {
      _sig = sig;
      _ms.value = ms;
    }
  }

  @override
  void dispose() {
    _clock?.removeListener(_onTick);
    _ms.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final scene = _PetScene.of(widget.pet, widget.hat, _draw);
    final cell = widget.cell;
    return RepaintBoundary(
      child: CustomPaint(
        size: Size(
          widget.width,
          cell == null ? widget.width * 13 / 18 : widget.height ?? widget.width,
        ),
        painter: _PetPainter(
          scene: scene,
          hatch: PetHatchScope.of(context),
          still: _still,
          cell: cell,
          fit: widget.fit,
          ms: _still || !scene.moves ? null : _ms,
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
    this.cell,
    this.fit = false,
    this.hatch,
    this.ms,
  }) : _scene = scene,
       super(repaint: ms);

  final _PetScene _scene;

  /// Reduce motion: the first frame of each pair; no glow, sparks, snow or
  /// glint; stars stay lit.
  final bool still;
  final double? cell;
  final bool fit;

  /// A hatch or a pop-in in progress (see [PetHatchScope]).
  final PetHatchStep? hatch;
  final ValueListenable<int>? ms;

  static final _paint = Paint()..isAntiAlias = false;

  @override
  void paint(Canvas canvas, Size size) {
    final s = _scene;
    final t = ms?.value ?? 0;
    final m = canvas.getTransform();
    final scale = m[0] > 0 ? m[0] : 1.0;
    double snap(double v, double tr) =>
        ((v + tr) * scale).roundToDouble() / scale - tr;
    final double u, ox, oy;
    if (fit) {
      // Everything it can reach fits the box: the biggest whole number of
      // device pixels per cell up to [cell], standing on the bottom edge
      // (its stroke just above it), centred, pushed in from the sides.
      final e = s.extent;
      final most = ((cell ?? 5) * scale).roundToDouble();
      final ud = math.max(
        1.0,
        math.min(
          most,
          math.min(
            (size.width * scale / e.width).floorToDouble(),
            (size.height * scale / e.height).floorToDouble(),
          ),
        ),
      );
      u = ud / scale;
      var x = size.width / 2 - 8 * u;
      if (x + e.left * u < 0) x = -e.left * u;
      if (x + e.right * u > size.width) x = size.width - e.right * u;
      ox = snap(x, m[12] / scale);
      oy = snap(size.height - e.bottom * u, m[13] / scale);
    } else if (cell case final c?) {
      // On a ground line: the art's lowest row on the box's bottom edge,
      // its middle column on the box's centre.
      u = math.max(1.0, (c * scale).roundToDouble()) / scale;
      ox = snap(size.width / 2 - 8 * u, m[12] / scale);
      oy = snap(size.height - s.look.bottom * u, m[13] / scale);
    } else {
      // Whole device pixels per cell, as many as fit the critter's 18
      // cells; the 16-cell grid sits one cell in from the critter box's
      // left, and a row higher (the pets' feet end where the critter's do).
      final ud = (size.width * scale / 18).floorToDouble().clamp(
        1.0,
        double.infinity,
      );
      u = ud / scale;
      ox = snap((size.width - u * 18) / 2 + u, m[12] / scale);
      oy = snap((size.height - u * 13) / 2 - u, m[13] / scale);
    }
    canvas.save();
    canvas.translate(ox, oy);
    canvas.scale(u);
    // A ready egg wobbles, all of it.
    if (s.ready && !still) canvas.translate(petReadyWobble(t).toDouble(), 0);
    // Hatching shakes and pops in by whole cells, all of it (aura too).
    final h = hatch;
    if (h != null) {
      if (!h.shown) {
        canvas.restore();
        return;
      }
      canvas.translate(h.dx.toDouble(), h.dy.toDouble());
    }
    final (_, gx, gy) = s.motion(s.grp, t, still);
    final (_, qx, qy) = s.motion(s.squeeze, t, still);
    final squeezed = s.classes.contains(s.squeeze);

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

    void posed(List<_Layer> layers) {
      for (final l in layers) {
        final (a, dx, dy) = s.motion(l.cls, t, still);
        if (a <= 0) continue;
        final x =
            dx + (l.group > 0 ? gx : 0) + (l.group == 2 && squeezed ? qx : 0);
        final y =
            dy + (l.group > 0 ? gy : 0) + (l.group == 2 && squeezed ? qy : 0);
        draw([l], a, x.toDouble(), y.toDouble());
      }
    }

    // The aura moves with the pet (its jog, hop or rock).
    final ax = gx.toDouble(), ay = gy.toDouble();
    draw(s.behind, 1, ax, ay);
    // The second flame layer: off, then on, half of 1.8 s each.
    if (!still && petGlowOn(t)) draw(s.glow, 1, ax, ay);
    draw(s.under);
    posed(s.strokes);
    posed(s.body);
    // Hatching: the egg's cells flash white.
    if (h != null && h.white) {
      _paint.color = const Color(0xFFFFFFFF);
      for (final (x, y) in s.look.pix.keys) {
        canvas.drawRect(Rect.fromLTWH(x + ax, y + ay, 1, 1), _paint);
      }
    }
    posed(s.outer);
    if (!still) {
      // Sparks rise a cell a step (9 steps over 2.4 s), shown steps 1 to 7.
      _paint.color = s.spark;
      for (final (i, (x, y)) in s.sparks.indexed) {
        final (k, shown) = petSparkAt(i, t);
        if (!shown) continue;
        canvas.drawRect(Rect.fromLTWH(x + ax, y - k + ay, 1, 1), _paint);
      }
    }
    // A legend's stars: a dot, then a cross for half of 1.1 s, in turn;
    // crosses, still, with no motion.
    for (final (i, (dot, arms)) in s.stars.indexed) {
      draw([dot], 1, ax, ay);
      if (still || petStarArms(i, t)) draw([arms], 1, ax, ay);
    }
    // The ice over everything.
    draw(s.over);
    if (!still &&
        s.glint != null &&
        (t ~/ 150) % 32 >= 28 &&
        (t ~/ 150) % 32 <= 30) {
      draw([s.glint!]);
    }
    if (!still) {
      // Snow falls a cell a step from 3 above (12 steps over 2.6 s), shown
      // from 17 % to 85 %.
      _paint.color = const Color(0xFFE8F6FF);
      for (final (i, (x, y)) in s.snow.indexed) {
        final (k, shown) = petSnowAt(i, t);
        if (!shown) continue;
        canvas.drawRect(Rect.fromLTWH(x.toDouble(), y - 3.0 + k, 1, 1), _paint);
      }
    }
    canvas.restore();
  }

  @override
  bool shouldRepaint(_PetPainter old) =>
      old._scene != _scene ||
      old.still != still ||
      old.cell != cell ||
      old.fit != fit ||
      old.hatch != hatch ||
      old.ms != ms;
}

/// The aura's steps, as the web's classes run them (docs/pet-poses.md,
/// "Timings"): every one moves whole cells or turns on and off.

/// pfc: the second flame layer, off then on, half of 1.8 s each.
bool petGlowOn(int ms) => ms % 1800 >= 900;

/// prs: spark [i] (0.53 s later each) rises a cell a step, 9 steps over
/// 2.4 s, and shows from 12 % to 78 % (steps 1 to 7): its rise and whether
/// it shows.
(int, bool) petSparkAt(int i, int ms) {
  final t = (ms + 530 * i) % 2400 / 2400;
  return ((t * 9).floor(), t >= 0.12 && t < 0.78);
}

/// ptw: star [i] (0.27 s later each) shows its 4 arms for the second half
/// of 1.1 s.
bool petStarArms(int i, int ms) => (ms + 270 * i) % 1100 >= 550;

/// pfall: snowflake [i] (0.65 s later each) falls a cell a step from 3
/// above, 12 steps over 2.6 s, shown from 17 % to 85 %.
(int, bool) petSnowAt(int i, int ms) {
  final t = (ms + 650 * i) % 2600 / 2600;
  return ((t * 12).floor(), t >= 0.17 && t < 0.85);
}

/// One step of a hatch or a pop-in: where the whole pet is, in cells,
/// whether it shows, and whether the egg flashes white.
@immutable
class PetHatchStep {
  const PetHatchStep({
    this.dx = 0,
    this.dy = 0,
    this.shown = true,
    this.white = false,
  });

  /// phx (1.4 s): -1, +1, -1, +1, -2, +2 cells in x, a tenth each, then
  /// still; white from 70 %; gone from 85 %.
  factory PetHatchStep.hatch(double t) {
    const xs = [0, -1, 1, -1, 1, -2, 2];
    final i = (t * 10).floor();
    return PetHatchStep(
      dx: i < xs.length ? xs[i] : 0,
      shown: t < 0.85,
      white: t >= 0.7,
    );
  }

  /// pborn (0.6 s): +2 cells in y hidden, +2 shown, -1, then in place, a
  /// quarter each.
  factory PetHatchStep.born(double t) => switch ((t * 4).floor()) {
    0 => const PetHatchStep(dy: 2, shown: false),
    1 => const PetHatchStep(dy: 2),
    2 => const PetHatchStep(dy: -1),
    _ => const PetHatchStep(),
  };

  final int dx, dy;
  final bool shown, white;

  @override
  bool operator ==(Object other) =>
      other is PetHatchStep &&
      other.dx == dx &&
      other.dy == dy &&
      other.shown == shown &&
      other.white == white;

  @override
  int get hashCode => Object.hash(dx, dy, shown, white);
}

/// Tells the pets under it they are hatching or popping in ([PetHatcher]).
class PetHatchScope extends InheritedWidget {
  const PetHatchScope({super.key, required this.step, required super.child});

  final PetHatchStep? step;

  static PetHatchStep? of(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<PetHatchScope>()?.step;

  @override
  bool updateShouldNotify(PetHatchScope old) => old.step != step;
}
