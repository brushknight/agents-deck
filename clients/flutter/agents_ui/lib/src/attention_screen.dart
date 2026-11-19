import 'package:flutter/widgets.dart';

import 'agents_app.dart' show AgentsEmptyTile;
import 'agents_model.dart';
import 'attention.dart';
import 'widgets.dart';

/// The "Needs you" screen: one page that looks exactly like a page of the
/// deck (same grid, same tiles, same empty cells), holding only the agents
/// that are working or asking, each in its spot ([AttentionSpots]). Only its
/// mark in the page bars tells it apart.
class AttentionScreen extends StatelessWidget {
  const AttentionScreen({
    super.key,
    required this.spots,
    required this.state,
    required this.tile,
  });

  final AttentionSpots spots;
  final AgentsState state;

  /// The deck's own tile for an agent (same look, same tap).
  final Widget Function(Agent agent) tile;

  @override
  Widget build(BuildContext context) {
    final byId = {for (final a in state.agents) a.id: a};
    return KeyedSubtree(
      key: const Key('attention-screen'),
      child: DeckGrid(
        square: false,
        cells: [
          for (final id in spots.spots)
            if (byId[id] case final a?)
              tile(a)
            else
              const AgentsEmptyTile(key: Key('attention-spot-empty')),
        ],
      ),
    );
  }
}
