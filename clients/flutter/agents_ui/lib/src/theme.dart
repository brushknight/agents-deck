import 'package:flutter/widgets.dart';

/// The deck's design tokens: near-black surfaces, warm ink, one accent, and
/// two monospace faces (Share Tech Mono for labels, Roboto Mono for values).
abstract final class DeckHud {
  static const bg = Color(0xFF0A0A0A);
  static const panel = Color(0xFF141412);
  static const ink = Color(0xFFE8E4DC);
  static const dim = Color(0xFF7A776F);
  static const accent = Color(0xFFFF4D00);

  /// Where the font families come from. This package bundles both faces; an
  /// app that registers the same families itself (the desk panel) sets this
  /// to null so text resolves to its own fonts.
  static String? fontPackage = 'agents_deck_ui';

  /// Values and headings: Roboto Mono (variable weight).
  static TextStyle rm(
    double size,
    Color color, {
    double weight = 400,
    double? spacing,
  }) => TextStyle(
    fontFamily: 'RobotoMono',
    package: fontPackage,
    fontSize: size,
    color: color,
    height: 1.0,
    letterSpacing: spacing,
    fontVariations: [FontVariation('wght', weight)],
  );

  /// Labels and status lines: Share Tech Mono, slightly tracked.
  static TextStyle mono({double size = 13, Color color = ink}) => TextStyle(
    fontFamily: 'ShareTechMono',
    package: fontPackage,
    fontSize: size,
    color: color,
    letterSpacing: 0.5,
  );
}
