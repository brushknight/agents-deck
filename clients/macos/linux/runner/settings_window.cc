#include "settings_window.h"

#include <gio/gio.h>
#ifdef HAVE_WEBKIT
#include <webkit2/webkit2.h>
#endif

// "http://127.0.0.1:7340" for a dashboard URL, else nullptr. Free with g_free.
static char* dashboard_origin(const char* url) {
  if (url == nullptr) return nullptr;
  g_autoptr(GUri) uri = g_uri_parse(url, G_URI_FLAGS_NONE, nullptr);
  if (uri == nullptr) return nullptr;
  const char* host = g_uri_get_host(uri);
  const int port = g_uri_get_port(uri);
  if (g_strcmp0(g_uri_get_scheme(uri), "http") != 0 || port <= 0 ||
      g_uri_get_userinfo(uri) != nullptr ||
      (g_strcmp0(host, "127.0.0.1") != 0 && g_strcmp0(host, "localhost") != 0)) {
    return nullptr;
  }
  return g_strdup_printf("http://%s:%d", host, port);
}

gboolean settings_window_is_dashboard(const char* url) {
  g_autofree char* origin = dashboard_origin(url);
  return origin != nullptr;
}

// Web pages and mail links that aren't the dashboard: the default browser.
static void open_outside(const char* url) {
  if (url == nullptr || !(g_str_has_prefix(url, "http://") ||
                          g_str_has_prefix(url, "https://") ||
                          g_str_has_prefix(url, "mailto:"))) {
    return;
  }
  g_autoptr(GError) error = nullptr;
  if (g_app_info_launch_default_for_uri(url, nullptr, &error)) return;
  // No default handler through GIO (a bare window manager): xdg-open.
  char* argv[] = {const_cast<char*>("xdg-open"), const_cast<char*>(url),
                  nullptr};
  g_autoptr(GError) spawn_error = nullptr;
  if (!g_spawn_async(nullptr, argv, nullptr, G_SPAWN_SEARCH_PATH, nullptr,
                     nullptr, nullptr, &spawn_error)) {
    g_warning("agents deck: could not open %s: %s", url, spawn_error->message);
  }
}

static struct {
  SettingsActionFunc func;
  gpointer data;
} actions;

void settings_window_on_action(SettingsActionFunc func, gpointer data) {
  actions.func = func;
  actions.data = data;
}

G_GNUC_UNUSED static void ask_app(const char* action) {
  if (actions.func != nullptr) actions.func(action, actions.data);
}

#ifdef HAVE_WEBKIT

static const char kTitle[] = "agents deck Settings";

// What the page learns at document start: it runs in the app (so it drops
// its own top bar; the header bar is the title), and how to ask the app for
// the deck or the browser.
static const char kBridge[] =
    "window.agentsDeckApp = Object.freeze({ platform: 'linux',"
    "  post: function (m) { window.webkit.messageHandlers.agentsDeck.postMessage(m); } });"
    "(function () {"
    "  var mark = function () { var e = document.documentElement; if (!e) return false;"
    "    e.classList.add('in-app', 'app-linux'); return true; };"
    "  if (!mark()) new MutationObserver(function (m, o) { if (mark()) o.disconnect(); })"
    "    .observe(document, { childList: true });"
    "})();";

static struct {
  GtkWindow* window;
  WebKitWebView* view;
  char* origin;            // the dashboard's origin; nothing else loads
  char* home;              // /settings#page on that origin: where the window belongs
  gboolean login_pending;  // the login link loads once
} settings;

static gboolean is_dashboard(const char* url) {
  g_autofree char* origin = dashboard_origin(url);
  return origin != nullptr && g_strcmp0(origin, settings.origin) == 0;
}

static gboolean is_settings_path(const char* path) {
  return g_strcmp0(path, "/settings") == 0 || g_strcmp0(path, "/settings/") == 0;
}

// Where the window belongs for the URL Dart sent:
// `/login?code=…&next=/settings#boards` → `/settings#boards` on the same
// origin. Always a Settings page: anything else becomes `/settings`.
static char* settings_home(const char* url) {
  g_autoptr(GUri) uri = g_uri_parse(url, G_URI_FLAGS_NONE, nullptr);
  g_autofree char* origin = dashboard_origin(url);
  if (uri == nullptr || origin == nullptr) return nullptr;
  const char* path = g_uri_get_path(uri);
  g_autoptr(GHashTable) params = nullptr;
  if (g_strcmp0(path, "/login") == 0 && g_uri_get_query(uri) != nullptr) {
    params = g_uri_parse_params(g_uri_get_query(uri), -1, "&",
                                G_URI_PARAMS_NONE, nullptr);
    path = params ? static_cast<const char*>(g_hash_table_lookup(params, "next"))
                  : nullptr;
  }
  if (!is_settings_path(path)) path = "/settings";
  const char* fragment = g_uri_get_fragment(uri);
  return g_strconcat(origin, path, fragment ? "#" : "", fragment ? fragment : "",
                     nullptr);
}

