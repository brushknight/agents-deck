import 'dart:math' as math;

import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/widgets.dart';

import 'service.dart';

/// The status line's height under the deck, in window points. The native
/// shells make the popover this much taller than it is wide.
const statusLineHeight = 32.0;

/// One part of the line: its words, the Settings page it opens, and whether
/// it wants attention (accent).
typedef StatusPart = ({String text, String page, bool attention});

/// What the line says, in order: the service, the boards, then whatever
/// wants attention.
List<StatusPart> statusParts(ServiceStatus s) {
  final parts = <StatusPart>[];
  switch (s.state) {
    case ServiceState.checking:
      return [(text: 'checking', page: 'troubleshoot', attention: false)];
    case ServiceState.notRunning:
      return [
        (
          text: s.starting ? 'starting' : 'not running',
          page: 'troubleshoot',
          attention: !s.starting,
        ),
      ];
    case ServiceState.running:
      parts.add((text: 'running', page: 'troubleshoot', attention: false));
  }
  final sys = s.system;
  if (sys == null) return parts; // an older service: nothing more to say
  final online = sys.boardsOnline;
  final total = sys.boardsTotal;
  parts.add((
    text: total == 0
        ? 'no boards'
        : online == total
        ? '$online board${online == 1 ? '' : 's'}'
        : '$online of $total boards',
    page: 'boards',
    attention: false,
  ));
  // The version is on the Updates page; the line stays short.
  if (sys.updateState == 'ready') {
    parts.add((text: 'update ready', page: 'updates', attention: true));
  } else if (sys.updateState == 'available') {
    parts.add((text: 'update available', page: 'updates', attention: true));
  } else if (sys.updateState == 'installing') {
    parts.add((text: 'updating', page: 'updates', attention: false));
  }
  if (sys.newPorts > 0) {
    parts.add((
      text: sys.newPorts == 1
          ? 'new board on USB'
          : '${sys.newPorts} new boards on USB',
      page: 'boards',
      attention: true,
    ));
  }
  if (sys.boardsPending > 0) {
    parts.add((
      text: '${sys.boardsPending} to approve',
      page: 'boards',
      attention: true,
    ));
  }
  if (sys.firmwareUpdates > 0) {
    parts.add((
      text:
          '${sys.firmwareUpdates} firmware update'
          '${sys.firmwareUpdates == 1 ? '' : 's'}',
      page: 'updates',
      attention: true,
    ));
  }
  return parts;
}

/// The row under the deck: is the service running, how many boards are
/// online, and what wants attention. Each part opens Settings on its page;
/// the Settings button at the end opens Settings. While the service isn't
/// running there is no Settings button (Settings comes from the service).
class StatusLine extends StatelessWidget {
  const StatusLine({super.key, required this.status, required this.onOpen});

  final ServiceStatus status;

  /// Opens Settings on a page (see [settingsPages]).
  final void Function(String page) onOpen;

