import Cocoa
import WebKit

/// The Settings window: the service's own Settings page (the dashboard on
/// 127.0.0.1) in a web view, with the window's title bar made part of the
/// page: no title, transparent, the traffic lights over the page's sidebar.
/// It only ever shows Settings: any other page of the dashboard is turned
/// back to /settings, a link to the deck shows the deck, and links to
/// anywhere else open in the default browser. AppDelegate gives the app a
/// Dock icon and a menu bar while it is open.
final class SettingsWindowController: NSWindowController, NSWindowDelegate,
  WKNavigationDelegate, WKUIDelegate
{
  static let title = "agents deck Settings"
  /// The page's background (settings.css --bg), so nothing flashes white.
  static let background = NSColor(red: 0x0A / 255.0, green: 0x0A / 255.0, blue: 0x0A / 255.0, alpha: 1)

  private let webView: WKWebView
  /// Drags the window from the title bar row over the sidebar; the page's
  /// status strip shares the row to its right and keeps its clicks.
  private let strip: DragStrip
  /// The dashboard's origin: http, the loopback host and its port.
  private var origin: (host: String, port: Int)?
  /// Where this window belongs: /settings#<page> on the dashboard.
  private var home: URL?
  /// The login link is loaded once; after that /login is never shown.
  private var loginPending = false
  var onClose: (() -> Void)?
  /// The page asked for the deck (the tray popover).
  var onShowDeck: (() -> Void)?
  /// The page asked for the dashboard in the default browser.
  var onOpenDashboard: (() -> Void)?

  init() {
    let window = NSWindow(
      contentRect: NSRect(x: 0, y: 0, width: 960, height: 660),
      styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
      backing: .buffered, defer: false)
    window.title = Self.title  // the Window menu and Mission Control still name it
    window.titleVisibility = .hidden
    window.titlebarAppearsTransparent = true
    // An empty toolbar makes the title bar as tall as the page's top strip,
    // with the traffic lights centred in it.
    let toolbar = NSToolbar(identifier: "settings")
    toolbar.showsBaselineSeparator = false
    window.toolbar = toolbar
    window.toolbarStyle = .unifiedCompact
    window.titlebarSeparatorStyle = .none
    window.appearance = NSAppearance(named: .darkAqua)
    window.backgroundColor = Self.background
    window.minSize = NSSize(width: 760, height: 560)
    window.isReleasedWhenClosed = false
    window.tabbingMode = .disallowed

    let top = max(28, window.frame.height - window.contentLayoutRect.height)
    let config = WKWebViewConfiguration()
    config.applicationNameForUserAgent = "AgentsDeckApp/macos"
    config.userContentController.addUserScript(
      WKUserScript(source: Self.bridge(top: top), injectionTime: .atDocumentStart, forMainFrameOnly: true))
    // The default (persistent) website data store keeps the dashboard's
    // session cookie between openings.
    let content = NSView(frame: NSRect(origin: .zero, size: window.contentRect(forFrameRect: window.frame).size))
    webView = WKWebView(frame: content.bounds, configuration: config)
    webView.autoresizingMask = [.width, .height]
    webView.setValue(false, forKey: "drawsBackground")
    webView.underPageBackgroundColor = Self.background
    content.addSubview(webView)
    // The strip under the title bar moves the window, as a title bar does.
    // It spans the whole row until the page says how wide its sidebar is.
    strip = DragStrip(
      frame: NSRect(x: 0, y: content.bounds.height - top, width: content.bounds.width, height: top))
    strip.autoresizingMask = [.width, .minYMargin]
    strip.scrollTarget = webView
    content.addSubview(strip)
    super.init(window: window)
    window.contentView = content
    window.delegate = self
    webView.navigationDelegate = self
    webView.uiDelegate = self
    config.userContentController.add(MessageProxy(self), name: "agentsDeck")
    window.center()
    // Remembers where the window was and how big (in the app's defaults).
    window.setFrameAutosaveName(Self.title)
  }

  required init?(coder: NSCoder) { nil }

  /// What the page learns at document start: it runs in the app (so it
  /// drops its own top bar), how tall the title bar is, and how to ask the
  /// app for the deck or the browser.
  static func bridge(top: CGFloat) -> String {
    """
    window.agentsDeckApp = Object.freeze({ platform: 'macos', titlebar: \(Int(top)), titlebarRow: true,
      post: function (m) { window.webkit.messageHandlers.agentsDeck.postMessage(m); } });
    (function () {
      var mark = function () {
        var e = document.documentElement;
        if (!e) return false;
        e.classList.add('in-app', 'app-macos', 'app-row');
        e.style.setProperty('--app-top', '\(Int(top))px');
        return true;
      };
      if (!mark()) new MutationObserver(function (m, o) { if (mark()) o.disconnect(); })
        .observe(document, { childList: true });
    })();
    """
  }

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
    home = Self.settingsHome(for: url)
    loginPending = url.path == "/login"
    webView.load(URLRequest(url: url))
    showWindow(nil)
    window?.makeKeyAndOrderFront(nil)
  }

  /// Where the window belongs for the URL Dart sent:
  /// `/login?code=…&next=/settings#boards` → `/settings#boards` on the same
  /// origin. Always a Settings page: anything else becomes `/settings`.
  static func settingsHome(for url: URL) -> URL? {
    guard var c = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return nil }
    var path = url.path
    if path == "/login" {
      path = c.queryItems?.first(where: { $0.name == "next" })?.value ?? ""
    }
    c.path = isSettingsPath(path) ? path : "/settings"
    c.query = nil
    return c.url
  }

  static func isSettingsPath(_ path: String) -> Bool {
    path == "/settings" || path == "/settings/"
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

  private func goHome() {
    guard let home else { return }
    NSLog("agents deck: Settings window back to %@", home.path)
    webView.load(URLRequest(url: home))
  }

  /// What the page asked for through window.agentsDeckApp.post.
  fileprivate func received(_ body: Any) {
    guard let m = body as? [String: Any], let action = m["action"] as? String else { return }
    switch action {
    case "deck": onShowDeck?()
    case "dashboard": onOpenDashboard?()
    case "titlebar":
      // The page's status strip sits in the title bar row right of its
      // sidebar: drag only over the sidebar, so the strip's links click.
      guard let left = (m["left"] as? NSNumber)?.doubleValue, left >= 0 else { return }
      let w = min(CGFloat(left), strip.superview?.bounds.width ?? CGFloat(left))
      strip.autoresizingMask = [.minYMargin]
      strip.frame.size.width = max(w, 80)
    default: break
    }
  }

  // MARK: WKNavigationDelegate

  func webView(
    _ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
    decisionHandler: @escaping (WKNavigationActionPolicy) -> Void
  ) {
    guard let url = action.request.url else { return decisionHandler(.cancel) }
    if url.absoluteString == "about:blank" { return decisionHandler(.allow) }
    guard isDashboard(url) else {
      decisionHandler(.cancel)
      if action.navigationType == .linkActivated { openOutside(url) }
      return
    }
    if action.targetFrame?.isMainFrame == false || Self.isSettingsPath(url.path) {
      return decisionHandler(.allow)
    }
    if url.path == "/login" && loginPending {
      loginPending = false
      return decisionHandler(.allow)
    }
    // Any other page of the dashboard never shows here: a link to the deck
    // shows the deck, anything else (a login that lands on the front page,
    // an old link) goes back to Settings.
    decisionHandler(.cancel)
    if action.navigationType == .linkActivated && url.path == "/" {
      onShowDeck?()
    } else {
      goHome()
    }
  }

  /// A login link that didn't work (used already, or expired): Settings
  /// itself, which works while the session cookie does and says so when not.
  func webView(
    _ webView: WKWebView, decidePolicyFor response: WKNavigationResponse,
    decisionHandler: @escaping (WKNavigationResponsePolicy) -> Void
  ) {
    if response.isForMainFrame, let r = response.response as? HTTPURLResponse,
      r.url?.path == "/login", r.statusCode >= 400
    {
      decisionHandler(.cancel)
      goHome()
      return
    }
    decisionHandler(.allow)
  }

  func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
    NSLog("agents deck: Settings window shows %@#%@", webView.url?.path ?? "", webView.url?.fragment ?? "")
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
    webView.configuration.userContentController.removeScriptMessageHandler(forName: "agentsDeck")
    onClose?()
  }
}

