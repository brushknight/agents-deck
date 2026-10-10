import Cocoa
import FlutterMacOS

/// agents deck in the menu bar: a critter icon (orange when an agent needs
/// you) that drops the deck down underneath it. No Dock icon, no main menu.
@main
class AppDelegate: FlutterAppDelegate, NSWindowDelegate {
  private var statusItem: NSStatusItem!
  private var channel: FlutterMethodChannel?
  private var attention = 0

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
    let open = NSMenuItem(title: "Open dashboard in browser", action: #selector(openDashboard), keyEquivalent: "")
    open.target = self
    menu.addItem(open)
    menu.addItem(.separator())
    menu.addItem(withTitle: "Quit agents deck", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
    statusItem.menu = menu
    statusItem.button?.performClick(nil)  // pops the menu up under the icon
    statusItem.menu = nil  // a left click goes back to toggling the deck
  }

  /// `agentctl web`: opens the dashboard with a one-time login link.
  @objc private func openDashboard() {
    let home = FileManager.default.homeDirectoryForCurrentUser.path
    let candidates = ["\(home)/.local/bin/agentctl", "/opt/homebrew/bin/agentctl", "/usr/local/bin/agentctl"]
    guard let path = candidates.first(where: { FileManager.default.isExecutableFile(atPath: $0) }) else { return }
    let p = Process()
    p.executableURL = URL(fileURLWithPath: path)
    p.arguments = ["web"]
    try? p.run()
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
    // Pinned to the top-right corner of the screen with the menu bar icon,
    // just under the menu bar (visibleFrame already leaves it out).
    let screen = (statusItem.button?.window?.screen ?? NSScreen.main)?.visibleFrame ?? .zero
    let size = window.frame.size
    let margin: CGFloat = 8
    window.setFrameOrigin(
      NSPoint(x: screen.maxX - size.width - margin, y: screen.maxY - size.height - margin))
    NSApp.activate(ignoringOtherApps: true)
    window.makeKeyAndOrderFront(nil)
    channel?.invokeMethod("visible", arguments: true)
  }

  private func hideDeck() {
    mainFlutterWindow?.orderOut(nil)
    channel?.invokeMethod("visible", arguments: false)
  }

  /// Clicking anywhere else puts the deck away, like any menu bar popover.
  func windowDidResignKey(_ notification: Notification) {
    hideDeck()
  }
}

/// The menu bar icon: the deck's pixel critter. A template image (it follows
/// the menu bar's light or dark look); orange when an agent needs you.
enum CritterIcon {
  // Body rows (y 0..6) as [x0, x1) spans on a 14-wide grid; eyes at x 4 and
  // 9 (rows 2-3); legs (rows 7-8) at x 2, 4, 9, 11.
  private static let rows: [(Int, Int)] = [(2, 12), (1, 13), (0, 14), (0, 14), (1, 13), (1, 13), (2, 12)]
  private static let legs = [2, 4, 9, 11]
  private static let accent = NSColor(red: 1, green: 0x4D / 255.0, blue: 0, alpha: 1)

  static func image(attention: Bool) -> NSImage {
    let cell: CGFloat = 1.25
    let size = NSSize(width: 18, height: 18)
    let image = NSImage(size: size, flipped: true) { _ in
      let ox = (size.width - 14 * cell) / 2
      let oy = (size.height - 9 * cell) / 2
      func fill(_ x: Int, _ y: Int, _ w: Int, _ h: Int) {
        NSRect(
          x: ox + CGFloat(x) * cell, y: oy + CGFloat(y) * cell,
          width: CGFloat(w) * cell, height: CGFloat(h) * cell
        ).fill()
      }
      (attention ? accent : NSColor.black).setFill()
      for (y, span) in rows.enumerated() { fill(span.0, y, span.1 - span.0, 1) }
      for x in legs { fill(x, 7, 1, 2) }
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
