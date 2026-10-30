#include "my_application.h"

#include <flutter_linux/flutter_linux.h>
#ifdef GDK_WINDOWING_X11
#include <gdk/gdkx.h>
#endif

#include "flutter/generated_plugin_registrant.h"
#include "tray.h"

// agents deck in the system tray: a critter icon (orange when an agent needs
// you) that opens the deck in a small borderless window next to it. The
// Linux twin of macos/Runner/AppDelegate.swift: the same "agents_deck/menubar"
// channel, so lib/main.dart runs unchanged on both.

static const int kSide = 440;    // the window, in logical pixels (as on macOS)
static const int kMargin = 8;    // from the screen edges and the panel
static const char kBackground[] = "#0A0A0A";  // DeckHud.bg

struct _MyApplication {
  GtkApplication parent_instance;
  char** dart_entrypoint_arguments;

  GtkWindow* window;
  FlMethodChannel* channel;
  Tray* tray;
  gboolean visible;
  gboolean had_focus;   // focus arrived since the deck was shown
  gint64 hidden_at;     // when focus loss last put the deck away
  gboolean tray_host;   // a tray is there to show the icon
  gboolean background;  // --background: started at login, wait for the tray
  guint no_tray_timer;
};

G_DEFINE_TYPE(MyApplication, my_application, GTK_TYPE_APPLICATION)

static void tell_dart_visible(MyApplication* self) {
  if (self->channel == nullptr) return;
  g_autoptr(FlValue) arg = fl_value_new_bool(self->visible);
  fl_method_channel_invoke_method(self->channel, "visible", arg, nullptr,
                                  nullptr, nullptr);
}

// Where the deck goes: next to the pointer (which is on the tray icon when
// it was clicked), kept inside the monitor's work area, under a top panel or
// above a bottom one. Without a pointer position (Wayland), centred at the
// top. Wayland compositors place windows themselves, so this is a hint there.
static void place_deck(MyApplication* self) {
  GdkDisplay* display = gtk_widget_get_display(GTK_WIDGET(self->window));
  gint px = 0, py = 0;
  gboolean have_pointer = FALSE;
#ifdef GDK_WINDOWING_X11
  if (GDK_IS_X11_DISPLAY(display)) {
    GdkSeat* seat = gdk_display_get_default_seat(display);
    GdkDevice* pointer = seat ? gdk_seat_get_pointer(seat) : nullptr;
    if (pointer != nullptr) {
      gdk_device_get_position(pointer, nullptr, &px, &py);
      have_pointer = TRUE;
    }
  }
#endif
  GdkMonitor* monitor =
      have_pointer ? gdk_display_get_monitor_at_point(display, px, py)
                   : gdk_display_get_primary_monitor(display);
  if (monitor == nullptr) monitor = gdk_display_get_monitor(display, 0);
  if (monitor == nullptr) return;
  GdkRectangle area;
  gdk_monitor_get_workarea(monitor, &area);

  int x, y;
  if (have_pointer) {
    x = CLAMP(px - kSide / 2, area.x + kMargin,
              MAX(area.x + kMargin, area.x + area.width - kSide - kMargin));
    const gboolean top_half = py < area.y + area.height / 2;
    y = top_half ? area.y + kMargin
                 : area.y + area.height - kSide - kMargin;
  } else {
    x = area.x + (area.width - kSide) / 2;
    y = area.y + kMargin;
  }
  gtk_window_move(self->window, x, y);
}

static void show_deck(MyApplication* self) {
  if (self->window == nullptr) return;
  place_deck(self);
  self->had_focus = FALSE;
  gtk_widget_show(GTK_WIDGET(self->window));
  guint32 when = GDK_CURRENT_TIME;
#ifdef GDK_WINDOWING_X11
  GdkWindow* gdk_window = gtk_widget_get_window(GTK_WIDGET(self->window));
  if (gdk_window != nullptr && GDK_IS_X11_WINDOW(gdk_window)) {
    // A fresh timestamp, so focus-stealing prevention lets the deck have the
    // keyboard (Esc closes it, clicking elsewhere puts it away).
    when = gdk_x11_get_server_time(gdk_window);
  }
#endif
  gtk_window_present_with_time(self->window, when);
  self->visible = TRUE;
  tell_dart_visible(self);
}

static void hide_deck(MyApplication* self) {
  if (self->window == nullptr || !self->visible) return;
  gtk_widget_hide(GTK_WIDGET(self->window));
  self->visible = FALSE;
  tell_dart_visible(self);
}

static void toggle_deck(MyApplication* self) {
  // Clicking the tray icon takes focus from the deck first, which already put
  // it away: that click meant "close", not "open again".
  const gboolean just_hidden =
      g_get_monotonic_time() - self->hidden_at < 400 * G_TIME_SPAN_MILLISECOND;
  if (self->visible) {
    hide_deck(self);
  } else if (!just_hidden) {
    show_deck(self);
  }
}

