#include "tray.h"

#include <gio/gio.h>
#include <unistd.h>

#include <initializer_list>

// The StatusNotifierItem spec:
// https://www.freedesktop.org/wiki/Specifications/StatusNotifierItem/
// The menu side (com.canonical.dbusmenu) follows libdbusmenu's interface.

static const char kItemPath[] = "/StatusNotifierItem";
static const char kMenuPath[] = "/MenuBar";
static const char kWatcherName[] = "org.kde.StatusNotifierWatcher";
static const char kWatcherPath[] = "/StatusNotifierWatcher";

static const char kIntrospection[] =
    "<node>"
    " <interface name='org.kde.StatusNotifierItem'>"
    "  <property name='Category' type='s' access='read'/>"
    "  <property name='Id' type='s' access='read'/>"
    "  <property name='Title' type='s' access='read'/>"
    "  <property name='Status' type='s' access='read'/>"
    "  <property name='WindowId' type='i' access='read'/>"
    "  <property name='IconName' type='s' access='read'/>"
    "  <property name='IconPixmap' type='a(iiay)' access='read'/>"
    "  <property name='OverlayIconName' type='s' access='read'/>"
    "  <property name='OverlayIconPixmap' type='a(iiay)' access='read'/>"
    "  <property name='AttentionIconName' type='s' access='read'/>"
    "  <property name='AttentionIconPixmap' type='a(iiay)' access='read'/>"
    "  <property name='AttentionMovieName' type='s' access='read'/>"
    "  <property name='ToolTip' type='(sa(iiay)ss)' access='read'/>"
    "  <property name='ItemIsMenu' type='b' access='read'/>"
    "  <property name='Menu' type='o' access='read'/>"
    "  <method name='ContextMenu'><arg name='x' type='i' direction='in'/>"
    "   <arg name='y' type='i' direction='in'/></method>"
    "  <method name='Activate'><arg name='x' type='i' direction='in'/>"
    "   <arg name='y' type='i' direction='in'/></method>"
    "  <method name='SecondaryActivate'><arg name='x' type='i' direction='in'/>"
    "   <arg name='y' type='i' direction='in'/></method>"
    "  <method name='Scroll'><arg name='delta' type='i' direction='in'/>"
    "   <arg name='orientation' type='s' direction='in'/></method>"
    "  <method name='ProvideXdgActivationToken'>"
    "   <arg name='token' type='s' direction='in'/></method>"
    "  <signal name='NewTitle'/>"
    "  <signal name='NewIcon'/>"
    "  <signal name='NewAttentionIcon'/>"
    "  <signal name='NewOverlayIcon'/>"
    "  <signal name='NewToolTip'/>"
    "  <signal name='NewStatus'><arg name='status' type='s'/></signal>"
    " </interface>"
    " <interface name='com.canonical.dbusmenu'>"
    "  <property name='Version' type='u' access='read'/>"
    "  <property name='TextDirection' type='s' access='read'/>"
    "  <property name='Status' type='s' access='read'/>"
    "  <property name='IconThemePath' type='as' access='read'/>"
    "  <method name='GetLayout'>"
    "   <arg name='parentId' type='i' direction='in'/>"
    "   <arg name='recursionDepth' type='i' direction='in'/>"
    "   <arg name='propertyNames' type='as' direction='in'/>"
    "   <arg name='revision' type='u' direction='out'/>"
    "   <arg name='layout' type='(ia{sv}av)' direction='out'/>"
    "  </method>"
    "  <method name='GetGroupProperties'>"
    "   <arg name='ids' type='ai' direction='in'/>"
    "   <arg name='propertyNames' type='as' direction='in'/>"
    "   <arg name='properties' type='a(ia{sv})' direction='out'/>"
    "  </method>"
    "  <method name='GetProperty'>"
    "   <arg name='id' type='i' direction='in'/>"
    "   <arg name='name' type='s' direction='in'/>"
    "   <arg name='value' type='v' direction='out'/>"
    "  </method>"
    "  <method name='Event'>"
    "   <arg name='id' type='i' direction='in'/>"
    "   <arg name='eventId' type='s' direction='in'/>"
    "   <arg name='data' type='v' direction='in'/>"
    "   <arg name='timestamp' type='u' direction='in'/>"
    "  </method>"
    "  <method name='EventGroup'>"
    "   <arg name='events' type='a(isvu)' direction='in'/>"
    "   <arg name='idErrors' type='ai' direction='out'/>"
    "  </method>"
    "  <method name='AboutToShow'>"
    "   <arg name='id' type='i' direction='in'/>"
    "   <arg name='needUpdate' type='b' direction='out'/>"
    "  </method>"
    "  <method name='AboutToShowGroup'>"
    "   <arg name='ids' type='ai' direction='in'/>"
    "   <arg name='updatesNeeded' type='ai' direction='out'/>"
    "   <arg name='idErrors' type='ai' direction='out'/>"
    "  </method>"
    "  <signal name='ItemsPropertiesUpdated'>"
    "   <arg name='updatedProps' type='a(ia{sv})'/>"
    "   <arg name='removedProps' type='a(ias)'/>"
    "  </signal>"
    "  <signal name='LayoutUpdated'>"
    "   <arg name='revision' type='u'/><arg name='parent' type='i'/>"
    "  </signal>"
    "  <signal name='ItemActivationRequested'>"
    "   <arg name='id' type='i'/><arg name='timestamp' type='u'/>"
    "  </signal>"
    " </interface>"
    "</node>";

