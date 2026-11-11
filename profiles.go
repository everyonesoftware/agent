package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type credentialProfileView struct {
	ID                  string `json:"id"`
	Label               string `json:"label"`
	Email               string `json:"email,omitempty"`
	Connected           bool   `json:"connected"`
	OAuthSelected       bool   `json:"oauth_selected"`
	PlanUsageEnabled    bool   `json:"plan_usage_enabled"`
	RegistrationPending bool   `json:"registration_pending"`
}

func (s *siwcService) profilesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if requestIsCrossSite(r) {
		writeChatError(w, http.StatusForbidden, "Manage ChatGPT accounts from a browser on the Agent host.")
		return
	}
	profiles, err := s.store.ListProfiles()
	if err != nil {
		writeChatError(w, http.StatusInternalServerError, "ChatGPT accounts could not be loaded on this host.")
		return
	}
	activeID, err := s.store.ActiveProfileID()
	if err != nil {
		writeChatError(w, http.StatusInternalServerError, "The active ChatGPT account could not be loaded on this host.")
		return
	}
	views := make([]credentialProfileView, 0, len(profiles))
	for index, profile := range profiles {
		label := safeProfileLabel(profile.Label)
		if label == "" {
			label = "ChatGPT account " + strconv.Itoa(index+1)
		}
		credentials := profile.Credentials
		views = append(views, credentialProfileView{
			ID: profile.ID, Label: label, Email: safeProfileLabel(profile.Email), Connected: credentialsConnected(credentials),
			OAuthSelected: credentialsUseOAuth(credentials), PlanUsageEnabled: credentialsPlanUsageEnabled(credentials), RegistrationPending: credentials.RegistrationPending,
		})
	}
	w.Header().Set("Cache-Control", "no-store")
	writeChatJSON(w, http.StatusOK, map[string]any{"active_profile_id": activeID, "profiles": views})
}

func (s *siwcService) selectProfileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if requestIsCrossSite(r) {
		writeChatError(w, http.StatusForbidden, "Manage ChatGPT accounts from a browser on the Agent host.")
		return
	}
	var body struct {
		ProfileID string `json:"profile_id"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || !validProfileID(body.ProfileID) {
		writeChatError(w, http.StatusBadRequest, "A valid ChatGPT account must be selected.")
		return
	}
	// Match disconnect/refresh lock ordering: tokenMu before store/model locks.
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	profile, err := s.store.LoadProfile(body.ProfileID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeChatError(w, http.StatusNotFound, "That ChatGPT account was not found on this host.")
			return
		}
		writeChatError(w, http.StatusInternalServerError, "The active ChatGPT account could not be loaded.")
		return
	}
	if profile.Credentials.RegistrationPending || profile.Credentials.Subject == "" {
		writeChatError(w, http.StatusConflict, "Complete sign-in before selecting this ChatGPT account.")
		return
	}
	if err := s.store.SetActiveProfile(body.ProfileID); err != nil {
		writeChatError(w, http.StatusInternalServerError, "The active ChatGPT account could not be changed.")
		return
	}
	s.modelsMu.Lock()
	s.models, s.modelsAt = nil, time.Time{}
	s.modelsMu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	writeChatJSON(w, http.StatusOK, map[string]string{"active_profile_id": body.ProfileID})
}

func safeProfileLabel(label string) string {
	label = strings.TrimSpace(label)
	label = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, label)
	characters := []rune(label)
	if len(characters) > 128 {
		label = string(characters[:128])
	}
	return label
}
