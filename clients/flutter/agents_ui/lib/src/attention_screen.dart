import 'package:flutter/widgets.dart';

import 'agents_model.dart';
import 'agents_style.dart';
import 'attention.dart';
import 'theme.dart';
import 'widgets.dart';

/// The "Needs you" screen: one page, the deck's own grid size, with the
/// agents that are working or asking in their spots ([AttentionSpots]).
/// Empty spots stay as faint outlines so nobody seems to move.
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

  static const headerHeight = 40.0;

  @override
  Widget build(BuildContext context) {
    final byId = {for (final a in state.agents) a.id: a};
    var working = 0, asking = 0;
    final cells = <Widget>[
      for (final id in spots.spots)
        if (byId[id] case final a?)
          tile(a)
        else
          const _EmptySpot(key: Key('attention-spot-empty')),
    ];
    for (final id in spots.spots) {
      final a = byId[id];
      if (a == null) continue;
      if (a.status == AgentStatus.waiting) {
        asking++;
      } else if (wantsAttention(a.status)) {
        working++;
      }
    }
    final more = spots.waiting;
    final summary = [
      if (asking > 0) '$asking asking',
      if (working > 0) '$working working',
    ].join(' · ');
    final empty = spots.spots.every((id) => byId[id] == null);
    return Container(
      key: const Key('attention-screen'),
      decoration: BoxDecoration(
        color: attentionTint,
        border: Border.all(color: attentionFrame, width: 2),
      ),
      padding: const EdgeInsets.fromLTRB(6, 0, 6, 6),
      child: Column(
        children: [
          SizedBox(
            height: headerHeight,
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 6),
              child: Row(
                children: [
                  Text(
                    'Needs you',
                    style: DeckHud.rm(19, DeckHud.accent, weight: 700),
                  ),
                  const SizedBox(width: 14),
                  Expanded(
                    child: Text(
                      summary,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: DeckHud.mono(size: 14, color: DeckHud.dim),
                    ),
                  ),
                  if (more > 0)
                    Text(
                      '+$more more',
                      key: const Key('attention-more'),
                      style: DeckHud.mono(size: 15, color: DeckHud.accent),
                    ),
                ],
              ),
            ),
          ),
          Expanded(
            child: Stack(
              fit: StackFit.expand,
              children: [
                DeckGrid(cells: cells, square: false),
                if (empty)
                  Center(
                    child: Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 22,
                        vertical: 14,
                      ),
                      color: attentionTint,
                      child: Text(
                        'Nothing needs you right now',
                        textAlign: TextAlign.center,
                        style: DeckHud.mono(size: 20, color: DeckHud.ink),
                      ),
                    ),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// A spot with nobody in it: kept for the agent that had it.
class _EmptySpot extends StatelessWidget {
  const _EmptySpot({super.key});

  @override
  Widget build(BuildContext context) => const DecoratedBox(
    decoration: BoxDecoration(
      color: attentionSpotFill,
      border: Border.fromBorderSide(BorderSide(color: attentionSpotOutline)),
    ),
    child: SizedBox.expand(),
  );
}
