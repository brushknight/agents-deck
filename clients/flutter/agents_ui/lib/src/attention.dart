/// Who sits where on the "Needs you" screen. The service decides it (see
/// docs/protocol.md, "The Needs you grid"): each agent carries its spot in
/// [Agent.attentionSlot], so the panel, the menu bar app, the browser and the
/// boards all show the same agents in the same spots. Plain Dart, no widgets.
library;

import 'agents_model.dart';

/// Working (starting, running) or asking (waiting): the statuses that put an
/// agent on the "Needs you" screen.
bool wantsAttention(AgentStatus s) =>
    s == AgentStatus.starting ||
    s == AgentStatus.running ||
    s == AgentStatus.waiting;

/// The service's rule for the "Needs you" screen: an agent that is working
/// or asking, and one that finished a turn you haven't looked at yet
/// (hungry). It stays until you check it.
bool onNeedsYou(Agent a) => wantsAttention(a.status) || a.hungry;

/// The spots of the "Needs you" grid: 4 x 4.
const needsYouSpots = 16;

/// The "Needs you" screen of one state.
class NeedsYou {
  const NeedsYou._(this.spots, this.count, this.fromService);

  /// Reads the spots from [state]. Spot n is `spots[n - 1]`.
  ///
  /// An older service sends no spots (every [Agent.attentionSlot] is 0).
  /// When no agent has one but some need you, they fill the spots in deck
  /// order instead ([fromService] false): no 5 s stay, no remembered spots.
  factory NeedsYou.of(AgentsState state) {
    final spots = List<Agent?>.filled(needsYouSpots, null);
    var placed = 0;
    for (final a in state.agents) {
      final s = a.attentionSlot;
      if (s >= 1 && s <= needsYouSpots && spots[s - 1] == null) {
        spots[s - 1] = a;
        placed++;
      }
    }
    final wanting = [
      for (final a in state.agents)
        if (onNeedsYou(a)) a,
    ];
    if (placed == 0 && wanting.isNotEmpty) {
      for (final (i, a) in wanting.take(needsYouSpots).indexed) {
        spots[i] = a; // state.agents is in slot order
      }
      return NeedsYou._(List.unmodifiable(spots), wanting.length, false);
    }
    // The ones waiting for a spot (the grid is full) count too.
    final waiting = wanting.where((a) => a.attentionSlot == 0).length;
    return NeedsYou._(List.unmodifiable(spots), placed + waiting, true);
  }

  /// The agent in each spot, or null for a free one; always 16 long.
  final List<Agent?> spots;

  /// Every agent on the screen, with the ones waiting for a spot.
  final int count;

  /// False when the spots were filled here because the service sent none.
  final bool fromService;

  /// The agents in order of their spot, lowest first, at most [cells]: for
  /// a screen with fewer cells than 16.
  List<Agent> packed(int cells) =>
      [for (final a in spots) ?a].take(cells).toList();
}
