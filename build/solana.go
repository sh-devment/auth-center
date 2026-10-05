package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/mr-tron/base58"
)

// ── nonce store ───────────────────────────────────────────────────────────────

var (
	nonces   = make(map[string]string)
	noncesMu sync.Mutex
)

// ── handlers ──────────────────────────────────────────────────────────────────

// POST /solana/nonce
func handleSolanaNonce(w http.ResponseWriter, r *http.Request) {
	nonce := "Sign in to Auth Center\nNonce: " + randHex(16)
	token := randHex(16)
	noncesMu.Lock()
	nonces[token] = nonce
	noncesMu.Unlock()
	jsonOK(w, map[string]string{"nonce": nonce, "token": token})
}

// POST /solana/auth
func handleSolanaAuth(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PublicKey  string `json:"public_key"`
		Signature  string `json:"signature"`
		Nonce      string `json:"nonce"`
		NonceToken string `json:"nonce_token"`
		Redirect   string `json:"redirect"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, "no data", http.StatusBadRequest)
		return
	}
	if body.PublicKey == "" || body.Signature == "" || body.Nonce == "" || body.NonceToken == "" {
		jsonErr(w, "missing fields", http.StatusBadRequest)
		return
	}

	noncesMu.Lock()
	expected, ok := nonces[body.NonceToken]
	if ok {
		delete(nonces, body.NonceToken)
	}
	noncesMu.Unlock()
	if !ok || expected != body.Nonce {
		jsonErr(w, "invalid or expired nonce", http.StatusForbidden)
		return
	}

	pubKeyBytes, err := base58.Decode(body.PublicKey)
	if err != nil || len(pubKeyBytes) != 32 {
		jsonErr(w, "invalid public key", http.StatusBadRequest)
		return
	}
	sigBytes, err := base64.StdEncoding.DecodeString(body.Signature)
	if err != nil {
		jsonErr(w, "invalid signature encoding", http.StatusBadRequest)
		return
	}
	if !ed25519.Verify(ed25519.PublicKey(pubKeyBytes), []byte(body.Nonce), sigBytes) {
		log.Printf("solana error=invalid_signature pubkey=%s", body.PublicKey)
		jsonErr(w, "invalid signature", http.StatusForbidden)
		return
	}

	log.Printf("solana pubkey=%s", body.PublicKey)
	resp := map[string]any{"ok": true, "public_key": body.PublicKey}
	if body.Redirect != "" {
		user := map[string]any{"id": body.PublicKey}
		resp["code"] = newCode(user, "solana")
		resp["redirect"] = body.Redirect
	}
	jsonOK(w, resp)
}
