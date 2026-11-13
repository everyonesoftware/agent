package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type budgetWire struct {
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

func budgetResult(t *testing.T, r *httptest.ResponseRecorder, status int) (researchSettings, budgetWire) {
	t.Helper()
	settings := settingsResult(t, r, status)
	var data struct {
		Settings struct {
			Budget *budgetWire `json:"budget"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data.Settings.Budget == nil {
		t.Fatalf("missing always-present budget: %s", r.Body.String())
	}
	return settings, *data.Settings.Budget
}
func budgetBody(rev string, b budgetWire) string {
	data, _ := json.Marshal(map[string]any{"expected_revision": rev, "expected_budget_revision": b.Revision, "enabled": b.Enabled, "maximum": b.Maximum, "period": b.Period, "timezone": b.Timezone})
	return string(data)
}
func budgetRead(t *testing.T, h http.Handler, c *http.Cookie) (researchSettings, budgetWire) {
	t.Helper()
	return budgetResult(t, researchRequest(h, c, "GET", "/api/research/settings", ""), 200)
}
func budgetSave(t *testing.T, h http.Handler, c *http.Cookie, rev string, b budgetWire) (researchSettings, budgetWire) {
	t.Helper()
	return budgetResult(t, researchRequest(h, c, "POST", "/api/research/settings/budget", budgetBody(rev, b)), 200)
}
func budgetSuccess(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"web":{"results":[]}}`)
}
func TestResearchBudgetLifecycle(t *testing.T) {
	var calls atomic.Int32
	a, s, h, c, path := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); budgetSuccess(w, r) })
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	initial, b := budgetRead(t, h, c)
	if initial.Configured || b.Enabled || b.Maximum != 1000 || b.Period != "month" || b.Timezone != "UTC" || b.Revision == "" || b.Used != 0 || b.Reserved != 0 || b.TrackingStartedAt != now.Format(time.RFC3339) || b.StartAt != "2026-11-01T00:00:00Z" || b.ResetAt != "2026-12-01T00:00:00Z" {
		t.Fatalf("defaults: %+v", b)
	}
	saved, b := budgetResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	if b.Used != 1 || b.Reserved != 0 {
		t.Fatalf("verify not counted: %+v", b)
	}
	b.Enabled = true
	b.Maximum = 2
	unchanged, b := budgetSave(t, h, c, saved.Revision, b)
	if unchanged.Revision != saved.Revision {
		t.Fatal("cap changed credential revision")
	}
	user := mustSettingsUser(t, a, c)
	web, _, err := s.webForUserLocked(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = web.search(context.Background(), "ordinary"); err != nil {
		t.Fatal(err)
	}
	rr := researchRequest(h, c, "POST", "/api/research/settings/check", settingsRevision(saved.Revision))
	checked, b := budgetResult(t, rr, 429)
	if !strings.Contains(rr.Body.String(), `"code":"budget_exhausted"`) || checked.Status != "connected" || b.Used != 2 || calls.Load() != 2 {
		t.Fatalf("cap classification/count: %s calls=%d", rr.Body.String(), calls.Load())
	}
	removed, b := budgetResult(t, researchRequest(h, c, "POST", "/api/research/settings/remove", settingsRevision(saved.Revision)), 200)
	if removed.Configured || b.Used != 2 {
		t.Fatal("removal reset account usage", b)
	}
	rr = researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody("synthetic-replacement-9999", removed.Revision))
	_, b = budgetResult(t, rr, 429)
	if calls.Load() != 2 {
		t.Fatal("cap bypassed by candidate verify")
	}
	b.Enabled = false
	_, b = budgetSave(t, h, c, removed.Revision, b)
	_, b = budgetResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody("synthetic-replacement-9999", removed.Revision)), 200)
	if b.Used != 3 {
		t.Fatal("disabled cap stopped tracking", b)
	}
	reopened, err := newAccountStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.db.Close()
	srv := newAccountResearchService(reopened)
	srv.now = s.now
	mux := http.NewServeMux()
	srv.register(mux, reopened)
	_, b = budgetRead(t, mux, c)
	if b.Used != 3 || b.Enabled {
		t.Fatal("restart lost usage/config", b)
	}
	_, token, err := a.register("budget-other", "synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	_, other := budgetRead(t, h, &http.Cookie{Name: authCookieName, Value: token})
	if other.Used != 0 || other.Revision == b.Revision {
		t.Fatal("owner leak", other)
	}
	if calls.Load() != 3 {
		t.Fatal("settings read contacted provider")
	}
}
func TestResearchBudgetCalendarAndPeriodHistory(t *testing.T) {
	a, s, h, c, _ := settingsHarness(t, budgetSuccess)
	now := time.Date(2026, 3, 7, 20, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	saved, b := budgetResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	b.Period = "day"
	b.Timezone = "America/Los_Angeles"
	_, b = budgetSave(t, h, c, saved.Revision, b)
	if b.Used != 1 || b.StartAt != "2026-03-07T08:00:00Z" || b.ResetAt != "2026-03-08T08:00:00Z" {
		t.Fatal("day boundary", b)
	}
	now = time.Date(2026, 3, 8, 8, 0, 0, 0, time.UTC)
	_, b = budgetRead(t, h, c)
	if b.Used != 0 || b.StartAt != "2026-03-08T08:00:00Z" || b.ResetAt != "2026-03-09T07:00:00Z" {
		t.Fatal("DST spring day", b)
	}
	user := mustSettingsUser(t, a, c)
	web, _, err := s.webForUserLocked(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = web.search(context.Background(), "next day"); err != nil {
		t.Fatal(err)
	}
	b.Period = "month"
	_, b = budgetSave(t, h, c, saved.Revision, b)
	if b.Used != 2 || b.StartAt != "2026-03-01T08:00:00Z" || b.ResetAt != "2026-04-01T07:00:00Z" {
		t.Fatal("period change erased timestamp history", b)
	}
	now = time.Date(2026, 4, 1, 7, 0, 0, 0, time.UTC)
	_, b = budgetRead(t, h, c)
	if b.Used != 0 {
		t.Fatal("month did not reset", b)
	}
	now = time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	b.Period = "day"
	_, b = budgetSave(t, h, c, saved.Revision, b)
	if b.StartAt != "2026-11-01T07:00:00Z" || b.ResetAt != "2026-11-02T08:00:00Z" {
		t.Fatal("DST fall day", b)
	}
}
func TestResearchBudgetConcurrentReservations(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{}, 20)
	release := make(chan struct{})
	a, s, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n > 1 {
			entered <- struct{}{}
			<-release
		}
		budgetSuccess(w, r)
	})
	saved, b := budgetResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	b.Enabled = true
	b.Maximum = 4
	_, b = budgetSave(t, h, c, saved.Revision, b)
	user := mustSettingsUser(t, a, c)
	web, _, err := s.webForUserLocked(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var successes atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := web.search(context.Background(), "concurrent"); err == nil {
				successes.Add(1)
			}
		}()
	}
	for i := 0; i < 3; i++ {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			close(release)
			t.Fatal("requests did not reserve")
		}
	}
	_, b = budgetRead(t, h, c)
	if b.Used != 1 || b.Reserved != 3 || calls.Load() != 4 {
		close(release)
		wg.Wait()
		t.Fatalf("overshoot/pending %+v calls=%d", b, calls.Load())
	}
	close(release)
	wg.Wait()
	_, b = budgetRead(t, h, c)
	if successes.Load() != 3 || b.Used != 4 || b.Reserved != 0 || calls.Load() != 4 {
		t.Fatalf("settle %+v success=%d calls=%d", b, successes.Load(), calls.Load())
	}
}

