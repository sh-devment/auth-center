package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

// ── shared secret ─────────────────────────────────────────────────────────────

// auth-miniapp verifies Telegram initData itself, so it already holds BOT_TOKEN.
// Rather than introduce a second credential, both sides derive the same value
// from that token and compare it. The token itself never travels over the wire.
const miniappSecretPurpose = "auth-center/miniapp/v1"

var miniappSecret string

// miniappHomeRedirect is where a Telegram user who walked in on their own ends
// up — see handleMiniappHome. Empty disables that route and nothing else.
//
// It lives here rather than in auth-miniapp on purpose. auth-miniapp holds a
// secret that lets it assert Telegram identities it has verified, and that is
// all it should hold: if it also named the destination, it could point a valid
// login code at any address it liked. Destinations stay auth-center's to
// decide, exactly as they are for every session that comes in with a redirect.
//
// It is the Telegram twin of DIRECT_REDIRECT, which answers the same question
// for a browser that arrives at auth-center with no ?redirect= — same idea,
// different front door, deliberately a separate value so the two can point at
// different apps.
var miniappHomeRedirect string

func miniappSecretFrom(token string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(miniappSecretPurpose)) //nolint:errcheck
	return hex.EncodeToString(mac.Sum(nil))
}

func initMiniapp() {
	if botToken == "" {
		log.Print("miniapp: BOT_TOKEN not set — POST /miniapp/auth and /miniapp/home disabled")
		return
	}
	miniappSecret = miniappSecretFrom(botToken)

	if miniappHomeRedirect == "" {
		log.Print("miniapp: MINIAPP_DIRECT_REDIRECT not set — POST /miniapp/home disabled")
	}
}

// ── what both miniapp routes share ────────────────────────────────────────────

// miniappUser is the identity auth-miniapp asserts. It has already checked the
// Telegram signature over it; nothing here re-verifies anything, which is
// precisely why the secret below matters.
type miniappUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

func (u miniappUser) asMap() map[string]any {
	return map[string]any{
		"id":         u.ID,
		"first_name": u.FirstName,
		"last_name":  u.LastName,
		"username":   u.Username,
	}
}

// authorizeMiniapp is the door both routes come through: it answers whether the
// caller proved it holds the same BOT_TOKEN we do, and writes the refusal
// itself when it does not. route names the caller in the log, since "disabled"
// and "unauthorized" mean very different things depending on which one it was.
func authorizeMiniapp(w http.ResponseWriter, r *http.Request, route, secret string) bool {
	if miniappSecret == "" {
		log.Printf("miniapp/%s error=disabled from=%s", route, r.RemoteAddr)
		jsonErr(w, "miniapp auth not configured", http.StatusServiceUnavailable)
		return false
	}
	if !hmac.Equal([]byte(secret), []byte(miniappSecret)) {
		log.Printf("miniapp/%s error=unauthorized from=%s", route, r.RemoteAddr)
		jsonErr(w, "unauthorized", http.StatusForbidden)
		return false
	}
	return true
}

// ── handler ───────────────────────────────────────────────────────────────────