// `agentctl web`: opens the dashboard with a one-time login link.
static void open_dashboard() {
  g_autofree char* local =
      g_build_filename(g_get_home_dir(), ".local", "bin", "agentctl", nullptr);
  const char* candidates[] = {local, "/usr/local/bin/agentctl",
                              "/usr/bin/agentctl"};
  g_autofree char* found = nullptr;
  for (const char* c : candidates) {
    if (g_file_test(c, G_FILE_TEST_IS_EXECUTABLE)) {
      found = g_strdup(c);
      break;
    }
  }
  if (found == nullptr) found = g_find_program_in_path("agentctl");
  if (found == nullptr) {
    g_warning("agents deck: agentctl not found");
    return;
  }
  char* argv[] = {found, const_cast<char*>("web"), nullptr};
  g_autoptr(GError) error = nullptr;
  if (!g_spawn_async(nullptr, argv, nullptr, G_SPAWN_DEFAULT, nullptr, nullptr,
                     nullptr, &error)) {
    g_warning("agents deck: agentctl web: %s", error->message);
  }
}

// Takes the window (and with it the Flutter engine) down before the process
// ends, so no engine thread is left running during exit.
static void quit_app(MyApplication* self) {
  g_clear_object(&self->channel);
  if (self->window != nullptr) {
    GtkWidget* window = GTK_WIDGET(self->window);
    self->window = nullptr;
    gtk_widget_destroy(window);
  }
  g_application_quit(G_APPLICATION(self));
}

static void on_tray_action(TrayAction action, gpointer data) {
  MyApplication* self = MY_APPLICATION(data);
  switch (action) {
    case TRAY_TOGGLE:
      toggle_deck(self);
      break;
    case TRAY_SHOW:
      show_deck(self);
      break;
    case TRAY_DASHBOARD:
      open_dashboard();
      break;
    case TRAY_QUIT:
      quit_app(self);
      break;
  }
}

static void on_tray_host(gboolean present, gpointer data) {
  MY_APPLICATION(data)->tray_host = present;
}

// No tray after a few seconds (GNOME without the AppIndicator extension, say):
// show the deck so starting the app does something. Starting it again shows
// the deck again.
static gboolean no_tray_check(gpointer data) {
  MyApplication* self = MY_APPLICATION(data);
  self->no_tray_timer = 0;
  if (!self->tray_host) {
    g_message(
        "agents deck: no system tray found (on GNOME, install the "
        "AppIndicator extension); run agents-deck again to open the deck");
    show_deck(self);
  }
  return G_SOURCE_REMOVE;
}

static void method_call_cb(FlMethodChannel* channel, FlMethodCall* call,
                           gpointer data) {
  MyApplication* self = MY_APPLICATION(data);
  const gchar* method = fl_method_call_get_name(call);
  if (g_strcmp0(method, "attention") == 0) {
    FlValue* args = fl_method_call_get_args(call);
    int n = 0;
    if (args != nullptr && fl_value_get_type(args) == FL_VALUE_TYPE_INT) {
      n = static_cast<int>(fl_value_get_int(args));
    }
    if (self->tray != nullptr) tray_set_attention(self->tray, n);
    fl_method_call_respond_success(call, nullptr, nullptr);
  } else if (g_strcmp0(method, "hide") == 0) {
    hide_deck(self);
    fl_method_call_respond_success(call, nullptr, nullptr);
  } else {
    fl_method_call_respond_not_implemented(call, nullptr);
  }
}

static gboolean on_focus_in(GtkWidget* widget, GdkEvent* event,
                            gpointer data) {
  MY_APPLICATION(data)->had_focus = TRUE;
  return FALSE;
}

// Clicking anywhere else puts the deck away, like a menu bar popover.
static gboolean on_focus_out(GtkWidget* widget, GdkEvent* event,
                             gpointer data) {
  MyApplication* self = MY_APPLICATION(data);
  if (self->visible && self->had_focus) {
    hide_deck(self);
    self->hidden_at = g_get_monotonic_time();
  }
  return FALSE;
}

// The window manager's close (Alt+F4) hides the deck; Quit is in the menu.
static gboolean on_delete(GtkWidget* widget, GdkEvent* event, gpointer data) {
  hide_deck(MY_APPLICATION(data));
  return TRUE;
}

