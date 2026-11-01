# Serves /v1/events (SSE) on the daemon's unix socket with the fixture state.
import os, socketserver, http.server, json, time, sys
sock = sys.argv[1]
state = open(sys.argv[2]).read()
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
            b = state.encode()
            self.send_response(200); self.send_header("Content-Type","application/json")
            self.send_header("Content-Length", str(len(b))); self.end_headers(); self.wfile.write(b)
        else:
            self.send_response(404); self.send_header("Content-Length","0"); self.end_headers()
class S(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True
    def get_request(self):
        r, _ = super().get_request(); return r, ("local", 0)
if os.path.exists(sock): os.remove(sock)
S(sock, H).serve_forever()
