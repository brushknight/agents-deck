import 'dart:async';
import 'dart:io' show Platform;

import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

import 'service.dart';
import 'status_line.dart';

export 'service.dart';
export 'status_line.dart';

/// The native side: the menu bar or tray icon, the deck's window and the
/// Settings window (macos/Runner/AppDelegate.swift,
/// linux/runner/my_application.cc).
const _menubar = MethodChannel('agents_deck/menubar');

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  final visible = ValueNotifier(false);
  final source = HttpAgentsClient(loadConfig: localAgentsConfig);
  final service = LocalDeckService();
  _menubar.setMethodCallHandler((call) async {
    switch (call.method) {
      case 'visible':
        visible.value = call.arguments == true;
      case 'openSettings':
        // "Settings…" in the icon's menu. False tells the shell to show the
        // deck instead, which says why (the service isn't running).
        final page = call.arguments is String
            ? call.arguments as String
            : 'general';
        return openSettings(service, page);
    }
    return null;
  });
  visible.addListener(() => service.visible = visible.value);
  var link = source.snapshot.value.link;
  source.snapshot.addListener(() {
    final snap = source.snapshot.value;
    _report(snap);
    // The event stream came up or went down: ask the service right away.
    if (snap.link != link) {
      link = snap.link;
      unawaited(service.refresh());
    }
  });
  runApp(
    MenuBarDeck(
      source: source,
      visible: visible,
      service: service,
      onSettings: (page) => unawaited(openSettings(service, page)),
    ),
  );
}

/// Opens the Settings window on [page]: a fresh login link from the service,
/// handed to the shell (`settings(url)`). False when the service didn't
/// answer.
Future<bool> openSettings(DeckService service, String page) async {
  final url = await service.settingsUrl(page);
  if (url == null) return false;
  try {
    await _menubar.invokeMethod('settings', url.toString());
    return true;
  } on MissingPluginException {
    return false;
  } on PlatformException {
    return false;
  }
}

/// Tells the menu bar or tray icon how many agents need you (0 = plain icon).
int _lastReported = -1;
void _report(AgentsSnapshot snap) {
  final n = snap.state?.waitingCount ?? 0;
  if (n == _lastReported) return;
  _lastReported = n;
  _menubar.invokeMethod('attention', n).ignore();
}

/// The deck exactly as the desk panel draws it: laid out at the panel's
/// 720×720 and scaled to the window's width, with the status line under it
/// when there is a [service].
class MenuBarDeck extends StatelessWidget {
  const MenuBarDeck({
    super.key,
    required this.source,
    required this.visible,
    this.service,
    this.onSettings,
  });

  final AgentsSource source;
  final ValueListenable<bool> visible;

  /// The service behind the deck; null leaves out the status line.
  final DeckService? service;

  /// Opens Settings on a page (see [settingsPages]).
  final void Function(String page)? onSettings;

  static const design = 720.0;

  @override
  Widget build(BuildContext context) {
    final service = this.service;
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
          child: service == null
              ? _deck(null)
              : ValueListenableBuilder<ServiceStatus>(
                  valueListenable: service.status,
                  builder: (context, status, _) => Column(
                    children: [
                      Expanded(child: _deck(status)),
                      StatusLine(
                        status: status,
                        onOpen: (page) => onSettings?.call(page),
                      ),
                    ],
                  ),
                ),
        ),
      ),
    );
  }

  Widget _deck(ServiceStatus? status) {
    final down = status != null && status.state == ServiceState.notRunning;
    return FittedBox(
      child: SizedBox.square(
        dimension: design,
        child: down
            ? NotRunningCard(status: status, onStart: service!.start)
            // An Overlay for drag-to-rearrange (there's no Navigator).
            : Overlay(
                initialEntries: [
                  OverlayEntry(
                    builder: (context) => AgentsApp(
                      source: source,
                      visible: visible,
                      backButton: true, // no edge swipe here
                      pairingCard: (problem) => NotRunningCard(
                        status: service?.status.value,
                        onStart: service?.start,
                        problem: problem,
                      ),
                    ),
                  ),
                ],
              ),
      ),
    );
  }
}

/// The service isn't running on this computer: Start, when the app came
/// with it, or how to start it by hand.
class NotRunningCard extends StatelessWidget {
  const NotRunningCard({super.key, this.status, this.onStart, this.problem});

  final ServiceStatus? status;
  final Future<void> Function()? onStart;
  final String? problem;

  @override
  Widget build(BuildContext context) {
    final canStart = (status?.canStart ?? false) && onStart != null;
    final starting = status?.starting ?? false;
    final problem = status?.problem ?? this.problem;
    final command = Platform.isLinux
        ? 'systemctl --user start agents-deck'
        : 'agentctl install';
    return Padding(
      key: const Key('not-running'),
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
            'the service isn\'t running',
            style: DeckHud.rm(32, DeckHud.ink, weight: 700),
          ),
          const SizedBox(height: 18),
          if (canStart) ...[
            Text(
              'agentctl runs in the background and watches your agents.\n'
              'start it here; after that it starts by itself when you log in.',
              style: DeckHud.mono(
                size: 20,
                color: DeckHud.dim,
              ).copyWith(height: 1.45),
            ),
            const SizedBox(height: 30),
            SizedBox(
              width: 260,
              child: AgentButton(
                key: const Key('start-service'),
                label: starting ? 'starting…' : 'Start',
                onTap: starting ? null : () => unawaited(onStart!()),
                fill: starting ? DeckHud.panel : DeckHud.accent,
                ink: starting ? DeckHud.dim : DeckHud.bg,
                outline: starting ? DeckHud.panel : DeckHud.accent,
                pressedFill: DeckHud.ink,
                pressedInk: DeckHud.bg,
              ),
            ),
          ] else
            Text(
              'install it and start it:\n\n'
              '  $command\n\n'
              'the deck connects by itself once it is up.',
              style: DeckHud.mono(
                size: 20,
                color: DeckHud.dim,
              ).copyWith(height: 1.45),
            ),
          if (problem != null) ...[
            const SizedBox(height: 22),
            Text(
              problem,
              maxLines: 3,
              overflow: TextOverflow.ellipsis,
              style: DeckHud.mono(size: 17, color: DeckHud.accent),
            ),
          ],
        ],
      ),
    );
  }
}
