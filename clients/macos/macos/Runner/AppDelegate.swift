import Cocoa
import FlutterMacOS

/// agents deck in the menu bar: a critter icon (orange when an agent needs
/// you) that drops the deck down underneath it. No Dock icon and no main menu,
/// except while the Settings window is open.
@main
class AppDelegate: FlutterAppDelegate, NSWindowDelegate {
  private var statusItem: NSStatusItem!
  private var channel: FlutterMethodChannel?
  private var attention = 0
  private var settings: SettingsWindowController?
  /// Pinned, the deck stays open above other windows (also when you click
  /// elsewhere) and stays where you dragged it. Remembered across launches.
  private var pinned = UserDefaults.standard.bool(forKey: "deckPinned")
  /// The deck has been put under the menu bar icon at least once.
  private var placed = false
  /// While the unpinned deck is open: a click outside it puts it away.
  private var clickMonitors: [Any] = []

  override func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
    false
  }

  override func applicationSupportsSecureRestorableState(_ app: NSApplication) -> Bool {
    true
  }

  override func applicationDidFinishLaunching(_ notification: Notification) {
    NSApp.setActivationPolicy(.accessory)

    statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
    if let button = statusItem.button {
      button.image = CritterIcon.image(attention: false)
      button.toolTip = "agents deck"
      button.target = self
      button.action = #selector(statusItemClicked(_:))
      button.sendAction(on: [.leftMouseUp, .rightMouseUp])
    }

    guard let window = mainFlutterWindow,
      let flutter = window.contentViewController as? FlutterViewController
    else { return }
    window.delegate = self
    window.orderOut(nil)
    applyPin()

    let channel = FlutterMethodChannel(
      name: "agents_deck/menubar", binaryMessenger: flutter.engine.binaryMessenger)
    channel.setMethodCallHandler { [weak self] call, result in
      switch call.method {
      case "attention":
        self?.setAttention((call.arguments as? Int) ?? 0)
        result(nil)
      case "hide":
        self?.hideDeck()
        result(nil)
      case "isPinned":
        result(self?.pinned ?? false)
      case "pin":
        self?.setPinned((call.arguments as? Bool) ?? false)
        result(nil)
      case "drag":
        self?.dragDeck()
        result(nil)
      case "settings":
        guard let s = call.arguments as? String,
          let url = SettingsWindowController.dashboardURL(s)
        else {
          result(FlutterError(code: "bad-url", message: "not the dashboard on this computer", details: nil))
          return
        }
        self?.showSettings(url)
        result(nil)
      default:
        result(FlutterMethodNotImplemented)
      }
    }
    self.channel = channel
  }

  // MARK: status item

  @objc private func statusItemClicked(_ sender: NSStatusBarButton) {
    if NSApp.currentEvent?.type == .rightMouseUp {
      showMenu()
    } else if mainFlutterWindow?.isVisible == true {
      hideDeck()
    } else {
      showDeck()
    }
  }

  private func showMenu() {
    let menu = NSMenu()
    let deck = NSMenuItem(title: "Show the Deck", action: #selector(showDeckFromMenu(_:)), keyEquivalent: "")
    deck.target = self
    menu.addItem(deck)
    let pin = NSMenuItem(title: "Keep the Deck on Top", action: #selector(togglePinFromMenu(_:)), keyEquivalent: "")
    pin.target = self
    pin.state = pinned ? .on : .off
    menu.addItem(pin)
    let settings = NSMenuItem(title: "Settings…", action: #selector(openSettings), keyEquivalent: ",")
    settings.target = self
    menu.addItem(settings)
    let open = NSMenuItem(title: "Open the Dashboard in Your Browser", action: #selector(openDashboard), keyEquivalent: "")
    open.target = self
    menu.addItem(open)
    menu.addItem(.separator())
    menu.addItem(withTitle: "Quit agents deck", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
    statusItem.menu = menu
    statusItem.button?.performClick(nil)  // pops the menu up under the icon
    statusItem.menu = nil  // a left click goes back to toggling the deck
  }

  /// "Show the Deck" in the icon's menu and the Window menu.
  @objc func showDeckFromMenu(_ sender: Any?) {
    showDeck()
  }

  /// "Keep the Deck on Top" in the icon's menu: the same as the Pin button.
  @objc private func togglePinFromMenu(_ sender: Any?) {
    setPinned(!pinned)
    channel?.invokeMethod("pinned", arguments: pinned)
    if pinned { showDeck() }
  }

  /// `agentctl web`: opens the dashboard with a one-time login link. The
  /// agentctl that came with the app first, then the usual places.
  @objc private func openDashboard() {
    let home = FileManager.default.homeDirectoryForCurrentUser.path
    let bundled = Bundle.main.executableURL?.deletingLastPathComponent().appendingPathComponent("agentctl").path
    let candidates = [bundled, "\(home)/.local/bin/agentctl", "/opt/homebrew/bin/agentctl", "/usr/local/bin/agentctl"].compactMap { $0 }
    guard let path = candidates.first(where: { FileManager.default.isExecutableFile(atPath: $0) }) else { return }
    let p = Process()
    p.executableURL = URL(fileURLWithPath: path)
    p.arguments = ["web"]
    try? p.run()
  }

  // MARK: Settings window

  /// "Settings…" in the icon's menu: Dart gets a login link from the service
  /// and calls `settings(url)` back. When it can't (the service isn't
  /// running), the deck opens instead and says so.
  @objc private func openSettings() {
    channel?.invokeMethod("openSettings", arguments: "general") { [weak self] result in
      if (result as? Bool) != true { self?.showDeck() }
    }
  }

  private func showSettings(_ url: URL) {
    hideDeck()
    let controller = settings ?? SettingsWindowController()
    if settings == nil {
      controller.onClose = { [weak self] in self?.settingsClosed() }
      controller.onShowDeck = { [weak self] in self?.showDeck() }
      controller.onOpenDashboard = { [weak self] in self?.openDashboard() }
      settings = controller
    }
    // A normal app while the window is open: a Dock icon, the app switcher,
    // and a menu bar for Cmd-W, Cmd-Q, copy and paste.
    NSApp.mainMenu = AppMenu.make()
    NSApp.setActivationPolicy(.regular)
    NSApp.activate(ignoringOtherApps: true)
    controller.open(url)
  }

  private func settingsClosed() {
    // Let the web view (and its web content process) go; the next opening
    // starts fresh with the remembered frame and the saved session cookie.
    settings = nil
    NSApp.setActivationPolicy(.accessory)
    if pinned { showDeck() }  // it made way for Settings; a pinned deck comes back
  }

  /// Clicking the Dock icon while Settings is open brings it to the front.
  override func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
    settings?.showWindow(nil)
    return true
  }

  private func setAttention(_ n: Int) {
    guard n != attention else { return }
    attention = n
    statusItem.button?.image = CritterIcon.image(attention: n > 0)
    statusItem.button?.toolTip = n > 0 ? "agents deck · \(n) need\(n == 1 ? "s" : "") you" : "agents deck"
  }

  // MARK: deck window

  private func showDeck() {
    guard let window = mainFlutterWindow else { return }
    // In the top-right corner of the screen with the menu bar icon, just
    // under the menu bar (visibleFrame already leaves it out). A pinned deck
    // stays where you dragged it, as long as that is still on a screen.
    let onScreen = NSScreen.screens.contains { $0.visibleFrame.intersects(window.frame) }
    if !pinned || !placed || !onScreen {
      let screen = (statusItem.button?.window?.screen ?? NSScreen.main)?.visibleFrame ?? .zero
      let size = window.frame.size
      let margin: CGFloat = 8
      window.setFrameOrigin(
        NSPoint(x: screen.maxX - size.width - margin, y: screen.maxY - size.height - margin))
      placed = true
    }
    NSApp.activate(ignoringOtherApps: true)
    window.makeKeyAndOrderFront(nil)
    watchClicks()
    channel?.invokeMethod("visible", arguments: true)
  }

  private func hideDeck() {
    unwatchClicks()
    mainFlutterWindow?.orderOut(nil)
    channel?.invokeMethod("visible", arguments: false)
  }

  /// Clicking anywhere else puts the deck away, like any menu bar popover,
  /// unless it is pinned. Only a click does: another app taking the focus by
  /// itself (a permission prompt, an alert) leaves the deck where it is.
  private func watchClicks() {
    guard clickMonitors.isEmpty else { return }
    let mask: NSEvent.EventTypeMask = [.leftMouseDown, .rightMouseDown, .otherMouseDown]
    if let g = NSEvent.addGlobalMonitorForEvents(matching: mask, handler: { [weak self] _ in
      guard let self, !self.pinned else { return }
      self.hideDeck()
    }) { clickMonitors.append(g) }
    // In this app's own other windows (Settings); the menu bar icon toggles by itself.
    if let l = NSEvent.addLocalMonitorForEvents(matching: mask, handler: { [weak self] e in
      if let self, !self.pinned, let w = e.window, w !== self.mainFlutterWindow, w !== self.statusItem.button?.window {
        self.hideDeck()
      }
      return e
    }) { clickMonitors.append(l) }
  }

  private func unwatchClicks() {
    clickMonitors.forEach(NSEvent.removeMonitor)
    clickMonitors.removeAll()
  }

  /// The Pin button (or the menu): pinned, the deck floats above other apps'
  /// windows (below menus) and stays open; unpinned, it is a popover again.
  private func setPinned(_ on: Bool) {
    pinned = on
    UserDefaults.standard.set(on, forKey: "deckPinned")
    applyPin()
    if !on, let window = mainFlutterWindow, window.isVisible, !window.isKeyWindow {
      hideDeck()  // unpinned while you work elsewhere: put it away, as a popover would be
    }
  }

  private func applyPin() {
    mainFlutterWindow?.level = pinned ? .floating : .popUpMenu
  }

  /// Dragging the status line of a pinned deck moves the window.
  private func dragDeck() {
    guard pinned, let window = mainFlutterWindow, let event = NSApp.currentEvent else { return }
    window.performDrag(with: event)
  }
}

/// The menu bar icon: the deck's pixel critter. A template image (it follows
/// the menu bar's light or dark look); orange when an agent needs you.
enum CritterIcon {
  // The deck's critter: a 12×6 block body (x 1..13, rows 0..5) with 1×2 arms
  // at x 0 and 13 (rows 2-3), eyes at x 4 and 9 (rows 2-3), and legs (rows 6-7)
  // at x 2, 4, 9, 11, on a 14×8 grid.
  private static let legs = [2, 4, 9, 11]
  private static let accent = NSColor(red: 1, green: 0x4D / 255.0, blue: 0, alpha: 1)

  static func image(attention: Bool) -> NSImage {
    let cell: CGFloat = 1.25
    let size = NSSize(width: 18, height: 18)
    let image = NSImage(size: size, flipped: true) { _ in
      let ox = (size.width - 14 * cell) / 2
      let oy = (size.height - 8 * cell) / 2
      func fill(_ x: Int, _ y: Int, _ w: Int, _ h: Int) {
        NSRect(
          x: ox + CGFloat(x) * cell, y: oy + CGFloat(y) * cell,
          width: CGFloat(w) * cell, height: CGFloat(h) * cell
        ).fill()
      }
      (attention ? accent : NSColor.black).setFill()
      fill(1, 0, 12, 6)  // body
      fill(0, 2, 1, 2)  // arms
      fill(13, 2, 1, 2)
      for x in legs { fill(x, 6, 1, 2) }
      // Eyes: punched out of the body.
      NSGraphicsContext.current?.compositingOperation = .clear
      fill(4, 2, 1, 2)
      fill(9, 2, 1, 2)
      return true
    }
    image.isTemplate = !attention
    return image
  }
}
