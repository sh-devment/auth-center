# Telegram Mini App — replacing the bot-chat button

**Status: BUILT.** Shipped as [auth-miniapp](/auth-miniapp/README.md) and now the only
Telegram login path — the bot and its webhook are gone (see
[`old_bot_auth.md`](old_bot_auth.md)). This document is kept as the record of the design
and of what the PoC proved; for how the thing actually works now, read auth-miniapp's
README. Where the two disagree, the README wins.

---

## 1. The problem being solved

The dead end is at the very end of a flow that otherwise works:

```
User reads a Telegram chat
   │  taps a link to an app built on auth-center
   ▼
Telegram's in-app browser opens the app          ← user is INSIDE Telegram
   │  taps "login"
   ▼
auth-center page, still in the in-app browser
   │  picks Telegram (they are on a phone, so no QR — the statistics show this
   │  is what most of these users pick, which is the obvious choice)
   ▼
"open in telegram"  →  t.me/<bot>?start=<token>
   ▼
Chat with the bot: "You are authenticated!"      ← THE DEAD END
   │
   ?  How does the user get back to the app? Not obvious. The message carries a
      link, but the user has been thrown into a chat window and has to work out
      that they should tap it or navigate back themselves.
```

The authentication itself succeeds. The user is simply left somewhere that does not
look like the app they came from, and the way back is not obvious.

**The idea:** send that user to a Mini App instead of a bot chat. A mini app is a
window on top of Telegram that our own code can close with `WebApp.close()` the
moment the identity is confirmed. Closing it drops the user back on the screen they
came from — the in-app browser tab, which has been polling all along and has already
redirected itself to the client app. Nobody has to find their way back.

---

## 2. What the PoC proved

Throwaway app on a separate domain, structured like `auth-client`:
`poc-telegram-app/` → `https://poc-telegram-auth.sh-development.ru`, bot
`@sh_pocapp_bot`, mini app short name `direct`.

Production log from a real phone, opened through
`https://t.me/sh_pocapp_bot/direct?startapp=sess_abc123`:

```
Sep 07 22:43:07 la-vps poc-telegram-app[2494401]: whoami: VERIFIED user id=507717647 username="sergey_showmelove" name="Сергей" "Шумилов" premium=false lang=en start_param="sess_abc123"
Sep 07 22:43:07 la-vps poc-telegram-app[2494401]: POST /api/whoami 200 3ms
```

Everything the real implementation depends on is in that one line:

| Proven | Why it matters |
|---|---|
| A plain `https://t.me/<bot>/<app>` link opens the mini app directly | The button in auth-center can be an ordinary link — no bot chat in between |
| `id=507717647` — the permanent Telegram ID | Same identity auth-center already uses as a primary key |
| `VERIFIED` — HMAC checked against `BOT_TOKEN` | The backend may trust the ID. Without this the whole thing is worthless |
| `start_param="sess_abc123"` arrived **inside the signed data** | A session token can ride through the link and come back untampered — this is what binds the mini app to the waiting browser tab |
| `WebApp.close()` closes the window on the client | The dead end disappears |
| No webhook involved | This path needs no `auth-proxy` |

---

## 3. How direct-link Mini Apps work

### Link formats

| Link | Effect |
|---|---|
| `https://t.me/<bot>/<app_short_name>` | Opens that mini app |
| `https://t.me/<bot>/<app_short_name>?startapp=<param>` | …and passes a parameter |
| `https://t.me/<bot>?startapp=<param>` | Main Mini App, only if one is configured for the bot |
| `tg://resolve?domain=<bot>&appname=<app>&startapp=<param>` | Native scheme, skips the `t.me` page |
| `&mode=compact` / `&mode=fullscreen` | Optional display mode |

Use the `https://t.me/…` form, never `tg://`. On a phone with Telegram installed it
opens the app immediately; on desktop it falls back to a `t.me` page with an "Open in
Telegram" button. `tg://` from a browser can silently do nothing when no client is
installed.

`<app_short_name>` is set in @BotFather via `/newapp`.

### startapp → start_param

The `?startapp=` value reaches the page two ways:

- `window.Telegram.WebApp.initDataUnsafe.start_param`
- as a field named `start_param` **inside `initData`** — i.e. covered by the HMAC

The second is the one that matters. It cannot be altered in transit without breaking
the signature, so a session token passed through the link comes back trustworthy.

Telegram does not document a length or character limit for `startapp` (for ordinary
`?start=` deep links it documents 64 base64url characters). Keep the value short and
inside `A-Za-z0-9_-`. **auth-center's existing session token already satisfies this:**
`randToken(32)` is `base64.RawURLEncoding` of 32 bytes = 43 characters from exactly
that alphabet. No new token format is needed.

