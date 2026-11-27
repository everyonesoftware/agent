package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type usageRequest struct {
	ProfileID            string  `json:"profile_id"`
	ExpectedConnectionID *string `json:"expected_connection_id,omitempty"`
	ConnectionID         string  `json:"connection_id,omitempty"`
	LoginID              string  `json:"login_id,omitempty"`
	Password             string  `json:"password,omitempty"`
}

func registerUsageRoutes(mux *http.ServeMux, accounts *accountStore, base *siwcService) *usageService {
	s := newUsageService(accounts, base)
	s.register(mux)
	return s
}

func (s *usageService) register(mux *http.ServeMux) {
	mux.Handle("POST /api/usage/resets/consume", s.accounts.middleware(http.HandlerFunc(s.consumeUsageReset)))
	for _, route := range []string{"GET /api/usage/history", "GET /api/usage/status", "POST /api/usage/connect", "POST /api/usage/cancel", "POST /api/usage/refresh", "POST /api/usage/disconnect"} {
		mux.Handle(route, s.accounts.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if requestIsCrossSite(r) {
				writeChatError(w, http.StatusForbidden, "Cross-site account requests are not allowed.")
				return
			}
			payload := usageRequest{ProfileID: r.URL.Query().Get("profile_id"), ConnectionID: r.URL.Query().Get("connection_id")}
			if r.Method == http.MethodPost {
				r.Body = http.MaxBytesReader(w, r.Body, 4096)
				var body *usageRequest
				decoder := json.NewDecoder(r.Body)
				decoder.DisallowUnknownFields()
				if decoder.Decode(&body) != nil || body == nil || decoder.Decode(new(any)) != io.EOF {
					writeChatError(w, http.StatusBadRequest, "Enter a valid usage connection request.")
					return
				}
				payload = *body
			}
			if !validProfileID(payload.ProfileID) {
				writeChatError(w, http.StatusBadRequest, "Select a valid ChatGPT account.")
				return
			}
			if s.accounts.codex == s && r.URL.Path == "/api/usage/connect" {
				writeChatError(w, http.StatusGone, "Use Continue with ChatGPT in Settings to start a device sign-in.")
				return
			}
			user, _ := currentAuthUser(r.Context())
			service := s.accounts.siwcForUser(s.base, user)
			active, err := service.store.ActiveProfileID()
			if err != nil {
				writeChatError(w, 503, "ChatGPT account selection is unavailable.")
				return
			}
			profile, err := service.store.LoadProfile(payload.ProfileID)
			if err != nil {
				writeChatError(w, 409, "The ChatGPT account is unavailable.")
				return
			}
			profileEmail := profile.Email
			// Codex is the sole credential owner for a unified connection.
			if s.accounts.codex == s {
				profileEmail = ""
			}
			action := strings.TrimPrefix(r.URL.Path, "/api/usage/")
			allowPending := profile.Credentials.RegistrationPending && (action == "status" || action == "cancel")
			// Reconcile the exact captured ceremony even after another observer
			// adopts its account. This never authorizes selecting that profile.
			allowCaptured := payload.ConnectionID != "" && (action == "status" || action == "cancel")
			if active != payload.ProfileID && !allowPending && !allowCaptured {
				writeChatError(w, 409, "The active ChatGPT account changed. Refresh Settings.")
				return
			}
			if action == "connect" && payload.ExpectedConnectionID == nil {
				writeChatError(w, 400, "Confirm the current usage connection before connecting.")
				return
			}
			if action != "disconnect" && payload.Password != "" {
				writeChatError(w, 400, "Do not send a password for this usage action.")
				return
			}
			if action == "disconnect" && !s.confirmPassword(w, r, user, payload.Password) {
				return
			}
			payload.Password = ""
			entry, err := s.session(user, payload.ProfileID)
			if err != nil {
				writeChatError(w, 503, "Usage credential storage is unavailable.")
				return
			}
			entry.mu.Lock()
			defer entry.mu.Unlock()
			// Recheck selection after any password/worker wait, before an external action.
			active, err = service.store.ActiveProfileID()
			if err != nil || (active != payload.ProfileID && !allowPending && !allowCaptured) {
				writeChatError(w, 409, "The active ChatGPT account changed. Refresh Settings.")
				return
			}
			if action == "history" {
				if _, err := historyRangeStart(r.URL.Query().Get("range"), s.now(), 0); err != nil {
					writeChatError(w, 400, err.Error())
					return
				}
				ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
				defer cancel()
				s.status(ctx, entry, profileEmail)
				history, err := s.history(entry, r.URL.Query().Get("range"), r.URL.Query().Get("window_id"))
				if err != nil {
					if errors.Is(err, errUsageHistoryUnavailable) {
						writeChatError(w, 503, err.Error())
						return
					}
					writeChatError(w, 400, err.Error())
					return
				}
				writeChatJSON(w, 200, history)
				return
			}
			if action != "status" && action != "connect" && (payload.ConnectionID == "" || payload.ConnectionID != entry.record.ConnectionID) {
				writeChatError(w, 409, "The usage connection changed. Refresh Settings.")
				return
			}
			if action == "status" && payload.ConnectionID != "" && payload.ConnectionID != entry.record.ConnectionID {
				writeChatError(w, 409, "The usage connection changed. Refresh Settings.")
				return
			}
			if action == "connect" && *payload.ExpectedConnectionID != entry.record.ConnectionID {
				writeChatError(w, 409, "The usage connection changed. Refresh Settings.")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			switch action {
			case "connect":
				if entry.view.Login != nil {
					writeChatError(w, http.StatusConflict, "A usage sign-in is already awaiting approval. Cancel it first.")
					return
				}
				if !s.starts.allow(user.ID) {
					w.Header().Set("Retry-After", "60")
					writeChatError(w, 429, "Too many usage sign-in attempts. Try again in a minute.")
					return
				}
				if err = s.connect(ctx, entry); err != nil {
					entry.view.State = "unavailable"
					entry.view.Error = err.Error()
					entry.view.Snapshot = nil
					entry.view.Login = nil
					s.stopWorker(entry)
					writeChatError(w, 503, err.Error())
					return
				}
			case "cancel":
				if entry.view.Login == nil {
					// A completed ceremony can only be observed here, not logged out.
					s.status(ctx, entry, profileEmail)
					writeChatJSON(w, http.StatusOK, s.view(entry))
					return
				}
				if entry.view.Login.LoginID != payload.LoginID {
					writeChatError(w, 409, "The usage sign-in request changed. Refresh Settings.")
					return
				}
				var result struct{}
				_ = s.call(ctx, entry, "account/login/cancel", map[string]string{"loginId": payload.LoginID}, &result)
				if err = s.clear(ctx, entry); err != nil {
					writeChatError(w, 503, err.Error())
					return
				}
			case "disconnect":
				if err = s.clear(ctx, entry); err != nil {
					writeChatError(w, 503, err.Error())
					return
				}
			case "status":
				s.status(ctx, entry, profileEmail)
			case "refresh":
				// Cached snapshots do not authorize skipping an account check on refresh.
				entry.lastRead = time.Time{}
				entry.lastAttempt = time.Time{}
				before := entry.view.Snapshot
				s.status(ctx, entry, profileEmail)
				if entry.view.State == "connected" && entry.view.Snapshot == before {
					s.refreshQuota(ctx, entry)
				}
			}
			if s.accounts.codex == s {
				if err := s.adoptCodexIdentity(service, entry); err != nil {
					writeChatError(w, 503, "ChatGPT profile metadata could not be saved.")
					return
				}
			}
			writeChatJSON(w, http.StatusOK, s.view(entry))
		})))
	}
}
