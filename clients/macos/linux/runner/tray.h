#ifndef RUNNER_TRAY_H_
#define RUNNER_TRAY_H_

#include <glib.h>

// The tray icon: a StatusNotifierItem on the session bus, with its menu
// served over com.canonical.dbusmenu. KDE Plasma, XFCE, Cinnamon, MATE and
// GNOME with the AppIndicator extension all show these. Written here rather
// than with a plugin so a left click reaches the app (AppIndicator libraries
// keep it and open the menu instead).

typedef enum {
  TRAY_TOGGLE,     // left or middle click on the icon
  TRAY_SHOW,       // "Show the deck" in the menu
  TRAY_DASHBOARD,  // "Open dashboard in browser"
  TRAY_QUIT,       // "Quit agents deck"
} TrayAction;

typedef void (*TrayActionFunc)(TrayAction action, gpointer user_data);
// Called when a tray host appears (TRUE) or goes away (FALSE).
typedef void (*TrayHostFunc)(gboolean present, gpointer user_data);

typedef struct _Tray Tray;

Tray* tray_new(TrayActionFunc on_action,
               TrayHostFunc on_host,
               gpointer user_data);

// How many agents need you: 0 is the plain icon, more turns it orange.
void tray_set_attention(Tray* tray, int waiting);

void tray_free(Tray* tray);

// The critter icon as an ARGB32 pixmap in network byte order (A, R, G, B per
// pixel), size × size, the layout StatusNotifierItem wants. Exposed for tests.
// Returns a newly allocated buffer of size * size * 4 bytes.
guint8* tray_critter_pixmap(int size, gboolean attention);

#endif  // RUNNER_TRAY_H_
