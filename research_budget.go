package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata" // IANA calendar boundaries also work on hosts without zoneinfo.

	"github.com/google/uuid"
)

// Account-scoped history is deliberately independent of credential revisions.
// Pending requests survive crashes as unknown reservations, never guessed successes.
type researchBudget struct {
	Enabled           bool   `json:"enabled"`
	Maximum           int    `json:"maximum"`
	Period            string `json:"period"`
	Timezone          string `json:"timezone"`
	Revision          string `json:"revision"`
	Used              int    `json:"used"`
	Reserved          int    `json:"reserved"`
	TrackingStartedAt string `json:"tracking_started_at"`
	StartAt           string `json:"start_at"`
	ResetAt           string `json:"reset_at"`
}

var errResearchBudgetExhausted = errors.New("Agent's Brave Search request cap has been reached. New searches are stopped until the period resets or you change the cap in Research settings.")
var errResearchBudgetUnavailable = errors.New("Brave Search request tracking is unavailable. No new search can start until tracking is available again.")

func migrateResearchBudget(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS research_budget (
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 enabled INTEGER NOT NULL, maximum INTEGER NOT NULL, period TEXT NOT NULL,
 timezone TEXT NOT NULL, revision TEXT NOT NULL, tracking_started_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS research_budget_events (
 id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 started_ns INTEGER NOT NULL, state TEXT NOT NULL CHECK(state IN ('reserved','used')));
 CREATE INDEX IF NOT EXISTS research_budget_events_owner_time ON research_budget_events(user_id,started_ns)`)
	return err
}
func ensureResearchBudget(tx *sql.Tx, userID string, now time.Time) error {
	_, err := tx.Exec(`INSERT INTO research_budget(user_id,enabled,maximum,period,timezone,revision,tracking_started_at) VALUES(?,0,1000,'month','UTC',?,?) ON CONFLICT(user_id) DO NOTHING`, userID, uuid.NewString(), now.UTC().Format(time.RFC3339Nano))
	return err
}
func beginResearchBudget(db *sql.DB) (*sql.Tx, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	// modernc SQLite uses _pragma, not the existing store's _busy_timeout
	// DSN spelling. Set it on this pinned connection, including fresh ones
	// (the account store deliberately keeps no idle connections).
	if _, err = tx.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}
func researchBudgetBounds(now time.Time, period, zone string) (time.Time, time.Time, error) {
	if zone == "" || zone == "Local" || strings.Contains(zone, "\\") {
		return time.Time{}, time.Time{}, errors.New("invalid budget timezone")
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	local := now.In(loc)
	day := local.Day()
	if period == "month" {
		day = 1
	} else if period != "day" {
		return time.Time{}, time.Time{}, errors.New("invalid budget period")
	}
	// Construct the intended civil dates independently in UTC. A zone's
	// midnight may not exist (Santiago), or may be repeated. time.Date in
	// that zone can normalize into the previous date, and AddDate from that
	// normalized instant can omit the final hour of the actual budget day.
	date := time.Date(local.Year(), local.Month(), day, 0, 0, 0, 0, time.UTC)
	resetDate := date.AddDate(0, 0, 1)
	if period == "month" {
		resetDate = date.AddDate(0, 1, 0)
	}
	start, err := researchCivilBoundary(date, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	reset, err := researchCivilBoundary(resetDate, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if now.Before(start) || !now.Before(reset) {
		return time.Time{}, time.Time{}, errors.New("invalid research budget calendar interval")
	}
	return start, reset, nil
}

// Find the first valid instant of the intended civil date, including midnight
// gaps/folds and skipped dates. TZif transitions have whole-second precision.
func researchCivilBoundary(date time.Time, loc *time.Location) (time.Time, error) {
	civil := func(seconds int64) int64 {
		y, m, d := time.Unix(seconds, 0).In(loc).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix()
	}
	target := date.Unix()
	low, high := target-48*60*60, target+48*60*60
	if civil(low) >= target || civil(high) < target {
		return time.Time{}, errors.New("unsupported research budget calendar boundary")
	}
	for high-low > 1 {
		mid := low + (high-low)/2
		if civil(mid) >= target {
			high = mid
		} else {
			low = mid
		}
	}
	return time.Unix(high, 0).UTC(), nil
}
func loadResearchBudget(q researchRowQuerier, userID string, now time.Time) (researchBudget, error) {
	var b researchBudget
	err := q.QueryRow(`SELECT enabled,maximum,period,timezone,revision,tracking_started_at FROM research_budget WHERE user_id=?`, userID).Scan(&b.Enabled, &b.Maximum, &b.Period, &b.Timezone, &b.Revision, &b.TrackingStartedAt)
	if err != nil {
		return b, err
	}
	start, reset, err := researchBudgetBounds(now, b.Period, b.Timezone)
	if err != nil {
		return b, err
	}
	b.StartAt = start.Format(time.RFC3339Nano)
	b.ResetAt = reset.Format(time.RFC3339Nano)
	err = q.QueryRow(`SELECT COALESCE(SUM(CASE WHEN state='used' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN state='reserved' THEN 1 ELSE 0 END),0) FROM research_budget_events WHERE user_id=? AND started_ns>=? AND started_ns<?`, userID, start.UnixNano(), reset.UnixNano()).Scan(&b.Used, &b.Reserved)
	return b, err
}
func (a *accountStore) researchBudgetForUser(userID string, now time.Time) (researchBudget, error) {
	tx, err := beginResearchBudget(a.db)
	if err != nil {
		return researchBudget{}, err
	}
	defer tx.Rollback()
	if err = ensureResearchBudget(tx, userID, now); err != nil {
		return researchBudget{}, err
	}
	b, err := loadResearchBudget(tx, userID, now)
	if err != nil {
		return b, err
	}
	return b, tx.Commit()
}
func (s *researchSettingsService) current(userID string) (researchCredential, error) {
	v, err := s.accounts.researchCredentialForUser(userID)
	if err != nil {
		return v, err
	}
	b, err := s.accounts.researchBudgetForUser(userID, s.research.now())
	if err != nil {
		return v, err
	}
	v.Budget = &b
	return v, nil
}

// SQLite's writer lock serializes configuration and reservations even across
// processes. Do not hold a transaction or a connection during provider I/O.
func (a *accountStore) reserveResearchRequest(userID string, clock func() time.Time) (string, error) {
	tx, err := beginResearchBudget(a.db)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// Lock first, then sample once for both interval enforcement and the event.
	// Waiting for another writer must not spend the previous calendar period.
	if _, err = tx.Exec(`UPDATE research_budget SET revision=revision WHERE 0`); err != nil {
		return "", err
	}
	now := clock()
	if err = ensureResearchBudget(tx, userID, now); err != nil {
		return "", err
	}
	b, err := loadResearchBudget(tx, userID, now)
	if err != nil {
		return "", err
	}
	if b.Enabled && b.Used+b.Reserved >= b.Maximum {
		return "", errResearchBudgetExhausted
	}
	id := uuid.NewString()
	if _, err = tx.Exec(`INSERT INTO research_budget_events(id,user_id,started_ns,state) VALUES(?,?,?,'reserved')`, id, userID, now.UnixNano()); err != nil {
		return "", err
	}
	return id, tx.Commit()
}
func (a *accountStore) settleResearchRequest(userID, id string, status int) error {
	// No definitive response: retain the durable reservation for enforcement.
	if status == 0 {
		return nil
	}
	tx, err := beginResearchBudget(a.db)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if status >= 200 && status < 300 {
		_, err = tx.Exec(`UPDATE research_budget_events SET state='used' WHERE id=? AND user_id=?`, id, userID)
	} else {
		_, err = tx.Exec(`DELETE FROM research_budget_events WHERE id=? AND user_id=? AND state='reserved'`, id, userID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *researchSettingsService) trackRequests(userID string, w *researchWeb) {
	w.reserveRequest = func() (func(int) error, error) {
		id, err := s.accounts.reserveResearchRequest(userID, s.research.now)
		if errors.Is(err, errResearchBudgetExhausted) {
			return nil, err
		}
		if err != nil {
			return nil, errResearchBudgetUnavailable
		}
		return func(status int) error {
			if err := s.accounts.settleResearchRequest(userID, id, status); err != nil {
				return errResearchBudgetUnavailable
			}
			return nil
		}, nil
	}
}

type researchBudgetRequest struct {
	ExpectedRevision       *string `json:"expected_revision"`
	ExpectedBudgetRevision *string `json:"expected_budget_revision"`
	Enabled                *bool   `json:"enabled"`
	Maximum                *int    `json:"maximum"`
	Period                 *string `json:"period"`
	Timezone               *string `json:"timezone"`
}

func (s *researchSettingsService) saveBudget(userID, token string, req researchBudgetRequest) error {
	tx, err := beginResearchBudget(s.accounts.db)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Acquire writer lock before checking expiry; a wait must not extend a session.
	if _, err = tx.Exec(`UPDATE sessions SET expires_at=expires_at WHERE 0`); err != nil {
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
	if current.Revision != *req.ExpectedRevision {
		return errResearchSettingsConflict
	}
	if err = ensureResearchBudget(tx, userID, s.research.now()); err != nil {
		return err
	}
	b, err := loadResearchBudget(tx, userID, s.research.now())
	if err != nil {
		return err
	}
	if b.Revision != *req.ExpectedBudgetRevision {
		return errResearchSettingsConflict
	}
	_, err = tx.Exec(`UPDATE research_budget SET enabled=?,maximum=?,period=?,timezone=?,revision=? WHERE user_id=?`, *req.Enabled, *req.Maximum, *req.Period, *req.Timezone, uuid.NewString(), userID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *researchSettingsService) budgetHandler(w http.ResponseWriter, r *http.Request, userID string) {
	var req researchBudgetRequest
	if !decodeResearchJSON(w, r, &req, 4096) {
		return
	}
	if req.ExpectedRevision == nil || req.ExpectedBudgetRevision == nil || req.Enabled == nil || req.Maximum == nil || req.Period == nil || req.Timezone == nil {
		settingsFailure(w, 400, "bad_input", "Send both current revisions and all budget settings.", nil)
		return
	}
	if *req.Maximum < 1 || *req.Maximum > 10_000_000 || len(*req.Timezone) > 128 {
		settingsFailure(w, 400, "bad_input", "Choose a maximum between 1 and 10000000 and an IANA timezone.", nil)
		return
	}
	if _, _, err := researchBudgetBounds(s.research.now(), *req.Period, *req.Timezone); err != nil {
		settingsFailure(w, 400, "bad_input", "Choose a day or month and a valid IANA timezone.", nil)
		return
	}
	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		settingsFailure(w, 401, "signed_out", "Sign in to continue.", nil)
		return
	}
	err = s.saveBudget(userID, cookie.Value, req)
	if errors.Is(err, errResearchSettingsSignedOut) {
		settingsFailure(w, 401, "signed_out", "Sign in again before changing settings.", nil)
		return
	}
	if err != nil && !errors.Is(err, errResearchSettingsConflict) {
		settingsFailure(w, 503, "unavailable", "Research budget could not be saved.", nil)
		return
	}
	current, e := s.current(userID)
	if e != nil {
		settingsFailure(w, 503, "unavailable", "Research settings are unavailable.", nil)
		return
	}
	if errors.Is(err, errResearchSettingsConflict) {
		settingsFailure(w, 409, "conflict", "Settings changed. Reload before trying again.", &current)
		return
	}
	settingsReply(w, 200, current)
}
