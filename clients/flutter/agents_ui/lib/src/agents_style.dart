import 'dart:io';

import 'package:flutter/widgets.dart';

import 'theme.dart';
import 'widgets.dart';
import 'agents_model.dart';

/// Corner-chip colour per agent tool (the only non-palette colours in the
/// app, as small as the deck's configured tile chips).
Color agentToolColor(String tool) => switch (tool) {
  'claude' => const Color(0xFFC98B6B),
  'codex' => const Color(0xFF4FB3A6),
  'gemini' => const Color(0xFF6F8FD8),
  _ => DeckHud.dim,
};

/// Empty grid slot (the deck's barely-there frame, a touch lifted).
const agentsEmptyFill = Color(0xFF0B0B0B);
const agentsEmptyOutline = Color(0xFF262622);

/// Focused tile fills: a step lighter than [DeckHud.panel] / [DeckHud.accent].
const agentsFocusedFill = Color(0xFF24241F);
const agentsFocusedAccent = Color(0xFFFF6A26);

/// Context ring tracks (the unfilled part of the border).
const agentsRingTrack = Color(0xFF2A2925);
const agentsRingTrackOnAccent = Color(0x400A0A0A);

/// Question option rows: resting outline, and the sub-line on a selected
/// (ink) row.
const agentsRowOutline = Color(0xFF2A2925);
const agentsRowSubOnInk = Color(0xFF3B3A36);

/// How a status paints its grid tile and critter.
class AgentLook {
  const AgentLook({
    required this.fill,
    required this.body,
    required this.label,
    required this.meta,
    required this.chip,
    this.border,
    this.borderWidth = 0,
    this.bubble,
    this.ring = DeckHud.accent,
    this.ringTrack = agentsRingTrack,
    this.stripes = false,
  });

  final Color fill;
  final Color body;
  final Color label;
  final Color meta;
  final Color chip;
  final Color? border;
  final double borderWidth;

  /// Critter bubble colour when it differs from the body (error "!").
  final Color? bubble;

  /// Context ring around the tile: filled part and the faint full track.
  final Color ring;
  final Color ringTrack;

  /// Sliding orange zebra stripes behind the critter (hungry: review me).
  final bool stripes;

  static AgentLook of(Agent a) {
    final tool = agentToolColor(a.tool);
    final look = switch (a.status) {
      AgentStatus.waiting => const AgentLook(
        fill: DeckHud.accent,
        body: DeckHud.bg,
        label: DeckHud.bg,
        meta: DeckHud.bg,
        chip: DeckHud.bg,
        ring: DeckHud.bg,
        ringTrack: agentsRingTrackOnAccent,
      ),
      // Orange means "working"; an error is an ink critter with an accent "!".
      AgentStatus.error => AgentLook(
        fill: DeckHud.panel,
        body: DeckHud.ink,
        bubble: DeckHud.accent,
        label: DeckHud.ink,
        meta: DeckHud.accent,
        chip: tool,
      ),
      AgentStatus.running => AgentLook(
        fill: DeckHud.panel,
        body: DeckHud.accent,
        label: DeckHud.ink,
        meta: DeckHud.dim,
        chip: tool,
      ),
      AgentStatus.starting => AgentLook(
        fill: DeckHud.panel,
        body: DeckHud.dim,
        label: DeckHud.ink,
        meta: DeckHud.dim,
        chip: tool,
      ),
      AgentStatus.idle when a.hungry => AgentLook(
        fill: DeckHud.panel,
        body: DeckHud.accent,
        label: DeckHud.ink,
        meta: DeckHud.accent,
        chip: tool,
        stripes: true,
      ),
      AgentStatus.idle ||
      AgentStatus.exited ||
      AgentStatus.unknown => AgentLook(
        fill: DeckHud.panel,
        body: DeckHud.dim,
        label: DeckHud.dim,
        meta: DeckHud.dim,
        chip: tool,
      ),
    };
    if (!a.focused) return look;
    // The agent whose terminal is in front on the Mac: a lifted fill.
    return AgentLook(
      fill: look.fill == DeckHud.accent
          ? agentsFocusedAccent
          : agentsFocusedFill,
      body: look.body,
      label: look.label,
      meta: look.meta,
      chip: look.chip,
      bubble: look.bubble,
      ring: look.ring,
      ringTrack: look.ringTrack,
      stripes: look.stripes,
    );
  }
}

/// Hostname for the pairing card's setup address. Tests/goldens set it so
/// screenshots never bake in the developer machine's name.
@visibleForTesting
String? debugAgentsSetupHostname;

/// The panel's setup page address, as the settings card prints it.
String agentsSetupUrl() {
  final h = debugAgentsSetupHostname ?? Platform.localHostname;
  final host = h.isEmpty ? 'panel' : (h.contains('.') ? h : '$h.local');
  return 'https://$host:8443';
}

/// Rounded command button (68 px, 14 radius, 2 px outline). Pressed
/// inverts to [pressedFill]/[pressedInk] — the house press feedback.
class AgentButton extends StatelessWidget {
  const AgentButton({
    super.key,
    required this.label,
    required this.onTap,
    required this.fill,
    required this.ink,
    required this.outline,
    required this.pressedFill,
    required this.pressedInk,
    this.height = 68,
    this.fontSize = 19,
    this.alignLeft = false,
  });

  final String label;

  /// Null: disabled (no press feedback).
  final VoidCallback? onTap;
  final Color fill;
  final Color ink;
  final Color outline;
  final Color pressedFill;
  final Color pressedInk;
  final double height;
  final double fontSize;
  final bool alignLeft;

  @override
  Widget build(BuildContext context) {
    return Pressable(
      onTap: onTap,
      builder: (context, pressed) => Container(
        height: height,
        alignment: alignLeft ? Alignment.centerLeft : Alignment.center,
        padding: const EdgeInsets.symmetric(horizontal: 22),
        decoration: BoxDecoration(
          color: pressed ? pressedFill : fill,
          borderRadius: BorderRadius.circular(14),
          border: Border.all(color: pressed ? pressedFill : outline, width: 2),
        ),
        child: Text(
          label,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          style: DeckHud.mono(
            size: fontSize,
            color: pressed ? pressedInk : ink,
          ),
        ),
      ),
    );
  }
}