// The menu: id 0 is the root; separators have no action.
typedef struct {
  int id;
  const char* label;  // nullptr for a separator
  TrayAction action;
} MenuItem;

static const MenuItem kMenu[] = {
    {1, "Show the deck", TRAY_SHOW},
    {2, "Open dashboard in browser", TRAY_DASHBOARD},
    {3, nullptr, TRAY_TOGGLE},
    {4, "Quit agents deck", TRAY_QUIT},
};

struct _Tray {
  TrayActionFunc on_action;
  TrayHostFunc on_host;
  gpointer user_data;

  GDBusNodeInfo* node;
  GDBusConnection* bus;
  char* bus_name;
  guint own_id;
  guint watch_id;
  guint item_reg;
  guint menu_reg;
  int waiting;
};

// ---- the icon ------------------------------------------------------------

// The deck's critter, as on macOS (CritterIcon in AppDelegate.swift): a 12×6
// body at x 1..12, 1×2 arms at x 0 and 13 (rows 2-3), eyes punched out at x 4
// and 9 (rows 2-3), legs at x 2, 4, 9, 11 (rows 6-7), on a 14×8 grid.
static gboolean critter_cell(int x, int y) {
  if (y >= 0 && y < 6 && x >= 1 && x <= 12) {
    return !((x == 4 || x == 9) && (y == 2 || y == 3));
  }
  if ((x == 0 || x == 13) && (y == 2 || y == 3)) return TRUE;
  if ((y == 6 || y == 7) && (x == 2 || x == 4 || x == 9 || x == 11)) {
    return TRUE;
  }
  return FALSE;
}

guint8* tray_critter_pixmap(int size, gboolean attention) {
  guint8* px = static_cast<guint8*>(g_malloc0(size * size * 4));
  // Panels can be light or dark, so the critter gets a thin dark outline:
  // light ink (the deck's) normally, the deck's orange when an agent needs
  // you.
  const guint8 fill[3] = {static_cast<guint8>(attention ? 0xFF : 0xE8),
                          static_cast<guint8>(attention ? 0x4D : 0xE4),
                          static_cast<guint8>(attention ? 0x00 : 0xDC)};
  int cell = (size - 2) / 14;
  if (cell < 1) cell = 1;
  const int ox = (size - 14 * cell) / 2, oy = (size - 8 * cell) / 2;
  auto inside = [&](int x, int y) -> gboolean {
    if (x < ox || y < oy) return FALSE;
    return critter_cell((x - ox) / cell, (y - oy) / cell) &&
           (x - ox) < 14 * cell && (y - oy) < 8 * cell;
  };
  for (int y = 0; y < size; y++) {
    for (int x = 0; x < size; x++) {
      guint8* p = px + (y * size + x) * 4;
      if (inside(x, y)) {
        p[0] = 0xFF;
        p[1] = fill[0];
        p[2] = fill[1];
        p[3] = fill[2];
        continue;
      }
      gboolean edge = FALSE;
      for (int dy = -1; dy <= 1 && !edge; dy++) {
        for (int dx = -1; dx <= 1 && !edge; dx++) {
          edge = inside(x + dx, y + dy);
        }
      }
      if (edge) p[0] = 0x90;  // translucent black
    }
  }
  return px;
}