static char* path_of(const char* url) {
  g_autoptr(GUri) uri = g_uri_parse(url, G_URI_FLAGS_NONE, nullptr);
  return uri ? g_strdup(g_uri_get_path(uri)) : nullptr;
}

static void go_home() {
  if (settings.view == nullptr || settings.home == nullptr) return;
  g_message("agents deck: Settings window back to Settings");
  webkit_web_view_load_uri(settings.view, settings.home);
}

static gboolean on_decide_policy(WebKitWebView* view,
                                 WebKitPolicyDecision* decision,
                                 WebKitPolicyDecisionType type,
                                 gpointer data) {
  if (type == WEBKIT_POLICY_DECISION_TYPE_RESPONSE) {
    // A login link that didn't work (used already, or expired): Settings
    // itself, which works while the session cookie does.
    WebKitURIResponse* r = webkit_response_policy_decision_get_response(
        WEBKIT_RESPONSE_POLICY_DECISION(decision));
    g_autofree char* path = path_of(webkit_uri_response_get_uri(r));
    if (webkit_response_policy_decision_is_main_frame_main_resource(
            WEBKIT_RESPONSE_POLICY_DECISION(decision)) &&
        g_strcmp0(path, "/login") == 0 &&
        webkit_uri_response_get_status_code(r) >= 400) {
      webkit_policy_decision_ignore(decision);
      go_home();
      return TRUE;
    }
    return FALSE;
  }
  WebKitNavigationAction* action =
      webkit_navigation_policy_decision_get_navigation_action(
          WEBKIT_NAVIGATION_POLICY_DECISION(decision));
  const char* url =
      webkit_uri_request_get_uri(webkit_navigation_action_get_request(action));
  const gboolean clicked = webkit_navigation_action_get_navigation_type(action) ==
                           WEBKIT_NAVIGATION_TYPE_LINK_CLICKED;
  if (g_strcmp0(url, "about:blank") == 0) {
    webkit_policy_decision_use(decision);
    return TRUE;
  }
  if (!is_dashboard(url)) {
    webkit_policy_decision_ignore(decision);
    if (clicked || type == WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION) {
      open_outside(url);
    }
    return TRUE;
  }
  g_autofree char* path = path_of(url);
  if (type == WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION) {
    if (is_settings_path(path)) {
      webkit_policy_decision_use(decision);
      return TRUE;
    }
    if (g_strcmp0(path, "/login") == 0 && settings.login_pending) {
      settings.login_pending = FALSE;
      webkit_policy_decision_use(decision);
      return TRUE;
    }
  }
  // Any other page of the dashboard never shows here: a link to the deck
  // asks for the deck; anything else (a login that lands on the front page,
  // an old link) goes back to Settings.
  webkit_policy_decision_ignore(decision);
  if (clicked && g_strcmp0(path, "/") == 0) {
    ask_app("deck");
  } else if (is_settings_path(path)) {
    webkit_web_view_load_uri(view, url);  // a new-window link to Settings
  } else {
    go_home();
  }
  return TRUE;
}

// window.agentsDeckApp.post({action}) from the page.
static void on_page_message(WebKitUserContentManager* manager,
                            WebKitJavascriptResult* result, gpointer data) {
  JSCValue* value = webkit_javascript_result_get_js_value(result);
  if (!jsc_value_is_object(value)) return;
  g_autoptr(JSCValue) action = jsc_value_object_get_property(value, "action");
  if (!jsc_value_is_string(action)) return;
  g_autofree char* name = jsc_value_to_string(action);
  if (g_strcmp0(name, "deck") == 0 || g_strcmp0(name, "dashboard") == 0) {
    ask_app(name);
  }
}

static void on_load_changed(WebKitWebView* view, WebKitLoadEvent event,
                            gpointer data) {
  if (event != WEBKIT_LOAD_FINISHED) return;
  g_autofree char* path = path_of(webkit_web_view_get_uri(view));
  g_message("agents deck: Settings window shows %s", path ? path : "");
}

// Ctrl+W closes the window, Ctrl+R (and F5) reloads the page.
static gboolean on_key(GtkWidget* widget, GdkEventKey* event, gpointer data) {
  const gboolean ctrl = (event->state & GDK_CONTROL_MASK) != 0;
  if (ctrl && (event->keyval == GDK_KEY_w || event->keyval == GDK_KEY_W)) {
    gtk_window_close(settings.window);
    return TRUE;
  }
  if ((ctrl && (event->keyval == GDK_KEY_r || event->keyval == GDK_KEY_R)) ||
      event->keyval == GDK_KEY_F5) {
    webkit_web_view_reload(settings.view);
    return TRUE;
  }
  return FALSE;
}

// The window's size, kept in ~/.config/agents-deck/settings-window.ini.
static char* size_file() {
  return g_build_filename(g_get_user_config_dir(), "agents-deck",
                          "settings-window.ini", nullptr);
}

