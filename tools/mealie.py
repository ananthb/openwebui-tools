"""
title: Mealie Recipes
author: calculon.tech
license: AGPL-3.0-only
description: Search and read Mealie recipes from Open WebUI.
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
        base_url: str = Field(default="http://localhost:9000", description="Mealie base URL.")
        api_token: str = Field(default="", description="A shared Mealie API token (optional).")

    class UserValves(BaseModel):
        api_token: str = Field(default="", description="Your Mealie API token, from your profile.")

    def __init__(self):
        self.valves = self.Valves()

    def _headers(self, __user__):
        token = (__user__.get("valves") and __user__["valves"].api_token) or self.valves.api_token
        return {"Authorization": "Bearer " + token} if token else {}

    def _get(self, __user__, path, params=None):
        return _http("GET", self.valves.base_url.rstrip("/") + path, headers=self._headers(__user__), params=params)

    @staticmethod
    def _summary(r):
        return {
            "name": r.get("name"),
            "slug": r.get("slug"),
            "description": (r.get("description") or "")[:400],
            "tags": [t.get("name") for t in (r.get("tags") or [])],
            "categories": [c.get("name") for c in (r.get("category") or [])],
        }

    async def mealie_search(self, query: str, __user__: dict) -> str:
        """Search recipes by name or ingredient."""
        d = self._get(__user__, "/api/recipes", {"search": query, "perPage": 10})
        items = d.get("items", []) if isinstance(d, dict) else []
        return json.dumps([self._summary(r) for r in items])

    async def mealie_recipe(self, slug: str, __user__: dict) -> str:
        """Fetch one recipe by its slug (the id from mealie_search)."""
        r = self._get(__user__, "/api/recipes/" + urllib.parse.quote(slug))
        if not isinstance(r, dict) or "error" in r:
            return json.dumps(r)
        out = self._summary(r)
        out["servings"] = r.get("recipeServings") or r.get("yieldQuantity")
        out["time_minutes"] = r.get("totalTime")
        out["ingredients"] = [
            {"food": (i.get("food") or {}).get("name"), "quantity": i.get("quantity"), "note": i.get("note")}
            for i in r.get("recipeIngredient", [])
        ]
        out["instructions"] = [s.get("text") for s in (r.get("recipeInstructions") or []) if s.get("text")]
        return json.dumps(out)

    async def mealie_random(self, __user__: dict) -> str:
        """A random recipe."""
        r = self._get(__user__, "/api/recipes/random")
        if not isinstance(r, dict) or "error" in r:
            return json.dumps(r)
        return json.dumps(self._summary(r))

    async def mealie_tags(self, __user__: dict) -> str:
        """List the tags and categories used across your recipes."""
        tags = self._get(__user__, "/api/organizers/tags")
        cats = self._get(__user__, "/api/organizers/categories")
        pick = lambda d: [x.get("name") for x in d.get("items", [])] if isinstance(d, dict) else []  # noqa: E731
        return json.dumps({"tags": pick(tags), "categories": pick(cats)})


Valves = Tools.Valves
UserValves = Tools.UserValves
