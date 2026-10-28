package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/color"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
)

// ── config ────────────────────────────────────────────────────────────────────

var (
	botToken    string
	botUsername string

	// miniappShortName is the mini app registered with @BotFather on the same
	// bot. Telegram logins go there and nowhere else — the bot deeplink and its
	// webhook were removed once the mini app proved itself in production; see
	// tools/old_bot_auth.md for how that worked.
	miniappShortName string
)

// Link flags. The session token alone cannot say how the user reached the mini
// app, and the mini app has to know: a scanned QR means the browser waiting for
// the login is on another machine, so there is nothing to send the user "back"
// to on the phone in their hand.
//
// The flag is one character in front of the token, always present, so the token
// is always everything after the first character. A prefix that could be absent
// would be ambiguous — session tokens are base64url and may themselves start
// with any letter.
const (
	linkFromQR     = "q"
	linkFromButton = "b"
)

func initTelegram() {
	botToken = os.Getenv("BOT_TOKEN")
	botUsername = os.Getenv("BOT_USERNAME")
	miniappShortName = os.Getenv("MINIAPP_SHORT_NAME")
	if miniappShortName == "" {
		log.Print("telegram: MINIAPP_SHORT_NAME not set — the Telegram login has nowhere to send users")
		return
	}
	log.Printf("telegram: logins go to mini app t.me/%s/%s", botUsername, miniappShortName)
}

// telegramLink is where a login session sends the user, flagged with how the
// link is being handed over. The QR image and the button carry the same session
// and differ only in that flag.
func telegramLink(tok, from string) string {
	return fmt.Sprintf("https://t.me/%s/%s?startapp=%s%s", botUsername, miniappShortName, from, tok)
}

// ── session store ─────────────────────────────────────────────────────────────

type Session struct {
	Status    string
	User      map[string]any
	CreatedAt time.Time
	Redirect  string
	Code      string
}

var (
	sessions   = make(map[string]*Session)
	sessionsMu sync.Mutex
)

func cleanSessions() {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	for k, v := range sessions {
		if time.Since(v.CreatedAt) > sessionTTL {
			delete(sessions, k)
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func makeQR(url string) (string, error) {
	qr, err := qrcode.New(url, qrcode.Medium)
	if err != nil {
		return "", err
	}
	// matches web/style.css: --bg-sunk on --ink
	qr.BackgroundColor = color.RGBA{R: 10, G: 10, B: 11, A: 255}
	qr.ForegroundColor = color.RGBA{R: 245, G: 245, B: 247, A: 255}
	png, err := qr.PNG(256)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(png), nil
}

// ── handlers ──────────────────────────────────────────────────────────────────

// POST /qr-session
func handleQRSession(w http.ResponseWriter, r *http.Request) {
	// Without a mini app there is no Telegram login at all, and building the
	// link anyway would hand the browser a t.me URL with an empty path. Say so
	// instead — Google and Solana are unaffected and the page keeps working.
	if miniappShortName == "" {
		jsonErr(w, "telegram login not configured", http.StatusServiceUnavailable)
		return
	}

	cleanSessions()
	var body struct {
		Redirect string `json:"redirect"`
	}
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

	tok := randToken(32)
	sessionsMu.Lock()
	sessions[tok] = &Session{
		Status:    "pending",
		CreatedAt: time.Now(),
		Redirect:  body.Redirect,
	}
	sessionsMu.Unlock()

	// The QR is scanned by a phone while the browser waiting for the login sits
	// on another machine; the button is pressed on the machine that is waiting.
	// Same session either way — the mini app just needs to know which, so it
	// can leave out a "back" button that would lead to the wrong device.
	qr, err := makeQR(telegramLink(tok, linkFromQR))
	if err != nil {
		jsonErr(w, "qr error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"token": tok, "qr": qr, "url": telegramLink(tok, linkFromButton)})
}

// GET /poll/{token}
func handlePoll(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")

	sessionsMu.Lock()
	sess, ok := sessions[tok]
	if !ok {
		sessionsMu.Unlock()
		jsonErr(w, "expired", http.StatusNotFound)
		return
	}
	if time.Since(sess.CreatedAt) > sessionTTL {
		delete(sessions, tok)
		sessionsMu.Unlock()
		jsonErr(w, "expired", http.StatusNotFound)
		return
	}
	resp := map[string]any{"status": sess.Status, "user": sess.User}
	if sess.Status == "authenticated" && sess.Redirect != "" {
		resp["code"] = sess.Code
		resp["redirect"] = sess.Redirect
	}
	sessionsMu.Unlock()

	jsonOK(w, resp)
}
