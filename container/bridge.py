#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
#
# Seafile bridge for Open WebUI's Seafile tool.
#
# Holds one local Seafile admin credential, mints each caller's token on demand
# (POST /api/v2.1/admin/generate-user-auth-token/) and reads their files as them,
# so nothing per person is stored. Callers (the Open WebUI tool) sign each request
# with BRIDGE_SECRET and name the person by email.
#
# Env: SEAHUB, HOST (default 0.0.0.0), PORT (default 3090), ADMIN_EMAIL,
#      ADMIN_PASSWORD (or ADMIN_TOKEN), BRIDGE_SECRET.

import hashlib
import hmac
import json
import os
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

SEAHUB = os.environ["SEAHUB"].rstrip("/")
HOST = os.environ.get("HOST", "0.0.0.0")
PORT = int(os.environ.get("PORT", "3090"))
ADMIN_EMAIL = os.environ["ADMIN_EMAIL"]
ADMIN_PASSWORD = os.environ.get("ADMIN_PASSWORD", "")
ADMIN_TOKEN = os.environ.get("ADMIN_TOKEN", "")
SECRET = os.environ["BRIDGE_SECRET"].encode()

TEXT_EXT = (
    ".txt",
    ".md",
    ".markdown",
    ".csv",
    ".json",
    ".log",
    ".xml",
    ".yml",
    ".yaml",
    ".html",
    ".htm",
    ".py",
    ".js",
    ".ts",
    ".sh",
    ".c",
    ".h",
    ".go",
    ".rs",
    ".toml",
    ".ini",
    ".conf",
    ".sql",
)

_lock = threading.Lock()
_admin = {"token": None}
_tokens = {}
_repos_cache = {}


def _call(method, path, token=None, form=None, raw=False, retries=4):
    url = path if path.startswith(("http://", "https://")) else SEAHUB + path
    data = None
    headers = {"Accept": "application/json"}
    if token:
        headers["Authorization"] = "Token " + token
    if form is not None:
        data = urllib.parse.urlencode(form).encode()
        headers["Content-Type"] = "application/x-www-form-urlencoded"
    for attempt in range(retries):
        req = urllib.request.Request(url, data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=120) as resp:
                body = resp.read()
            return body if raw else (json.loads(body) if body else {})
        except urllib.error.HTTPError as e:
            if e.code == 429 and attempt < retries - 1:
                time.sleep(2 * (attempt + 1))
                continue
            raise


def admin_token():
    with _lock:
        if _admin["token"] is None:
            if ADMIN_TOKEN:
                _admin["token"] = ADMIN_TOKEN
            else:
                _admin["token"] = _call(
                    "POST", "/api2/auth-token/", form={"username": ADMIN_EMAIL, "password": ADMIN_PASSWORD}
                )["token"]
        return _admin["token"]


def user_token(email):
    with _lock:
        if email in _tokens:
            return _tokens[email]
    token = _call("POST", "/api/v2.1/admin/generate-user-auth-token/", token=admin_token(), form={"email": email})[
        "token"
    ]
    with _lock:
        _tokens[email] = token
    return token


def repos(token):
    with _lock:
        cached = _repos_cache.get(token)
        if cached and cached[0] > time.time():
            return cached[1]
    listing = _call("GET", "/api/v2.1/repos/?per_page=100", token=token)["repos"]
    with _lock:
        _repos_cache[token] = (time.time() + 30, listing)
    return listing


def _resolve(token, path):
    parts = [p for p in path.split("/") if p]
    if not parts:
        return None, None
    for r in repos(token):
        if r["repo_name"] == parts[0]:
            return r["repo_id"], "/" + "/".join(parts[1:])
    return None, None


def libraries(token):
    return [{"name": r["repo_name"], "size": r.get("size"), "mtime": r.get("mtime")} for r in repos(token)]


def browse(token, path):
    repo, sub = _resolve(token, path)
    if not repo:
        return {"error": "no library named %r" % path.split("/")[0]}
    # v2, not v2.1: /dir/?path= ignores the path and returns the root.
    q = urllib.parse.urlencode({"p": sub})
    entries = _call("GET", "/api2/repos/%s/dir/?%s" % (repo, q), token=token)
    return {"path": path, "entries": [{"name": e["name"], "type": e["type"], "size": e.get("size")} for e in entries]}


def read(token, path):
    repo, sub = _resolve(token, path)
    if not repo:
        return "error: no library named %r" % path.split("/")[0]
    # The file endpoint answers with a seafhttp link, not bytes.
    link = _call("GET", "/api2/repos/%s/file/?%s" % (repo, urllib.parse.urlencode({"p": sub})), token=token)
    if not isinstance(link, str):
        return json.dumps(link)
    if os.path.splitext(path)[1].lower() not in TEXT_EXT:
        return "Not a text file; download it at %s" % link
    parts = urllib.parse.urlsplit(link)
    body = _call("GET", parts.path + (("?" + parts.query) if parts.query else ""), raw=True)
    return body[:200000].decode("utf-8", "replace")


OPS = {
    "libraries": lambda t, r: libraries(t),
    "browse": lambda t, r: browse(t, r["path"]),
    "read": lambda t, r: read(t, r["path"]),
}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def _reply(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/health":
            self._reply(200, {"ok": True})
        else:
            self._reply(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/seafile":
            self._reply(404, {"error": "not found"})
            return
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        sig = hmac.new(SECRET, body, hashlib.sha256).hexdigest()
        if not hmac.compare_digest(sig, self.headers.get("X-Bridge-Signature", "")):
            self._reply(403, {"error": "bad signature"})
            return
        try:
            req = json.loads(body)
            op = req["op"]
            if op not in OPS:
                self._reply(400, {"error": "unknown op %r" % op})
                return
            self._reply(200, OPS[op](user_token(req["user"]), req))
        except urllib.error.HTTPError as e:
            # A live 403 is the admin or user token gone stale: drop both caches.
            if e.code == 403:
                with _lock:
                    _tokens.clear()
                    _repos_cache.clear()
                    _admin["token"] = None
            self._reply(502, {"error": "seahub %s" % e.code, "detail": e.read().decode("utf-8", "replace")[:500]})
        except Exception as e:  # noqa: BLE001
            self._reply(500, {"error": str(e)})


if __name__ == "__main__":
    ThreadingHTTPServer((HOST, PORT), Handler).serve_forever()