static GVariant* icon_pixmaps(gboolean attention) {
  GVariantBuilder b;
  g_variant_builder_init(&b, G_VARIANT_TYPE("a(iiay)"));
  for (int size : {16, 22, 24, 32, 48, 64}) {
    guint8* px = tray_critter_pixmap(size, attention);
    GVariant* bytes = g_variant_new_fixed_array(G_VARIANT_TYPE_BYTE, px,
                                                size * size * 4, 1);
    g_variant_builder_add(&b, "(ii@ay)", size, size, bytes);
    g_free(px);
  }
  return g_variant_builder_end(&b);
}

static GVariant* empty_pixmaps() {
  return g_variant_new_array(G_VARIANT_TYPE("(iiay)"), nullptr, 0);
}

static char* tooltip_text(int waiting) {
  if (waiting <= 0) return g_strdup("agents deck");
  return g_strdup_printf("agents deck · %d need%s you", waiting,
                         waiting == 1 ? "s" : "");
}

// ---- org.kde.StatusNotifierItem -----------------------------------------

static GVariant* item_property(Tray* self, const char* name) {
  if (g_strcmp0(name, "Category") == 0) {
    return g_variant_new_string("ApplicationStatus");
  }
  if (g_strcmp0(name, "Id") == 0) return g_variant_new_string("agents-deck");
  if (g_strcmp0(name, "Title") == 0) {
    return g_variant_new_string("agents deck");
  }
  if (g_strcmp0(name, "Status") == 0) {
    return g_variant_new_string(self->waiting > 0 ? "NeedsAttention"
                                                  : "Active");
  }
  if (g_strcmp0(name, "WindowId") == 0) return g_variant_new_int32(0);
  if (g_strcmp0(name, "IconPixmap") == 0) {
    return icon_pixmaps(self->waiting > 0);
  }
  if (g_strcmp0(name, "AttentionIconPixmap") == 0) {
    return icon_pixmaps(TRUE);
  }
  if (g_strcmp0(name, "OverlayIconPixmap") == 0) return empty_pixmaps();
  if (g_strcmp0(name, "IconName") == 0 ||
      g_strcmp0(name, "OverlayIconName") == 0 ||
      g_strcmp0(name, "AttentionIconName") == 0 ||
      g_strcmp0(name, "AttentionMovieName") == 0) {
    return g_variant_new_string("");
  }
  if (g_strcmp0(name, "ToolTip") == 0) {
    g_autofree char* text = tooltip_text(self->waiting);
    return g_variant_new("(s@a(iiay)ss)", "", empty_pixmaps(), "agents deck",
                         text);
  }
  if (g_strcmp0(name, "ItemIsMenu") == 0) return g_variant_new_boolean(FALSE);
  if (g_strcmp0(name, "Menu") == 0) return g_variant_new_object_path(kMenuPath);
  return nullptr;
}

static void item_method(GDBusConnection* bus, const char* sender,
                        const char* path, const char* iface,
                        const char* method, GVariant* params,
                        GDBusMethodInvocation* inv, gpointer data) {
  Tray* self = static_cast<Tray*>(data);
  if (g_strcmp0(method, "Activate") == 0 ||
      g_strcmp0(method, "SecondaryActivate") == 0) {
    self->on_action(TRAY_TOGGLE, self->user_data);
  } else if (g_strcmp0(method, "ContextMenu") == 0) {
    // Hosts that ask for this instead of reading our Menu: open the deck,
    // which has everything the menu has except Quit.
    self->on_action(TRAY_SHOW, self->user_data);
  }
  // Scroll and ProvideXdgActivationToken: nothing to do.
  g_dbus_method_invocation_return_value(inv, nullptr);
}

