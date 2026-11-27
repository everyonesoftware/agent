package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

const codexConsumeResetMethod = "account/rateLimitResetCredit/consume"

type providerResetCredits struct {
	AvailableCount *int64                `json:"availableCount"`
	Credits        []providerResetCredit `json:"credits"`
}

type providerResetCredit struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Type      string `json:"resetType"`
	ExpiresAt *int64 `json:"expiresAt"`
}

type usageResets struct {
	AvailableCount     int64                  `json:"available_count"`
	Expirations        []usageResetExpiration `json:"expirations"`
	DetailsComplete    bool                   `json:"details_complete"`
	Next               *usageResetTarget      `json:"next,omitempty"`
	History            []usageResetEvent      `json:"history"`
	HistoryUnavailable bool                   `json:"history_unavailable,omitempty"`
}

type usageResetExpiration struct {
	Count     int    `json:"count"`
	ExpiresAt *int64 `json:"expires_at"`
}

type usageResetTarget struct {
	CreditID         string `json:"credit_id"`
	ExpiresAt        *int64 `json:"expires_at"`
	PendingAttemptID string `json:"pending_attempt_id,omitempty"`
}

type usageResetEvent struct {
	Kind string `json:"kind"`
	At   int64  `json:"at"`
}

type usageResetRequest struct {
	ProfileID          string          `json:"profile_id"`
	ConnectionID       string          `json:"connection_id"`
	CreditID           string          `json:"credit_id"`
	AttemptID          string          `json:"attempt_id"`
	ConfirmedCount     int64           `json:"confirmed_count"`
	ConfirmedExpiresAt json.RawMessage `json:"confirmed_expires_at"`
}

type usageResetResponse struct {
	Outcome string      `json:"outcome"`
	Status  usageStatus `json:"status"`
}

func validResetCreditID(id string) bool {
	return id != "" && len(id) <= 1024 && strings.TrimSpace(id) == id && strings.IndexFunc(id, unicode.IsControl) == -1
}

func validResetAttemptID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed != uuid.Nil && parsed.String() == id
}

func normalizeResetCredits(raw *providerResetCredits, now int64) *usageResets {
	if raw == nil || raw.AvailableCount == nil || *raw.AvailableCount < 0 {
		return nil
	}
	view := &usageResets{AvailableCount: *raw.AvailableCount, Expirations: []usageResetExpiration{}, History: []usageResetEvent{}}
	ids := make(map[string]int, len(raw.Credits))
	for _, row := range raw.Credits {
		ids[row.ID]++
	}
	targets := make([]usageResetTarget, 0, len(raw.Credits))
	for _, row := range raw.Credits {
		if row.Status != "available" || row.Type != "codexRateLimits" || !validResetCreditID(row.ID) || ids[row.ID] != 1 ||
			(row.ExpiresAt != nil && (*row.ExpiresAt <= now || *row.ExpiresAt > 253402300799)) {
			continue
		}
		targets = append(targets, usageResetTarget{CreditID: row.ID, ExpiresAt: row.ExpiresAt})
	}
	if int64(len(targets)) > view.AvailableCount {
		return view
	} // Contradictory evidence.
	sort.Slice(targets, func(i, j int) bool {
		a, b := targets[i], targets[j]
		if a.ExpiresAt != nil && b.ExpiresAt == nil {
			return true
		}
		if a.ExpiresAt == nil && b.ExpiresAt != nil {
			return false
		}
		if a.ExpiresAt != nil && *a.ExpiresAt != *b.ExpiresAt {
			return *a.ExpiresAt < *b.ExpiresAt
		}
		return a.CreditID < b.CreditID
	})
	for _, target := range targets {
		n := len(view.Expirations)
		if n > 0 && sameResetExpiration(view.Expirations[n-1].ExpiresAt, target.ExpiresAt) {
			view.Expirations[n-1].Count++
		} else {
			view.Expirations = append(view.Expirations, usageResetExpiration{Count: 1, ExpiresAt: target.ExpiresAt})
		}
	}
	view.DetailsComplete = raw.Credits != nil && len(targets) == len(raw.Credits) && int64(len(targets)) == view.AvailableCount
	if view.DetailsComplete && len(targets) > 0 {
		view.Next = &targets[0]
	}
	return view
}

