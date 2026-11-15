package main

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var errUsageHistoryUnavailable = errors.New("Usage history storage is unavailable.")

func migrateUsageHistory(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS usage_observations (
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 profile_id TEXT NOT NULL, connection_id TEXT NOT NULL, identity_key TEXT NOT NULL,
 window_id TEXT NOT NULL, label TEXT NOT NULL, minutes INTEGER NOT NULL,
 at INTEGER NOT NULL, remaining REAL NOT NULL CHECK(remaining>=0 AND remaining<=100), reset_at INTEGER NOT NULL,
 PRIMARY KEY(user_id,profile_id,connection_id,identity_key,window_id,at));
 CREATE INDEX IF NOT EXISTS usage_observations_retention ON usage_observations(at);`)
	return err
}

func (s *usageService) recordHistory(ctx context.Context, entry *usageSession) error {
	if s.historyErr != nil {
		return errUsageHistoryUnavailable
	}
	if entry.record.ConnectionID == "" || entry.identityKey == "" || entry.view.IdentityMatch == "different" || entry.view.Snapshot == nil {
		return nil
	}
	tx, err := s.accounts.db.BeginTx(ctx, nil)
	if err != nil {
		return errUsageHistoryUnavailable
	}
	defer tx.Rollback()
	for _, w := range entry.view.Snapshot.Windows {
		_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO usage_observations(user_id,profile_id,connection_id,identity_key,window_id,label,minutes,at,remaining,reset_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, entry.userID, entry.view.ProfileID, entry.record.ConnectionID, entry.identityKey, w.ID, w.Label, w.WindowMinutes, entry.view.Snapshot.FetchedAt, 100-w.UsedPercent, w.ResetAt)
		if err != nil {
			return errUsageHistoryUnavailable
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM usage_observations WHERE at<?`, s.now().Add(-90*24*time.Hour).Unix()); err != nil {
		return errUsageHistoryUnavailable
	}
	if tx.Commit() != nil {
		return errUsageHistoryUnavailable
	}
	return nil
}

