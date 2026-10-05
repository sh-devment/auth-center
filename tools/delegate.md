# Delegate — cross-app login between apps in the auth-center system

Hand this document to a developer of any app in the system. After reading it they
should be able to (a) let their app send a logged-in user into another app, and
(b) correctly receive a user delegated from another app — with the **real provider
and name**, not "no name".

---

## What "delegate" is

A logged-in user in **app A** opens **app B** without re-authenticating. App A asks
auth-center for a one-time login code on the user's behalf, then redirects the
browser to app B with that code. App B redeems the code exactly like a normal login.

```
User (logged into App A)
      │  clicks "open App B"
      ▼
App A backend ──POST /delegate {user_id, app_token, method, name…}──▶ auth-center
      │                                                         returns { "code": … }
      │  302 redirect browser to  https://app-B/?code=<code>
      ▼
App B backend ──POST /exchange {code, app_token}──▶ auth-center
                                              returns { ok, method, via:"delegate", user{…} }
      │
      ▼
App B creates its own session and renders the user's name + provider.
```

**auth-center is stateless.** It stores no user profile. `/delegate` forwards only
what app A puts in the request body. If app A sends just `user_id`, app B gets back
only the id and shows "no name". So **app A must persist the user's name + provider
at login time and forward them on every `/delegate` call.** That is the whole trick.

---

## Endpoints (auth-center)

Base URL (production): `https://auth-center.sh-development.ru`
Server-to-server calls use your `AUTH_INTERNAL` URL (same host, or `http://localhost:8886`
if co-located). The browser redirect uses the public URL.

### `POST /delegate` — server-to-server only, never from the browser

Request body:

```json
{
  "user_id":    "105993223363753461270",
  "app_token":  "<APP_A_TOKEN>",
  "method":     "google",
  "name":       "Сергей Шумилов",
  "first_name": "Сергей Шумилов"
}
```

| Field        | Required | Notes |
|--------------|:--------:|-------|
| `user_id`    | **yes**  | Permanent provider id (Google `sub`, Telegram numeric id, Solana pubkey). This is the user's identity across all apps. |
| `app_token`  | **yes**  | App A's secret. Must be listed in auth-center's `APP_TOKENS`. |
| `method`     | no\*     | The **real** identity provider: `google` / `telegram` / `solana`. If omitted, auth-center defaults it to `delegate`, which makes app B mislabel the provider and drop the name — **always send it**. |
| `name`       | no       | Display name, Google style. Send for Google users. |
| `first_name` | no       | Telegram / Solana style. |
| `last_name`  | no       | Telegram / Solana style. |

\* Not enforced by the server, but functionally required — always send it.

Send the fields that match the original provider:

- **Google user:** `method: "google"` + `name`
- **Telegram user:** `method: "telegram"` + `first_name` (+ `last_name`)
- **Solana user:** `method: "solana"` (+ `first_name` if you have a label)

> **Tip:** If your app stores only a single collapsed display name, send it as **both**
> `name` and `first_name`. The receiver reads `name` for Google and joins
> `first_name`/`last_name` for Telegram/Solana, so covering both costs nothing and the
> receiver picks whichever its renderer needs.

Success response:

```json
{ "code": "<one-time-code>" }
```

Error response (e.g. bad/unknown `app_token`):

```json
{ "error": "unauthorized" }
```

The `code` is **single-use** and lives **60 seconds**. Use it immediately by
redirecting the browser to app B.

### `POST /exchange` — how app B redeems the code (server-to-server only)

This is the same endpoint every app already uses for normal login. Request:

```json
{ "code": "<one-time-code>", "app_token": "<APP_B_TOKEN>" }
```

Response for a delegated user:

```json
{
  "ok": true,
  "method": "google",
  "via": "delegate",
  "user": {
    "id": "105993223363753461270",
    "name": "Сергей Шумилов"
  }
}
```

- `method` — the **true** provider (`google`/`telegram`/`solana`), carried through from app A.
- `via` — `"delegate"` when the code came from `/delegate`; **absent** for first-party logins. It's a transport marker for logging/audit; apps may ignore it. Do **not** treat `via` as the provider.
- `user` — exactly the profile fields app A forwarded, plus `id`.

---

## App A — the delegating (sending) app

