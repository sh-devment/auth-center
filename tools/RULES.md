# RULES.md

Project rules for **auth-center** — for humans and for Claude Code alike (the local, gitignored
`CLAUDE.md` just imports this file). Single source of truth: update it together with the code.

## What this is

**auth-center** is a stateless, database-free centralized authentication service. It supports
Telegram (via the auth-miniapp Mini App), Solana (wallet signature), and Google OAuth. It returns
one-time codes (60s TTL) that client apps exchange server-to-server for user data.

```
User → Client App → Auth Center → (Telegram / Solana / Google)
                         ↓
                   One-time Code
                         ↓
           Client Backend → POST /exchange → User Data
```

One instance per zone: `https://auth-center.sh-development.ru` / `https://auth-center.sh-development.com`.
SSO works only within one zone.

The repo lives in the `sh-devment` GitHub org (`git@github.com:sh-devment/auth-center.git`), so a
single org-level self-hosted runner per server serves every project there. The demo client
(`auth-client`) and the Telegram Mini App (`auth-miniapp`) are separate projects; `auth-proxy` is gone.

## Layout

```
build/          # Go source (main.go, telegram.go, miniapp.go, solana.go, google.go,
                #            exchange.go, delegate.go + web/)
bin/
  auth-center   # compiled linux/amd64 binary (committed to git)
  deploy.sh     # run by the Deploy workflow on each runner
tools/          # docs (delegate.md, telegram-miniapp.md, …) and images
.github/workflows/deploy.yml
Dockerfile, dev-compose.yml, prod-compose.yml
```

## Commands

**Local dev (hot-reload via Air):**
```bash
cp .env.example .env   # fill in variables once
docker-compose -f dev-compose.yml up        # port 8886
```

**Quick check (native):**
```bash
cd build && gofmt -l . && go vet ./... && go test ./... && go build -o /dev/null .
```

**Production binary (linux/amd64, committed to git):**
```bash
docker-compose -f prod-compose.yml run --rm release   # binary lands in bin/auth-center
```

**Deploy:** GitHub Actions → `Deploy` (manual `workflow_dispatch`). Matrix over `ru` / `com`, each
on its own self-hosted runner (`production-<region>` label) + environment; runs `bin/deploy.sh`,
which installs `bin/$APP`, writes `/opt/$APP/$APP.env` and the systemd unit, restarts,
health-checks `http://127.0.0.1:$PORT/` and rolls back on failure (previous state kept in
`/opt/$APP/last-deploy-backup`). No build step on the server — commit the rebuilt
`bin/auth-center` before deploying. Same scheme as `menu`, minus its SQLite backup and nightly
cron: auth-center has no database.

GitHub configuration:
- repo variable: `APP=auth-center`
- per environment (`production-ru`, `production-com`), variables: `PORT`, `BOT_USERNAME`,
  `MINIAPP_SHORT_NAME`, `MINIAPP_DIRECT_REDIRECT`, `DIRECT_REDIRECT`, `GOOGLE_CLIENT_ID`,
  `GOOGLE_CALLBACK_URL`; secrets: `BOT_TOKEN`, `APP_TOKENS`, `GOOGLE_CLIENT_SECRET`
- `REGION` comes from the matrix. The code does not read it — every zone difference is an env
  value — it is written to the env file for troubleshooting only.

Every key in `ENV_KEYS` is required by `deploy.sh`; an empty one fails the deploy before anything
is installed. To make a key optional, drop it from `ENV_KEYS`.

## Environment variables

- `PORT` — defaults to `8886`
- `BOT_TOKEN`, `BOT_USERNAME` — Telegram bot. The token is not used to call Telegram; it derives
  the shared secret with auth-miniapp
- `MINIAPP_SHORT_NAME` — mini app short name on the same bot. The entire Telegram login goes through it
- `MINIAPP_DIRECT_REDIRECT` — where a Telegram user who opened the mini app on their own is sent.
  Empty disables `POST /miniapp/home`. The Telegram twin of `DIRECT_REDIRECT`
- `APP_TOKENS` — comma-separated secrets clients use for `/exchange` and `/delegate`
- `DIRECT_REDIRECT` — redirect if a user opens auth-center directly without `?redirect=`
- `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_CALLBACK_URL`

The app loads `.env` via `godotenv.Load()` at startup (dev only; prod uses the generated env file).

## Architecture notes

**Stateless design:** all state (sessions, one-time codes, nonces, Google OAuth states) lives in
in-memory maps with mutexes and TTLs. No database. Lazy cleanup on access.
`sessionTTL = 5 min`, `codeTTL = 60 sec`. Codes are deleted immediately after `/exchange`.
A restart (every deploy) drops in-flight logins — acceptable at a 5-minute horizon.