type budgetTransportFunc func(*http.Request) (*http.Response, error)

func (f budgetTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestResearchBudgetOutcomesAndFailClosed(t *testing.T) {
	var status atomic.Int32
	status.Store(200)
	a, s, h, c, path := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(int(status.Load()))
		fmt.Fprint(w, "not valid JSON")
	})
	// A successful HTTP request counts even if its body cannot be used or its candidate is never saved.
	_, b := budgetResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 503)
	if b.Used != 1 || b.Reserved != 0 {
		t.Fatal("successful failed-candidate verify not counted", b)
	}
	for _, code := range []int{401, 403, 429, 500, 302} {
		status.Store(int32(code))
		result := researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, ""))
		_, b = budgetResult(t, result, result.Code)
		if b.Used != 1 || b.Reserved != 0 {
			t.Fatal("failed HTTP status counted", code, b)
		}
		s.settings.mu.Lock()
		s.settings.budgets = map[string]researchVerificationBudget{}
		s.settings.mu.Unlock()
	}
	var dispatched atomic.Int32
	s.settings.webFactory = func(key string) *researchWeb {
		return &researchWeb{apiKey: key, searchTransport: budgetTransportFunc(func(*http.Request) (*http.Response, error) {
			dispatched.Add(1)
			return nil, errors.New("ambiguous private failure")
		})}
	}
	result := researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, ""))
	_, b = budgetResult(t, result, 503)
	if b.Used != 1 || b.Reserved != 1 {
		t.Fatal("unknown reservation released", b)
	}
	reopened, err := newAccountStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.db.Close()
	srv := newAccountResearchService(reopened)
	mux := http.NewServeMux()
	srv.register(mux, reopened)
	_, b = budgetRead(t, mux, c)
	if b.Reserved != 1 {
		t.Fatal("restart lost unknown", b)
	}
	b.Enabled = true
	b.Maximum = 2
	_, b = budgetSave(t, h, c, "", b)
	result = researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, ""))
	budgetResult(t, result, 429)
	if dispatched.Load() != 1 {
		t.Fatal("unknown did not enforce cap")
	}
	user := mustSettingsUser(t, a, c)
	cred := emptyResearchCredential()
	cred.apiKey = settingsTestKey
	web := s.settings.webWithUsage(user.ID, cred)
	if _, err = a.db.Exec(`DROP TABLE research_budget_events`); err != nil {
		t.Fatal(err)
	}
	if _, err = web.search(context.Background(), "must fail closed"); err == nil || dispatched.Load() != 1 {
		t.Fatal("storage failure dispatched request", err)
	}
}
func TestResearchBudgetMutationGuards(t *testing.T) {
	a, _, h, c, _ := settingsHarness(t, budgetSuccess)
	saved, b := budgetRead(t, h, c)
	body := budgetBody(saved.Revision, b)
	_, updated := budgetSave(t, h, c, saved.Revision, b)
	if updated.Revision == b.Revision {
		t.Fatal("budget revision not changed")
	}
	budgetResult(t, researchRequest(h, c, "POST", "/api/research/settings/budget", body), 409)
	badCredential := budgetBody("stale", updated)
	budgetResult(t, researchRequest(h, c, "POST", "/api/research/settings/budget", badCredential), 409)
	for _, tc := range []struct {
		field string
		value any
	}{{"maximum", 0}, {"maximum", -1}, {"maximum", 1.5}, {"maximum", 10_000_001}, {"period", "week"}, {"timezone", "Local"}, {"timezone", "bogus/place"}, {"enabled", "yes"}, {"extra", true}} {
		var v map[string]any
		json.Unmarshal([]byte(budgetBody(saved.Revision, updated)), &v)
		v[tc.field] = tc.value
		data, _ := json.Marshal(v)
		rr := researchRequest(h, c, "POST", "/api/research/settings/budget", string(data))
		if rr.Code != 400 {
			t.Fatalf("accepted bad %s: %d %s", tc.field, rr.Code, rr.Body.String())
		}
	}
	for _, field := range []string{"expected_revision", "expected_budget_revision", "enabled", "maximum", "period", "timezone"} {
		var v map[string]any
		json.Unmarshal([]byte(budgetBody(saved.Revision, updated)), &v)
		delete(v, field)
		data, _ := json.Marshal(v)
		rr := researchRequest(h, c, "POST", "/api/research/settings/budget", string(data))
		if rr.Code != 400 {
			t.Fatalf("accepted missing %s: %d", field, rr.Code)
		}
	}
	req := httptest.NewRequest("POST", "/api/research/settings/budget", strings.NewReader(budgetBody(saved.Revision, updated)))
	req.AddCookie(c)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatal("cross-site allowed", rr.Code)
	}
	user := mustSettingsUser(t, a, c)
	if _, err := a.db.Exec(`UPDATE sessions SET expires_at=0 WHERE user_id=?`, user.ID); err != nil {
		t.Fatal(err)
	}
	rr = researchRequest(h, c, "POST", "/api/research/settings/budget", budgetBody(saved.Revision, updated))
	if rr.Code != 401 {
		t.Fatal("expired session allowed", rr.Code)
	}
}
