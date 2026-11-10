import Cocoa
import WebKit

/// The Settings window: the service's own Settings page (the dashboard on
/// 127.0.0.1) in a web view. It loads nothing but that one address; links to
/// anywhere else open in the default browser. AppDelegate gives the app a
/// Dock icon and a menu bar while it is open.
final class SettingsWindowController: NSWindowController, NSWindowDelegate,
  WKNavigationDelegate, WKUIDelegate
{
  static let title = "agents deck Settings"

  private let webView: WKWebView
  /// The dashboard's origin: http, the loopback host and its port.
  private var origin: (host: String, port: Int)?
  /// Where to go once the login link has set the session cookie, for a
  /// service that sends every login to its front page.
  private var landing: URL?
  var onClose: (() -> Void)?

  init() {
    let window = NSWindow(
      contentRect: NSRect(x: 0, y: 0, width: 900, height: 640),
      styleMask: [.titled, .closable, .miniaturizable, .resizable],
      backing: .buffered, defer: false)
    window.title = Self.title
    window.minSize = NSSize(width: 560, height: 400)
    window.isReleasedWhenClosed = false
    window.tabbingMode = .disallowed
    // The default (persistent) website data store keeps the dashboard's
    // session cookie between openings.
    webView = WKWebView(frame: window.contentLayoutRect, configuration: WKWebViewConfiguration())
    webView.autoresizingMask = [.width, .height]
    super.init(window: window)
    window.contentView = webView
    window.delegate = self
    webView.navigationDelegate = self
    webView.uiDelegate = self
    window.center()
    // Remembers where the window was and how big (in the app's defaults).
    window.setFrameAutosaveName(Self.title)
  }

  required init?(coder: NSCoder) { nil }

  /// The URL as Dart sent it, if it is the dashboard on this computer:
  /// http://127.0.0.1:<port>/... (or localhost). Nothing else is ever loaded.
  static func dashboardURL(_ s: String) -> URL? {
    guard let url = URL(string: s), url.scheme == "http", url.port != nil,
      let host = url.host, host == "127.0.0.1" || host == "localhost",
      url.user == nil, url.password == nil
    else { return nil }
    return url
  }

  func open(_ url: URL) {
    origin = (url.host ?? "", url.port ?? 80)
    landing = Self.landing(for: url)
    webView.load(URLRequest(url: url))
    showWindow(nil)
    window?.makeKeyAndOrderFront(nil)
  }

  /// `/login?code=…&next=/settings#boards` → `/settings#boards` on the same
  /// origin; nil for any other URL.
  static func landing(for url: URL) -> URL? {
    guard url.path == "/login",
      var c = URLComponents(url: url, resolvingAgainstBaseURL: false),
      let next = c.queryItems?.first(where: { $0.name == "next" })?.value,
      next.hasPrefix("/"), !next.hasPrefix("//")
    else { return nil }
    c.path = next
    c.query = nil
    return c.url
  }

  private func isDashboard(_ url: URL) -> Bool {
    guard let o = origin else { return false }
    return url.scheme == "http" && url.host == o.host && (url.port ?? 80) == o.port
  }

  private func openOutside(_ url: URL) {
    if ["http", "https", "mailto"].contains(url.scheme ?? "") {
      NSWorkspace.shared.open(url)
    }
  }

  // MARK: WKNavigationDelegate

  func webView(
    _ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
    decisionHandler: @escaping (WKNavigationActionPolicy) -> Void
  ) {
    guard let url = action.request.url else { return decisionHandler(.cancel) }
    if isDashboard(url) || url.absoluteString == "about:blank" {
      return decisionHandler(.allow)
    }
    decisionHandler(.cancel)
    if action.navigationType == .linkActivated { openOutside(url) }
  }

  func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
    // A service that ignores `next` lands the login on its front page; go on
    // to Settings from there (the session cookie is set by now).
    guard let target = landing, let now = webView.url else { return }
    landing = nil
    if now.path != target.path { webView.load(URLRequest(url: target)) }
  }

  // MARK: WKUIDelegate

  /// Links that ask for a new window: the dashboard's own load here, the
  /// rest go to the browser.
  func webView(
    _ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
    for action: WKNavigationAction, windowFeatures: WKWindowFeatures
  ) -> WKWebView? {
    if let url = action.request.url {
      if isDashboard(url) { webView.load(action.request) } else { openOutside(url) }
    }
    return nil
  }

  func webView(
    _ webView: WKWebView, runJavaScriptAlertPanelWithMessage message: String,
    initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping () -> Void
  ) {
    let alert = NSAlert()
    alert.messageText = message
    alert.addButton(withTitle: "OK")
    present(alert) { _ in completionHandler() }
  }

  func webView(
    _ webView: WKWebView, runJavaScriptConfirmPanelWithMessage message: String,
    initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping (Bool) -> Void
  ) {
    let alert = NSAlert()
    alert.messageText = message
    alert.addButton(withTitle: "OK")
    alert.addButton(withTitle: "Cancel")
    present(alert) { completionHandler($0 == .alertFirstButtonReturn) }
  }

  func webView(
    _ webView: WKWebView, runOpenPanelWith parameters: WKOpenPanelParameters,
    initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping ([URL]?) -> Void
  ) {
    let panel = NSOpenPanel()
    panel.allowsMultipleSelection = parameters.allowsMultipleSelection
    panel.canChooseDirectories = parameters.allowsDirectories
    guard let window else { return completionHandler(nil) }
    panel.beginSheetModal(for: window) { completionHandler($0 == .OK ? panel.urls : nil) }
  }

  private func present(_ alert: NSAlert, _ done: @escaping (NSApplication.ModalResponse) -> Void) {
    if let window { alert.beginSheetModal(for: window, completionHandler: done) } else {
      done(alert.runModal())
    }
  }

  // MARK: NSWindowDelegate

  func windowWillClose(_ notification: Notification) {
    onClose?()
  }
}

