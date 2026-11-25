#ifndef RUNNER_SETTINGS_WINDOW_H_
#define RUNNER_SETTINGS_WINDOW_H_

#include <gtk/gtk.h>

// The Settings window: the service's own Settings page (the dashboard on
// 127.0.0.1) in a WebKitGTK web view, when the app was built with
// webkit2gtk-4.1, under a GTK header bar (the page drops its own top bar). It
// only ever shows Settings: other pages of the dashboard turn back to
// /settings, a link to the deck asks for the deck, and links anywhere else
// open in the default browser. Built without WebKitGTK, Settings opens in the
// default browser instead.

// TRUE when url is the dashboard on this computer: http://127.0.0.1:<port>/…
// (or localhost), with no user name or password in it.
gboolean settings_window_is_dashboard(const char* url);

// Opens (or brings up) the Settings window on url, a dashboard address.
void settings_window_open(GtkApplication* app, const char* url);

// Closes the window if it is open (the app is quitting).
void settings_window_close();

// What the page may ask the app for: "deck" (show the deck) and "dashboard"
// (the dashboard in the default browser).
typedef void (*SettingsActionFunc)(const char* action, gpointer data);
void settings_window_on_action(SettingsActionFunc func, gpointer data);

#endif  // RUNNER_SETTINGS_WINDOW_H_
