"""
title: Actual Budget
author: calculon.tech
license: AGPL-3.0-only
description: Read your Actual Budget accounts, balances and transactions from Open WebUI.
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
        base_url: str = Field(default="http://localhost:5007", description="actual-http-api base URL.")
        api_key: str = Field(default="", description="The API_KEY the wrapper was started with.")
        budget_sync_id: str = Field(
            default="", description="The budget's sync id (Settings I show it, or from actual_budgets)."
        )

    class UserValves(BaseModel):
        api_key: str = Field(default="", description="Your API key, if the wrapper gives people separate keys.")

    def __init__(self):
        self.valves = self.Valves()

    def _call(self, __user__, method, path, params=None, body=None):
        key = (__user__.get("valves") and __user__["valves"].api_key) or self.valves.api_key
        headers = {"x-api-key": key} if key else {}
        return _http(method, self.valves.base_url.rstrip("/") + path, headers=headers, params=params, body=body)

    def _budget(self, path):
        return "/v1/budgets/" + urllib.parse.quote(self.valves.budget_sync_id) + path

    async def actual_budgets(self, __user__: dict) -> str:
        """List the budgets the wrapper can see (their sync ids)."""
        return json.dumps(self._call(__user__, "GET", "/v1/budgets"))

    async def actual_accounts(self, __user__: dict) -> str:
        """List the accounts in the budget, with their balances."""
        d = self._call(__user__, "GET", self._budget("/accounts"))
        accounts = d if isinstance(d, list) else (d.get("data") if isinstance(d, dict) else None)
        if not isinstance(accounts, list):
            return json.dumps(d)
        return json.dumps(
            [
                {"id": a.get("id"), "name": a.get("name"), "type": a.get("type"), "closed": a.get("closed")}
                for a in accounts
            ]
        )

    async def actual_balance(self, account_id: str, __user__: dict) -> str:
        """The current balance of one account (id from actual_accounts)."""
        return json.dumps(
            self._call(__user__, "GET", self._budget("/accounts/" + urllib.parse.quote(account_id) + "/balance"))
        )

    async def actual_transactions(self, account_id: str, since: str = "", __user__: dict = None) -> str:
        """Recent transactions for an account. since is an ISO date (optional)."""
        params = {"since": since} if since else None
        d = self._call(
            __user__, "GET", self._budget("/accounts/" + urllib.parse.quote(account_id) + "/transactions"), params
        )
        txs = d if isinstance(d, list) else (d.get("data") if isinstance(d, dict) else None)
        if not isinstance(txs, list):
            return json.dumps(d)
        return json.dumps(
            [
                {
                    "date": t.get("date"),
                    "amount": t.get("amount"),
                    "payee": t.get("payee_name") or t.get("payee"),
                    "category": t.get("category_name") or t.get("category"),
                    "notes": t.get("notes"),
                }
                for t in txs
            ]
        )

    async def actual_budget(self, month: str, __user__: dict) -> str:
        """Category budget vs spent for a month, 'YYYY-MM' (defaults handled by the wrapper)."""
        return json.dumps(
            self._call(__user__, "GET", self._budget("/months/" + urllib.parse.quote(month) + "/categories"))
        )

    async def actual_query(self, query: str, __user__: dict) -> str:
        """Run an ActualQL query against the budget, e.g. {transactions: {filter: {amount: {$lt: 0}}}}."""
        try:
            parsed = json.loads(query)
        except ValueError:
            return json.dumps({"error": "query must be JSON (ActualQL)"})
        return json.dumps(self._call(__user__, "POST", self._budget("/run-query"), body={"query": parsed}))


Valves = Tools.Valves
UserValves = Tools.UserValves
