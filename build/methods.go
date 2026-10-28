package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

// knownMethods are the login methods this binary can serve, in tile order.
// Which of them an instance offers is decided at deploy time by METHODS (the
// Deploy matrix), not by the region: one binary, zones differ only in config.
var knownMethods = []string{"google", "yandex", "telegram", "solana"}

// Methods this instance offers, set once by initMethods before anything serves.
var methods map[string]bool

// initMethods reads METHODS, a comma-separated subset of knownMethods. Empty
// means local dev and turns everything on. An unknown name is a config error
// and stops the process — the health check then fails and deploy.sh rolls back.
func initMethods() {
	m, err := parseMethods(os.Getenv("METHODS"))
	if err != nil {
		log.Fatal(err)
	}
	methods = m

	var on []string
	for _, name := range knownMethods {
		if methods[name] {
			on = append(on, name)
		}
	}
	log.Printf("methods: %s", strings.Join(on, ","))
}

func parseMethods(s string) (map[string]bool, error) {
	m := make(map[string]bool)
	if strings.TrimSpace(s) == "" {
		for _, name := range knownMethods {
			m[name] = true
		}
		return m, nil
	}
	for _, name := range strings.Split(s, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		known := false
		for _, k := range knownMethods {
			known = known || k == name
		}
		if !known {
			return nil, fmt.Errorf("unknown method %q in METHODS (want %s)", name, strings.Join(knownMethods, ","))
		}
		m[name] = true
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("METHODS %q names no login method", s)
	}
	return m, nil
}

// onlyIf serves h when this instance offers the method and refuses with 403
// otherwise. The page has no tile for a method that is off, so this is the
// server side of the same rule.
func onlyIf(method string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !methods[method] {
			jsonErr(w, method+" login not available here", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}