func (s *usageService) history(entry *usageSession, rangeID, windowID string) (usageHistory, error) {
	h := usageHistory{Provider: "chatgpt", Scope: entry.view.ProfileID, Points: []usageHistoryPoint{}, Stale: true, GapSeconds: 180}
	if _, err := historyRangeStart(rangeID, s.now(), 0); err != nil {
		return h, err
	}
	if s.historyErr != nil || entry.historyWriteErr != nil {
		return h, errUsageHistoryUnavailable
	}
	if entry.record.ConnectionID == "" || entry.identityKey == "" || entry.view.IdentityMatch == "different" || entry.view.Identity == nil {
		return h, nil
	}
	args := []any{entry.userID, entry.view.ProfileID, entry.record.ConnectionID, entry.identityKey}
	const scope = `user_id=? AND profile_id=? AND connection_id=? AND identity_key=?`
	// Only windows from the newest observed snapshot are selectable.
	rows, err := s.accounts.db.Query(`SELECT window_id,label,minutes FROM usage_observations WHERE `+scope+` AND at=(SELECT MAX(at) FROM usage_observations WHERE `+scope+`) ORDER BY minutes DESC,window_id`, append(append([]any{}, args...), args...)...)
	if err != nil {
		return h, errUsageHistoryUnavailable
	}
	for rows.Next() {
		var w usageHistoryWindow
		var minutes int64
		if rows.Scan(&w.ID, &w.Label, &minutes) != nil {
			rows.Close()
			return h, errUsageHistoryUnavailable
		}
		h.Windows = append(h.Windows, w)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return h, errUsageHistoryUnavailable
	}
	if len(h.Windows) == 0 {
		return h, nil
	}
	if windowID == "" {
		windowID = h.Windows[0].ID
	}
	found := false
	for _, w := range h.Windows {
		if w.ID == windowID {
			found = true
		}
	}
	if !found {
		return h, errors.New("Select a valid quota window.")
	}
	h.WindowID = windowID
	args = append(args, windowID)
	var remaining float64
	var minutes int64
	if err = s.accounts.db.QueryRow(`SELECT at,remaining,reset_at,minutes FROM usage_observations WHERE `+scope+` AND window_id=? ORDER BY at DESC LIMIT 1`, args...).Scan(&h.ObservedAt, &remaining, &h.ResetAt, &minutes); err != nil {
		return h, errUsageHistoryUnavailable
	}
	h.RemainingPercent = &remaining
	if h.ResetAt > 0 && minutes > 0 {
		h.PeriodStart = h.ResetAt - minutes*60
	}
	if err = s.accounts.db.QueryRow(`SELECT MIN(at) FROM usage_observations WHERE `+scope+` AND window_id=?`, args...).Scan(&h.TrackingStartedAt); err != nil {
		return h, errUsageHistoryUnavailable
	}
	start, _ := historyRangeStart(rangeID, s.now(), h.PeriodStart)
	// Include nearby real observations before the range for forecast baselines.
	start -= h.GapSeconds
	// Bound results while retaining real observations across the entire range.
	bucket := int64(1)
	var count int64
	countArgs := append(append([]any{}, args...), start, s.now().Unix())
	if err = s.accounts.db.QueryRow(`SELECT COUNT(*) FROM usage_observations WHERE `+scope+` AND window_id=? AND at>=? AND at<=?`, countArgs...).Scan(&count); err != nil {
		return h, errUsageHistoryUnavailable
	}
	if count > 4096 {
		bucket = (s.now().Unix()-start)/4095 + 1
	}
	pointArgs := append(append([]any{}, args...), start, s.now().Unix(), start, bucket)
	pointArgs = append(pointArgs, args...)
	rows, err = s.accounts.db.Query(`SELECT at,remaining,reset_at FROM usage_observations WHERE at IN (SELECT MAX(at) FROM usage_observations WHERE `+scope+` AND window_id=? AND at>=? AND at<=? GROUP BY (at-?)/?) AND `+scope+` AND window_id=? ORDER BY at LIMIT 4096`, pointArgs...)
	if err != nil {
		return h, errUsageHistoryUnavailable
	}
	for rows.Next() {
		var p usageHistoryPoint
		if rows.Scan(&p.At, &p.RemainingPercent, &p.ResetAt) != nil {
			rows.Close()
			return h, errUsageHistoryUnavailable
		}
		h.Points = append(h.Points, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return h, errUsageHistoryUnavailable
	}
	// Forecasts use recent raw observations, not display-range buckets. Keeping
	// the newest bounded suffix preserves genuine gaps without introducing gaps
	// through downsampling, and shortens the labeled duration for dense bursts.
	forecastArgs := append(append([]any{}, args...), s.now().Add(-24*time.Hour).Unix()-h.GapSeconds, s.now().Unix())
	rows, err = s.accounts.db.Query(`SELECT at,remaining,reset_at FROM (SELECT at,remaining,reset_at FROM usage_observations WHERE `+scope+` AND window_id=? AND at>=? AND at<=? ORDER BY at DESC LIMIT 4096) ORDER BY at`, forecastArgs...)
	if err != nil {
		return h, errUsageHistoryUnavailable
	}
	for rows.Next() {
		var p usageHistoryPoint
		if rows.Scan(&p.At, &p.RemainingPercent, &p.ResetAt) != nil {
			rows.Close()
			return h, errUsageHistoryUnavailable
		}
		h.ForecastPoints = append(h.ForecastPoints, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return h, errUsageHistoryUnavailable
	}
	h.Stale = s.now().Unix()-h.ObservedAt > 60 || (h.ResetAt > 0 && h.ResetAt <= s.now().Unix()) || entry.view.State != "connected" || entry.view.Error != ""
	h.Warning = entry.view.Error
	return h, nil
}
