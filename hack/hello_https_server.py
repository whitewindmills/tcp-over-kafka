#!/usr/bin/env python3
import argparse
import http.server
import signal
import ssl
import sys


class HelloHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = b"hello world\n"
        self.send_response(200)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt, *args):
        sys.stderr.write("%s - - [%s] %s\n" % (self.client_address[0], self.log_date_time_string(), fmt % args))


class HelloHTTPServer(http.server.ThreadingHTTPServer):
    # The concurrency sweep fans out multiple TLS probes in parallel; Python's
    # default request queue of 5 is too small and causes connect timeouts.
    request_queue_size = 128
    daemon_threads = True


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--bind", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=443)
    parser.add_argument("--cert", required=True)
    parser.add_argument("--key", required=True)
    args = parser.parse_args()

    server = HelloHTTPServer((args.bind, args.port), HelloHandler)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(certfile=args.cert, keyfile=args.key)
    server.socket = context.wrap_socket(server.socket, server_side=True)

    def stop(_signum, _frame):
        server.shutdown()

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    server.serve_forever()


if __name__ == "__main__":
    main()
