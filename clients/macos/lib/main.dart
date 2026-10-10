import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

/// The native side (AppDelegate.swift): the menu bar icon and the window.
const _menubar = MethodChannel('agents_deck/menubar');

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  final visible = ValueNotifier(false);
  _menubar.setMethodCallHandler((call) async {
    if (call.method == 'visible') visible.value = call.arguments == true;
  });
  final source = HttpAgentsClient(loadConfig: localAgentsConfig);
  source.snapshot.addListener(() => _report(source.snapshot.value));
  runApp(MenuBarDeck(source: source, visible: visible));
}

/// Tells the menu bar icon how many agents need you (0 = plain icon).
int _lastReported = -1;
void _report(AgentsSnapshot snap) {
  final n = snap.state?.waitingCount ?? 0;
  if (n == _lastReported) return;
  _lastReported = n;
  _menubar.invokeMethod('attention', n).ignore();
}

/// The deck exactly as the desk panel draws it: laid out at the panel's
/// 720×720 and scaled to the window.
class MenuBarDeck extends StatelessWidget {
  const MenuBarDeck({super.key, required this.source, required this.visible});

  final AgentsSource source;
  final ValueListenable<bool> visible;

  static const design = 720.0;

  @override
  Widget build(BuildContext context) {
    return WidgetsApp(
      color: DeckHud.accent,
      debugShowCheckedModeBanner: false,
      builder: (context, _) => Focus(
        // Esc with no card open puts the deck away (a card takes Esc first).
        autofocus: true,
        onKeyEvent: (_, e) {
          if (e is KeyDownEvent && e.logicalKey == LogicalKeyboardKey.escape) {
            _menubar.invokeMethod('hide').ignore();
            return KeyEventResult.handled;
          }
          return KeyEventResult.ignored;
        },
        child: ColoredBox(
          color: DeckHud.bg,
          child: FittedBox(
            child: SizedBox.square(
              dimension: design,
              // An Overlay for drag-to-rearrange (there's no Navigator).
              child: Overlay(
                initialEntries: [
                  OverlayEntry(
                    builder: (context) => AgentsApp(
                      source: source,
                      visible: visible,
                      pairingCard: (problem) => _NotPaired(problem: problem),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// No daemon config on this Mac yet.
class _NotPaired extends StatelessWidget {
  const _NotPaired({this.problem});

  final String? problem;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(48, 56, 48, 48),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const CritterSprite(
            pose: CritterPose.wait,
            width: 108,
            body: DeckHud.ink,
            cut: DeckHud.bg,
          ),
          const SizedBox(height: 30),
          Text(
            'agentctl isn\'t running here',
            style: DeckHud.rm(32, DeckHud.ink, weight: 700),
          ),
          const SizedBox(height: 18),
          Text(
            'install it and start the daemon:\n\n'
            '  agentctl install\n  agentctl pair\n\n'
            'the deck connects by itself once it is up.',
            style: DeckHud.mono(
              size: 20,
              color: DeckHud.dim,
            ).copyWith(height: 1.45),
          ),
          if (problem != null) ...[
            const SizedBox(height: 22),
            Text(
              problem!,
              style: DeckHud.mono(size: 17, color: DeckHud.accent),
            ),
          ],
        ],
      ),
    );
  }
}
