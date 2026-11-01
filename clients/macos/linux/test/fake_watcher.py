# A minimal org.kde.StatusNotifierWatcher that records registrations to a file.
import sys
from gi.repository import Gio, GLib
out = sys.argv[1]
XML = """<node><interface name='org.kde.StatusNotifierWatcher'>
<method name='RegisterStatusNotifierItem'><arg type='s' direction='in'/></method>
<method name='RegisterStatusNotifierHost'><arg type='s' direction='in'/></method>
<property name='IsStatusNotifierHostRegistered' type='b' access='read'/>
<property name='ProtocolVersion' type='i' access='read'/>
<property name='RegisteredStatusNotifierItems' type='as' access='read'/>
</interface></node>"""
node = Gio.DBusNodeInfo.new_for_xml(XML)
def call(conn, sender, path, iface, method, params, inv):
    if method == 'RegisterStatusNotifierItem':
        open(out, 'a').write(params.unpack()[0] + "\n")
    inv.return_value(None)
def get(conn, sender, path, iface, name):
    return {'IsStatusNotifierHostRegistered': GLib.Variant('b', True), 'ProtocolVersion': GLib.Variant('i', 0),
            'RegisteredStatusNotifierItems': GLib.Variant('as', [])}[name]
def acquired(conn, name):
    conn.register_object('/StatusNotifierWatcher', node.interfaces[0], call, get, None)
Gio.bus_own_name(Gio.BusType.SESSION, 'org.kde.StatusNotifierWatcher', 0, acquired, None, None)
GLib.MainLoop().run()
