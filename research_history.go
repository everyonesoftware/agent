package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

func (s *researchSettingsService) historyHandler(w http.ResponseWriter, r *http.Request) {
	if requestIsCrossSite(r) {
		settingsFailure(w, 403, "forbidden", "Cross-site history requests are not allowed.", nil)
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	rangeID := query.Get("range")
	if err != nil || len(query) != 1 || len(query["range"]) != 1 {
		settingsFailure(w, 400, "bad_input", "Choose 24h, week, or period.", nil)
		return
	}
	now := s.research.now()
	if _, err = historyRangeStart(rangeID, now, 0); err != nil {
		settingsFailure(w, 400, "bad_input", err.Error(), nil)
		return
	}
	user, _ := currentAuthUser(r.Context())
	history, err := s.accounts.researchHistory(user.ID, rangeID, now)
	if err != nil {
		settingsFailure(w, 503, "unavailable", "Research history is unavailable.", nil)
		return
	}
	writeChatJSON(w, 200, history)
}

// This is an account-local cap ledger, not a Brave billing/quota observation.
// A read transaction keeps the baseline, bucket counts and current total coherent.
func (a *accountStore) researchHistory(userID, rangeID string, now time.Time) (usageHistory, error) {
	h := usageHistory{Provider: "brave", Scope: "Brave Search · Agent cap", ObservedAt: now.Unix(), WindowID: rangeID,
		Windows: []usageHistoryWindow{{ID: "24h", Label: "24 hours"}, {ID: "week", Label: "Week"}, {ID: "period", Label: "Entire period"}}, Points: []usageHistoryPoint{}}
	tx, err := a.db.BeginTx(context.Background(), nil)
	if err != nil {
		return h, err
	}
	defer tx.Rollback()
	b, err := loadResearchBudget(tx, userID, now)
	if errors.Is(err, sql.ErrNoRows) {
		b = researchBudget{Maximum: 1000, Period: "month", Timezone: "UTC"}
	} else if err != nil {
		return h, err
	}
	start, reset, err := researchBudgetBounds(now, b.Period, b.Timezone)
	if err != nil {
		return h, err
	}
	h.PeriodStart = start.Unix()
	h.ResetAt = reset.Unix()
	if b.TrackingStartedAt != "" {
		tracked, err := time.Parse(time.RFC3339Nano, b.TrackingStartedAt)
		if err != nil {
			return h, err
		}
		h.TrackingStartedAt = tracked.Unix()
	}
	if !b.Enabled {
		h.Warning = "Enable an Agent cap to chart remaining searches."
		return h, tx.Commit()
	}
	if b.TrackingStartedAt == "" {
		h.Warning = "Search tracking start is unknown; history is unavailable."
		return h, tx.Commit()
	}
	if b.Maximum < 1 {
		return h, errors.New("invalid Agent cap")
	}
	h.Scope = fmt.Sprintf("Brave Search · Agent %s cap", b.Period)
	selected, err := historyRangeStart(rangeID, now, h.PeriodStart)
	if err != nil {
		return h, err
	}
	if selected > start.Unix() {
		start = time.Unix(selected, 0)
	}
	// Tracking coverage is not inferred from zero events. Never draw a flat
	// pre-installation line, even when the selected calendar cycle is older.
	tracked, err := time.Parse(time.RFC3339Nano, b.TrackingStartedAt)
	if err != nil {
		return h, err
	}
	if tracked.After(start) {
		start = tracked
	}
	if start.After(now) {
		h.Warning = "Search tracking has not started yet."
		return h, tx.Commit()
	}
	var baseline int64
	if err = tx.QueryRow(`SELECT COUNT(*) FROM research_budget_events WHERE user_id=? AND state='used' AND started_ns>=? AND started_ns<?`, userID, time.Unix(h.PeriodStart, 0).UnixNano(), start.UnixNano()).Scan(&baseline); err != nil {
		return h, err
	}
	remaining := func(used int64) float64 {
		return 100 * float64(max(int64(0), int64(b.Maximum)-used)) / float64(b.Maximum)
	}
	h.Points = append(h.Points, usageHistoryPoint{At: start.Unix(), RemainingPercent: remaining(baseline), ResetAt: h.ResetAt})
	// Aggregate in SQLite: at most 980 buckets, each at its final real event time.
	width := (now.UnixNano()-start.UnixNano())/980 + 1
	rows, err := tx.Query(`SELECT MAX(started_ns),COUNT(*) FROM research_budget_events WHERE user_id=? AND state='used' AND started_ns>=? AND started_ns<=? GROUP BY (started_ns-?)/? ORDER BY MAX(started_ns)`, userID, start.UnixNano(), now.UnixNano(), start.UnixNano(), width)
	if err != nil {
		return h, err
	}
	for rows.Next() {
		var at, n int64
		if err = rows.Scan(&at, &n); err != nil {
			rows.Close()
			return h, err
		}
		baseline += n
		h.Points = append(h.Points, usageHistoryPoint{At: time.Unix(0, at).Unix(), RemainingPercent: remaining(baseline), ResetAt: h.ResetAt})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return h, err
	}
	count := max(int64(0), int64(b.Maximum)-baseline)
	percent := remaining(baseline)
	h.RemainingCount = &count
	h.RemainingPercent = &percent
	h.Points = append(h.Points, usageHistoryPoint{At: now.Unix(), RemainingPercent: percent, ResetAt: h.ResetAt})
	return h, tx.Commit()
}