func sameResetExpiration(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (s *usageService) consumeUsageReset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if requestIsCrossSite(r) {
		writeChatError(w, 403, "Cross-site account requests are not allowed.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var req *usageResetRequest
	var confirmedExpiresAt *int64
	if decoder.Decode(&req) != nil || req == nil || decoder.Decode(new(any)) != io.EOF || !validProfileID(req.ProfileID) || !validResetCreditID(req.CreditID) || !validResetAttemptID(req.AttemptID) ||
		req.ConfirmedCount <= 0 || json.Unmarshal(req.ConfirmedExpiresAt, &confirmedExpiresAt) != nil {
		writeChatError(w, 400, "Enter a valid confirmed reset request.")
		return
	}
	user, _ := currentAuthUser(r.Context())
	service := s.accounts.siwcForUser(s.base, user)
	// Profile switch and unified disconnect take this lock first. Keep the
	// selection fixed until the provider operation has finished.
	service.tokenMu.Lock()
	defer service.tokenMu.Unlock()
	active, err := service.store.ActiveProfileID()
	if err != nil || active != req.ProfileID {
		writeChatError(w, 409, "The active ChatGPT account changed. Refresh Settings.")
		return
	}
	entry, err := s.session(user, req.ProfileID)
	if err != nil {
		writeChatError(w, 503, "Usage credential storage is unavailable.")
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	profile, err := service.store.LoadProfile(req.ProfileID)
	if err != nil || profile.Credentials.RegistrationPending || strings.TrimSpace(profile.Email) == "" || req.ConnectionID == "" || req.ConnectionID != entry.record.ConnectionID || entry.view.Login != nil {
		writeChatError(w, 409, "The usage connection changed. Refresh Settings.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	s.checkUsageStatus(ctx, entry, profile.Email, true)
	if entry.view.State == "expired" || entry.view.IdentityMatch == "different" || (entry.identityKey != "" && !strings.EqualFold(entry.identityKey, strings.TrimSpace(profile.Email))) {
		writeChatError(w, 409, "The ChatGPT account changed. Refresh Settings.")
		return
	}
	if entry.view.State != "connected" || entry.view.Error != "" || entry.identityKey == "" {
		writeChatError(w, 503, "Current ChatGPT allowance could not be verified. Refresh Settings.")
		return
	}
	scope := resetScope(entry)
	attempt, err := s.readResetAttempt(ctx, scope, req.AttemptID)
	if err != nil {
		writeChatError(w, 503, "Reset history is unavailable. No new reset was sent.")
		return
	}
	if attempt != nil {
		if !attempt.matches(scope, req.CreditID) {
			writeChatError(w, 409, "The reset confirmation does not match this account and credit.")
			return
		}
		if attempt.Outcome != "pending" {
			outcome := attempt.Outcome
			if outcome == "reset" {
				outcome = "alreadyRedeemed"
			}
			writeChatJSON(w, 200, usageResetResponse{Outcome: outcome, Status: s.view(entry)})
			return
		}
	}
	view := s.view(entry)
	if view.Resets == nil || !view.Resets.DetailsComplete || view.Resets.Next == nil || view.Resets.Next.CreditID != req.CreditID ||
		view.Resets.AvailableCount != req.ConfirmedCount || !sameResetExpiration(view.Resets.Next.ExpiresAt, confirmedExpiresAt) {
		writeChatError(w, 409, "The available reset credit changed. Refresh Settings and confirm again.")
		return
	}
	if view.Resets.HistoryUnavailable {
		writeChatError(w, 503, "Reset history is unavailable. No new reset was sent.")
		return
	}
	if attempt == nil {
		claimed, err := s.claimResetAttempt(ctx, scope, *req)
		if err != nil {
			writeChatError(w, 503, "Reset history is unavailable. No new reset was sent.")
			return
		}
		if !claimed {
			writeChatError(w, 409, "This credit or confirmation already belongs to a reset attempt.")
			return
		}
	}
	var result struct {
		Outcome string `json:"outcome"`
	}
	err = s.call(ctx, entry, codexConsumeResetMethod, map[string]string{"creditId": req.CreditID, "idempotencyKey": req.AttemptID}, &result)
	switch result.Outcome {
	case "reset", "alreadyRedeemed", "nothingToReset", "noCredit":
	default:
		result.Outcome = ""
	}
	// Only a new provider observation may replace the pre-mutation balance.
	entry.resetCredits = nil
	entry.view.Snapshot.Stale = true
	entry.view.Error = "Refresh to verify current ChatGPT allowance."
	entry.lastAttempt = time.Time{}
	if err != nil || result.Outcome == "" {
		writeChatError(w, 503, "The reset outcome could not be confirmed. Refresh Settings before trying again.")
		return
	}
	// Persist a known outcome even if the HTTP client disconnected after Codex replied.
	persistCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	err = s.finishResetAttempt(persistCtx, scope, req.AttemptID, result.Outcome)
	done()
	if err != nil {
		writeChatError(w, 503, "The reset outcome could not be saved. Refresh Settings before trying again.")
		return
	}
	s.refreshQuota(ctx, entry)
	writeChatJSON(w, 200, usageResetResponse{Outcome: result.Outcome, Status: s.view(entry)})
}
