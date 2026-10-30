#include <stdio.h>
#include <unistd.h>

#include "my_application.h"

int main(int argc, char** argv) {
  // X11 first (XWayland on a Wayland desktop): the deck has to be placed next
  // to the tray icon, and Wayland doesn't let an app place its own window.
  // GDK_BACKEND in the environment still wins.
  gdk_set_allowed_backends("x11,wayland");

  int status;
  {
    g_autoptr(MyApplication) app = my_application_new();
    status = g_application_run(G_APPLICATION(app), argc, argv);
  }
  // Leave without running static destructors: some engine and GL driver
  // threads (Mesa's shader compiler, for one) can still be busy, and tearing
  // their libraries down under them crashes on the way out.
  fflush(nullptr);
  _exit(status);
}