Add a backend route, behind your own auth, that the "open App B" button points to.
It must run on the **server** (it uses `app_token`, which never touches the browser).

1. Resolve the current user's permanent `user_id`, display `name`, and `method`
   (provider) from your session / DB.
2. `POST {AUTH_INTERNAL}/delegate` with `user_id` + `app_token` + `method` + name fields.
3. Read the returned one-time `code`.
4. `302` redirect the browser to `https://<app-B>/?code=<code>`.

### Reference implementation (Go)

This is the working implementation from the reference client (`auth-client/build/auth-server.go`):

```go
// delegateCode obtains a one-time cross-app login code from auth-center,
// letting the current user open another app without re-authenticating.
func delegateCode(uid int64) (string, error) {
	user, err := getUserByID(uid)
	if err != nil || user == nil {
		return "", fmt.Errorf("user not found")
	}
	// auth-center is stateless: forward our stored identity so app B can show
	// the real provider + name. We store one collapsed Name, so mirror it into
	// first_name as well (Google renderers read name; Telegram/Solana join
	// first_name/last_name).
	payload := map[string]string{
		"user_id":   user.AuthID,   // permanent provider id
		"app_token": appToken,
		"method":    user.Method,   // "google" / "telegram" / "solana"
	}
	if user.Name != "" {
		payload["name"] = user.Name
		payload["first_name"] = user.Name
	}
	body, _ := json.Marshal(payload)
	resp, err := httpClient.Post(authInternal+"/delegate", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("delegate: %w", err)
	}
	defer resp.Body.Close()
	var data struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&data)
	if data.Code == "" {
		return "", fmt.Errorf("delegate failed: %s", data.Error)
	}
	return data.Code, nil
}

// HTTP handler behind auth — the "open App B" button targets this.
func handleOpenAppB(w http.ResponseWriter, r *http.Request) {
	uid := sessionUserID(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	code, err := delegateCode(uid)
	if err != nil {
		log.Printf("open-app-b uid=%d error=%v", uid, err)
		http.Error(w, "could not open app", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "https://<app-B-domain>/?code="+code, http.StatusFound)
}
```

---

## App B — the receiving app

**No delegate-specific code is needed.** App B already handles `/?code=…` for normal
auth-center login: it reads `code` from the query, calls `POST {AUTH_INTERNAL}/exchange`,
and creates a session from the response. A delegated code flows through the exact same
path. App B only needs to render the user correctly from the `/exchange` response.

### Name-rendering rule (must match across all apps)

```go
func extractName(method string, user map[string]any) string {
	if method == "google" {
		return user["name"]                       // Google → user.name
	}
	return join(user["first_name"], user["last_name"]) // Telegram/Solana style
}
```

Because app A sends `method` = the real provider (not `"delegate"`) and includes the
matching name fields, this existing logic renders the correct name with **zero changes**
for a delegated session.

---

## Setup requirements

- Both app A and app B must have their `app_token` registered in auth-center's
  `APP_TOKENS` (comma-separated env var on the auth-center server). After adding a
  token, restart auth-center.
- App A needs `AUTH_INTERNAL` (server-to-server base URL) and its own `APP_TOKEN`.
- App B needs the same to call `/exchange`. No extra config beyond a normal client app.

---

## Acceptance check

1. Log into app A (e.g. with Google) → app A shows `GOOGLE` + the real name + id.
2. From app A, click "open App B" → browser lands on app B auto-logged-in.
3. App B's profile shows the **same** provider (`GOOGLE`) + **same** name + **same** id
   as app A — not `DELEGATE` / "no name".
4. Confirm app B needed **no** delegate-specific code — only its normal `/exchange` path.

---

## Responsibilities summary

| Side        | What it must do |
|-------------|-----------------|
| auth-center | Nothing to change — `/delegate` already forwards `method` / `name` / `first_name` / `last_name` and `/exchange` echoes them with `via:"delegate"`. |
| App A       | Persist `method` + name at login; expose a behind-auth backend route that POSTs them to `/delegate` and 302-redirects to `https://<app-B>/?code=<code>`. |
| App B       | Nothing delegate-specific — its existing `/?code=` → `/exchange` login path handles it; just render name via the shared `extractName` rule. |
