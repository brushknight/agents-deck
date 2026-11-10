# The daemon's unix socket with the fixture state: /v1/events (SSE), /v1/state,
# /admin/v1/system and /local/login-code. With a third argument, also a
# pretend dashboard on 127.0.0.1:<port> that logs the pages it serves.
import os, socketserver, http.server, json, time, sys, threading
sock = sys.argv[1]
state = open(sys.argv[2]).read()
web_port = int(sys.argv[3]) if len(sys.argv) > 3 else 0
SYSTEM = {"version": "0.2.0", "platform": "linux/amd64", "install": "deb",
          "started": "2026-11-10T10:00:00Z",
          "boards": {"online": 2, "total": 3, "pending": 0}, "ports": {"new": 1},
          "update": {"state": "ready", "version": "0.2.1"}, "firmware_updates": 0}
def reply(h, code, body=b"", ctype="application/json"):
    h.send_response(code); h.send_header("Content-Type", ctype)
    h.send_header("Content-Length", str(len(body))); h.end_headers(); h.wfile.write(body)
class H(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_message(self, *a): pass
    def do_GET(self):
        if self.path.startswith("/v1/events"):
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-cache")
            self.end_headers()
            data = json.dumps(json.loads(state))
            self.wfile.write(f"event: state\ndata: {data}\n\n".encode()); self.wfile.flush()
            print("daemon: sent state", flush=True)
            while True:
                time.sleep(5); self.wfile.write(b": ping\n\n"); self.wfile.flush()
        elif self.path.startswith("/v1/state"):
            reply(self, 200, state.encode())
        elif self.path == "/admin/v1/system":
            reply(self, 200, json.dumps(SYSTEM).encode())
            print("daemon: sent system", flush=True)
        else:
            reply(self, 404)
    def do_POST(self):
        if self.path == "/local/login-code":
            reply(self, 200, b'{"code":"smoke-code"}')
            print("daemon: login code", flush=True)
        else:
            reply(self, 404)
class S(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True
    def get_request(self):
        r, _ = super().get_request(); return r, ("local", 0)
class Web(http.server.BaseHTTPRequestHandler):
    # Like a service from before /login learned `next`: every login lands on /.
    def log_message(self, *a): pass
    def do_GET(self):
        print("web: GET " + self.path, flush=True)
        if self.path.startswith("/login"):
            self.send_response(303); self.send_header("Location", "/")
            self.send_header("Content-Length", "0"); self.end_headers()
        else:
            reply(self, 200, b"<!doctype html><title>dashboard</title><p>dashboard</p>", "text/html")
if web_port:
    w = socketserver.ThreadingTCPServer(("127.0.0.1", web_port), Web)
    w.daemon_threads = True
    threading.Thread(target=w.serve_forever, daemon=True).start()
if os.path.exists(sock): os.remove(sock)
S(sock, H).serve_forever()
