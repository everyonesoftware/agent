package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

func (s *usageService) registerAuth(mux *http.ServeMux) {
	mux.Handle("POST /auth/start", s.accounts.middleware(http.HandlerFunc(s.startChatGPT)))
	mux.Handle("GET /api/auth/status", s.accounts.middleware(http.HandlerFunc(s.chatGPTStatus)))
	mux.Handle("GET /api/auth/profiles", s.accounts.middleware(http.HandlerFunc(s.chatGPTProfiles)))
	mux.Handle("POST /api/auth/profiles/active", s.accounts.scopedPasswordSIWCHandler(s.base, false, func(service *siwcService, w http.ResponseWriter, r *http.Request) { service.selectProfileHandler(w, r) }))
	mux.Handle("POST /api/auth/disconnect", s.accounts.scopedPasswordSIWCHandler(s.base, false, func(service *siwcService, w http.ResponseWriter, r *http.Request) {
		var body struct {
			ProfileID string `json:"profile_id"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			writeChatError(w, 400, "Select a ChatGPT account.")
			return
		}
		service.tokenMu.Lock()
		defer service.tokenMu.Unlock()
		active, err := service.store.ActiveProfileID()
		if err != nil || active != body.ProfileID {
			writeChatError(w, 409, "The active ChatGPT account changed. Review Settings.")
			return
		}
		user, _ := currentAuthUser(r.Context())
		entry, err := s.session(user, active)
		if err != nil {
			writeChatError(w, 503, "ChatGPT storage is unavailable.")
			return
		}
		entry.mu.Lock()
		defer entry.mu.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		if err = s.clear(ctx, entry); err != nil {
			writeChatError(w, 503, err.Error())
			return
		}
		profile, err := service.store.LoadProfile(active)
		if err == nil {
			err = service.clearProfileSessionTokens(profile, false, false, true)
		}
		if err != nil {
			writeChatError(w, 503, "Legacy connection metadata could not be cleared.")
			return
		}
		writeChatJSON(w, 200, map[string]any{"connected": false, "remote_revocation_confirmed": false})
	}))
}
func (s *usageService) adoptCodexIdentity(service *siwcService, entry *usageSession) error {
	if entry.view.Identity == nil || entry.identityKey == "" || entry.view.Login != nil || entry.view.State == "expired" || entry.view.State == "disconnected" {
		return nil
	}
	profile, err := service.store.LoadProfile(entry.view.ProfileID)
	if err != nil {
		return err
	}
	email := entry.view.Identity.Email
	wasPending := profile.Credentials.RegistrationPending
	if profile.Email == email && profile.Credentials.ClientID == "codex-managed" && !profile.Credentials.RegistrationPending {
		return nil
	}
	profile.Email = email
	profile.Credentials = oauthCredentials{ClientID: "codex-managed", Issuer: "https://auth.openai.com", HostID: profile.Credentials.HostID, Subject: "codex-local:" + profile.ID, OAuthPreferred: true}
	if err := service.store.SaveProfile(profile); err != nil {
		return err
	}
	if wasPending {
		return service.store.SetActiveProfileIfEmpty(profile.ID)
	}
	return nil
}

func (s *usageService) authService(r *http.Request) (*siwcService, authUser) {
	user, _ := currentAuthUser(r.Context())
	return s.accounts.siwcForUser(s.base, user), user
}
func (s *usageService) startChatGPT(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if requestIsCrossSite(r) {
		writeChatError(w, 403, "Cross-site account requests are not allowed.")
		return
	}
	var body *struct {
		Expected  *string `json:"expected_active_profile_id"`
		ProfileID string  `json:"profile_id"`
		Password  string  `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&body) != nil || body == nil || d.Decode(new(any)) != io.EOF {
		writeChatError(w, 400, "Enter a valid ChatGPT connection request.")
		return
	}
	service, user := s.authService(r)
	service.tokenMu.Lock()
	defer service.tokenMu.Unlock()
	active, err := service.store.ActiveProfileID()
	if err != nil {
		writeChatError(w, 503, "ChatGPT profiles are unavailable.")
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode != "" && mode != "add" && mode != "retry" {
		writeChatError(w, 400, "Invalid connection mode.")
		return
	}
	if body.Expected == nil && mode != "add" {
		writeChatError(w, 400, "Confirm the current ChatGPT account before reconnecting.")
		return
	}
	if body.Expected != nil && *body.Expected != active {
		writeChatError(w, 409, "The active ChatGPT account changed. Refresh Settings.")
		return
	}
	target := body.ProfileID
	if target == "" {
		target = r.URL.Query().Get("profile_id")
	}
	if target == "" && mode != "add" {
		target = active
	}
	if target != "" && !validProfileID(target) {
		writeChatError(w, 400, "Select a valid ChatGPT account.")
		return
	}
	if target != "" && target != active {
		profile, err := service.store.LoadProfile(target)
		if mode != "retry" || err != nil || !profile.Credentials.RegistrationPending {
			writeChatError(w, 409, "The active ChatGPT account changed. Refresh Settings.")
			return
		}
	}
	if !s.starts.allow(user.ID) {
		writeChatError(w, 429, "Too many ChatGPT sign-in attempts. Try again in a minute.")
		return
	}
	if target == "" {
		target, err = newProfileID()
		if err != nil {
			writeChatError(w, 503, "ChatGPT profile could not be prepared.")
			return
		}
		host, err := service.store.HostID()
		if err != nil {
			writeChatError(w, 503, "ChatGPT profiles are unavailable.")
			return
		}
		profiles, _ := service.store.ListProfiles()
		// Subject is a local migration marker, never a claimed provider identity.
		profile := credentialProfile{ID: target, Label: nextProfileLabel(profiles), Credentials: oauthCredentials{ClientID: "codex-managed", Issuer: "https://auth.openai.com", HostID: host, RegistrationPending: true, OAuthPreferred: true}}
		if service.store.SaveProfile(profile) != nil {
			writeChatError(w, 503, "ChatGPT profile could not be saved.")
			return
		}
	}
	entry, err := s.session(user, target)
	if err != nil {
		writeChatError(w, 503, "ChatGPT storage is unavailable.")
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	// A previously linked Codex account is the connection; never require a second login.
	if entry.record.ConnectionID != "" && entry.view.Login == nil {
		s.status(ctx, entry, "")
		if entry.view.Identity != nil && !entry.lastRead.IsZero() && entry.lastRead.Equal(entry.lastAttempt) && entry.view.State != "expired" {
			if err := s.adoptCodexIdentity(service, entry); err != nil {
				writeChatError(w, 503, "ChatGPT profile metadata could not be saved.")
				return
			}
			writeChatJSON(w, 200, map[string]any{"profile_id": target, "connection_id": entry.record.ConnectionID, "connected": true})
			return
		}
		if entry.view.State == "unavailable" {
			writeChatError(w, 503, "The existing Codex account could not be checked. Its credentials were kept; try again shortly.")
			return
		}
	}
	if entry.view.Login != nil {
		if mode != "retry" {
			writeChatError(w, 409, "A ChatGPT sign-in is already awaiting approval. Cancel it first.")
			return
		}
		if s.now().Unix() >= entry.view.Login.ExpiresAt {
			s.stopWorker(entry)
			entry.view.Login = nil
		} else {
			login := entry.view.Login
			writeChatJSON(w, 200, map[string]any{"profile_id": target, "connection_id": entry.record.ConnectionID, "login_id": login.LoginID, "verification_url": login.VerificationURL, "user_code": login.UserCode, "expires_at": login.ExpiresAt})
			return
		}
	}
	if err = s.connect(ctx, entry); err != nil {
		writeChatError(w, 503, err.Error())
		return
	}
	login := entry.view.Login
	writeChatJSON(w, 200, map[string]any{"profile_id": target, "connection_id": entry.record.ConnectionID, "login_id": login.LoginID, "verification_url": login.VerificationURL, "user_code": login.UserCode, "expires_at": login.ExpiresAt})
}
func (s *usageService) chatGPTStatus(w http.ResponseWriter, r *http.Request) {
	if requestIsCrossSite(r) {
		writeChatError(w, 403, "Cross-site account requests are not allowed.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	service, user := s.authService(r)
	active, err := service.store.ActiveProfileID()
	if err != nil {
		writeChatError(w, 503, "ChatGPT profiles are unavailable.")
		return
	}
	view := usageStatus{State: "disconnected"}
	if active != "" {
		entry, err := s.session(user, active)
		if err != nil {
			writeChatError(w, 503, "ChatGPT storage is unavailable.")
			return
		}
		entry.mu.Lock()
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		s.status(ctx, entry, "")
		cancel()
		if err := s.adoptCodexIdentity(service, entry); err != nil {
			entry.mu.Unlock()
			writeChatError(w, 503, "ChatGPT profile metadata could not be saved.")
			return
		}
		view = s.view(entry)
		entry.mu.Unlock()
		active, _ = service.store.ActiveProfileID()
	}
	connected := view.Identity != nil && view.State != "expired" && view.State != "disconnected" && view.State != "pending"
	email := ""
	if view.Identity != nil {
		email = view.Identity.Email
	}
	writeChatJSON(w, 200, map[string]any{"connected": connected, "oauth_selected": active != "", "plan_usage_enabled": connected, "local_url": "", "active_profile_id": active, "email": email, "state": view.State, "available": view.Available, "connection_id": view.ConnectionID, "error": view.Error})
}
func (s *usageService) chatGPTProfiles(w http.ResponseWriter, r *http.Request) {
	if requestIsCrossSite(r) {
		writeChatError(w, 403, "Cross-site account requests are not allowed.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	service, user := s.authService(r)
	profiles, err := service.store.ListProfiles()
	if err != nil && !os.IsNotExist(err) {
		writeChatError(w, 503, "ChatGPT profiles are unavailable.")
		return
	}
	active, _ := service.store.ActiveProfileID()
	views := make([]credentialProfileView, 0, len(profiles))
	for index, p := range profiles {
		label := safeProfileLabel(p.Label)
		if label == "" {
			label = "ChatGPT account " + strconv.Itoa(index+1)
		}
		v := credentialProfileView{ID: p.ID, Label: label, Email: safeProfileLabel(p.Email), OAuthSelected: true}
		entry, err := s.session(user, p.ID)
		if err == nil {
			entry.mu.Lock()
			if entry.record.ConnectionID != "" {
				ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
				s.status(ctx, entry, "")
				cancel()
			}
			if err := s.adoptCodexIdentity(service, entry); err != nil {
				entry.mu.Unlock()
				writeChatError(w, 503, "ChatGPT profile metadata could not be saved.")
				return
			}
			v.Connected = entry.view.Identity != nil && entry.view.State != "expired" && entry.view.State != "disconnected" && entry.view.State != "pending"
			v.PlanUsageEnabled = v.Connected
			v.RegistrationPending = (p.Credentials.RegistrationPending && !v.Connected) || entry.view.Login != nil
			if entry.view.Identity != nil {
				v.Email = entry.view.Identity.Email
			}
			entry.mu.Unlock()
		}
		views = append(views, v)
	}
	active, _ = service.store.ActiveProfileID()
	writeChatJSON(w, 200, map[string]any{"active_profile_id": active, "profiles": views})
}
