#ifndef RUNNER_SETTINGS_WINDOW_H_
#define RUNNER_SETTINGS_WINDOW_H_

#include <gtk/gtk.h>

// The Settings window: the service's own Settings page (the dashboard on
// 127.0.0.1) in a WebKitGTK web view, when the app was built with
// webkit2gtk-4.1. It loads nothing but that one address; links anywhere else
// open in the default browser. Built without WebKitGTK, Settings opens in the
// default browser instead.

// TRUE when url is the dashboard on this computer: http://127.0.0.1:<port>/…
// (or localhost), with no user name or password in it.
gboolean settings_window_is_dashboard(const char* url);

// Opens (or brings up) the Settings window on url, a dashboard address.
void settings_window_open(GtkApplication* app, const char* url);

// Closes the window if it is open (the app is quitting).
void settings_window_close();

#endif  // RUNNER_SETTINGS_WINDOW_H_