There is no way to add your own query parameters to the page URL — the URL is fixed
in @BotFather and `startapp` is the only channel in.

### Verifying initData

Per <https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app>:

```
secret_key = HMAC_SHA256(key="WebAppData", data=<bot_token>)
hash       = HMAC_SHA256(key=secret_key, data=<data_check_string>)
```

`data_check_string` is every field of `initData` except `hash`, sorted by key, joined
as `key=value` with `\n`, using the already-URL-decoded values. Compare against the
`hash` field in constant time. Also check `auth_date` for freshness.

---

## 4. Target flow in auth-center

```
User inside Telegram's in-app browser, on the auth-center page
   │  picks Telegram → POST /qr-session  (unchanged, already exists)
   │      ← { token, qr, url, miniapp_url }
   │  the tab starts polling GET /poll/{token}  (unchanged, already exists)
   │
   │  taps "open mini app"  →  https://t.me/<bot>/<app>?startapp=<token>
   ▼
Telegram opens the mini app  →  GET /tg  on auth-center
   │  page reads WebApp.initData and POSTs it
   ▼
POST /miniapp/auth { init_data }
   │  verify HMAC with BOT_TOKEN
   │  start_param → session token → look up the pending session
   │  session.Status = "authenticated"; session.Code = newCode(user, "telegram")
   │  ← { ok: true }
   ▼
mini app calls WebApp.close()
   ▼
User lands back on the in-app browser tab — which has already polled,
received { status:"authenticated", code, redirect } and navigated to
<redirect>?code=<code>. The app is simply logged in.
```

The client-app half of the protocol does not change at all: same `/poll`, same
one-time code, same `POST /exchange`, same `method: "telegram"`.

### Where the mini app page lives

**On auth-center itself**, at `https://auth-center.sh-development.ru/tg`. No new
service, no new domain, no new certificate. The PoC used a separate host only because
it was a PoC.

---

## 5. Implementation plan

### New files

| File | Contents |
|---|---|
| `auth-center/build/telegram_miniapp.go` | initData verification + `GET /tg` + `POST /miniapp/auth` |
| `auth-center/build/web/tg.html` | the mini app page |
| `auth-center/build/web/tg.js` | reads `initData`, POSTs it, calls `WebApp.close()` |

Keeping this in its own file matches the rule in `auth-center-core.md`: each provider
owns its store and handlers. The store here is the **existing** `sessions` map in
`telegram.go` — the mini app is a second way into the same session, not a new one.

### Changed files

| File | Change |
|---|---|
| `build/telegram.go` | `initTelegram()` reads `BOT_APP_NAME`; `handleQRSession` also returns `miniapp_url` |
| `build/main.go` | register `GET /tg`, `GET /tg.js`, `POST /miniapp/auth` |
| `build/web/index.html` | second button in `#section-tg` |
| `build/web/script.js` | fill in the new button; **fix the poll timeout** (see §7) |
| `.env.example`, `bin/example.auth-center.service`, `README.md`, `CLAUDE.md` | document `BOT_APP_NAME` |

### New env var

```
BOT_APP_NAME=      # mini app short name from @BotFather (/newapp)
```

Leave it blank and the mini app button is never rendered — everything behaves exactly
as it does today. That is the kill switch (§10).

### The button

Today `#section-tg` has a QR image and one link:

```html
<a id="open-btn" href="#" target="_blank" class="action-btn">open in telegram</a>
```

That link is `t.me/<bot>?start=<token>` — the bot chat, i.e. the dead end. Plan:

- add `<a id="miniapp-btn" class="action-btn">open mini app</a>`, filled from
  `miniapp_url`, shown only when the server returned one;
- make it the primary button, and **demote the bot-chat link, do not delete it** — it
  is the fallback for clients too old for direct-link mini apps, where the same link
  opens the bot chat and the existing webhook path takes over unchanged.

Keep the QR exactly as it is. It is for desktop users and is not part of this problem.

---

## 6. Code to reuse (verified in the PoC)

### Verification — drop into `telegram_miniapp.go`

```go
// initDataMaxAge bounds how long a signed initData string stays acceptable. A
// mini app tab can legitimately sit open for a while, so this is generous; the
// signature — not the clock — is what proves identity.
const initDataMaxAge = 24 * time.Hour

// TGWebAppUser is the `user` object Telegram packs into initData.
type TGWebAppUser struct {
	ID           int64  `json:"id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Username     string `json:"username"`
	LanguageCode string `json:"language_code"`
	IsPremium    bool   `json:"is_premium"`
	PhotoURL     string `json:"photo_url"`
}

