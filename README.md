# openwebui-tools

Open WebUI **Tools** that let a chat read your self-hosted services: **Seafile**,
**Mealie**, **Immich** and **Actual Budget**. Each tool acts as the person talking
to the model — it uses their token, not a shared one — so permissions are theirs.

License: [AGPL-3.0-only](LICENSE).

## Tools

| tool | what it reads | auth |
|---|---|---|
| [`tools/seafile.py`](tools/seafile.py) | libraries, folders, text files | per-user API token, or the [bridge](container/) |
| [`tools/mealie.py`](tools/mealie.py) | recipes, search, tags | per-user (or shared) API token |
| [`tools/immich.py`](tools/immich.py) | photo search, albums, people | per-user (or shared) API key |
| [`tools/actual.py`](tools/actual.py) | accounts, balances, transactions, ActualQL | [`actual-http-api`](https://github.com/jhonderson/actual-http-api) key |

## Install

In Open WebUI: **Workspace → Tools → +**, paste a tool's file, then set its
**Valves**. Attach it to a model (Workspace → Models → Edit → Tools), or turn it
on per chat. Each file is self-contained — no shared imports — so one paste is all
it takes.

Valves come in two scopes:

- **Valves** (admin) — the service URL and, optionally, a shared token, so the
  tool works without any per-user setup.
- **UserValves** (per person) — *your* token. Open WebUI stores valves encrypted
  with `WEBUI_SECRET_KEY`, so a per-user token is not plaintext at rest.

A person's token wins over the shared one when both are set.

## Tokens

- **Mealie** — profile → API Tokens.
- **Immich** — Account Settings → API Keys.
- **Actual** — run `actual-http-api`; the tool uses its `API_KEY`.
- **Seafile** — Settings → API token, **or** the bridge below when users have no
  token of their own (e.g. they only sign in through SSO).

## The Seafile bridge

Some Seafile deployments (OIDC-only) give people no API token to copy. The
`seafile-bridge` container holds **one** local admin credential, mints the
caller's token on demand, and reads their files as them — no per-person secret is
stored. Point the tool at it by setting `Valves.bridge_url` and
`Valves.bridge_secret`; leave them empty to call Seafile directly with a token.

```
docker run -d -p 3090:3090 \
  -e SEAHUB=http://seafile:80 \
  -e ADMIN_EMAIL=seafile-automation@example.com \
  -e ADMIN_PASSWORD=... \
  -e BRIDGE_SECRET=... \
  ghcr.io/ananthb/openwebui-tools/seafile-bridge:latest
```

The admin account must exist and be able to log in with a password. Create it
with Seafile's own `User.objects.create_superuser(email, password)` (Django's
`createsuperuser` sets no ccnet password). The tool signs every request with
`BRIDGE_SECRET`; terminate it on a private network or the tailnet, not the open
internet.

## Releases

Tag `v*` and the workflow builds and pushes the `seafile-bridge` image to
`ghcr.io/<owner>/openwebui-tools/seafile-bridge` and attaches the tool files to
the GitHub release.
