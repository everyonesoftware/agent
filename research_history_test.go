package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type researchHistoryWire struct {
	Provider          string   `json:"provider"`
	Scope             string   `json:"scope"`
	ObservedAt        int64    `json:"observed_at"`
	TrackingStartedAt int64    `json:"tracking_started_at"`
	PeriodStart       int64    `json:"period_start"`
	ResetAt           int64    `json:"reset_at"`
	RemainingPercent  *float64 `json:"remaining_percent"`
	RemainingCount    *int64   `json:"remaining_count"`
	WindowID          string   `json:"window_id"`
	Windows           []struct {
		ID string `json:"id"`
	} `json:"windows"`
	Points []struct {
		At               int64   `json:"at"`
		RemainingPercent float64 `json:"remaining_percent"`
		ResetAt          int64   `json:"reset_at"`
	} `json:"points"`
	GapSeconds int64  `json:"gap_seconds"`
	Warning    string `json:"warning"`
}

func readResearchHistory(t *testing.T, h http.Handler, c *http.Cookie, window string) researchHistoryWire {
	t.Helper()
	w := researchRequest(h, c, "GET", "/api/research/history?range="+window, "")
	if w.Code != 200 {
		t.Fatalf("history status=%d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
	var v researchHistoryWire
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func TestResearchHistoryDisabledCap(t *testing.T) {
	_, s, h, c, _ := settingsHarness(t, func(http.ResponseWriter, *http.Request) { t.Error("history called Brave") })
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	v := readResearchHistory(t, h, c, "24h")
	if v.Provider != "brave" || v.RemainingPercent != nil || v.RemainingCount != nil || v.Points == nil || len(v.Points) != 0 || v.Warning != "Enable an Agent cap to chart remaining searches." {
		t.Fatalf("disabled: %+v", v)
	}
	if v.ObservedAt != now.Unix() || v.GapSeconds != 0 || v.WindowID != "24h" || len(v.Windows) != 3 {
		t.Fatalf("metadata: %+v", v)
	}
}
func TestResearchHistorySecurity(t *testing.T) {
	_, _, h, c, _ := settingsHarness(t, func(http.ResponseWriter, *http.Request) { t.Error("history called Brave") })
	for _, tc := range []struct {
		query, origin, site string
		cookie              *http.Cookie
		status              int
	}{
		{"range=24h", "", "", nil, 401},
		{"range=24h", "https://evil.invalid", "", c, 403},
		{"range=24h", "", "cross-site", c, 403},
		{"range=bad", "", "", c, 400},
		{"range=24h&range=week", "", "", c, 400},
		{"range=24h&user_id=other", "", "", c, 400},
	} {
		r := httptest.NewRequest("GET", "/api/research/history?"+tc.query, nil)
		if tc.cookie != nil {
			r.AddCookie(tc.cookie)
		}
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
	}
}
func TestResearchHistoryTrackingCoverage(t *testing.T) {
	a, _, h, c, user, now := setupResearchHistory(t)
	tracked := now.Add(-2 * time.Hour)
	if _, err := a.db.Exec(`UPDATE research_budget SET tracking_started_at=? WHERE user_id=?`, tracked.Format(time.RFC3339Nano), user); err != nil {
		t.Fatal(err)
	}
	v := readResearchHistory(t, h, c, "period")
	if len(v.Points) != 2 || v.Points[0].At != tracked.Unix() || v.Points[1].At != now.Unix() {
		t.Fatalf("invented pretracking coverage: %+v", v.Points)
	}
	if v.TrackingStartedAt != tracked.Unix() {
		t.Fatalf("tracking metadata: %+v", v)
	}
}

func TestResearchHistoryUnknownTrackingIsNotZero(t *testing.T) {
	a, _, h, c, user, _ := setupResearchHistory(t)
	if _, err := a.db.Exec(`UPDATE research_budget SET tracking_started_at='' WHERE user_id=?`, user); err != nil {
		t.Fatal(err)
	}
	v := readResearchHistory(t, h, c, "period")
	if v.TrackingStartedAt != 0 || len(v.Points) != 0 || v.RemainingPercent != nil || v.Warning == "" {
		t.Fatalf("unknown tracking invented history: %+v", v)
	}
}

func TestResearchHistorySettlementBoundaryAndRevisionIsolation(t *testing.T) {
	a, s, h, c, user, now := setupResearchHistory(t)
	boundary := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	if _, err := a.db.Exec(`UPDATE research_budget SET period='day',tracking_started_at='2026-11-01T00:00:00Z' WHERE user_id=?`, user); err != nil {
		t.Fatal(err)
	}
	old, err := a.reserveResearchRequest(user, func() time.Time { return boundary.Add(-time.Nanosecond) })
	if err != nil {
		t.Fatal(err)
	}
	current, err := a.reserveResearchRequest(user, func() time.Time { return boundary })
	if err != nil {
		t.Fatal(err)
	}
	v := readResearchHistory(t, h, c, "period")
	if *v.RemainingCount != 10 {
		t.Fatal("reservation counted as success", v)
	}
	if err = a.settleResearchRequest(user, old, 200); err != nil {
		t.Fatal(err)
	}
	if err = a.settleResearchRequest(user, current, 200); err != nil {
		t.Fatal(err)
	}
	v = readResearchHistory(t, h, c, "period")
	if *v.RemainingCount != 9 || v.PeriodStart != boundary.Unix() || v.Points[1].At != boundary.Unix() {
		t.Fatalf("start boundary attribution: %+v", v)
	}
	// Changing or removing credentials never rewrites account-scoped cap history.
	if _, err = a.db.Exec(`INSERT INTO research_settings(user_id,api_key,revision,verified_at,last_checked_at,status) VALUES(?,'synthetic-Brave-key-1234','new-key','','','connected')`, user); err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.Exec(`UPDATE research_settings SET api_key='',revision='removed' WHERE user_id=?`, user); err != nil {
		t.Fatal(err)
	}
	if got := readResearchHistory(t, h, c, "period"); *got.RemainingCount != 9 {
		t.Fatal("credential revision erased cap history", got)
	}
	s.now = func() time.Time { return now.Add(24 * time.Hour) }
	if got := readResearchHistory(t, h, c, "week"); *got.RemainingCount != 10 || got.Points[0].At != boundary.Add(24*time.Hour).Unix() {
		t.Fatal("day did not reset", got)
	}
}

func TestResearchHistoryBucketsBoundedAndReadOnly(t *testing.T) {
	a, _, h, c, user, now := setupResearchHistory(t)
	tx, err := a.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5000; i++ {
		if _, err = tx.Exec(`INSERT INTO research_budget_events(id,user_id,started_ns,state) VALUES(?,?,?,'used')`, fmt.Sprintf("dense-%d", i), user, now.Add(-time.Duration(i)*time.Minute).UnixNano()); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	v := readResearchHistory(t, h, c, "week")
	if len(v.Points) > 1000 || *v.RemainingPercent != 0 || *v.RemainingCount != 0 {
		t.Fatalf("unbounded or unclamped: points=%d remainder=%v", len(v.Points), v.RemainingCount)
	}
	var count int
	if err = a.db.QueryRow(`SELECT COUNT(*) FROM research_budget_events WHERE user_id=?`, user).Scan(&count); err != nil || count != 5000 {
		t.Fatalf("read mutated ledger: count=%d err=%v", count, err)
	}
}

func setupResearchHistory(t *testing.T) (*accountStore, *researchService, http.Handler, *http.Cookie, string, time.Time) {
	t.Helper()
	a, s, h, c, _ := settingsHarness(t, func(http.ResponseWriter, *http.Request) { t.Error("history called Brave") })
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	user := mustSettingsUser(t, a, c).ID
	_, err := a.db.Exec(`INSERT INTO research_budget(user_id,enabled,maximum,period,timezone,revision,tracking_started_at) VALUES(?,1,10,'month','UTC','cap-revision','2026-10-25T00:00:00Z')`, user)
	if err != nil {
		t.Fatal(err)
	}
	return a, s, h, c, user, now
}
func historyEvent(t *testing.T, a *accountStore, user, id, at, state string) {
	t.Helper()
	stamp, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.Exec(`INSERT INTO research_budget_events(id,user_id,started_ns,state) VALUES(?,?,?,?)`, id, user, stamp.UnixNano(), state); err != nil {
		t.Fatal(err)
	}
}
func TestResearchHistorySuccessfulCycleAndWindows(t *testing.T) {
	a, _, h, c, user, now := setupResearchHistory(t)
	for i, at := range []string{"2026-10-31T23:59:59Z", "2026-11-02T01:00:00Z", "2026-11-08T01:00:00Z", "2026-11-13T12:00:00Z", "2026-11-14T10:00:00Z"} {
		historyEvent(t, a, user, fmt.Sprint(i), at, "used")
	}
	historyEvent(t, a, user, "pending", "2026-11-14T11:00:00Z", "reserved")
	other, _, err := a.register("history-other", "synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	historyEvent(t, a, other.ID, "other", "2026-11-14T11:00:00Z", "used")
	for _, tc := range []struct {
		window, start string
		initial       float64
	}{{"24h", "2026-11-13T12:00:00Z", 80}, {"week", "2026-11-07T12:00:00Z", 90}, {"period", "2026-11-01T00:00:00Z", 100}} {
		v := readResearchHistory(t, h, c, tc.window)
		start, _ := time.Parse(time.RFC3339, tc.start)
		if v.RemainingPercent == nil || *v.RemainingPercent != 60 || v.RemainingCount == nil || *v.RemainingCount != 6 {
			t.Fatalf("count: %+v", v)
		}
		if len(v.Points) < 3 || v.Points[0].At != start.Unix() || v.Points[0].RemainingPercent != tc.initial {
			t.Fatalf("%s baseline: %+v", tc.window, v.Points)
		}
		if v.Points[len(v.Points)-1].At != now.Unix() || v.Points[len(v.Points)-1].RemainingPercent != 60 {
			t.Fatalf("end: %+v", v.Points)
		}
		for i, p := range v.Points {
			if p.At < start.Unix() || p.At > now.Unix() || (i > 0 && (p.At < v.Points[i-1].At || p.RemainingPercent > v.Points[i-1].RemainingPercent)) {
				t.Fatalf("unordered: %+v", v.Points)
			}
		}
		if v.ResetAt != time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC).Unix() || v.PeriodStart != time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).Unix() || v.TrackingStartedAt != time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC).Unix() || !strings.Contains(v.Scope, "Agent") {
			t.Fatalf("metadata: %+v", v)
		}
	}
	_, b := budgetRead(t, h, c)
	v := readResearchHistory(t, h, c, "period")
	if int64(b.Maximum-b.Used) != *v.RemainingCount || b.Reserved != 1 {
		t.Fatalf("cap disagreement: %+v %+v", b, v)
	}
}