// validateInitData verifies the HMAC Telegram puts in `hash`:
//   secret_key = HMAC_SHA256(key="WebAppData", data=bot_token)
//   hash       = HMAC_SHA256(key=secret_key, data=data_check_string)
// data_check_string is every field except `hash`, sorted by key, joined by "\n"
// as "key=value" — using the raw, already-decoded values.
func validateInitData(initData, botToken string) (url.Values, error) {
	values, err := url.ParseQuery(initData)
	if err != nil {
		return nil, fmt.Errorf("init_data is not a query string: %w", err)
	}

	got := values.Get("hash")
	if got == "" {
		return nil, errors.New("init_data has no hash")
	}

	keys := make([]string, 0, len(values))
	for k := range values {
		if k == "hash" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(values.Get(k))
	}

	secret := hmacSHA256([]byte("WebAppData"), []byte(botToken))
	want := hex.EncodeToString(hmacSHA256(secret, []byte(b.String())))

	// Constant-time compare — this is the line that decides whether we believe
	// the Telegram ID at all.
	if !hmac.Equal([]byte(want), []byte(got)) {
		return nil, errors.New("init_data signature mismatch")
	}

	if raw := values.Get("auth_date"); raw != "" {
		sec, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("auth_date is not a unix timestamp: %w", err)
		}
		if age := time.Since(time.Unix(sec, 0)); age > initDataMaxAge {
			return nil, fmt.Errorf("init_data expired (%s old)", age.Round(time.Second))
		}
	}

	return values, nil
}

func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}
```

Imports: `crypto/hmac`, `crypto/sha256`, `encoding/hex`, `errors`, `fmt`,
`net/url`, `sort`, `strconv`, `strings`, `time`.

### Handler sketch

```go
// POST /miniapp/auth — called by the mini app page, never by a client app.
func handleMiniAppAuth(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		jsonErr(w, "bad json", http.StatusBadRequest)
		return
	}

	values, err := validateInitData(body.InitData, botToken)
	if err != nil {
		log.Printf("miniapp rejected: %v", err)
		jsonErr(w, "invalid init_data", http.StatusUnauthorized)
		return
	}

	var u TGWebAppUser
	if err := json.Unmarshal([]byte(values.Get("user")), &u); err != nil || u.ID == 0 {
		jsonErr(w, "no user in init_data", http.StatusBadRequest)
		return
	}

	tok := values.Get("start_param")   // trustworthy: covered by the signature
	if tok == "" {
		jsonErr(w, "no session", http.StatusBadRequest)
		return
	}

	// Same shape the webhook path builds, so /exchange consumers see no change.
	user := map[string]any{
		"id":         u.ID,
		"first_name": u.FirstName,
		"last_name":  u.LastName,
		"username":   u.Username,
	}

	sessionsMu.Lock()
	sess, ok := sessions[tok]
	if !ok || sess.Status != "pending" || time.Since(sess.CreatedAt) > sessionTTL {
		sessionsMu.Unlock()
		jsonErr(w, "expired", http.StatusNotFound)
		return
	}
	sess.Status = "authenticated"
	sess.User = user
	if sess.Redirect != "" {
		sess.Code = newCode(user, "telegram")
	}
	sessionsMu.Unlock()

	log.Printf("telegram uid=%d name=%q via=miniapp", u.ID, strings.TrimSpace(u.FirstName+" "+u.LastName))
	jsonOK(w, map[string]any{"ok": true})
}
```

### Page script — the essentials from the PoC

```js
const tg = window.Telegram?.WebApp;
if (!tg) { /* opened outside Telegram — show a message, do not close */ }

tg.ready();

const res  = await fetch('/miniapp/auth', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ init_data: tg.initData }),
});
const data = await res.json();

if (data.ok) {
  tg.close();                 // ← the entire point of this document
} else {
  // Show the reason and DO NOT close, or the user sees a window blink and
  // nothing happens.
}
```

Load `https://telegram.org/js/telegram-web-app.js` in `<head>` before this script.

Add a short delay or a "done" frame before `close()` only if the instant close feels
jarring in testing. The PoC used 3 s with a visible countdown, which was right for a
PoC and is probably too slow here — the user should not have to read anything.

---

## 7. Gotchas found while building the PoC

**The 20-second poll timeout will break this.** `web/script.js` currently does:

```js
pollTimeout = setTimeout(() => {
  clearInterval(pollInterval);
  document.getElementById('qr-area').classList.add('hidden');
  document.getElementById('tg-refresh').style.display = 'block';
}, 20000);
```

Twenty seconds is fine for scanning a QR code that is on screen the whole time. In the
mini app flow the user leaves the page, and a round trip through Telegram can easily
take longer. If the timeout fires, the tab stops polling and the user comes back to a
"refresh" button having already authenticated — a *worse* dead end than the one being
fixed. Raise the timeout toward `sessionTTL` (5 min) for the mini app path.

