# openwebui-tools

An **MCP server** that lets an Open WebUI chat read your self-hosted services —
**Seafile**, **Mealie**, **Immich** and **Actual Budget** — **as the person
talking to the model**. It authenticates each call from Open WebUI's forwarded
user identity and resolves a per-user token (falling back to a shared one).

- `openwebui-tools` (Go) — the MCP server, streamable HTTP.
- `actual-sidecar` (Node) — a thin wrapper around the official
  [`@actual-app/api`](https://actualbudget.org/docs/api/), because Actual's API is
  JS-only. Only needed if you use the Actual tools.

License: [AGPL-3.0-only](LICENSE).

## How it authenticates people

Open WebUI, given `ENABLE_FORWARD_USER_INFO_HEADERS=true` and a
`FORWARD_USER_INFO_HEADER_JWT_SECRET`, signs each call with an HS256 JWT in the
`X-OpenWebUI-User-Jwt` header carrying the caller's `sub` (id), `email`, `name`
and `role`. This server verifies that JWT with the same secret — which both
authenticates Open WebUI and names the person — and never trusts a plain header.
So it must only be reachable on a private network.

## Per-user tokens, with a shared fallback

Seafile mints a token per person automatically (one local admin credential).
Mealie, Immich and Actual need a credential the person supplies once; the server
stores it keyed by their Open WebUI id:

```
set_token(app="immich", token="...")     # or app="mealie"
```

Every call uses the person's token if present, else the shared token from the
server's environment. `set_token` with an empty token forgets it.

## Tools

| app | tools |
|---|---|
| Seafile | `seafile_libraries`, `seafile_browse`, `seafile_read` |
| Mealie | `mealie_search`, `mealie_recipe`, `mealie_random`, `mealie_tags` |
| Immich | `immich_search`, `immich_albums`, `immich_album`, `immich_asset`, `immich_people` |
| Actual | `actual_accounts`, `actual_balance`, `actual_transactions`, `actual_budget`, `actual_query` |
| — | `set_token` |

## Run

```
docker run -d -p 8090:8090 \
  -e JWT_SECRET=<same as FORWARD_USER_INFO_HEADER_JWT_SECRET> \
  -e SEAHUB=http://seafile:80 \
  -e ADMIN_EMAIL=seafile-automation@example.com -e ADMIN_PASSWORD=... \
  -e MEALIE_URL=https://mealie.example.com -e MEALIE_TOKEN=... \
  -e IMMICH_URL=https://immich.example.com -e IMMICH_KEY=... \
  -e ACTUAL_URL=http://actual-sidecar:8091 \
  -v openwebui-tools-data:/data \
  ghcr.io/ananthb/openwebui-tools:latest
```

Env (all optional except `JWT_SECRET`): `ADDR` (`:8090`), `STORE_PATH`
(`/data/tokens.json`), `SEAHUB`, `ADMIN_EMAIL`, `ADMIN_PASSWORD`/`ADMIN_TOKEN`,
`MEALIE_URL`, `MEALIE_TOKEN`, `IMMICH_URL`, `IMMICH_KEY`, `ACTUAL_URL`.

The Seafile admin must be a local account with a password (Seafile's
`User.objects.create_superuser`, not Django's `createsuperuser`).

The Actual sidecar takes `ACTUAL_SERVER_URL`, `ACTUAL_SERVER_PASSWORD`,
`ACTUAL_SYNC_ID` (optional) and a `/data` volume.

## Register in Open WebUI

Set on the Open WebUI server:

```
ENABLE_FORWARD_USER_INFO_HEADERS=true
FORWARD_USER_INFO_HEADER_JWT_SECRET=<secret>
TOOL_SERVER_CONNECTIONS=[{"type":"mcp","url":"http://openwebui-tools:8090/mcp","auth_type":"bearer","key":"<same secret>","config":{"enable":true},"info":{"id":"openwebui-tools","name":"openwebui-tools"}}]
```

Then attach it to a model (Workspace → Models → Tools) or enable it per chat.

## Releases

Tag `v*`: the workflow builds and pushes `ghcr.io/<owner>/openwebui-tools` (the
server) and `ghcr.io/<owner>/openwebui-tools/actual-sidecar`, and attaches the
source to the GitHub release.