// Builds the deck window, hidden, and the tray icon.
static void create_deck(MyApplication* self) {
  GtkWindow* window =
      GTK_WINDOW(gtk_application_window_new(GTK_APPLICATION(self)));
  self->window = window;
  gtk_window_set_title(window, "agents deck");
  gtk_window_set_decorated(window, FALSE);
  gtk_window_set_resizable(window, FALSE);
  gtk_window_set_default_size(window, kSide, kSide);
  gtk_widget_set_size_request(GTK_WIDGET(window), kSide, kSide);
  gtk_window_set_skip_taskbar_hint(window, TRUE);
  gtk_window_set_skip_pager_hint(window, TRUE);
  gtk_window_set_keep_above(window, TRUE);
  gtk_window_stick(window);  // on every workspace
  gtk_window_set_type_hint(window, GDK_WINDOW_TYPE_HINT_UTILITY);
  g_signal_connect(window, "focus-in-event", G_CALLBACK(on_focus_in), self);
  g_signal_connect(window, "focus-out-event", G_CALLBACK(on_focus_out), self);
  g_signal_connect(window, "delete-event", G_CALLBACK(on_delete), self);

  g_autoptr(FlDartProject) project = fl_dart_project_new();
  fl_dart_project_set_dart_entrypoint_arguments(
      project, self->dart_entrypoint_arguments);

  FlView* view = fl_view_new(project);
  GdkRGBA background_color;
  gdk_rgba_parse(&background_color, kBackground);
  fl_view_set_background_color(view, &background_color);
  gtk_widget_show(GTK_WIDGET(view));
  gtk_container_add(GTK_CONTAINER(window), GTK_WIDGET(view));
  // Realized but not shown: Flutter starts and connects to the daemon, and
  // the window stays hidden until the tray icon is clicked.
  gtk_widget_realize(GTK_WIDGET(view));

  fl_register_plugins(FL_PLUGIN_REGISTRY(view));

  g_autoptr(FlStandardMethodCodec) codec = fl_standard_method_codec_new();
  self->channel = fl_method_channel_new(
      fl_engine_get_binary_messenger(fl_view_get_engine(view)),
      "agents_deck/menubar", FL_METHOD_CODEC(codec));
  fl_method_channel_set_method_call_handler(self->channel, method_call_cb,
                                            self, nullptr);

  gtk_widget_grab_focus(GTK_WIDGET(view));

  self->tray = tray_new(on_tray_action, on_tray_host, self);
  // At login the panel may come up after the app: don't jump in with the deck.
  if (!self->background) {
    self->no_tray_timer = g_timeout_add_seconds(3, no_tray_check, self);
  }

  // Nothing is on screen most of the time: keep running anyway.
  g_application_hold(G_APPLICATION(self));
}

// Implements GApplication::activate: the first start builds the deck; starting
// the app again while it runs opens the deck (GApplication forwards it here).
// Arguments stay with the process that got them, so a second
// `agents-deck --background` opens the deck too.
static void my_application_activate(GApplication* application) {
  MyApplication* self = MY_APPLICATION(application);
  if (self->window == nullptr) {
    create_deck(self);
  } else {
    show_deck(self);
  }
}

// Implements GApplication::local_command_line.
static gboolean my_application_local_command_line(GApplication* application,
                                                  gchar*** arguments,
                                                  int* exit_status) {
  MyApplication* self = MY_APPLICATION(application);
  // Strip out the first argument as it is the binary name.
  self->dart_entrypoint_arguments = g_strdupv(*arguments + 1);
  for (char** a = self->dart_entrypoint_arguments; *a != nullptr; a++) {
    if (g_strcmp0(*a, "--background") == 0) self->background = TRUE;
  }

  g_autoptr(GError) error = nullptr;
  if (!g_application_register(application, nullptr, &error)) {
    g_warning("Failed to register: %s", error->message);
    *exit_status = 1;
    return TRUE;
  }

  g_application_activate(application);
  *exit_status = 0;

  return TRUE;
}

// Implements GApplication::shutdown.
static void my_application_shutdown(GApplication* application) {
  MyApplication* self = MY_APPLICATION(application);
  if (self->no_tray_timer) {
    g_source_remove(self->no_tray_timer);
    self->no_tray_timer = 0;
  }
  g_clear_pointer(&self->tray, tray_free);
  g_clear_object(&self->channel);
  G_APPLICATION_CLASS(my_application_parent_class)->shutdown(application);
}

// Implements GObject::dispose.
static void my_application_dispose(GObject* object) {
  MyApplication* self = MY_APPLICATION(object);
  g_clear_pointer(&self->dart_entrypoint_arguments, g_strfreev);
  G_OBJECT_CLASS(my_application_parent_class)->dispose(object);
}

static void my_application_class_init(MyApplicationClass* klass) {
  G_APPLICATION_CLASS(klass)->activate = my_application_activate;
  G_APPLICATION_CLASS(klass)->local_command_line =
      my_application_local_command_line;
  G_APPLICATION_CLASS(klass)->shutdown = my_application_shutdown;
  G_OBJECT_CLASS(klass)->dispose = my_application_dispose;
}

static void my_application_init(MyApplication* self) {}

MyApplication* my_application_new() {
  // Set the program name to the application ID, which helps various systems
  // like GTK and desktop environments map this running application to its
  // corresponding .desktop file.
  g_set_prgname(APPLICATION_ID);

  // One instance per session: starting it again opens the running one's deck.
  return MY_APPLICATION(g_object_new(my_application_get_type(),
                                     "application-id", APPLICATION_ID, "flags",
                                     G_APPLICATION_DEFAULT_FLAGS, nullptr));
}