**Web assets** are embedded via `//go:embed web` — `build/web/` is baked in at compile time.

**Routing** uses Go 1.22+ method-prefixed patterns. All routes — including static files — must use
method prefixes to avoid mux conflicts (e.g. `mux.Handle("GET /style.css", fileServer)`).

**`/exchange` is server-to-server only.** The client backend calls it with `code + app_token`.
The `app_token` is never sent to the browser.

**User identity** is always the provider's permanent unique ID: Telegram numeric ID, Solana public
key (base58), Google `sub`.

**Telegram via auth-miniapp.** The browser tab opens a session (`POST /qr-session`), the user
follows `https://t.me/<bot>/<app>?startapp=<flag><session token>`, and auth-miniapp verifies
`initData` by HMAC and reports the identity via `POST /miniapp/auth`. auth-center fills the
session, so `/poll`, the one-time code and `/exchange` are untouched and `method` stays `telegram`.

The two services trust each other by a value both derive from `BOT_TOKEN` —
`hex(HMAC_SHA256(key=BOT_TOKEN, data="auth-center/miniapp/v1"))` — so no extra credential exists and
the token itself never travels. In each zone, auth-center and auth-miniapp must use the same bot.
A drift surfaces only as a bare 403, so the derivation is pinned by twin fixture tests:
`build/miniapp_test.go` here and `build/authcenter_test.go` in auth-miniapp.

`telegramLink` (`build/telegram.go`) puts one flag character in front of the session token in
`?startapp=`: `b` for the button, `q` for the QR. The token is always everything after the first
character — a prefix that could be absent would be ambiguous, since tokens are base64url. A `q`
login gets no return code: the waiting browser is on another machine.

`POST /miniapp/auth` answers with the session's `redirect` plus a **second** one-time code minted
for the mini app page itself — the session's own code belongs to the polling tab, and a one-time
code spent twice fails for whoever is second. Errors: `403` bad secret, `404 expired`,
`409 session already used`, `503` no `BOT_TOKEN`. auth-miniapp treats 404 `expired` and 409 as
"stale link" and falls through to the front door, so keep those status codes and the word `expired`.

**The front door** (`POST /miniapp/home`): somebody opened the mini app themselves, with no login
started. It mints a code for `MINIAPP_DIRECT_REDIRECT` with no session to bind and nobody polling;
to the receiving app it is an ordinary Telegram login. The destination lives in auth-center on
purpose — the shared secret lets auth-miniapp assert identities it verified, and naming a
destination too would let it point a valid code anywhere. An empty `session_token` on
`/miniapp/auth` stays a 400, so a caller bug can never become a silent login.

The browser tab polls for the full `sessionTTL` (`web/script.js`, `SESSION_TTL_MS`); a session
that expires server-side answers 404 and the tab stops on that.

**Solana wallet detection** (`build/web/script.js`) tries four paths in order:
1. Injected provider — `window.phantom.solana`, `window.solflare`, `window.solana`
2. Wallet Standard — `@wallet-standard/app` (covers Seeker and other compliant wallets)
3. Mobile Wallet Adapter — `@solana-mobile/mobile-wallet-adapter-protocol` (Android only)
4. No wallet fallback — Phantom deep-link on mobile, inline warning on desktop

Wallet Standard and MWA modules are preloaded at page startup (before any user gesture) to keep the
Android intent within Chrome's user-gesture window.

**Cross-app login** (`POST /delegate`): see `tools/delegate.md`.

## Integrating a new app

```
AUTH_URL=https://auth-center.sh-development.<zone>       # sent to user's browser
AUTH_INTERNAL=https://auth-center.sh-development.<zone>  # used server-side for /exchange
APP_URL=https://yourapp.com                              # where auth-center redirects back
APP_TOKEN=<secret>                                       # must be listed in that zone's APP_TOKENS
```

1. Send the user to `GET $AUTH_URL/?redirect=<APP_URL>/callback`.
2. auth-center appends `?code=<one-time-code>`; exchange it server-side:
   `POST $AUTH_INTERNAL/exchange {"code": "...", "app_token": "..."}` →
   `{"ok": true, "method": "telegram|solana|google", "user": {"id": "..."}}`.

`user.id` is the permanent unique ID — use it as the primary key. Code is single-use, 60s TTL.

**Local development against prod:** set `APP_URL=http://localhost:<port>` (the redirect happens in
the developer's browser). Add the local `APP_TOKEN` to the zone's `APP_TOKENS` secret and re-run Deploy.
