# Auth Center — Code Rules

## File Structure

```
build/
  main.go      — server setup, shared config, shared helpers, request logger
  telegram.go  — Telegram QR/deeplink auth (sessions store, webhook, poll)
  solana.go    — Solana wallet auth (nonce store, signature verification)
  google.go    — Google OAuth (state store, login redirect, callback)
  exchange.go  — one-time code store + /exchange endpoint
  delegate.go  — /delegate endpoint: trusted app issues a cross-app login code
  web/
    RULES.md
    favicon.svg
    index.html
    style.css
    script.js
```

Each provider owns its own in-memory store, TTL cleanup, and handlers. Shared utilities live in `main.go`.

---

## Shared State (main.go)

| Symbol | Type | Used by |
|---|---|---|
| `appTokens` | `map[string]bool` | exchange.go |
| `directRedirect` | `string` | main.go, google.go |
| `httpClient` | `*http.Client` | telegram.go, google.go |
| `sessionTTL` | `const` | telegram.go, google.go |
| `codeTTL` | `const` | exchange.go |
| `randToken` | `func` | telegram.go, google.go, exchange.go |
| `randHex` | `func` | solana.go |
| `jsonOK / jsonErr` | `func` | all handlers |

---

## Adding a New Auth Provider

1. Create `<provider>.go` in `build/`
2. Add any in-memory state and a `init<Provider>()` function if config is needed
3. Call `init<Provider>()` from `main()` in `main.go`
4. Register routes in the `mux` block in `main.go`
5. Call `newCode(user, "<provider>")` on successful auth to issue a one-time code
6. Add a tile + flow to `web/index.html` and `web/script.js`

> Cross-app login (`delegate.go`) issues codes via `newCodeVia(user, method, "delegate")`,
> which records a `Via` transport marker on the code alongside the real provider in `method`.
> `/exchange` echoes `via` back in its response when set; first-party logins leave it empty.

---

## Logging

One line per auth event. Format: `key=value` pairs.

```
telegram uid=123456 name="Ivan Petrov"
solana pubkey=5ZX8wKF...
google sub=117... email=ivan@gmail.com
exchange method=telegram via= user=map[...]
exchange error=unauthorized from=1.2.3.4
delegate user_id=123456 method=google
delegate error=unauthorized from=1.2.3.4
```

Rules:
- Log successful auth and all errors — nothing in between
- Always include the provider and the permanent user identifier
- Never log tokens, codes, or secrets

The request logger in `main.go` covers HTTP-level logging automatically.

---

## In-Memory TTLs

| Store | TTL | Cleaned |
|---|---|---|
| Telegram sessions | `sessionTTL` = 5 min | on each `POST /qr-session` |
| Google OAuth states | `sessionTTL` = 5 min | on expiry check in callback |
| One-time codes | `codeTTL` = 60 sec | on each `POST /exchange` |
| Solana nonces | no TTL — single use | deleted immediately on use |

---

## Deployment

Production binary committed to git. Server does `git pull` + `systemctl restart auth-center`.

After adding a new client app, add its `APP_TOKEN` to `APP_TOKENS` in the systemd service file and restart.