/// The menu bar while the Settings window is open: enough for Cmd-W, Cmd-Q,
/// copy and paste, and reloading the page.
enum AppMenu {
  static func make() -> NSMenu {
    let main = NSMenu()
    func submenu(_ title: String, _ items: [NSMenuItem]) {
      let menu = NSMenu(title: title)
      items.forEach(menu.addItem)
      let item = NSMenuItem(title: title, action: nil, keyEquivalent: "")
      item.submenu = menu
      main.addItem(item)
    }
    func item(_ title: String, _ action: Selector, _ key: String, _ mods: NSEvent.ModifierFlags = .command) -> NSMenuItem {
      let i = NSMenuItem(title: title, action: action, keyEquivalent: key)
      i.keyEquivalentModifierMask = mods
      return i
    }
    submenu("agents deck", [
      item("About agents deck", #selector(NSApplication.orderFrontStandardAboutPanel(_:)), ""),
      .separator(),
      item("Hide agents deck", #selector(NSApplication.hide(_:)), "h"),
      .separator(),
      item("Quit agents deck", #selector(NSApplication.terminate(_:)), "q"),
    ])
    submenu("Edit", [
      item("Undo", Selector(("undo:")), "z"),
      item("Redo", Selector(("redo:")), "z", [.command, .shift]),
      .separator(),
      item("Cut", #selector(NSText.cut(_:)), "x"),
      item("Copy", #selector(NSText.copy(_:)), "c"),
      item("Paste", #selector(NSText.paste(_:)), "v"),
      item("Select All", #selector(NSText.selectAll(_:)), "a"),
    ])
    submenu("View", [
      item("Reload", #selector(WKWebView.reload(_:)), "r")
    ])
    submenu("Window", [
      item("Close", #selector(NSWindow.performClose(_:)), "w"),
      item("Minimize", #selector(NSWindow.performMiniaturize(_:)), "m"),
    ])
    return main
  }
}
