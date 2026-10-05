package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

// ── code store ────────────────────────────────────────────────────────────────

type Code struct {
	User      map[string]any
	Method    string
	Via       string // transport marker, e.g. "delegate"; empty for first-party logins
	CreatedAt time.Time
}

var (
	codes   = make(map[string]*Code)
	codesMu sync.Mutex
)

func cleanCodes() {
	codesMu.Lock()
	defer codesMu.Unlock()
	for k, v := range codes {
		if time.Since(v.CreatedAt) > codeTTL {
			delete(codes, k)
		}
	}
}

// newCode issues a one-time code for a verified user. Any trusted app can
// redeem it via POST /exchange.
func newCode(user map[string]any, method string) string {
	return newCodeVia(user, method, "")
}

// newCodeVia is like newCode but records a transport marker (e.g. "delegate")
// alongside the real identity provider in method.
func newCodeVia(user map[string]any, method, via string) string {
	cleanCodes()
	c := randToken(32)
	codesMu.Lock()
	codes[c] = &Code{User: user, Method: method, Via: via, CreatedAt: time.Now()}
	codesMu.Unlock()
	return c
}

// ── handler ───────────────────────────────────────────────────────────────────

// POST /exchange — server-to-server only, never called from the browser.
func handleExchange(w http.ResponseWriter, r *http.Request) {
	cleanCodes()
	var body struct {
		Code     string `json:"code"`
		AppToken string `json:"app_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		log.Printf("exchange error=decode: %v", err)
		jsonErr(w, "no data", http.StatusBadRequest)
		return
	}
	if len(appTokens) > 0 && !appTokens[body.AppToken] {
		log.Printf("exchange error=unauthorized from=%s", r.RemoteAddr)
		jsonErr(w, "unauthorized", http.StatusForbidden)
		return
	}
	if body.Code == "" {
		jsonErr(w, "missing code", http.StatusBadRequest)
		return
	}

	codesMu.Lock()
	entry, ok := codes[body.Code]
	if !ok || time.Since(entry.CreatedAt) > codeTTL {
		if ok {
			delete(codes, body.Code)
		}
		codesMu.Unlock()
		log.Printf("exchange error=invalid_code from=%s", r.RemoteAddr)
		jsonErr(w, "invalid or expired code", http.StatusForbidden)
		return
	}
	user := entry.User
	method := entry.Method
	via := entry.Via
	delete(codes, body.Code)
	codesMu.Unlock()

	log.Printf("exchange method=%s via=%s user=%v", method, via, user)
	resp := map[string]any{"ok": true, "user": user, "method": method}
	if via != "" {
		resp["via"] = via
	}
	jsonOK(w, resp)
}
