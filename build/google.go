package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
)

// ── config ────────────────────────────────────────────────────────────────────

var (
	googleClientID     string
	googleClientSecret string
	googleCallbackURL  string
)

func initGoogle() {
	googleClientID = os.Getenv("GOOGLE_CLIENT_ID")
	googleClientSecret = os.Getenv("GOOGLE_CLIENT_SECRET")
	googleCallbackURL = os.Getenv("GOOGLE_CALLBACK_URL")
	if googleCallbackURL == "" {
		googleCallbackURL = "http://localhost:8886/google/callback"
	}
}

// ── handlers ──────────────────────────────────────────────────────────────────

// GET /google/login
func handleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	state := newOAuthState("google", r.URL.Query().Get("redirect"))

	params := url.Values{
		"client_id":     {googleClientID},
		"redirect_uri":  {googleCallbackURL},
		"response_type": {"code"},
		"scope":         {"openid email profile"},
		"state":         {state},
	}
	http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?"+params.Encode(), http.StatusFound)
}

// GET /google/callback
func handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		log.Printf("google error=%s", errParam)
		http.Error(w, "google auth error: "+errParam, http.StatusBadRequest)
		return
	}

	authCode := r.URL.Query().Get("code")
	redirect, ok := takeOAuthState("google", r.URL.Query().Get("state"))
	if !ok {
		http.Error(w, "invalid or expired state", http.StatusBadRequest)
		return
	}

	tokenBody, _ := json.Marshal(map[string]string{
		"code":          authCode,
		"client_id":     googleClientID,
		"client_secret": googleClientSecret,
		"redirect_uri":  googleCallbackURL,
		"grant_type":    "authorization_code",
	})
	tokenResp, err := httpClient.Post(
		"https://oauth2.googleapis.com/token",
		"application/json",
		bytes.NewReader(tokenBody),
	)
	if err != nil {
		log.Printf("google error=token_exchange: %v", err)
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}
	defer tokenResp.Body.Close()
	var tokenData map[string]any
	json.NewDecoder(tokenResp.Body).Decode(&tokenData) //nolint:errcheck

	if _, hasErr := tokenData["error"]; hasErr {
		log.Printf("google error=token_response: %v", tokenData["error"])
		http.Error(w, fmt.Sprintf("token error: %v", tokenData["error"]), http.StatusBadRequest)
		return
	}
	accessToken, _ := tokenData["access_token"].(string)

	req, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	userResp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("google error=userinfo: %v", err)
		http.Error(w, "userinfo fetch failed", http.StatusInternalServerError)
		return
	}
	defer userResp.Body.Close()
	var userInfo map[string]any
	json.NewDecoder(userResp.Body).Decode(&userInfo) //nolint:errcheck

	sub, _ := userInfo["sub"].(string)
	email, _ := userInfo["email"].(string)
	name, _ := userInfo["name"].(string)
	user := map[string]any{"id": sub, "email": email, "name": name}

	log.Printf("google sub=%s email=%s", sub, email)

	finishOAuth(w, r, redirect, user, "google")
}
