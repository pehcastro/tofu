import http.server
import sys

lines = int(sys.argv[2])
filler = "." * int(sys.argv[3])
page = "".join("line %d %s\n" % (i, filler) for i in range(1, lines + 1)).encode()
log = open(sys.argv[1], "a", buffering=1)


class Page(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        log.write("GET %s\n" % self.path)
        self.send_response(200)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(page)))
        self.end_headers()
        self.wfile.write(page)

    def log_message(self, *args):
        pass


server = http.server.HTTPServer(("127.0.0.1", 0), Page)
print(server.server_address[1], flush=True)
server.serve_forever()
