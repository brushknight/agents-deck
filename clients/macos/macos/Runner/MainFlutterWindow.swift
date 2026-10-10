import Cocoa
import FlutterMacOS

/// The deck's drop-down window: borderless, rounded, above other windows and
/// on every Space. AppDelegate positions it under the menu bar icon.
class MainFlutterWindow: NSWindow {
  static let side: CGFloat = 440

  override func awakeFromNib() {
    let flutter = FlutterViewController()
    flutter.backgroundColor = .clear
    contentViewController = flutter

    styleMask = [.borderless, .fullSizeContentView]
    setContentSize(NSSize(width: Self.side, height: Self.side))
    isOpaque = false
    backgroundColor = .clear
    hasShadow = true
    level = .popUpMenu
    collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .transient]
    isReleasedWhenClosed = false

    flutter.view.wantsLayer = true
    flutter.view.layer?.cornerRadius = 14
    flutter.view.layer?.masksToBounds = true

    RegisterGeneratedPlugins(registry: flutter)
    super.awakeFromNib()
  }

  // Borderless windows can't take keyboard focus unless they say so (Esc,
  // and clicks that should resign key when you click elsewhere).
  override var canBecomeKey: Bool { true }
  override var canBecomeMain: Bool { true }
}
