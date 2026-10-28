package main

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// OAuth providers (Google, Yandex) share the round trip: /<provider>/login
// keeps the client's redirect under a random state, the provider sends the
// user back to /<provider>/callback with that state, and finishOAuth ends it.

// ── state store ───────────────────────────────────────────────────────────────

type oauthState struct {
	Provider  string
	Redirect  string
	CreatedAt time.Time
}

var (
	oauthStates   = make(map[string]oauthState)
	oauthStatesMu sync.Mutex
)

// newOAuthState stores the redirect and returns the state to send the provider.
func newOAuthState(provider, redirect string) string {
	state := randToken(32)
	oauthStatesMu.Lock()
	defer oauthStatesMu.Unlock()
	for k, s := range oauthStates {
		if time.Since(s.CreatedAt) > sessionTTL {
			delete(oauthStates, k)
		}
	}
	oauthStates[state] = oauthState{Provider: provider, Redirect: redirect, CreatedAt: time.Now()}
	return state
}

// takeOAuthState spends a state: it works once, within sessionTTL, and only on
// the callback of the provider it was minted for.
func takeOAuthState(provider, state string) (redirect string, ok bool) {
	oauthStatesMu.Lock()
	s, ok := oauthStates[state]
	if ok {
		delete(oauthStates, state)
	}
	oauthStatesMu.Unlock()

	if !ok || s.Provider != provider || time.Since(s.CreatedAt) > sessionTTL {
		return "", false
	}
	return s.Redirect, true
}

// finishOAuth sends the user back to the client with a one-time code, or to
// DIRECT_REDIRECT when the login was started without ?redirect=.
func finishOAuth(w http.ResponseWriter, r *http.Request, redirect string, user map[string]any, method string) {
	if redirect != "" {
		oneTimeCode := newCode(user, method)
		sep := "?"
		if strings.Contains(redirect, "?") {
			sep = "&"
		}
		http.Redirect(w, r, redirect+sep+"code="+oneTimeCode, http.StatusFound)
		return
	}

	target := directRedirect
	if target == "" {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusFound)
}
