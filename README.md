# openwebui-tools

An **MCP server** that lets an Open WebUI chat read and write your self-hosted
services — **Seafile**, **Mealie**, **Immich**, and **Obsidian Fast Note Sync
(FNS)** — **as the person talking to the model**. It authenticates each call
from Open WebUI's forwarded user identity and resolves a per-user token (minted
automatically or stored per user, falling back to a shared one where supported).

License: [AGPL-3.0-only](LICENSE).

## How it authenticates people

Open WebUI, given `ENABLE_FORWARD_USER_INFO_HEADERS=true` and a
`FORWARD_USER_INFO_HEADER_JWT_SECRET`, signs each call with an HS256 JWT in the
`X-OpenWebUI-User-Jwt` header carrying the caller's `sub` (id), `email`, `name`
and `role`. This server verifies that JWT with the same secret — which both
authenticates Open WebUI and names the person — and never trusts a plain header.
So it must only be reachable on a private network.

## Per-user tokens, with a shared fallback

Seafile mints a token per person automatically using the caller's `email` (and
refreshes it once on `401 Unauthorized`).
Mealie, Immich, and Obsidian FNS need a credential the person supplies once;
the server stores it keyed by their Open WebUI id:

```
set_token(app="immich", token="...")     # or app="mealie", app="obsidian" (alias: "fns")
```

For `obsidian`, mint a REST token in `https://obsidian.calculon.tech` WebGUI
with scope `p:rest c:* f:*` or `c:openwebui-tools`.

Every call uses the person's token if present, else the shared token from the
server's environment (where supported: `MEALIE_TOKEN`, `IMMICH_KEY`).
`set_token` with an empty token forgets it. Call `token_status` to inspect your
logged-in identity and which apps have tokens or auto-auth configured.

## Tools

| app | tools |
|---|---|
| Seafile | `seafile_libraries`, `seafile_browse`, `seafile_read`, `seafile_search`, `seafile_mkdir`, `seafile_write`, `seafile_delete`, `seafile_rename_move` |
| Mealie | `mealie_search`, `mealie_recipe`, `mealie_random`, `mealie_tags`, `mealie_create_recipe`, `mealie_import_recipe_url`, `mealie_update_recipe`, `mealie_delete_recipe`, `mealie_mealplans`, `mealie_shopping_lists`, `mealie_shopping_add_item`, `mealie_shopping_update_item` |
| Immich | `immich_search`, `immich_albums`, `immich_album`, `immich_asset`, `immich_people`, `immich_create_album`, `immich_update_album`, `immich_album_assets`, `immich_update_asset`, `immich_delete_assets`, `immich_tags`, `immich_tag_assets` |
| Obsidian FNS | `fns_vaults`, `fns_folder_tree`, `fns_folder_create`, `fns_folder_delete`, `fns_notes_list`, `fns_note_get`, `fns_note_save`, `fns_note_append`, `fns_note_prepend`, `fns_note_replace`, `fns_note_frontmatter`, `fns_note_rename`, `fns_note_delete`, `fns_note_restore`, `fns_note_links` |
| Auth | `set_token`, `token_status` |

## Run

```
docker run -d -p 8090:8090 \
  -e JWT_SECRET=<same as FORWARD_USER_INFO_HEADER_JWT_SECRET> \
  -e SEAHUB=http://seafile:80 \
  -e ADMIN_EMAIL=seafile-automation@example.com -e ADMIN_PASSWORD=... \
  -e MEALIE_URL=https://mealie.example.com -e MEALIE_TOKEN=... \
  -e IMMICH_URL=https://immich.example.com -e IMMICH_KEY=... \
  -e FNS_URL=http://fns:9000 \
  -v openwebui-tools-data:/data \
  ghcr.io/ananthb/openwebui-tools:latest
```

Env (all optional except `JWT_SECRET`): `ADDR` (`:8090`), `STORE_PATH`
(`/data/tokens.json`), `SEAHUB`, `ADMIN_EMAIL`, `ADMIN_PASSWORD`/`ADMIN_TOKEN`,
`MEALIE_URL`, `MEALIE_TOKEN`, `IMMICH_URL`, `IMMICH_KEY`, `FNS_URL`.

The Seafile admin must be a local account with a password (Seafile's
`User.objects.create_superuser`, not Django's `createsuperuser`).

## Register in Open WebUI

Set on the Open WebUI server:

```
ENABLE_FORWARD_USER_INFO_HEADERS=true
FORWARD_USER_INFO_HEADER_JWT_SECRET=<secret>
TOOL_SERVER_CONNECTIONS=[{"type":"mcp","url":"http://openwebui-tools:8090/mcp","auth_type":"bearer","key":"<same secret>","config":{"enable":true},"info":{"id":"openwebui-tools","name":"openwebui-tools"}}]
```

Then attach it to a model (Workspace → Models → Tools) or enable it per chat.

## Releases

Tag `v*`: the workflow builds and pushes `ghcr.io/<owner>/openwebui-tools`,
and attaches the source to the GitHub release.
