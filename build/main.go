package main

import (
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// ── build info ────────────────────────────────────────────────────────────────

var buildTime = "unknown"

// ── embedded web files ────────────────────────────────────────────────────────

//go:embed web
var webFiles embed.FS

// ── shared config ─────────────────────────────────────────────────────────────

var (
	appTokens      map[string]bool
	directRedirect string
	httpClient     = &http.Client{Timeout: 10 * time.Second}
)

const (
	sessionTTL = 5 * time.Minute
	codeTTL    = 60 * time.Second
)

// ── shared helpers ────────────────────────────────────────────────────────────

func randToken(n int) string {
	b := make([]byte, n)
	rand.Read(b) //nolint:errcheck
	return base64.RawURLEncoding.EncodeToString(b)
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func jsonErr(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}

// ── request logging ───────────────────────────────────────────────────────────

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(status int) {
	sw.status = status
	sw.ResponseWriter.WriteHeader(status)
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
	})
}

// ── template ──────────────────────────────────────────────────────────────────

var indexTmpl *template.Template

func initTemplate() {
	src, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		log.Fatalf("index.html not found: %v", err)
	}
	indexTmpl = template.Must(template.New("index").Parse(string(src)))
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	redirectURL := r.URL.Query().Get("redirect")
	if redirectURL == "" && directRedirect != "" {
		http.Redirect(w, r, directRedirect, http.StatusFound)
		return
	}
	rdJSON, _ := json.Marshal(redirectURL)
	indexTmpl.Execute(w, struct{ RedirectURL template.JS }{ //nolint:errcheck
		RedirectURL: template.JS(rdJSON),
	})
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "--info") {
		fmt.Printf("auth-center built: %s\n", buildTime)
		os.Exit(0)
	}

	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	godotenv.Load() //nolint:errcheck

	directRedirect = os.Getenv("DIRECT_REDIRECT")
	miniappHomeRedirect = os.Getenv("MINIAPP_DIRECT_REDIRECT")

	appTokens = make(map[string]bool)
	for _, t := range strings.Split(os.Getenv("APP_TOKENS"), ",") {
		if t = strings.TrimSpace(t); t != "" {
			appTokens[t] = true
		}
	}

	initTelegram()
	initMiniapp()
	initGoogle()
	initTemplate()

	webFS, _ := fs.Sub(webFiles, "web")
	fileServer := http.FileServer(http.FS(webFS))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", handleIndex)
	mux.HandleFunc("POST /qr-session", handleQRSession)
	mux.HandleFunc("GET /poll/{token}", handlePoll)
	mux.HandleFunc("POST /miniapp/auth", handleMiniappAuth)
	mux.HandleFunc("POST /miniapp/home", handleMiniappHome)
	mux.HandleFunc("POST /solana/nonce", handleSolanaNonce)
	mux.HandleFunc("POST /solana/auth", handleSolanaAuth)
	mux.HandleFunc("GET /google/login", handleGoogleLogin)
	mux.HandleFunc("GET /google/callback", handleGoogleCallback)
	mux.HandleFunc("POST /exchange", handleExchange)
	mux.HandleFunc("POST /delegate", handleDelegate)
	mux.Handle("GET /style.css", fileServer)
	mux.Handle("GET /script.js", fileServer)
	mux.Handle("GET /favicon.svg", fileServer)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8886"
	}
	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, logMiddleware(mux)))
}
