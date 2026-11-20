import 'package:flutter/widgets.dart';

import 'agents_app.dart' show AgentsEmptyTile;
import 'agents_model.dart';
import 'attention.dart';
import 'widgets.dart';

/// The "Needs you" screen: one page that looks exactly like a page of the
/// deck (same grid, same tiles, same empty cells), holding only the agents
/// that need you, each in the spot the service gave it ([NeedsYou]): spot n
/// in cell n of the 4 x 4 grid. Only its mark in the page bars tells it apart.
class AttentionScreen extends StatelessWidget {
  const AttentionScreen({
    super.key,
    required this.needsYou,
    required this.tile,
  });

  final NeedsYou needsYou;

  /// The deck's own tile for an agent (same look, same tap).
  final Widget Function(Agent agent) tile;

  @override
  Widget build(BuildContext context) {
    return KeyedSubtree(
      key: const Key('attention-screen'),
      child: DeckGrid(
        square: false,
        cells: [
          for (final a in needsYou.spots)
            if (a != null)
              tile(a)
            else
              const AgentsEmptyTile(key: Key('attention-spot-empty')),
        ],
      ),
    );
  }
}
