package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
)

// ── config ────────────────────────────────────────────────────────────────────

var (
	yandexClientID     string
	yandexClientSecret string
	yandexCallbackURL  string
)

func initYandex() {
	yandexClientID = os.Getenv("YANDEX_CLIENT_ID")
	yandexClientSecret = os.Getenv("YANDEX_CLIENT_SECRET")
	yandexCallbackURL = os.Getenv("YANDEX_CALLBACK_URL")
	if yandexCallbackURL == "" {
		yandexCallbackURL = "http://localhost:8886/yandex/callback"
	}
}

// ── handlers ──────────────────────────────────────────────────────────────────

// GET /yandex/login
func handleYandexLogin(w http.ResponseWriter, r *http.Request) {
	state := newOAuthState("yandex", r.URL.Query().Get("redirect"))

	// No scope: Yandex grants what the app was registered with in
	// oauth.yandex.ru (login:info for the id and name, login:email).
	params := url.Values{
		"client_id":     {yandexClientID},
		"redirect_uri":  {yandexCallbackURL},
		"response_type": {"code"},
		"state":         {state},
	}
	http.Redirect(w, r, "https://oauth.yandex.ru/authorize?"+params.Encode(), http.StatusFound)
}

// GET /yandex/callback
func handleYandexCallback(w http.ResponseWriter, r *http.Request) {
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		log.Printf("yandex error=%s", errParam)
		http.Error(w, "yandex auth error: "+errParam, http.StatusBadRequest)
		return
	}

	authCode := r.URL.Query().Get("code")
	redirect, ok := takeOAuthState("yandex", r.URL.Query().Get("state"))
	if !ok {
		http.Error(w, "invalid or expired state", http.StatusBadRequest)
		return
	}

	tokenResp, err := httpClient.PostForm("https://oauth.yandex.ru/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {authCode},
		"client_id":     {yandexClientID},
		"client_secret": {yandexClientSecret},
	})
	if err != nil {
		log.Printf("yandex error=token_exchange: %v", err)
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}
	defer tokenResp.Body.Close()
	var tokenData struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	json.NewDecoder(tokenResp.Body).Decode(&tokenData) //nolint:errcheck

	if tokenData.AccessToken == "" {
		log.Printf("yandex error=token_response: %d %s", tokenResp.StatusCode, tokenData.Error)
		http.Error(w, "token error: "+tokenData.Error, http.StatusBadRequest)
		return
	}

	req, _ := http.NewRequest("GET", "https://login.yandex.ru/info?format=json", nil)
	req.Header.Set("Authorization", "OAuth "+tokenData.AccessToken)
	userResp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("yandex error=userinfo: %v", err)
		http.Error(w, "userinfo fetch failed", http.StatusInternalServerError)
		return
	}
	defer userResp.Body.Close()
	var info struct {
		ID           string `json:"id"`
		DefaultEmail string `json:"default_email"`
		RealName     string `json:"real_name"`
		DisplayName  string `json:"display_name"`
	}
	json.NewDecoder(userResp.Body).Decode(&info) //nolint:errcheck

	// Without the id there is no identity to hand out, whatever else came back.
	if info.ID == "" {
		log.Printf("yandex error=userinfo_response: %d", userResp.StatusCode)
		http.Error(w, "userinfo fetch failed", http.StatusBadGateway)
		return
	}

	name := info.RealName
	if name == "" {
		name = info.DisplayName
	}
	user := map[string]any{"id": info.ID, "email": info.DefaultEmail, "name": name}

	log.Printf("yandex id=%s email=%s", info.ID, info.DefaultEmail)

	finishOAuth(w, r, redirect, user, "yandex")
}