**Mobile WebViews throttle or freeze timers in the background.** The polling tab is in
the background for the whole trip. Do not rely on `setInterval` surviving it — poll
once immediately on return:

```js
document.addEventListener('visibilitychange', () => {
  if (!document.hidden && token) poll(token);
});
```

This is what actually makes the return feel instant.

**Keep the user map identical to the webhook path.** Build it from a typed struct so
`id` stays an `int64`. Client apps read Telegram IDs out of the `/exchange` JSON as
`float64` (noted in `CLAUDE.md`); changing the shape or the `method` string would
break them. `method` stays `"telegram"` — the mini app is a transport, not a new
provider.

**A mini app can be opened with no `start_param`** (from the bot menu, or a bare
`t.me/<bot>/<app>` link). There is no session to bind. Show "open this from the app you
are signing into" and do not close the window.

**The signature is the only thing that makes `start_param` safe.** Read it from the
`url.Values` returned by `validateInitData`, never by parsing `initData` separately
before verification, and never from `initDataUnsafe` on the client.

**Old clients degrade gracefully.** Direct-link mini apps arrived with Bot API 6.7
(April 2023) and were refined afterwards — check the exact minimum against the
changelog if it ever matters, because behaviour, not the version number, is what we
rely on: on a client that does not support them the same `t.me` link opens the bot
chat, `/start <token>` fires, and the existing webhook path completes the login as it
does today. This is why the webhook code and the bot-chat link stay.

**`?stay=1`-style debug flags cannot be passed through the link.** The page URL is
fixed in @BotFather; `startapp` is the only way in. If a debug switch is needed, key it
off a reserved `start_param` value.

**The webhook is still needed** — for the QR path and for old clients. This does not
remove the need for `auth-proxy`.

---

## 8. Honest limits

**This fixes the in-app-browser case, which is the case that matters.** A user who was
already inside Telegram gets closed back onto the tab they came from, and that tab has
already redirected itself. Seamless.

**A user in a standalone mobile browser (Safari/Chrome) is only partly helped.**
Tapping the link switches apps to Telegram; closing the mini app returns them to
Telegram, not to the browser. They still have to switch back by hand — but when they
do, the tab has already polled and redirected, and they never see a confusing bot chat.
Better, not solved.

**Desktop is unchanged.** The QR remains the right answer there.

---

## 9. @BotFather setup

Against the **production** bot, not the PoC one:

1. `/newapp` → pick the bot → title, short description, 640×360 image → **Web App URL**
   `https://auth-center.sh-development.ru/tg` → short name, e.g. `login`.
2. Put that short name in `BOT_APP_NAME` in the systemd unit, restart auth-center.
3. Resulting link: `https://t.me/<bot>/login?startapp=<session_token>`.

`/newapp` does not disturb the bot's existing webhook, commands, or menu button — the
QR path keeps working throughout.

---

## 10. Test plan

Backend, no Telegram needed — a bad signature must be refused:

```bash
curl -sS -X POST -H 'Content-Type: application/json' \
  -d '{"init_data":"garbage"}' https://auth-center.sh-development.ru/miniapp/auth
# expect: 401, {"error":"invalid init_data"}
```

To exercise the success path from the command line, sign a fake `initData` with the
bot token using the same algorithm (build `data_check_string`, HMAC it, append
`&hash=…`). The PoC carried this as a Go test — worth recreating as
`telegram_miniapp_test.go` with these cases, all of which passed there:

- correctly signed initData is accepted
- a Telegram ID edited after signing is rejected
- initData signed by a different bot token is rejected
- an expired `auth_date` is rejected
- initData with no `hash` is rejected
- `start_param` survives verification, and editing it breaks the signature

On a real phone, the whole point:

1. Post a link to a client app into a Telegram chat and open it **from inside
   Telegram** — this is the exact scenario from §1.
2. Log in → Telegram → "open mini app".
3. Expect: the mini app appears, closes itself, and the user is looking at the client
   app, logged in. No bot chat anywhere.
4. `journalctl -u auth-center -f` shows `telegram uid=… via=miniapp` and no webhook
   line for that login.
5. Repeat on iOS and Android — the return-after-close behaviour is the risky part and
   it is client-specific.
6. Repeat once with the QR path to confirm nothing regressed.

---

## 11. Rollout and rollback

`BOT_APP_NAME` is the kill switch. Blank → no `miniapp_url` from `/qr-session` → no
button → today's behaviour exactly. Nothing else keys off it.

Rollback is therefore: clear `BOT_APP_NAME`, `systemctl restart auth-center`. No
redeploy, no revert, and any login in flight still completes through the webhook path.

Worth measuring after the change, since the whole justification is a statistic:
compare completed logins against started Telegram sessions before and after. The
failure this fixes is invisible in the auth logs — the user *does* authenticate; they
just never arrive back at the app.
