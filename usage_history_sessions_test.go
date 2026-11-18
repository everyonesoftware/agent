package main

import (
	"encoding/json"
	"testing"
	"time"
)

// All session fixtures use the real owner-scoped SQLite observations table.
func sessionHistoryFixture(t *testing.T) (connectionFixture, *usageService, *usageSession, int64) {
	t.Helper()
	f, s, rpc := newUsageFixture(t)
	now := int64(1900000000)
	s.now = func() time.Time { return time.Unix(now, 0) }
	codexHistoryConnect(t, f, rpc)
	entry, err := s.session(f.user, f.id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.accounts.db.Exec(`DELETE FROM usage_observations`); err != nil {
		t.Fatal(err)
	}
	return f, s, entry, now
}

func seedSessionPoints(t *testing.T, f connectionFixture, entry *usageSession, points ...usageHistoryPoint) {
	t.Helper()
	tx, err := f.accounts.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO usage_observations(user_id,profile_id,connection_id,identity_key,window_id,label,minutes,at,remaining,reset_at) VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	for _, p := range points {
		if _, err = stmt.Exec(entry.userID, entry.view.ProfileID, entry.record.ConnectionID, entry.identityKey, "codex:primary", "Weekly allowance", 10080, p.At, p.RemainingPercent, p.ResetAt); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

type sessionHistoryWire struct {
	Sessions []struct {
		StartAt     int64   `json:"start_at"`
		EndAt       int64   `json:"end_at"`
		LastUsedAt  int64   `json:"last_used_at"`
		UsedPercent float64 `json:"used_percent"`
		Current     bool    `json:"current"`
		Estimated   bool    `json:"estimated"`
	} `json:"sessions"`
	SessionsTruncated bool `json:"sessions_truncated"`
	ForecastPoints    []struct {
		WindowMinutes int64 `json:"window_minutes"`
	} `json:"forecast_points"`
}

func readSessionHistory(t *testing.T, s *usageService, entry *usageSession, rangeID string) (usageHistory, sessionHistoryWire) {
	t.Helper()
	h, err := s.history(entry, rangeID, "codex:primary")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	var wire sessionHistoryWire
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	return h, wire
}

func TestUsageHistorySessionsEstimatedCurrentAndRawDefinition(t *testing.T) {
	f, s, entry, now := sessionHistoryFixture(t)
	seedSessionPoints(t, f, entry,
		usageHistoryPoint{At: now - 180, RemainingPercent: 90, ResetAt: now + 86400},
		usageHistoryPoint{At: now - 120, RemainingPercent: 88, ResetAt: now + 86400},
		usageHistoryPoint{At: now - 60, RemainingPercent: 85, ResetAt: now + 86400},
		usageHistoryPoint{At: now, RemainingPercent: 85, ResetAt: now + 86400})
	_, h := readSessionHistory(t, s, entry, "week")
	if len(h.Sessions) != 1 {
		t.Fatalf("want one estimated session, got %+v", h.Sessions)
	}
	got := h.Sessions[0]
	if got.StartAt != now-180 || got.EndAt != now || got.LastUsedAt != now-60 || got.UsedPercent != 5 || !got.Current || !got.Estimated {
		t.Fatalf("session evidence lost: %+v", got)
	}
	if len(h.ForecastPoints) != 4 || h.ForecastPoints[0].WindowMinutes != 10080 {
		t.Fatalf("raw forecast lost allowance definition: %+v", h.ForecastPoints)
	}
}

func TestUsageHistorySessionsRejectUnsafeIntervalsAndMissingTails(t *testing.T) {
	for _, tc := range []struct {
		name        string
		tail        []usageHistoryPoint
		state       string
		warning     string
		wantCount   int
		wantUsed    float64
		wantCurrent bool
	}{
		{name: "gap decline", tail: []usageHistoryPoint{{At: -300, RemainingPercent: 80}, {At: 0, RemainingPercent: 70}}, wantCount: 1, wantUsed: 10},
		{name: "gap then safe decline", tail: []usageHistoryPoint{{At: -300, RemainingPercent: 80}, {At: -60, RemainingPercent: 70}, {At: 0, RemainingPercent: 69}}, wantCount: 2, wantUsed: 10, wantCurrent: true},
		{name: "balance increase", tail: []usageHistoryPoint{{At: -300, RemainingPercent: 95}, {At: 0, RemainingPercent: 95}}, wantCount: 1, wantUsed: 2},
		{name: "changed reset", tail: []usageHistoryPoint{{At: -300, RemainingPercent: 80, ResetAt: 172800}, {At: 0, RemainingPercent: 80, ResetAt: 172800}}, wantCount: 1, wantUsed: 2},
		{name: "elapsed reset", tail: []usageHistoryPoint{{At: 86400, RemainingPercent: 80, ResetAt: 86400}}, wantCount: 1, wantUsed: 2},
		{name: "stale tail", tail: []usageHistoryPoint{{At: -181, RemainingPercent: 88}}, wantCount: 1, wantUsed: 2},
		{name: "disconnected", tail: []usageHistoryPoint{{At: -300, RemainingPercent: 88}, {At: -120, RemainingPercent: 88}, {At: 0, RemainingPercent: 88}}, state: "disconnected", wantCount: 1, wantUsed: 2},
		{name: "provider error", tail: []usageHistoryPoint{{At: -300, RemainingPercent: 88}, {At: -120, RemainingPercent: 88}, {At: 0, RemainingPercent: 88}}, warning: "fixture unavailable", wantCount: 1, wantUsed: 2},
		{name: "fresh exact gap", tail: []usageHistoryPoint{{At: -300, RemainingPercent: 88}, {At: -180, RemainingPercent: 88}}, wantCount: 1, wantUsed: 2, wantCurrent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, s, entry, now := sessionHistoryFixture(t)
			points := []usageHistoryPoint{{At: now - 420, RemainingPercent: 90, ResetAt: now + 86400}, {At: now - 360, RemainingPercent: 88, ResetAt: now + 86400}}
			for _, p := range tc.tail {
				p.At += now
				if p.ResetAt == 0 {
					p.ResetAt = 86400
				}
				p.ResetAt += now
				points = append(points, p)
			}
			seedSessionPoints(t, f, entry, points...)
			if tc.state != "" {
				entry.view.State = tc.state
			}
			entry.view.Error = tc.warning
			_, h := readSessionHistory(t, s, entry, "week")
			if len(h.Sessions) != tc.wantCount || h.Sessions[0].UsedPercent != tc.wantUsed || h.Sessions[0].LastUsedAt != now-300 && tc.wantUsed == 10 || h.Sessions[0].LastUsedAt != now-360 && tc.wantUsed == 2 {
				t.Fatalf("unsafe interval attributed: %+v", h.Sessions)
			}
			if h.Sessions[len(h.Sessions)-1].Current != tc.wantCurrent {
				t.Fatalf("wrong current flag: %+v", h.Sessions)
			}
			if !h.Sessions[0].Current && h.Sessions[0].EndAt != h.Sessions[0].LastUsedAt {
				t.Fatalf("completed session extended without usage: %+v", h.Sessions[0])
			}
		})
	}
}

func TestUsageHistorySessionsObservedIdleBoundary(t *testing.T) {
	for _, idle := range []int64{1799, 1800, 1801} {
		t.Run((time.Duration(idle) * time.Second).String(), func(t *testing.T) {
			f, s, entry, now := sessionHistoryFixture(t)
			lastUsed := now - idle
			points := []usageHistoryPoint{{At: lastUsed - 60, RemainingPercent: 90, ResetAt: now + 86400}, {At: lastUsed, RemainingPercent: 89, ResetAt: now + 86400}}
			for at := lastUsed + 60; at < now; at += 60 {
				points = append(points, usageHistoryPoint{At: at, RemainingPercent: 89, ResetAt: now + 86400})
			}
			points = append(points, usageHistoryPoint{At: now, RemainingPercent: 89, ResetAt: now + 86400})
			seedSessionPoints(t, f, entry, points...)
			_, h := readSessionHistory(t, s, entry, "week")
			if len(h.Sessions) != 1 || h.Sessions[0].Current != (idle < 1800) {
				t.Fatalf("idle freshness incorrect: %+v", h.Sessions)
			}
			wantEnd := lastUsed
			if idle < 1800 {
				wantEnd = now
			}
			if h.Sessions[0].EndAt != wantEnd {
				t.Fatalf("idle invented session end: %+v", h.Sessions)
			}
			seedSessionPoints(t, f, entry, usageHistoryPoint{At: now + 60, RemainingPercent: 88, ResetAt: now + 86400})
			s.now = func() time.Time { return time.Unix(now+60, 0) }
			_, h = readSessionHistory(t, s, entry, "week")
			wantCount := 1
			if idle > 1800 {
				wantCount = 2
			}
			if len(h.Sessions) != wantCount || !h.Sessions[len(h.Sessions)-1].Current {
				t.Fatalf("observed idle did not split correctly: %+v", h.Sessions)
			}
		})
	}
}

func TestUsageHistorySessionsDefinitionChangesAndElapsedReset(t *testing.T) {
	for _, changed := range []string{"definition", "elapsed reset"} {
		t.Run(changed, func(t *testing.T) {
			f, s, entry, now := sessionHistoryFixture(t)
			seedSessionPoints(t, f, entry,
				usageHistoryPoint{At: now - 180, RemainingPercent: 90, ResetAt: now + 86400},
				usageHistoryPoint{At: now - 120, RemainingPercent: 89, ResetAt: now + 86400},
				usageHistoryPoint{At: now - 60, RemainingPercent: 80, ResetAt: now + 86400},
				usageHistoryPoint{At: now, RemainingPercent: 79, ResetAt: now + 86400})
			var err error
			if changed == "definition" {
				_, err = f.accounts.db.Exec(`UPDATE usage_observations SET minutes=300 WHERE at>=?`, now-60)
			} else {
				_, err = f.accounts.db.Exec(`UPDATE usage_observations SET reset_at=? WHERE at<?`, now-60, now-60)
			}
			if err != nil {
				t.Fatal(err)
			}
			_, h := readSessionHistory(t, s, entry, "week")
			if len(h.Sessions) != 2 || h.Sessions[0].UsedPercent != 1 || h.Sessions[1].UsedPercent != 1 || h.Sessions[0].Current || !h.Sessions[1].Current {
				t.Fatalf("incomparable percentages combined: %+v", h.Sessions)
			}
			if changed == "definition" && (h.ForecastPoints[0].WindowMinutes != 10080 || h.ForecastPoints[3].WindowMinutes != 300) {
				t.Fatalf("forecast definitions lost: %+v", h.ForecastPoints)
			}
		})
	}
}

func TestUsageHistorySessionsRangeOverlapAndBoundedLookback(t *testing.T) {
	f, s, entry, now := sessionHistoryFixture(t)
	start := now - 86400
	seedSessionPoints(t, f, entry,
		usageHistoryPoint{At: now - 2*86400, RemainingPercent: 95, ResetAt: now + 86400},
		usageHistoryPoint{At: now - 2*86400 + 60, RemainingPercent: 94, ResetAt: now + 86400},
		usageHistoryPoint{At: start - 1981, RemainingPercent: 94, ResetAt: now + 86400},
		usageHistoryPoint{At: start - 1980, RemainingPercent: 93, ResetAt: now + 86400},
		usageHistoryPoint{At: start - 1900, RemainingPercent: 92, ResetAt: now + 86400},
		usageHistoryPoint{At: start - 120, RemainingPercent: 92, ResetAt: now + 86400},
		usageHistoryPoint{At: start + 60, RemainingPercent: 90, ResetAt: now + 86400},
		usageHistoryPoint{At: now - 60, RemainingPercent: 89, ResetAt: now + 86400},
		usageHistoryPoint{At: now, RemainingPercent: 88, ResetAt: now + 86400})
	for _, rangeID := range []string{"24h", "week", "period"} {
		_, h := readSessionHistory(t, s, entry, rangeID)
		wantCount := 4
		if rangeID == "24h" {
			wantCount = 2
		}
		if len(h.Sessions) != wantCount || h.SessionsTruncated {
			t.Fatalf("%s: range filtering failed: %+v", rangeID, h)
		}
		crossing := h.Sessions[len(h.Sessions)-2]
		if crossing.StartAt != start-120 || crossing.EndAt != start+60 || crossing.UsedPercent != 2 {
			t.Fatalf("%s: overlapping session clipped or lookback leaked: %+v", rangeID, crossing)
		}
	}
}

func TestUsageHistorySessionsRawNotDownsampledAndSessionLimit(t *testing.T) {
	for _, count := range []int{6000, 10000} {
		f, s, entry, now := sessionHistoryFixture(t)
		points := make([]usageHistoryPoint, count)
		for i := range points {
			points[i] = usageHistoryPoint{At: now - int64(count-1-i)*60, RemainingPercent: 90 - float64(i%2), ResetAt: now + 86400}
		}
		seedSessionPoints(t, f, entry, points...)
		chart, h := readSessionHistory(t, s, entry, "week")
		want := count / 2
		truncated := want >= 4096
		if want > 4096 {
			want = 4096
		}
		if len(chart.Points) > 4096 || len(chart.Points) >= count || len(h.Sessions) != want || h.SessionsTruncated != truncated {
			t.Fatalf("raw sessions not independently bounded: chart=%d sessions=%d truncated=%v", len(chart.Points), len(h.Sessions), h.SessionsTruncated)
		}
		for _, session := range h.Sessions {
			if session.UsedPercent != 1 || session.EndAt-session.StartAt != 60 || !session.Estimated {
				t.Fatalf("bucket spacing contaminated session: %+v", session)
			}
		}
		if !h.Sessions[len(h.Sessions)-1].Current || h.Sessions[len(h.Sessions)-1].LastUsedAt != now {
			t.Fatal("latest session lost")
		}
	}
}

func TestUsageHistorySessionsDenseRawSuffixDisclosesPartialCoverage(t *testing.T) {
	f, s, entry, now := sessionHistoryFixture(t)
	_, err := f.accounts.db.Exec(`WITH RECURSIVE seq(i) AS (VALUES(0) UNION ALL SELECT i+1 FROM seq WHERE i<131075)
	 INSERT INTO usage_observations(user_id,profile_id,connection_id,identity_key,window_id,label,minutes,at,remaining,reset_at)
	 SELECT ?,?,?,?,'codex:primary','Weekly allowance',10080,?-131075+i,90-i/1000000.0,? FROM seq`, entry.userID, entry.view.ProfileID, entry.record.ConnectionID, entry.identityKey, now, now+86400)
	if err != nil {
		t.Fatal(err)
	}
	// A boundary crosses the raw suffix cutoff. Fetching any extra evidence
	// would incorrectly turn the suppressed prefix into a session.
	if _, err = f.accounts.db.Exec(`UPDATE usage_observations SET remaining=94 WHERE at=?`, now-131072); err != nil {
		t.Fatal(err)
	}
	if _, err = f.accounts.db.Exec(`UPDATE usage_observations SET remaining=95 WHERE at=?`, now-131071); err != nil {
		t.Fatal(err)
	}
	_, bounded := readSessionHistory(t, s, entry, "24h")
	if !bounded.SessionsTruncated || len(bounded.Sessions) != 1 || bounded.Sessions[0].StartAt != now-86400-1800-180 {
		t.Fatalf("range-bounded session beginning must disclose incomplete coverage: sessions=%+v truncated=%v", bounded.Sessions, bounded.SessionsTruncated)
	}
	_, h := readSessionHistory(t, s, entry, "week")
	if !h.SessionsTruncated || len(h.Sessions) != 0 {
		t.Fatalf("dense truncation invented a cluster beginning: count=%d truncated=%v", len(h.Sessions), h.SessionsTruncated)
	}
	// A witnessed boundary in the retained suffix makes later clusters knowable.
	if _, err = f.accounts.db.Exec(`UPDATE usage_observations SET remaining=95 WHERE at=?`, now-60); err != nil {
		t.Fatal(err)
	}
	_, h = readSessionHistory(t, s, entry, "week")
	if !h.SessionsTruncated || len(h.Sessions) != 1 || h.Sessions[0].StartAt != now-60 || !h.Sessions[0].Current {
		t.Fatalf("dense suffix lost known complete cluster: %+v", h)
	}
}

func TestUsageHistorySessionsScopeWindowIsolationAndReadOnly(t *testing.T) {
	f, s, entry, now := sessionHistoryFixture(t)
	seedSessionPoints(t, f, entry, usageHistoryPoint{At: now - 60, RemainingPercent: 90, ResetAt: now + 86400}, usageHistoryPoint{At: now, RemainingPercent: 89, ResetAt: now + 86400})
	other, _, err := f.accounts.register("session-other-owner", "fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	for index, field := range []string{"user_id", "profile_id", "connection_id", "identity_key", "window_id"} {
		owner, profile, connection, identity, window := entry.userID, entry.view.ProfileID, entry.record.ConnectionID, entry.identityKey, "codex:primary"
		switch field {
		case "user_id":
			owner = other.ID
		case "profile_id":
			profile = "other-profile"
		case "connection_id":
			connection = "other-connection"
		case "identity_key":
			identity = "other-identity"
		case "window_id":
			window = "other-window"
		}
		for i := 0; i < 2; i++ {
			at := now - int64(20-index*2-i)
			if field == "window_id" {
				at = now - int64(1-i)*60
			}
			_, err = f.accounts.db.Exec(`INSERT INTO usage_observations(user_id,profile_id,connection_id,identity_key,window_id,label,minutes,at,remaining,reset_at) VALUES(?,?,?,?,?,'Other',10080,?,?,?)`, owner, profile, connection, identity, window, at, 90-i*50, now+86400)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	var before, after int
	if err = f.accounts.db.QueryRow(`SELECT COUNT(*) FROM usage_observations`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	_, h := readSessionHistory(t, s, entry, "week")
	if len(h.Sessions) != 1 || h.Sessions[0].UsedPercent != 1 || h.Sessions[0].StartAt != now-60 || len(h.ForecastPoints) != 2 {
		t.Fatalf("foreign raw evidence leaked: %+v", h)
	}
	otherWindow, err := s.history(entry, "week", "other-window")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(otherWindow)
	if err != nil {
		t.Fatal(err)
	}
	var otherWire sessionHistoryWire
	if err = json.Unmarshal(data, &otherWire); err != nil {
		t.Fatal(err)
	}
	if len(otherWire.Sessions) != 1 || otherWire.Sessions[0].UsedPercent != 50 {
		t.Fatalf("selected window not honored: %+v", otherWire)
	}
	if err = f.accounts.db.QueryRow(`SELECT COUNT(*) FROM usage_observations`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("history changed persisted evidence: before=%d after=%d", before, after)
	}
}

func TestUsageHistorySessionsDelayedObservationDoesNotInventIdle(t *testing.T) {
	f, s, entry, now := sessionHistoryFixture(t)
	points := []usageHistoryPoint{{At: now - 1900, RemainingPercent: 90, ResetAt: now + 86400}, {At: now - 1840, RemainingPercent: 89, ResetAt: now + 86400}}
	for at := now - 1780; at < now-180; at += 60 {
		points = append(points, usageHistoryPoint{At: at, RemainingPercent: 89, ResetAt: now + 86400})
	}
	points = append(points, usageHistoryPoint{At: now - 180, RemainingPercent: 89, ResetAt: now + 86400})
	seedSessionPoints(t, f, entry, points...)
	_, h := readSessionHistory(t, s, entry, "week")
	if len(h.Sessions) != 1 || !h.Sessions[0].Current || h.Sessions[0].EndAt != now-180 {
		t.Fatalf("wall time counted as observed idle: %+v", h.Sessions)
	}
	s.now = func() time.Time { return time.Unix(now+1, 0) }
	_, h = readSessionHistory(t, s, entry, "week")
	if h.Sessions[0].Current || h.Sessions[0].EndAt != now-1840 {
		t.Fatalf("missing tail extended completed usage: %+v", h.Sessions)
	}
}

func TestUsageHistorySessionsFlatAndResetOnlyDoNotCreateSessions(t *testing.T) {
	f, s, entry, now := sessionHistoryFixture(t)
	seedSessionPoints(t, f, entry,
		usageHistoryPoint{At: now - 180, RemainingPercent: 90, ResetAt: now - 60},
		usageHistoryPoint{At: now - 120, RemainingPercent: 90, ResetAt: now - 60},
		usageHistoryPoint{At: now - 60, RemainingPercent: 100, ResetAt: now + 86400},
		usageHistoryPoint{At: now, RemainingPercent: 100, ResetAt: now + 86400})
	chart, h := readSessionHistory(t, s, entry, "week")
	if len(h.Sessions) != 0 || h.SessionsTruncated {
		t.Fatalf("reset itself became usage: %+v", h)
	}
	data, err := json.Marshal(chart)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["sessions"] != nil || fields["sessions_truncated"] != nil {
		t.Fatalf("empty optional session fields should be omitted: %s", data)
	}
	data, err = json.Marshal(usageHistoryPoint{At: now, RemainingPercent: 90})
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["window_minutes"] != nil {
		t.Fatalf("legacy points must not invent allowance definitions: %s", data)
	}
}
