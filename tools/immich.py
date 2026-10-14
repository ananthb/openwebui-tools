"""
title: Immich Photos
author: calculon.tech
license: AGPL-3.0-only
description: Search your Immich photos and albums from Open WebUI.
requirements:
"""

import json
import time
import urllib.error
import urllib.parse
import urllib.request

from pydantic import BaseModel, Field


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
        base_url: str = Field(default="http://localhost:2283", description="Immich base URL.")
        api_key: str = Field(default="", description="A shared Immich API key (optional).")

    class UserValves(BaseModel):
        api_key: str = Field(default="", description="Your Immich API key, from Account Settings -> API Keys.")

    def __init__(self):
        self.valves = self.Valves()

    def _headers(self, __user__):
        key = (__user__.get("valves") and __user__["valves"].api_key) or self.valves.api_key
        return {"x-api-key": key} if key else {}

    def _call(self, __user__, method, path, params=None, body=None):
        return _http(
            method,
            self.valves.base_url.rstrip("/") + path,
            headers=self._headers(__user__),
            params=params,
            body=body,
        )

    @staticmethod
    def _asset(a):
        exif = a.get("exifInfo") or {}
        return {
            "id": a.get("id"),
            "type": a.get("type"),
            "original_file_name": a.get("originalFileName"),
            "taken": exif.get("dateTimeOriginal") or a.get("fileCreatedAt"),
            "city": exif.get("city"),
            "country": exif.get("country"),
            "camera": " ".join(x for x in [exif.get("make"), exif.get("model")] if x) or None,
        }

    async def immich_search(self, query: str, __user__: dict) -> str:
        """Search your photos semantically (e.g. 'beach at sunset', 'dog on a couch')."""
        d = self._call(__user__, "POST", "/api/search/smart", body={"query": query, "size": 10, "withExif": True})
        if not isinstance(d, dict) or "error" in d:
            return json.dumps(d)
        items = (d.get("assets") or {}).get("items", [])
        return json.dumps([self._asset(a) for a in items])

    async def immich_albums(self, __user__: dict) -> str:
        """List your albums."""
        d = self._call(__user__, "GET", "/api/albums")
        if not isinstance(d, list):
            return json.dumps(d)
        return json.dumps([{"id": a.get("id"), "name": a.get("albumName"), "assets": a.get("assetCount")} for a in d])

    async def immich_album(self, album_id: str, __user__: dict) -> str:
        """List the assets in one album (id from immich_albums)."""
        d = self._call(__user__, "GET", "/api/albums/" + urllib.parse.quote(album_id))
        if not isinstance(d, dict) or "error" in d:
            return json.dumps(d)
        return json.dumps({"name": d.get("albumName"), "assets": [self._asset(a) for a in d.get("assets", [])]})

    async def immich_asset(self, asset_id: str, __user__: dict) -> str:
        """Get one asset's details (id from a search or album)."""
        a = self._call(__user__, "GET", "/api/assets/" + urllib.parse.quote(asset_id))
        return json.dumps(self._asset(a) if isinstance(a, dict) and "error" not in a else a)

    async def immich_people(self, __user__: dict) -> str:
        """List the people Immich has recognised in your photos."""
        d = self._call(__user__, "GET", "/api/people")
        people = d.get("people", []) if isinstance(d, dict) else []
        return json.dumps(
            [{"id": p.get("id"), "name": p.get("name"), "photos": p.get("numberOfAssets")} for p in people]
        )


Valves = Tools.Valves
UserValves = Tools.UserValves