// POST /miniapp/auth — server-to-server only, never called from the browser.
//
// This is how a Telegram identity reaches auth-center: auth-miniapp checks the
// initData signature and reports the identity here, naming the session by the
// token it carried in start_param. From this point the flow is the ordinary one
// — the browser tab picks the result up on /poll. The bot round-trip this
// replaced is written up in tools/old_bot_auth.md.
func handleMiniappAuth(w http.ResponseWriter, r *http.Request) {
	cleanSessions()

	var body struct {
		SessionToken string `json:"session_token"`
		Secret       string `json:"secret"`
		// FromQR says the user scanned the QR, which means the browser waiting
		// for this login is on another machine. No code is issued in that case:
		// the mini app has nowhere useful to send the phone in the user's hand,
		// and a one-time code nobody spends is a credential kept alive for no
		// reason.
		FromQR bool        `json:"from_qr"`
		User   miniappUser `json:"user"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, "no data", http.StatusBadRequest)
		return
	}

	if !authorizeMiniapp(w, r, "auth", body.Secret) {
		return
	}
	// An empty session token stays an error and must never quietly become
	// something else: a caller that lost the token on the way is a bug, and a
	// bug that ends in a successful login is the worst kind. The guest case has
	// a route of its own — see handleMiniappHome.
	if body.SessionToken == "" || body.User.ID == 0 {
		jsonErr(w, "missing fields", http.StatusBadRequest)
		return
	}

	from := body.User
	sessionsMu.Lock()
	sess, ok := sessions[body.SessionToken]
	if !ok {
		sessionsMu.Unlock()
		log.Printf("miniapp error=unknown_session uid=%d", from.ID)
		jsonErr(w, "expired", http.StatusNotFound)
		return
	}
	if time.Since(sess.CreatedAt) > sessionTTL {
		delete(sessions, body.SessionToken)
		sessionsMu.Unlock()
		log.Printf("miniapp error=expired_session uid=%d", from.ID)
		jsonErr(w, "expired", http.StatusNotFound)
		return
	}
	if sess.Status != "pending" {
		sessionsMu.Unlock()
		log.Printf("miniapp error=session_used uid=%d", from.ID)
		jsonErr(w, "session already used", http.StatusConflict)
		return
	}

	user := from.asMap()
	sess.Status = "authenticated"
	sess.User = user
	if sess.Redirect != "" {
		sess.Code = newCode(user, "telegram")
	}
	redirect := sess.Redirect
	sessionsMu.Unlock()

	log.Printf("miniapp uid=%d name=%q", from.ID, strings.TrimSpace(from.FirstName+" "+from.LastName))

	// Hand back the app's address plus a code of the mini app's own, so it can
	// offer a way home that lands signed in. A scanned QR gets the address but
	// no code — see FromQR above.
	//
	// It has to be a second code, not the one the session holds: the browser tab
	// that started the login is polling for that one and will spend it, and a
	// one-time code spent twice fails for whoever is second. Two codes for one
	// verified identity give nothing away — each is single-use, each expires in
	// codeTTL, and redeeming either still needs an app token.
	resp := map[string]any{"ok": true, "redirect": redirect}
	if redirect != "" && !body.FromQR {
		resp["code"] = newCode(user, "telegram")
	}
	jsonOK(w, resp)
}

// ── the front door ────────────────────────────────────────────────────────────

// POST /miniapp/home — server-to-server only, never called from the browser.
//
// The other route answers "a login is in progress, here is who it is". This one
// answers a different question: somebody opened the mini app on their own —
// from the bot, from a t.me link, from Telegram's menu button — with no login
// waiting anywhere. Until this existed that ended in an error, which is a poor
// greeting for the one entry point a human is likely to find by accident.
//
// So there is no session to bind and nobody polling: auth-center simply mints a
// code for the app named by MINIAPP_DIRECT_REDIRECT and hands it back, and the
// mini app page offers the way in. From the receiving app's side nothing is
// unusual — method is "telegram", there is no `via` marker, and the code is
// redeemed through the same POST /exchange as every other login. It is a normal
// Telegram login that simply began in Telegram rather than in a browser.
func handleMiniappHome(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Secret string      `json:"secret"`
		User   miniappUser `json:"user"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, "no data", http.StatusBadRequest)
		return
	}

	if !authorizeMiniapp(w, r, "home", body.Secret) {
		return
	}
	if miniappHomeRedirect == "" {
		log.Printf("miniapp/home error=no_redirect uid=%d", body.User.ID)
		jsonErr(w, "miniapp home not configured", http.StatusServiceUnavailable)
		return
	}
	if body.User.ID == 0 {
		jsonErr(w, "missing fields", http.StatusBadRequest)
		return
	}

	user := body.User.asMap()
	code := newCode(user, "telegram")

	log.Printf("miniapp/home uid=%d name=%q to=%s",
		body.User.ID, strings.TrimSpace(body.User.FirstName+" "+body.User.LastName), miniappHomeRedirect)

	jsonOK(w, map[string]any{"ok": true, "redirect": miniappHomeRedirect, "code": code})
}