static GVariant* item_get(GDBusConnection* bus, const char* sender,
                          const char* path, const char* iface,
                          const char* name, GError** error, gpointer data) {
  GVariant* v = item_property(static_cast<Tray*>(data), name);
  if (v == nullptr) {
    g_set_error(error, G_DBUS_ERROR, G_DBUS_ERROR_UNKNOWN_PROPERTY,
                "no property %s", name);
  }
  return v;
}

// ---- com.canonical.dbusmenu ---------------------------------------------

static GVariant* menu_item_props(int id) {
  GVariantBuilder b;
  g_variant_builder_init(&b, G_VARIANT_TYPE("a{sv}"));
  if (id == 0) {
    g_variant_builder_add(&b, "{sv}", "children-display",
                          g_variant_new_string("submenu"));
    return g_variant_builder_end(&b);
  }
  for (const MenuItem& m : kMenu) {
    if (m.id != id) continue;
    if (m.label == nullptr) {
      g_variant_builder_add(&b, "{sv}", "type",
                            g_variant_new_string("separator"));
    } else {
      g_variant_builder_add(&b, "{sv}", "label",
                            g_variant_new_string(m.label));
    }
    g_variant_builder_add(&b, "{sv}", "enabled", g_variant_new_boolean(TRUE));
    g_variant_builder_add(&b, "{sv}", "visible", g_variant_new_boolean(TRUE));
  }
  return g_variant_builder_end(&b);
}

static GVariant* menu_layout(int id, int depth) {
  GVariantBuilder kids;
  g_variant_builder_init(&kids, G_VARIANT_TYPE("av"));
  if (id == 0 && depth != 0) {
    for (const MenuItem& m : kMenu) {
      g_variant_builder_add(&kids, "v", menu_layout(m.id, depth - 1));
    }
  }
  return g_variant_new("(i@a{sv}av)", id, menu_item_props(id), &kids);
}

static gboolean menu_has(int id) {
  if (id == 0) return TRUE;
  for (const MenuItem& m : kMenu) {
    if (m.id == id) return TRUE;
  }
  return FALSE;
}

static void menu_clicked(Tray* self, int id, const char* event) {
  if (g_strcmp0(event, "clicked") != 0) return;
  for (const MenuItem& m : kMenu) {
    if (m.id == id && m.label != nullptr) {
      self->on_action(m.action, self->user_data);
    }
  }
}

