package main

import (
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func newServer() http.Handler {
	return newServerWithResearchWebFactory(nil)
}

// Package-private fixture injection; production never accepts an upstream override.
func newServerWithResearchWebFactory(factory func(string) *researchWeb) http.Handler {
	siwc := newSIWCServiceFromEnv()
	apiProvider := newModelProviderFromEnv()
	mux := http.NewServeMux()
	registerPWARoutes(mux)
	dataDir := strings.TrimSpace(os.Getenv("APP_DATA_DIR"))
	if dataDir == "" {
		dataDir = strings.TrimSpace(os.Getenv("SIWC_DATA_DIR"))
	}
	if dataDir == "" {
		dataDir = "/data"
	}
	accounts, accountErr := newAccountStore(filepath.Join(dataDir, "everyoneagent.sqlite"))
	if accountErr == nil {
		provider := &userScopedProvider{api: apiProvider, baseAuth: siwc, accounts: accounts}
		mux.HandleFunc("POST /api/auth/register", accounts.registerHandler)
		mux.HandleFunc("POST /api/auth/login", accounts.loginHandler)
		mux.HandleFunc("POST /api/auth/logout", accounts.logoutHandler)
		mux.Handle("GET /api/auth/me", accounts.middleware(http.HandlerFunc(accounts.meHandler)))
		mux.Handle("POST /api/auth/password", accounts.middleware(http.HandlerFunc(accounts.passwordChangeHandler)))
		registerMemoryRoutes(mux, accounts)
		research := newAccountResearchService(accounts)
		research.settings.webFactory = factory
		research.register(mux, accounts)
		mux.Handle("POST /api/chat", accounts.middleware(chatHandlerWithResearch(provider, accounts, research)))
		mux.Handle("GET /api/runs/{id}", accounts.middleware(http.HandlerFunc(accounts.runHandler)))
		mux.Handle("GET /api/models", accounts.middleware(modelsHandler(provider)))
		mux.Handle("GET /api/conversations", accounts.middleware(http.HandlerFunc(accounts.listConversations)))
		mux.Handle("GET /api/conversations/{id}", accounts.middleware(http.HandlerFunc(accounts.conversationMessages)))
		mux.Handle("DELETE /api/conversations/{id}", accounts.middleware(http.HandlerFunc(accounts.deleteConversation)))
		mux.HandleFunc("GET /auth/start", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", "POST")
			writeChatError(w, http.StatusMethodNotAllowed, "Use POST to confirm this connection action.")
		})
		mux.Handle("POST /auth/start", accounts.scopedPasswordSIWCHandler(siwc, true, (*siwcService).startHandler))
		mux.Handle("GET /auth/callback", accounts.scopedOAuthCallbackHandler(siwc, (*siwcService).callbackHandler))
		mux.Handle("GET /api/auth/status", accounts.scopedSIWCHandler(siwc, (*siwcService).statusHandler))
		mux.Handle("GET /api/auth/profiles", accounts.scopedSIWCHandler(siwc, (*siwcService).profilesHandler))
		mux.Handle("POST /api/auth/profiles/active", accounts.scopedPasswordSIWCHandler(siwc, false, (*siwcService).selectProfileHandler))
		mux.Handle("POST /api/auth/disconnect", accounts.scopedPasswordSIWCHandler(siwc, false, (*siwcService).disconnectHandler))
		registerUsageRoutes(mux, accounts, siwc)
	} else {
		log.Printf("local account database is unavailable: %v", accountErr)
		unavailable := func(w http.ResponseWriter, _ *http.Request) {
			writeChatError(w, http.StatusServiceUnavailable, "The local account database is unavailable.")
		}
		mux.HandleFunc("POST /api/auth/register", unavailable)
		mux.HandleFunc("POST /api/auth/login", unavailable)
	}
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
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = fmt.Fprint(w, strings.NewReplacer("{{APP_VERSION}}", html.EscapeString(version), "{{APP_BUILD_ID}}", appBuildID()).Replace(homePage))
	})
	return mux
}
