/// An agent's pet on its card: the pet, its name, what it is, its level
/// with how far it is to the next one, and its hearts.
library;

import 'package:flutter/widgets.dart';

import 'agents_model.dart';
import 'pet_art.dart';
import 'theme.dart';
import 'widgets.dart';

/// Its pet's eyes are shut: the agent is idle (or has ended).
bool petAgentIdle(Agent a) =>
    a.status == AgentStatus.idle || a.status == AgentStatus.exited;

/// The pet on an agent's card, beside its critter (the critter's height):
/// the pet, its name, what it is and its level; a bar to the next level
/// (from where this level started) and its hearts.
class PetSummary extends StatelessWidget {
  const PetSummary({
    super.key,
    required this.pet,
    this.hat,
    this.idle = false,
    this.ink = DeckHud.ink,
  });

  final AgentPet pet;

  /// Its agent's hat (worn when the pet has none of its own).
  final AgentHat? hat;

  /// Its agent is idle: eyes shut.
  final bool idle;

  /// Text and bar colour (the card's ink).
  final Color ink;

  @override
  Widget build(BuildContext context) {
    final dim = ink == DeckHud.ink ? DeckHud.dim : ink.withValues(alpha: 0.7);
    final pct = (pet.progress * 100).floor();
    final next = pet.isEgg ? 'to hatching' : 'to level ${pet.level + 1}';
    return Row(
      key: const Key('agent-pet'),
      crossAxisAlignment: CrossAxisAlignment.end,
      children: [
        PetSprite(pet: pet, hat: hat, idle: idle, width: 54),
        const SizedBox(width: 10),
        Expanded(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                crossAxisAlignment: CrossAxisAlignment.baseline,
                textBaseline: TextBaseline.alphabetic,
                children: [
                  Flexible(
                    child: Text(
                      pet.name.isEmpty ? 'An egg' : pet.name,
                      key: const Key('agent-pet-name'),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: DeckHud.rm(15, ink, weight: 700),
                    ),
                  ),
                  const SizedBox(width: 10),
                  Text(
                    pet.isEgg ? pet.kind : '${pet.kind} · level ${pet.level}',
                    key: const Key('agent-pet-kind'),
                    maxLines: 1,
                    style: DeckHud.mono(size: 13, color: dim),
                  ),
                ],
              ),
              const SizedBox(height: 3),
              Row(
                children: [
                  SizedBox(
                    width: 120,
                    child: TickBar(value: pet.progress, color: ink, height: 9),
                  ),
                  const SizedBox(width: 8),
                  Flexible(
                    child: Text(
                      '$pct% $next',
                      key: const Key('agent-pet-progress'),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: DeckHud.mono(size: 11, color: dim),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Semantics(
                    label: '${pet.hearts} of $petMaxHearts hearts',
                    child: PetHearts(hearts: pet.hearts, size: 2),
                  ),
                ],
              ),
            ],
          ),
        ),
      ],
    );
  }
}

/// A pet's hearts, full and empty, as pixel hearts.
class PetHearts extends StatelessWidget {
  const PetHearts({super.key, required this.hearts, this.size = 3});

  final int hearts;

  /// Pixels per heart cell (a heart is 7 × 6 cells).
  final double size;

  @override
  Widget build(BuildContext context) => Row(
    mainAxisSize: MainAxisSize.min,
    children: [
      for (var i = 0; i < petMaxHearts; i++) ...[
        if (i > 0) SizedBox(width: size + 1),
        CustomPaint(
          size: Size(7 * size, 6 * size),
          painter: _HeartPainter(full: i < hearts),
        ),
      ],
    ],
  );
}

class _HeartPainter extends CustomPainter {
  _HeartPainter({required this.full});

  final bool full;

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
      ..color = full ? const Color(0xFFE8452B) : const Color(0xFF3B3A36);
    for (final (y, row) in _rows.indexed) {
      for (var x = 0; x < row.length; x++) {
        if (row[x] == 'r') {
          canvas.drawRect(Rect.fromLTWH(x * u, y * u, u, u), p);
        }
      }
    }
  }

  @override
  bool shouldRepaint(_HeartPainter old) => old.full != full;
}