  @override
  Widget build(BuildContext context) {
    final running = status.state == ServiceState.running;
    final parts = statusParts(status);
    return Container(
      key: const Key('status-line'),
      height: statusLineHeight,
      color: DeckHud.bg,
      padding: const EdgeInsets.fromLTRB(8, 0, 8, 6),
      child: Container(
        padding: const EdgeInsets.only(left: 10, right: 3),
        decoration: BoxDecoration(
          color: DeckHud.panel,
          borderRadius: BorderRadius.circular(8),
        ),
        child: Row(
          children: [
            _Dot(state: status.state, starting: status.starting),
            const SizedBox(width: 4),
            Expanded(
              child: LayoutBuilder(
                builder: (context, c) => Row(
                  children: [
                    for (final (i, p) in _fit(parts, c.maxWidth).indexed)
                      _Part(
                        text: i == 0 ? p.text : '· ${p.text}',
                        color: p.attention
                            ? DeckHud.accent
                            : (i == 0 ? DeckHud.ink : DeckHud.dim),
                        onTap: running ? () => onOpen(p.page) : null,
                      ),
                  ],
                ),
              ),
            ),
            if (running) ...[
              const SizedBox(width: 4),
              _SettingsButton(
                key: const Key('status-gear'),
                onTap: () => onOpen('general'),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

const _partPadding = 4.0;

TextStyle _partStyle(Color color) => DeckHud.mono(size: 11, color: color);

double _partWidth(String text) {
  final tp = TextPainter(
    text: TextSpan(text: text, style: _partStyle(DeckHud.ink)),
    textDirection: TextDirection.ltr,
    maxLines: 1,
  )..layout();
  final w = tp.width + 2 * _partPadding;
  tp.dispose();
  return w;
}

/// The parts that fit in [width], whole: when some don't, the last one that
/// fits makes room for "+N", which opens the first left out.
List<StatusPart> _fit(List<StatusPart> parts, double width) {
  String label(int i, StatusPart p) => i == 0 ? p.text : '· ${p.text}';
  final widths = [for (final (i, p) in parts.indexed) _partWidth(label(i, p))];
  if (widths.fold(0.0, (a, b) => a + b) <= width) return parts;
  for (var keep = parts.length - 1; keep >= 1; keep--) {
    final rest = parts.length - keep;
    final more = (
      text: '+$rest',
      page: parts[keep].page,
      attention: parts.skip(keep).any((p) => p.attention),
    );
    final used =
        widths.take(keep).fold(0.0, (a, b) => a + b) +
        _partWidth(label(keep, more));
    if (used <= width) return [...parts.take(keep), more];
  }
  return parts.take(1).toList();
}

class _Dot extends StatelessWidget {
  const _Dot({required this.state, required this.starting});

  final ServiceState state;
  final bool starting;

  @override
  Widget build(BuildContext context) {
    final color = switch (state) {
      ServiceState.running => DeckHud.ink,
      ServiceState.notRunning when !starting => DeckHud.accent,
      _ => DeckHud.dim,
    };
    return Container(
      width: 7,
      height: 7,
      decoration: BoxDecoration(color: color, shape: BoxShape.circle),
    );
  }
}

class _Part extends StatelessWidget {
  const _Part({required this.text, required this.color, this.onTap});

  final String text;
  final Color color;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final label = Pressable(
      onTap: onTap,
      builder: (context, pressed) => Container(
        height: 22,
        alignment: Alignment.center,
        padding: const EdgeInsets.symmetric(horizontal: _partPadding),
        decoration: BoxDecoration(
          color: pressed ? agentsFocusedFill : null,
          borderRadius: BorderRadius.circular(5),
        ),
        child: Text(
          text,
          maxLines: 1,
          softWrap: false,
          overflow: TextOverflow.ellipsis,
          style: _partStyle(color),
        ),
      ),
    );
    if (onTap == null) return label;
    return MouseRegion(cursor: SystemMouseCursors.click, child: label);
  }
}

/// "Settings" with a gear: a button that says what it does.
class _SettingsButton extends StatelessWidget {
  const _SettingsButton({super.key, required this.onTap});

  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      button: true,
      label: 'Settings',
      excludeSemantics: true,
      child: MouseRegion(
        cursor: SystemMouseCursors.click,
        child: Pressable(
          onTap: onTap,
          builder: (context, pressed) => Container(
            height: 22,
            padding: const EdgeInsets.only(left: 7, right: 8),
            decoration: BoxDecoration(
              color: pressed ? DeckHud.ink : null,
              borderRadius: BorderRadius.circular(6),
              border: Border.all(color: pressed ? DeckHud.ink : DeckHud.dim),
            ),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                CustomPaint(
                  size: const Size.square(11),
                  painter: _GearPainter(pressed ? DeckHud.bg : DeckHud.ink),
                ),
                const SizedBox(width: 5),
                Text(
                  'Settings',
                  style: _partStyle(pressed ? DeckHud.bg : DeckHud.ink),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// A small gear: a ring with eight square teeth and a hole.
class _GearPainter extends CustomPainter {
  const _GearPainter(this.color);

  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    final c = size.center(Offset.zero);
    final r = size.shortestSide / 2;
    final paint = Paint()..color = color;
    final path = Path()
      ..addOval(Rect.fromCircle(center: c, radius: r * 0.68))
      ..fillType = PathFillType.evenOdd;
    for (var i = 0; i < 8; i++) {
      final a = i * math.pi / 4;
      canvas.save();
      canvas.translate(c.dx, c.dy);
      canvas.rotate(a);
      canvas.drawRect(Rect.fromLTWH(-r * 0.17, -r, r * 0.34, r * 0.4), paint);
      canvas.restore();
    }
    path.addOval(Rect.fromCircle(center: c, radius: r * 0.28));
    canvas.drawPath(path, paint);
  }

  @override
  bool shouldRepaint(_GearPainter old) => old.color != color;
}
