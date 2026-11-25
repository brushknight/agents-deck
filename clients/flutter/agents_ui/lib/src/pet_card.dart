/// Small pieces of an agent's pet on its page: its hearts.
library;

import 'package:flutter/widgets.dart';

import 'agents_model.dart';

/// A pet's hearts, full and empty, as pixel hearts.
class PetHearts extends StatelessWidget {
  const PetHearts({
    super.key,
    required this.hearts,
    this.size = 3,
    this.onOrange = false,
  });

  final int hearts;

  /// Pixels per heart cell (a heart is 7 × 6 cells).
  final double size;

  /// On solid orange: near-black hearts (empty ones at 25%), as red would
  /// vanish there.
  final bool onOrange;

  @override
  Widget build(BuildContext context) => Row(
    mainAxisSize: MainAxisSize.min,
    children: [
      for (var i = 0; i < petMaxHearts; i++) ...[
        if (i > 0) SizedBox(width: size + 1),
        CustomPaint(
          size: Size(7 * size, 6 * size),
          painter: _HeartPainter(full: i < hearts, onOrange: onOrange),
        ),
      ],
    ],
  );
}

class _HeartPainter extends CustomPainter {
  _HeartPainter({required this.full, this.onOrange = false});

  final bool full;
  final bool onOrange;

  static const _rows = [
    '.rr.rr.',
    'rrrrrrr',
    'rrrrrrr',
    '.rrrrr.',
    '..rrr..',
    '...r...',
  ];

  @override
  void paint(Canvas canvas, Size size) {
    final u = size.width / 7;
    final p = Paint()
      ..isAntiAlias = false
      ..color = onOrange
          ? (full ? const Color(0xFF0A0A0A) : const Color(0x400A0A0A))
          : (full ? const Color(0xFFE8452B) : const Color(0xFF3B3A36));
    for (final (y, row) in _rows.indexed) {
      for (var x = 0; x < row.length; x++) {
        if (row[x] == 'r') {
          canvas.drawRect(Rect.fromLTWH(x * u, y * u, u, u), p);
        }
      }
    }
  }

  @override
  bool shouldRepaint(_HeartPainter old) =>
      old.full != full || old.onOrange != onOrange;
}
