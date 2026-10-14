"""
title: Seafile Files
author: calculon.tech
license: AGPL-3.0-only
description: List, browse and read a person's Seafile libraries from Open WebUI.
requirements:
"""

import hashlib
import hmac
import json
import os
import time
import urllib.error
import urllib.parse
import urllib.request

from pydantic import BaseModel, Field

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


def _http(method, url, headers=None, params=None, body=None, retries=4):
    if params:
        url = url + ("&" if "?" in url else "?") + urllib.parse.urlencode(params)
    data = json.dumps(body).encode() if body is not None else None
    headers = dict(headers or {})
    if data is not None:
        headers["Content-Type"] = "application/json"
    last = None
    for attempt in range(retries):
        req = urllib.request.Request(url, data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=120) as r:
                raw = r.read()
            return json.loads(raw) if raw[:1] in (b"{", b"[", b'"') else raw.decode("utf-8", "replace")
        except urllib.error.HTTPError as e:
            detail = e.read().decode("utf-8", "replace")[:300]
            if e.code == 429 and attempt < retries - 1:
                time.sleep(2 * (attempt + 1))
                continue
            return {"error": "HTTP %s" % e.code, "detail": detail}
        except Exception as e:  # noqa: BLE001
            last = e
    return {"error": str(last)}


class Tools:
    class Valves(BaseModel):
        bridge_url: str = Field(
            default="",
            description="Bridge base URL, e.g. http://seafile-bridge:3090. Set this to act as each "
            "caller with no per-user token. Leave empty to call Seafile directly.",
        )
        bridge_secret: str = Field(default="", description="Shared secret the bridge checks (bridge mode).")
        base_url: str = Field(default="https://seafile.example.com", description="Seafile URL (direct mode).")
        api_token: str = Field(default="", description="A shared Seafile API token (direct mode, optional).")

    class UserValves(BaseModel):
        api_token: str = Field(default="", description="Your Seafile API token, from Settings -> API token.")

    def __init__(self):
        self.valves = self.Valves()

    # --- bridge mode -------------------------------------------------------
    def _bridge(self, email, op, **args):
        payload = {"user": email, "op": op, **args}
        body = json.dumps(payload).encode()
        sig = hmac.new(self.valves.bridge_secret.encode(), body, hashlib.sha256).hexdigest()
        req = urllib.request.Request(
            self.valves.bridge_url.rstrip("/") + "/seafile",
            data=body,
            headers={"Content-Type": "application/json", "X-Bridge-Signature": sig},
        )
        try:
            with urllib.request.urlopen(req, timeout=120) as r:
                return r.read().decode()
        except urllib.error.HTTPError as e:
            return json.dumps({"error": "bridge HTTP %s" % e.code, "detail": e.read().decode("utf-8", "replace")[:300]})
        except Exception as e:  # noqa: BLE001
            return json.dumps({"error": str(e)})

    # --- direct mode -------------------------------------------------------
    def _token(self, __user__):
        return (__user__.get("valves") and __user__["valves"].api_token) or self.valves.api_token

    def _api(self, token, path, params=None, raw=False):
        base = self.valves.base_url.rstrip("/")
        if raw:
            try:
                with urllib.request.urlopen(urllib.request.Request(base + path), timeout=120) as r:
                    return r.read()
            except Exception as e:  # noqa: BLE001
                return json.dumps({"error": str(e)})
        return _http("GET", base + path, headers={"Authorization": "Token " + token}, params=params)

    def _resolve(self, token, path):
        parts = [p for p in path.split("/") if p]
        if not parts:
            return None, None
        repos = self._api(token, "/api/v2.1/repos/", {"per_page": 100})
        for r in repos.get("repos", []) if isinstance(repos, dict) else []:
            if r["repo_name"] == parts[0]:
                return r["repo_id"], "/" + "/".join(parts[1:])
        return None, None

    # --- tools -------------------------------------------------------------
    async def seafile_libraries(self, __user__: dict) -> str:
        """List the Seafile libraries you can access."""
        if self.valves.bridge_url:
            return self._bridge(__user__.get("email", ""), "libraries")
        token = self._token(__user__)
        repos = self._api(token, "/api/v2.1/repos/", {"per_page": 100})
        items = repos.get("repos", []) if isinstance(repos, dict) else []
        return json.dumps([{"name": r["repo_name"], "size": r.get("size")} for r in items])

    async def seafile_browse(self, path: str, __user__: dict) -> str:
        """List one folder. path is 'Library' or 'Library/sub/dir'."""
        if self.valves.bridge_url:
            return self._bridge(__user__.get("email", ""), "browse", path=path)
        token = self._token(__user__)
        repo, sub = self._resolve(token, path)
        if not repo:
            return json.dumps({"error": "no library named %r" % path.split("/")[0]})
        entries = self._api(token, "/api2/repos/%s/dir/" % repo, {"p": sub})
        if not isinstance(entries, list):
            return json.dumps(entries)
        return json.dumps([{"name": e["name"], "type": e["type"], "size": e.get("size")} for e in entries])

    async def seafile_read(self, path: str, __user__: dict) -> str:
        """Read a text file. path is 'Library/sub/file.txt'."""
        if self.valves.bridge_url:
            return self._bridge(__user__.get("email", ""), "read", path=path)
        token = self._token(__user__)
        repo, sub = self._resolve(token, path)
        if not repo:
            return "error: no library named %r" % path.split("/")[0]
        link = self._api(token, "/api2/repos/%s/file/" % repo, {"p": sub})
        if not isinstance(link, str):
            return json.dumps(link)
        if os.path.splitext(path)[1].lower() not in TEXT_EXT:
            return "Not a text file; download it at %s" % link
        return self._api(token, urllib.parse.urlsplit(link).path, raw=True).decode("utf-8", "replace")[:200000]


Valves = Tools.Valves
UserValves = Tools.UserValves