static void menu_method(GDBusConnection* bus, const char* sender,
                        const char* path, const char* iface,
                        const char* method, GVariant* params,
                        GDBusMethodInvocation* inv, gpointer data) {
  Tray* self = static_cast<Tray*>(data);
  if (g_strcmp0(method, "GetLayout") == 0) {
    gint32 parent, depth;
    g_variant_get(params, "(ii^a&s)", &parent, &depth, nullptr);
    if (!menu_has(parent)) {
      g_dbus_method_invocation_return_error(inv, G_DBUS_ERROR,
                                            G_DBUS_ERROR_INVALID_ARGS,
                                            "no menu item %d", parent);
      return;
    }
    g_dbus_method_invocation_return_value(
        inv, g_variant_new("(u@(ia{sv}av))", 1u, menu_layout(parent, depth)));
  } else if (g_strcmp0(method, "GetGroupProperties") == 0) {
    g_autoptr(GVariantIter) ids = nullptr;
    g_variant_get(params, "(ai^a&s)", &ids, nullptr);
    GVariantBuilder b;
    g_variant_builder_init(&b, G_VARIANT_TYPE("a(ia{sv})"));
    gint32 id;
    while (g_variant_iter_next(ids, "i", &id)) {
      if (menu_has(id)) {
        g_variant_builder_add(&b, "(i@a{sv})", id, menu_item_props(id));
      }
    }
    g_dbus_method_invocation_return_value(inv, g_variant_new("(a(ia{sv}))",
                                                             &b));
  } else if (g_strcmp0(method, "GetProperty") == 0) {
    gint32 id;
    const char* name;
    g_variant_get(params, "(i&s)", &id, &name);
    g_autoptr(GVariant) props = menu_item_props(id);
    g_autoptr(GVariant) v = g_variant_lookup_value(props, name, nullptr);
    if (v == nullptr) {
      g_dbus_method_invocation_return_error(inv, G_DBUS_ERROR,
                                            G_DBUS_ERROR_INVALID_ARGS,
                                            "no property %s", name);
      return;
    }
    g_dbus_method_invocation_return_value(inv, g_variant_new("(v)", v));
  } else if (g_strcmp0(method, "Event") == 0) {
    gint32 id;
    const char* event;
    g_variant_get(params, "(i&svu)", &id, &event, nullptr, nullptr);
    g_dbus_method_invocation_return_value(inv, nullptr);
    menu_clicked(self, id, event);  // after replying: Quit ends the process
  } else if (g_strcmp0(method, "EventGroup") == 0) {
    g_autoptr(GVariantIter) events = nullptr;
    g_variant_get(params, "(a(isvu))", &events);
    GVariantBuilder errors;
    g_variant_builder_init(&errors, G_VARIANT_TYPE("ai"));
    GArray* clicked = g_array_new(FALSE, FALSE, sizeof(gint32));
    gint32 id;
    const char* event;
    while (g_variant_iter_next(events, "(i&svu)", &id, &event, nullptr,
                               nullptr)) {
      if (!menu_has(id)) {
        g_variant_builder_add(&errors, "i", id);
      } else if (g_strcmp0(event, "clicked") == 0) {
        g_array_append_val(clicked, id);
      }
    }
    g_dbus_method_invocation_return_value(inv,
                                          g_variant_new("(ai)", &errors));
    for (guint i = 0; i < clicked->len; i++) {
      menu_clicked(self, g_array_index(clicked, gint32, i), "clicked");
    }
    g_array_free(clicked, TRUE);
  } else if (g_strcmp0(method, "AboutToShow") == 0) {
    g_dbus_method_invocation_return_value(inv, g_variant_new("(b)", FALSE));
  } else if (g_strcmp0(method, "AboutToShowGroup") == 0) {
    g_dbus_method_invocation_return_value(
        inv, g_variant_new("(@ai@ai)",
                           g_variant_new_array(G_VARIANT_TYPE_INT32, nullptr, 0),
                           g_variant_new_array(G_VARIANT_TYPE_INT32, nullptr,
                                               0)));
  } else {
    g_dbus_method_invocation_return_error(inv, G_DBUS_ERROR,
                                          G_DBUS_ERROR_UNKNOWN_METHOD,
                                          "no method %s", method);
  }
}

static GVariant* menu_get(GDBusConnection* bus, const char* sender,
                          const char* path, const char* iface,
                          const char* name, GError** error, gpointer data) {
  if (g_strcmp0(name, "Version") == 0) return g_variant_new_uint32(3);
  if (g_strcmp0(name, "TextDirection") == 0) return g_variant_new_string("ltr");
  if (g_strcmp0(name, "Status") == 0) return g_variant_new_string("normal");
  if (g_strcmp0(name, "IconThemePath") == 0) {
    return g_variant_new_strv(nullptr, 0);
  }
  g_set_error(error, G_DBUS_ERROR, G_DBUS_ERROR_UNKNOWN_PROPERTY,
              "no property %s", name);
  return nullptr;
}

static const GDBusInterfaceVTable kItemVTable = {item_method, item_get,
                                                 nullptr, {nullptr}};
static const GDBusInterfaceVTable kMenuVTable = {menu_method, menu_get,
                                                 nullptr, {nullptr}};

// ---- registration -------------------------------------------------------

static void registered(GObject* source, GAsyncResult* res, gpointer data) {
  g_autoptr(GError) error = nullptr;
  g_autoptr(GVariant) r =
      g_dbus_connection_call_finish(G_DBUS_CONNECTION(source), res, &error);
  if (r == nullptr) {
    g_warning("agents deck: the tray host refused the icon: %s",
              error->message);
  }
}

static void watcher_appeared(GDBusConnection* bus, const char* name,
                             const char* owner, gpointer data) {
  Tray* self = static_cast<Tray*>(data);
  if (self->bus_name == nullptr) return;
  g_dbus_connection_call(bus, kWatcherName, kWatcherPath, kWatcherName,
                         "RegisterStatusNotifierItem",
                         g_variant_new("(s)", self->bus_name), nullptr,
                         G_DBUS_CALL_FLAGS_NONE, -1, nullptr, registered,
                         nullptr);
  self->on_host(TRUE, self->user_data);
}