static void load_size(int* width, int* height) {
  g_autofree char* path = size_file();
  g_autoptr(GKeyFile) kf = g_key_file_new();
  if (!g_key_file_load_from_file(kf, path, G_KEY_FILE_NONE, nullptr)) return;
  const int w = g_key_file_get_integer(kf, "window", "width", nullptr);
  const int h = g_key_file_get_integer(kf, "window", "height", nullptr);
  if (w >= 760 && h >= 560) {
    *width = w;
    *height = h;
  }
}

static gboolean on_delete(GtkWidget* widget, GdkEvent* event, gpointer data) {
  int w = 0, h = 0;
  gtk_window_get_size(settings.window, &w, &h);
  g_autofree char* path = size_file();
  g_autofree char* dir = g_path_get_dirname(path);
  g_mkdir_with_parents(dir, 0700);
  g_autoptr(GKeyFile) kf = g_key_file_new();
  g_key_file_set_integer(kf, "window", "width", w);
  g_key_file_set_integer(kf, "window", "height", h);
  g_key_file_save_to_file(kf, path, nullptr);
  return FALSE;  // and close
}

static void on_destroy(GtkWidget* widget, gpointer data) {
  settings.window = nullptr;
  settings.view = nullptr;
  g_clear_pointer(&settings.origin, g_free);
  g_clear_pointer(&settings.home, g_free);
}

void settings_window_open(GtkApplication* app, const char* url) {
  g_autofree char* origin = dashboard_origin(url);
  if (origin == nullptr) return;
  if (settings.window == nullptr) {
    GtkWindow* window = GTK_WINDOW(gtk_application_window_new(app));
    gtk_window_set_title(window, kTitle);
    // One title bar: the desktop's own, as a header bar, dark like the page.
    g_object_set(gtk_widget_get_settings(GTK_WIDGET(window)),
                 "gtk-application-prefer-dark-theme", TRUE, nullptr);
    GtkWidget* bar = gtk_header_bar_new();
    gtk_header_bar_set_title(GTK_HEADER_BAR(bar), kTitle);
    gtk_header_bar_set_show_close_button(GTK_HEADER_BAR(bar), TRUE);
    gtk_window_set_titlebar(window, bar);
    int width = 960, height = 660;
    load_size(&width, &height);
    gtk_window_set_default_size(window, width, height);
    gtk_widget_set_size_request(GTK_WIDGET(window), 760, 560);
    gtk_window_set_position(window, GTK_WIN_POS_CENTER);
    WebKitUserContentManager* content = webkit_user_content_manager_new();
    g_autoptr(WebKitUserScript) bridge = webkit_user_script_new(
        kBridge, WEBKIT_USER_CONTENT_INJECT_TOP_FRAME,
        WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START, nullptr, nullptr);
    webkit_user_content_manager_add_script(content, bridge);
    g_signal_connect(content, "script-message-received::agentsDeck",
                     G_CALLBACK(on_page_message), nullptr);
    webkit_user_content_manager_register_script_message_handler(content,
                                                                "agentsDeck");
    GtkWidget* view = webkit_web_view_new_with_user_content_manager(content);
    g_object_unref(content);  // the view holds it
    webkit_settings_set_user_agent_with_application_details(
        webkit_web_view_get_settings(WEBKIT_WEB_VIEW(view)), "AgentsDeckApp",
        "linux");
    const GdkRGBA background = {0x0A / 255.0, 0x0A / 255.0, 0x0A / 255.0, 1};
    webkit_web_view_set_background_color(WEBKIT_WEB_VIEW(view), &background);
    g_signal_connect(view, "decide-policy", G_CALLBACK(on_decide_policy),
                     nullptr);
    g_signal_connect(view, "load-changed", G_CALLBACK(on_load_changed),
                     nullptr);
    gtk_container_add(GTK_CONTAINER(window), view);
    g_signal_connect(window, "key-press-event", G_CALLBACK(on_key), nullptr);
    g_signal_connect(window, "delete-event", G_CALLBACK(on_delete), nullptr);
    g_signal_connect(window, "destroy", G_CALLBACK(on_destroy), nullptr);
    settings.window = window;
    settings.view = WEBKIT_WEB_VIEW(view);
    gtk_widget_show_all(GTK_WIDGET(window));
  }
  g_free(settings.origin);
  settings.origin = g_strdup(origin);
  g_free(settings.home);
  settings.home = settings_home(url);
  g_autofree char* path = path_of(url);
  settings.login_pending = g_strcmp0(path, "/login") == 0;
  webkit_web_view_load_uri(settings.view, url);
  gtk_window_present(settings.window);
}

void settings_window_close() {
  if (settings.window != nullptr) {
    gtk_widget_destroy(GTK_WIDGET(settings.window));
  }
}

#else  // no WebKitGTK: the default browser

void settings_window_open(GtkApplication* app, const char* url) {
  if (settings_window_is_dashboard(url)) open_outside(url);
}

void settings_window_close() {}

#endif
