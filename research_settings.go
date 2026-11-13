package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Credentials remain in the protected local account database, not encrypted at
// rest. Only this explicitly allowlisted metadata is serialized to the browser.
type researchSettings struct {
	Budget        *researchBudget `json:"budget"`
	Usage         *researchUsage  `json:"usage"`
	Configured    bool            `json:"configured"`
	Provider      string          `json:"provider"`
	KeySuffix     string          `json:"key_suffix"`
	Revision      string          `json:"revision"`
	VerifiedAt    string          `json:"verified_at"`
	LastCheckedAt string          `json:"last_checked_at"`
	Status        string          `json:"status"`
}
type researchCredential struct {
	researchSettings
	apiKey string
}

func emptyResearchCredential() researchCredential {
	return researchCredential{researchSettings: researchSettings{Provider: "Brave Search", Status: "not_configured"}}
}
func migrateResearchSettings(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS research_settings (
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 api_key TEXT NOT NULL, revision TEXT NOT NULL, verified_at TEXT NOT NULL,
 last_checked_at TEXT NOT NULL, status TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS research_usage (
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 revision TEXT NOT NULL, observed_ns INTEGER NOT NULL, snapshot TEXT NOT NULL)`)
	if err != nil {
		return err
	}
	return migrateResearchBudget(db)
}

type researchRowQuerier interface{ QueryRow(string, ...any) *sql.Row }

func loadResearchCredential(q researchRowQuerier, userID string) (researchCredential, error) {
	v := emptyResearchCredential()
	var snapshot string
	err := q.QueryRow(`SELECT s.api_key,s.revision,s.verified_at,s.last_checked_at,s.status,COALESCE(u.snapshot,'') FROM research_settings s LEFT JOIN research_usage u ON u.user_id=s.user_id AND u.revision=s.revision WHERE s.user_id=?`, userID).Scan(&v.apiKey, &v.Revision, &v.VerifiedAt, &v.LastCheckedAt, &v.Status, &snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return emptyResearchCredential(), nil
	}
	if err != nil {
		return researchCredential{}, err
	}
	v.Configured = v.apiKey != ""
	if snapshot != "" && v.Configured {
		if err := json.Unmarshal([]byte(snapshot), &v.Usage); err != nil {
			return researchCredential{}, err
		}
	}
	if v.Configured {
		if len(v.apiKey) < 4 {
			return researchCredential{}, errors.New("invalid stored research credential")
		}
		v.KeySuffix = v.apiKey[len(v.apiKey)-4:]
	}
	return v, nil
}
func (a *accountStore) researchCredentialForUser(userID string) (researchCredential, error) {
	return loadResearchCredential(a.db, userID)
}

type researchVerificationBudget struct {
	since  time.Time
	count  int
	active bool
}
type researchSettingsService struct {
	accounts   *accountStore
	research   *researchService
	webFactory func(string) *researchWeb
	mu         sync.Mutex
	budgets    map[string]researchVerificationBudget
	inflight   int
}

func newAccountResearchService(a *accountStore) *researchService {
	s := newResearchService(newResearchWeb())
	s.settings = &researchSettingsService{accounts: a, research: s, budgets: map[string]researchVerificationBudget{}}
	return s
}
func (s *researchSettingsService) web(key string) *researchWeb {
	if s.webFactory != nil {
		return s.webFactory(key)
	}
	return &researchWeb{apiKey: key}
}
func (s *researchSettingsService) reserve(userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.research.now()
	for id, b := range s.budgets {
		if !b.active && now.Sub(b.since) >= time.Minute {
			delete(s.budgets, id)
		}
	}
	b := s.budgets[userID]
	if b.active || b.count >= 5 || s.inflight >= 4 || (b.count == 0 && len(s.budgets) >= 1000) {
		return false
	}
	if b.count == 0 {
		b.since = now
	}
	b.count++
	b.active = true
	s.budgets[userID] = b
	s.inflight++
	return true
}
func (s *researchSettingsService) release(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.budgets[userID]
	b.active = false
	s.budgets[userID] = b
	s.inflight--
}
func (s *researchSettingsService) register(mux *http.ServeMux) {
	for _, pattern := range []string{"GET /api/research/settings", "POST /api/research/settings/verify", "POST /api/research/settings/check", "POST /api/research/settings/remove", "POST /api/research/settings/budget"} {
		// Include unauthenticated failures in the no-store policy.
		protected := s.accounts.middleware(http.HandlerFunc(s.handler))
		mux.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			protected.ServeHTTP(w, r)
		}))
	}
}
func settingsReply(w http.ResponseWriter, status int, v researchCredential) {
	writeChatJSON(w, status, map[string]any{"settings": v.researchSettings})
}
func settingsFailure(w http.ResponseWriter, status int, code, message string, v *researchCredential) {
	payload := map[string]any{"error": message, "code": code}
	if v != nil {
		payload["settings"] = v.researchSettings
	}
	writeChatJSON(w, status, payload)
}
func validResearchKey(key string) bool {
	if len(key) < 16 || len(key) > 256 {
		return false
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func researchVerificationFailure(err error) (int, string, string, string) {
	if errors.Is(err, errResearchBudgetExhausted) {
		return 429, "budget_exhausted", errResearchBudgetExhausted.Error(), "connected"
	}
	if errors.Is(err, errResearchBudgetUnavailable) {
		return 503, "unavailable", errResearchBudgetUnavailable.Error(), "unavailable"
	}
	var provider *researchSearchError
	if errors.As(err, &provider) {
		switch provider.status {
		case 401, 403:
			return 422, "invalid_key", "Brave Search rejected this key.", "invalid"
		case 429:
			return 429, "rate_limited", "Brave Search rate limit reached. Try again later.", "unavailable"
		}
	}
	return 503, "unavailable", "Brave Search could not be reached or returned an unsupported response. Try again later.", "unavailable"
}

var errResearchSettingsConflict = errors.New("research settings changed")
var errResearchSettingsSignedOut = errors.New("research settings session ended")

// Called with research.mu held, so commit and task invalidation are serialized
// with consume/check/final-save. SQLite's writer lock also guards the session
// and revision comparisons. Never call this while doing network I/O.
func (s *researchSettingsService) persist(userID, token, expected string, next researchCredential) error {
	tx, err := s.accounts.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// BEGIN is deferred: acquire SQLite's writer lock before capturing the
	// authorization time, since waiting for another writer can outlive a session.
	if _, err := tx.Exec(`UPDATE sessions SET expires_at=expires_at WHERE 0`); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE sessions SET expires_at=expires_at WHERE token_hash=? AND user_id=? AND expires_at>?`, hashSession(token), userID, time.Now().Unix())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errResearchSettingsSignedOut
	}
	current, err := loadResearchCredential(tx, userID)
	if err != nil {
		return err
	}
	if current.Revision != expected {
		return errResearchSettingsConflict
	}
	_, err = tx.Exec(`INSERT INTO research_settings(user_id,api_key,revision,verified_at,last_checked_at,status) VALUES(?,?,?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET api_key=excluded.api_key,revision=excluded.revision,verified_at=excluded.verified_at,last_checked_at=excluded.last_checked_at,status=excluded.status`, userID, next.apiKey, next.Revision, next.VerifiedAt, next.LastCheckedAt, next.Status)
	if err != nil {
		return err
	}
	// Same-revision checks never write a stale captured usage value. Replacement
	// and removal atomically discard the old revision's snapshot.
	if next.Revision != current.Revision {
		if next.apiKey != "" && next.apiKey == current.apiKey && current.Usage != nil {
			currentTime, _ := time.Parse(time.RFC3339Nano, current.Usage.ObservedAt)
			nextTime := time.Time{}
			if next.Usage != nil {
				nextTime, _ = time.Parse(time.RFC3339Nano, next.Usage.ObservedAt)
			}
			if currentTime.After(nextTime) {
				next.Usage = current.Usage
			}
		}
		if _, err = tx.Exec(`DELETE FROM research_usage WHERE user_id=?`, userID); err != nil {
			return err
		}
		if next.Usage != nil && next.apiKey != "" {
			observed, e := time.Parse(time.RFC3339Nano, next.Usage.ObservedAt)
			if e != nil {
				return e
			}
			data, e := json.Marshal(next.Usage)
			if e != nil {
				return e
			}
			if _, err = tx.Exec(`INSERT INTO research_usage(user_id,revision,observed_ns,snapshot) VALUES(?,?,?,?)`, userID, next.Revision, observed.UnixNano(), string(data)); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func (s *researchSettingsService) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, _ := currentAuthUser(r.Context())
	if r.URL.RawQuery != "" {
		settingsFailure(w, 400, "bad_input", "Settings requests do not accept query parameters.", nil)
		return
	}
	if r.Method != "GET" && requestIsCrossSite(r) {
		settingsFailure(w, 403, "forbidden", "Cross-site settings requests are not allowed.", nil)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/api/research/settings/budget" {
		s.budgetHandler(w, r, user.ID)
		return
	}
	var expected *string
	var key string
	if r.Method != "GET" {
		if r.URL.Path == "/api/research/settings/verify" {
			var req struct {
				APIKey           string  `json:"api_key"`
				ExpectedRevision *string `json:"expected_revision"`
			}
			if !decodeResearchJSON(w, r, &req, 4096) {
				return
			}
			expected = req.ExpectedRevision
			key = req.APIKey
			if !validResearchKey(key) {
				settingsFailure(w, 400, "bad_input", "Enter a valid Brave Search API key (16–256 characters, no whitespace).", nil)
				return
			}
		} else {
			var req struct {
				ExpectedRevision *string `json:"expected_revision"`
			}
			if !decodeResearchJSON(w, r, &req, 4096) {
				return
			}
			expected = req.ExpectedRevision
		}
		if expected == nil {
			settingsFailure(w, 400, "bad_input", "Send expected_revision from the current settings.", nil)
			return
		}
	}
	current, err := s.current(user.ID)
	if err != nil {
		settingsFailure(w, 503, "unavailable", "Research settings are unavailable.", nil)
		return
	}
	if r.Method == "GET" {
		settingsReply(w, 200, current)
		return
	}
	if current.Revision != *expected {
		settingsFailure(w, 409, "conflict", "Settings changed. Reload before trying again.", &current)
		return
	}
	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		settingsFailure(w, 401, "signed_out", "Sign in to continue.", nil)
		return
	}
	next := current
	mutation := r.URL.Path != "/api/research/settings/check"
	responseStatus, code, message := 200, "", ""
	if strings.HasSuffix(r.URL.Path, "/remove") {
		next = emptyResearchCredential()
		next.Revision = uuid.NewString()
	} else {
		if strings.HasSuffix(r.URL.Path, "/check") {
			key = current.apiKey
			if key == "" {
				settingsFailure(w, 400, "not_configured", "Save a verified key before checking the connection.", &current)
				return
			}
		}
		if !s.reserve(user.ID) {
			settingsFailure(w, 429, "rate_limited", "Too many connection checks. Try again later.", &current)
			return
		}
		defer s.release(user.ID)
		ctx, cancel := context.WithTimeout(r.Context(), researchTimeout)
		defer cancel()
		var adapter *researchWeb
		if mutation {
			adapter = s.web(key)
			copy := *adapter
			adapter = &copy
			s.trackRequests(user.ID, adapter)
			next.Usage = nil
			adapter.observeUsage = func(usage *researchUsage) {
				next.Usage = usage
				// Verification of the already stored key is also a real
				// observation, even when Brave replies 429. A failed candidate
				// replacement must never attach its quota to the current key.
				if key == current.apiKey {
					_ = s.accounts.saveResearchUsage(user.ID, current.Revision, usage)
				}
			}
		} else {
			adapter = s.webWithUsage(user.ID, current)
		}
		_, verifyErr := adapter.search(ctx, "Agent connection test")
		if verifyErr != nil {
			if errors.Is(verifyErr, errResearchBudgetExhausted) || errors.Is(verifyErr, errResearchBudgetUnavailable) {
				fresh, e := s.current(user.ID)
				if e != nil {
					settingsFailure(w, 503, "unavailable", "Research settings are unavailable.", nil)
					return
				}
				status, code, message, _ := researchVerificationFailure(verifyErr)
				settingsFailure(w, status, code, message, &fresh)
				return
			}
			var checkStatus string
			responseStatus, code, message, checkStatus = researchVerificationFailure(verifyErr)
			if mutation {
				// Even failed verification must not report stale state as current.
				fresh, e := s.current(user.ID)
				if e != nil {
					settingsFailure(w, 503, "unavailable", "Research settings are unavailable.", nil)
					return
				}
				if fresh.Revision != *expected {
					settingsFailure(w, 409, "conflict", "Settings changed. Reload before trying again.", &fresh)
					return
				}
				settingsFailure(w, responseStatus, code, message, &fresh)
				return
			}
			next.Status = checkStatus
		} else {
			next.Status = "connected"
			next.VerifiedAt = s.research.now().UTC().Format(time.RFC3339)
			if mutation {
				next.apiKey = key
				next.Configured = true
				next.KeySuffix = key[len(key)-4:]
				next.Revision = uuid.NewString()
			}
		}
		next.LastCheckedAt = s.research.now().UTC().Format(time.RFC3339)
	}
	s.research.mu.Lock()
	err = s.persist(user.ID, cookie.Value, *expected, next)
	if err == nil && mutation {
		s.research.invalidateUserLocked(user.ID)
	}
	s.research.mu.Unlock()
	// Reply with persisted metadata, not a pre-network snapshot that may have
	// been superseded by an ordinary search while this check was in flight.
	if err == nil {
		next, err = s.current(user.ID)
	}
	switch {
	case errors.Is(err, errResearchSettingsConflict):
		fresh, e := s.current(user.ID)
		if e != nil {
			settingsFailure(w, 503, "unavailable", "Research settings are unavailable.", nil)
			return
		}
		settingsFailure(w, 409, "conflict", "Settings changed. Reload before trying again.", &fresh)
	case errors.Is(err, errResearchSettingsSignedOut):
		settingsFailure(w, 401, "signed_out", "Sign in again before changing settings.", nil)
	case err != nil:
		settingsFailure(w, 503, "unavailable", "Research settings could not be saved.", nil)
	case responseStatus != 200:
		settingsFailure(w, responseStatus, code, message, &next)
	default:
		settingsReply(w, 200, next)
	}
}

func (s *researchService) invalidateUserLocked(userID string) {
	for _, task := range s.tasks {
		if task.owner == userID && !terminalResearch(task.Status) {
			task.Status = "revoked"
			if task.cancel != nil {
				task.cancel()
			}
		}
	}
}
func (s *researchService) webForUserLocked(userID string) (researchWebClient, string, error) {
	if s.settings == nil {
		return s.web, "", nil
	}
	v, err := s.settings.accounts.researchCredentialForUser(userID)
	if err != nil {
		return nil, "", err
	}
	return s.settings.webWithUsage(userID, v), v.Revision, nil
}
func (s *researchService) taskCredentialValidLocked(t *researchTask) bool {
	if s.settings == nil {
		return true
	}
	v, err := s.settings.accounts.researchCredentialForUser(t.owner)
	return err == nil && v.Revision == t.configRevision && (!t.SearchAvailable || v.Configured)
}
