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

#ifdef HAVE_WEBKIT

static const char kTitle[] = "agents deck Settings";

static struct {
  GtkWindow* window;
  WebKitWebView* view;
  char* origin;   // the dashboard's origin; nothing else loads
  char* landing;  // /settings#page, once the login link has set the cookie
} settings;

static gboolean is_dashboard(const char* url) {
  g_autofree char* origin = dashboard_origin(url);
  return origin != nullptr && g_strcmp0(origin, settings.origin) == 0;
}

// `/login?code=…&next=/settings#boards` → the same origin's
// `/settings#boards`; nullptr for any other URL.
static char* landing_for(const char* url) {
  g_autoptr(GUri) uri = g_uri_parse(url, G_URI_FLAGS_NONE, nullptr);
  if (uri == nullptr || g_strcmp0(g_uri_get_path(uri), "/login") != 0 ||
      g_uri_get_query(uri) == nullptr) {
    return nullptr;
  }
  g_autoptr(GHashTable) params = g_uri_parse_params(
      g_uri_get_query(uri), -1, "&", G_URI_PARAMS_NONE, nullptr);
  const char* next =
      params ? static_cast<const char*>(g_hash_table_lookup(params, "next"))
             : nullptr;
  if (next == nullptr || next[0] != '/' || next[1] == '/') return nullptr;
  const char* fragment = g_uri_get_fragment(uri);
  g_autofree char* origin = dashboard_origin(url);
  return g_strconcat(origin, next, fragment ? "#" : "", fragment ? fragment : "",
                     nullptr);
}

static char* path_of(const char* url) {
  g_autoptr(GUri) uri = g_uri_parse(url, G_URI_FLAGS_NONE, nullptr);
  return uri ? g_strdup(g_uri_get_path(uri)) : nullptr;
}

static gboolean on_decide_policy(WebKitWebView* view,
                                 WebKitPolicyDecision* decision,
                                 WebKitPolicyDecisionType type,
                                 gpointer data) {
  if (type == WEBKIT_POLICY_DECISION_TYPE_RESPONSE) return FALSE;
  WebKitNavigationAction* action =
      webkit_navigation_policy_decision_get_navigation_action(
          WEBKIT_NAVIGATION_POLICY_DECISION(decision));
  const char* url =
      webkit_uri_request_get_uri(webkit_navigation_action_get_request(action));
  if (type == WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION) {
    if (is_dashboard(url) || g_strcmp0(url, "about:blank") == 0) {
      webkit_policy_decision_use(decision);
      return TRUE;
    }
    webkit_policy_decision_ignore(decision);
    if (webkit_navigation_action_get_navigation_type(action) ==
        WEBKIT_NAVIGATION_TYPE_LINK_CLICKED) {
      open_outside(url);
    }
    return TRUE;
  }
  // A link that asks for a new window: the dashboard's own load here, the
  // rest go to the browser.
  webkit_policy_decision_ignore(decision);
  if (is_dashboard(url)) {
    webkit_web_view_load_uri(view, url);
  } else {
    open_outside(url);
  }
  return TRUE;
}

// A service that ignores `next` sends the login to its front page: go on to
// Settings from there (the session cookie is set by now).
static void on_load_changed(WebKitWebView* view, WebKitLoadEvent event,
                            gpointer data) {
  if (event != WEBKIT_LOAD_FINISHED || settings.landing == nullptr) return;
  g_autofree char* target = settings.landing;
  settings.landing = nullptr;
  g_autofree char* now = path_of(webkit_web_view_get_uri(view));
  g_autofree char* want = path_of(target);
  if (g_strcmp0(now, want) != 0) webkit_web_view_load_uri(view, target);
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
  if (w >= 560 && h >= 400) {
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
  g_clear_pointer(&settings.landing, g_free);
}

void settings_window_open(GtkApplication* app, const char* url) {
  g_autofree char* origin = dashboard_origin(url);
  if (origin == nullptr) return;
  if (settings.window == nullptr) {
    GtkWindow* window = GTK_WINDOW(gtk_application_window_new(app));
    gtk_window_set_title(window, kTitle);
    int width = 900, height = 640;
    load_size(&width, &height);
    gtk_window_set_default_size(window, width, height);
    gtk_window_set_position(window, GTK_WIN_POS_CENTER);
    GtkWidget* view = webkit_web_view_new();
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
  g_free(settings.landing);
  settings.landing = landing_for(url);
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
