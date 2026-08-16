#!/usr/bin/env python3
"""The outside world, faked, so both apps talk to the same one.

Two services on one port, because they are told apart by path:

  /api/v1/…   the connected app — device flow, federated search, visit
  /email      Postmark

Both sides of the parity run drive the real code paths over real HTTP: no mock
on one side and a live call on the other. The control endpoints are the
harness's, not any app's:

  POST /_reset          forget the mail, put the device flow back to pending
  POST /_mode?mode=X    what the token endpoint answers next
  GET  /_mails          what has been mailed since the last reset, as JSON

Everything it answers is fixed text, because a capture that changes between
runs is a capture that cannot be diffed.
"""

import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlparse, parse_qs

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 3098

# The device flow's answer to "is it approved yet?", switched by the harness
# between captures. Every state the flow can be in is one of these.
mode = "pending"

# Every message posted to the Postmark endpoint since the last reset. The
# harness reads the reset link out of it: each app signs its own token, so
# neither can be handed the other's.
mails = []

GRANT = {
    "device_code": "abc",
    "verification_url": "http://127.0.0.1:%d/device/new?code=abc" % PORT,
    "expires_in": 600,
    "interval": 5,
}
TOKEN = {
    "token": "a-token",
    "scopes": ["search", "visit"],
    "expires_at": "2027-01-01T00:00:00Z",
}
LINKS = {
    "links": [
        {"id": 1, "title": "Alpha", "url": "https://a.example", "description": "ignored"},
        {"id": 2, "title": "Beta", "url": "https://b.example", "tags": ["x"]},
    ]
}


class Handler(BaseHTTPRequestHandler):
    # Quiet: the harness's own log is the one worth reading.
    def log_message(self, *args):
        pass

    def reply(self, body, code=200, ctype="application/json"):
        raw = body.encode()
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_POST(self):
        global mode
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length)
        path = urlparse(self.path).path

        if path == "/_reset":
            mode = "pending"
            mails.clear()
            return self.reply("{}")
        if path == "/_mode":
            mode = parse_qs(urlparse(self.path).query).get("mode", ["pending"])[0]
            return self.reply("{}")

        if path == "/api/v1/device_authorizations":
            return self.reply(json.dumps(GRANT))
        if path == "/api/v1/device_authorizations/token":
            if mode == "approved":
                return self.reply(json.dumps(TOKEN))
            if mode == "denied":
                return self.reply(json.dumps({"error": "access_denied"}), 400)
            if mode == "expired":
                return self.reply(json.dumps({"error": "expired_token"}), 400)
            if mode == "garbage":
                # Something in front of the API answering with a page: the
                # "unreachable" branch on both sides.
                return self.reply("<html>nope</html>", 502, "text/html")
            return self.reply(json.dumps({"error": "authorization_pending"}), 400)
        if path.endswith("/visit"):
            return self.reply("{}")

        if path == "/email":
            # Postmark. Rails posts here through postmark-rails, the Go binary
            # through POSTMARK_API_URL; in development Rails used
            # letter_opener, which opens a browser tab nobody asked for.
            try:
                mails.append(json.loads(body))
            except ValueError:
                mails.append({"Raw": body.decode("utf-8", "replace")})
            return self.reply(json.dumps({"ErrorCode": 0, "Message": "OK", "MessageID": "fake"}))

        return self.reply(json.dumps({"error": "not_found"}), 404)

    def do_GET(self):
        path = urlparse(self.path).path
        if path == "/_mails":
            return self.reply(json.dumps(mails))
        if path == "/api/v1/search":
            return self.reply(json.dumps(LINKS))
        return self.reply(json.dumps({"error": "not_found"}), 404)


if __name__ == "__main__":
    HTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
