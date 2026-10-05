package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// POST /delegate — server-to-server only, never called from the browser.
//
// Called by a trusted app to issue a one-time cross-app login code for a user
// who is already authenticated in that app. Any other trusted app can redeem
// the code via POST /exchange — no target binding needed since all apps are trusted.
func handleDelegate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID    string `json:"user_id"`
		AppToken  string `json:"app_token"`
		Method    string `json:"method"`     // original identity provider (google/telegram/solana)
		Name      string `json:"name"`       // display name (Google style)
		FirstName string `json:"first_name"` // Telegram/Solana style
		LastName  string `json:"last_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, "no data", http.StatusBadRequest)
		return
	}
	if body.UserID == "" || body.AppToken == "" {
		jsonErr(w, "missing fields", http.StatusBadRequest)
		return
	}
	if len(appTokens) > 0 && !appTokens[body.AppToken] {
		log.Printf("delegate error=unauthorized from=%s", r.RemoteAddr)
		jsonErr(w, "unauthorized", http.StatusForbidden)
		return
	}

	// Echo the delegating app's identity back so the receiving app shows the real
	// provider + name (auth-center is stateless and stores no profile of its own).
	// method is the true provider; "delegate" is recorded separately as the transport
	// via the Via marker so the name/provider paths in client templates keep working.
	user := map[string]any{"id": body.UserID}
	if body.Name != "" {
		user["name"] = body.Name
	}
	if body.FirstName != "" {
		user["first_name"] = body.FirstName
	}
	if body.LastName != "" {
		user["last_name"] = body.LastName
	}

	method := body.Method
	if method == "" {
		method = "delegate" // backward compat: caller that sends no provider
	}

	code := newCodeVia(user, method, "delegate")
	log.Printf("delegate user_id=%s method=%s", body.UserID, method)
	jsonOK(w, map[string]string{"code": code})
}