/// Holds the window weakly: WKUserContentController keeps its message
/// handlers alive.
private final class MessageProxy: NSObject, WKScriptMessageHandler {
  weak var target: SettingsWindowController?
  init(_ target: SettingsWindowController) { self.target = target }

  func userContentController(_ c: WKUserContentController, didReceive message: WKScriptMessage) {
    // Only the dashboard's own page may ask (the window loads nothing else).
    guard message.frameInfo.isMainFrame, message.frameInfo.request.url?.scheme == "http" else { return }
    target?.received(message.body)
  }
}

/// The strip along the top of the window, under the transparent title bar:
/// drag it to move the window, double-click it to zoom (or minimize, as set
/// in System Settings), as with any title bar. Scrolling passes through.
final class DragStrip: NSView {
  weak var scrollTarget: NSView?

  override var mouseDownCanMoveWindow: Bool { true }

  override func mouseDown(with event: NSEvent) {
    guard let window else { return }
    if event.clickCount == 2 {
      switch UserDefaults.standard.string(forKey: "AppleActionOnDoubleClick") {
      case "Minimize": window.performMiniaturize(nil)
      case "None": break
      default: window.performZoom(nil)
      }
      return
    }
    window.performDrag(with: event)
  }

  override func scrollWheel(with event: NSEvent) {
    scrollTarget?.scrollWheel(with: event)
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
      .separator(),
      item("Show the Deck", #selector(AppDelegate.showDeckFromMenu(_:)), "d", [.command, .shift]),
    ])
    return main
  }
}
