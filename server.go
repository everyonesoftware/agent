package main

import (
	"fmt"
	"net/http"
)

func newServer() http.Handler {
	siwc := newSIWCServiceFromEnv()
	provider := newHybridProvider(newModelProviderFromEnv(), siwc)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/chat", chatHandler(provider))
	mux.HandleFunc("GET /api/models", modelsHandler(provider))
	mux.HandleFunc("GET /auth/start", siwc.startHandler)
	mux.HandleFunc("GET /auth/callback", siwc.callbackHandler)
	mux.HandleFunc("GET /api/auth/status", siwc.statusHandler)
	mux.HandleFunc("GET /api/auth/profiles", siwc.profilesHandler)
	mux.HandleFunc("POST /api/auth/profiles/active", siwc.selectProfileHandler)
	mux.HandleFunc("POST /api/auth/disconnect", siwc.disconnectHandler)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, version)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		_, _ = fmt.Fprint(w, homePage)
	})
	return mux
}
