package main

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestCodexHistoryForecastCoverageIndependentOfDisplayRange(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	now := time.Unix(1900000000, 0)
	s.now = func() time.Time { return now }
	codexHistoryConnect(t, f, rpc)
	entry, err := s.session(f.user, f.id)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.accounts.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO usage_observations(user_id,profile_id,connection_id,identity_key,window_id,label,minutes,at,remaining,reset_at) VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= 7*24*60; i++ {
		if _, err = stmt.Exec(f.user.ID, f.id, entry.record.ConnectionID, entry.identityKey, "codex:primary", "Weekly allowance", 10080, now.Unix()-int64(i)*60, 68, now.Add(3*24*time.Hour).Unix()); err != nil {
			t.Fatal(err)
		}
	}
	// Other profiles must not leak into the raw forecast baseline either.
	if _, err = stmt.Exec(f.user.ID, "other-profile", entry.record.ConnectionID, entry.identityKey, "codex:primary", "Other", 10080, now.Unix()-1, 1, now.Add(3*24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	stmt.Close()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var baseline []usageHistoryPoint
	for _, rangeID := range []string{"24h", "week", "period"} {
		t.Run(rangeID, func(t *testing.T) {
			out := f.request("GET", "/api/usage/history?profile_id="+f.id+"&range="+rangeID+"&window_id=codex:primary", "", "agent.example.test")
			if out.Code != 200 {
				t.Fatalf("history: %d %s", out.Code, out.Body.String())
			}
			var h struct {
				Points         []usageHistoryPoint `json:"points"`
				ForecastPoints []usageHistoryPoint `json:"forecast_points"`
			}
			if err := json.Unmarshal(out.Body.Bytes(), &h); err != nil {
				t.Fatal(err)
			}
			if len(h.ForecastPoints) != 1444 {
				t.Fatalf("need unbucketed trailing-day observations, got %d", len(h.ForecastPoints))
			}
			for i, p := range h.ForecastPoints {
				if p.At != now.Unix()-86400-180+int64(i)*60 || p.RemainingPercent != 68 {
					t.Fatalf("invented, downsampled or leaked forecast point: %+v", p)
				}
			}
			if len(h.Points) > 4096 {
				t.Fatalf("unbounded chart: %d", len(h.Points))
			}
			if baseline == nil {
				baseline = h.ForecastPoints
			} else if !reflect.DeepEqual(baseline, h.ForecastPoints) {
				t.Fatal("display range changed forecast evidence")
			}
		})
	}
}

func TestResearchHistoryForecastUsesExactLedgerCutoffs(t *testing.T) {
	a, _, _, _, user, now := setupResearchHistory(t)
	for i, at := range []time.Time{now.Add(-48 * time.Hour), now.Add(-25 * time.Hour), now.Add(-2 * time.Hour), now.Add(-30 * time.Minute)} {
		historyEvent(t, a, user, string(rune('a'+i)), at.Format(time.RFC3339Nano), "used")
	}
	historyEvent(t, a, user, "pending-forecast", now.Add(-10*time.Minute).Format(time.RFC3339Nano), "reserved")
	want := []usageHistoryPoint{
		{At: now.Add(-24 * time.Hour).Unix(), RemainingPercent: 80},
		{At: now.Add(-time.Hour).Unix(), RemainingPercent: 70},
		{At: now.Unix(), RemainingPercent: 60},
	}
	for _, rangeID := range []string{"24h", "week", "period"} {
		h, err := a.researchHistory(user, rangeID, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(h.ForecastPoints) != len(want) {
			t.Fatalf("%s: missing exact ledger baselines: %+v", rangeID, h.ForecastPoints)
		}
		for i, p := range h.ForecastPoints {
			if p.At != want[i].At || p.RemainingPercent != want[i].RemainingPercent || p.ResetAt != h.ResetAt {
				t.Fatalf("%s: cutoff %d = %+v, want %+v", rangeID, i, p, want[i])
			}
		}
	}
	if _, err := a.db.Exec(`UPDATE research_budget SET tracking_started_at=? WHERE user_id=?`, now.Add(-6*time.Hour).Format(time.RFC3339Nano), user); err != nil {
		t.Fatal(err)
	}
	h, err := a.researchHistory(user, "week", now)
	if err != nil {
		t.Fatal(err)
	}
	if h.ForecastPoints[0].At != now.Add(-6*time.Hour).Unix() {
		t.Fatal("forecast invented pretracking coverage")
	}
}