static void watcher_vanished(GDBusConnection* bus, const char* name,
                             gpointer data) {
  Tray* self = static_cast<Tray*>(data);
  self->on_host(FALSE, self->user_data);
}

static void bus_acquired(GDBusConnection* bus, const char* name,
                         gpointer data) {
  Tray* self = static_cast<Tray*>(data);
  self->bus = G_DBUS_CONNECTION(g_object_ref(bus));
  g_autoptr(GError) error = nullptr;
  self->item_reg = g_dbus_connection_register_object(
      bus, kItemPath, self->node->interfaces[0], &kItemVTable, self, nullptr,
      &error);
  if (self->item_reg == 0) {
    g_warning("agents deck: no tray icon: %s", error->message);
    return;
  }
  self->menu_reg = g_dbus_connection_register_object(
      bus, kMenuPath, self->node->interfaces[1], &kMenuVTable, self, nullptr,
      &error);
  if (self->menu_reg == 0) {
    g_warning("agents deck: no tray menu: %s", error->message);
  }
}

static void name_acquired(GDBusConnection* bus, const char* name,
                          gpointer data) {
  Tray* self = static_cast<Tray*>(data);
  // Only now can the watcher look us up by name.
  self->watch_id = g_bus_watch_name_on_connection(
      bus, kWatcherName, G_BUS_NAME_WATCHER_FLAGS_NONE, watcher_appeared,
      watcher_vanished, self, nullptr);
}

static void name_lost(GDBusConnection* bus, const char* name, gpointer data) {
  Tray* self = static_cast<Tray*>(data);
  if (bus == nullptr) {
    g_warning("agents deck: no session bus, so no tray icon");
  }
  self->on_host(FALSE, self->user_data);
}

Tray* tray_new(TrayActionFunc on_action,
               TrayHostFunc on_host, gpointer user_data) {
  Tray* self = g_new0(Tray, 1);
  self->on_action = on_action;
  self->on_host = on_host;
  self->user_data = user_data;
  self->node = g_dbus_node_info_new_for_xml(kIntrospection, nullptr);
  self->bus_name = g_strdup_printf("org.kde.StatusNotifierItem-%d-1",
                                   static_cast<int>(getpid()));
  self->own_id = g_bus_own_name(G_BUS_TYPE_SESSION, self->bus_name,
                                G_BUS_NAME_OWNER_FLAGS_NONE, bus_acquired,
                                name_acquired, name_lost, self, nullptr);
  return self;
}

void tray_set_attention(Tray* self, int waiting) {
  if (waiting < 0) waiting = 0;
  if (waiting == self->waiting) return;
  const gboolean changed = (waiting > 0) != (self->waiting > 0);
  self->waiting = waiting;
  if (self->bus == nullptr || self->item_reg == 0) return;
  const char* iface = "org.kde.StatusNotifierItem";
  if (changed) {
    g_dbus_connection_emit_signal(self->bus, nullptr, kItemPath, iface,
                                  "NewIcon", nullptr, nullptr);
    g_dbus_connection_emit_signal(
        self->bus, nullptr, kItemPath, iface, "NewStatus",
        g_variant_new("(s)", waiting > 0 ? "NeedsAttention" : "Active"),
        nullptr);
  }
  g_dbus_connection_emit_signal(self->bus, nullptr, kItemPath, iface,
                                "NewToolTip", nullptr, nullptr);
}

void tray_free(Tray* self) {
  if (self == nullptr) return;
  if (self->watch_id) g_bus_unwatch_name(self->watch_id);
  if (self->bus != nullptr) {
    if (self->item_reg) {
      g_dbus_connection_unregister_object(self->bus, self->item_reg);
    }
    if (self->menu_reg) {
      g_dbus_connection_unregister_object(self->bus, self->menu_reg);
    }
    g_object_unref(self->bus);
  }
  if (self->own_id) g_bus_unown_name(self->own_id);
  g_dbus_node_info_unref(self->node);
  g_free(self->bus_name);
  g_free(self);
}
