package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResearchBudgetMidnightGapBounds(t *testing.T) {
	_, s, h, c, _ := settingsHarness(t, budgetSuccess)
	now := time.Date(2026, 9, 7, 2, 30, 0, 0, time.UTC) // Santiago September 6, 23:30.
	s.now = func() time.Time { return now }
	saved, b := budgetRead(t, h, c)
	b.Period = "day"
	b.Timezone = "America/Santiago"
	_, b = budgetSave(t, h, c, saved.Revision, b)
	if b.StartAt != "2026-09-06T04:00:00Z" || b.ResetAt != "2026-09-07T03:00:00Z" {
		t.Fatal("midnight gap excluded final civil hour", b)
	}
	start, err := time.Parse(time.RFC3339Nano, b.StartAt)
	if err != nil {
		t.Fatal(err)
	}
	reset, err := time.Parse(time.RFC3339Nano, b.ResetAt)
	if err != nil {
		t.Fatal(err)
	}
	if now.Before(start) || !now.Before(reset) {
		t.Fatal("current time outside budget interval", b)
	}
}
func TestResearchBudgetMidnightGapFinalHourEnforcesCap(t *testing.T) {
	var calls int
	_, s, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) { calls++; budgetSuccess(w, r) })
	s.now = func() time.Time { return time.Date(2026, 9, 7, 2, 30, 0, 0, time.UTC) }
	saved, b := budgetRead(t, h, c)
	b.Enabled = true
	b.Maximum = 1
	b.Period = "day"
	b.Timezone = "America/Santiago"
	budgetSave(t, h, c, saved.Revision, b)
	saved, _ = budgetResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	rr := researchRequest(h, c, "POST", "/api/research/settings/check", settingsRevision(saved.Revision))
	_, b = budgetResult(t, rr, 429)
	if b.Used != 1 || calls != 1 || !strings.Contains(rr.Body.String(), `"budget_exhausted"`) {
		t.Fatal("final hour bypassed cap", b, calls, rr.Body.String())
	}
}
func TestResearchBudgetSettlementWaitsForOtherWriter(t *testing.T) {
	for _, status := range []int{200, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			a, s, _, c, path := settingsHarness(t, budgetSuccess)
			user := mustSettingsUser(t, a, c)
			id, err := a.reserveResearchRequest(user.ID, s.now)
			if err != nil {
				t.Fatal(err)
			}
			other, err := newAccountStore(path)
			if err != nil {
				t.Fatal(err)
			}
			defer other.db.Close()
			tx, err := beginResearchBudget(other.db)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err = tx.Exec(`UPDATE research_budget SET maximum=maximum WHERE user_id=?`, user.ID); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- a.settleResearchRequest(user.ID, id, status) }()
			select {
			case err := <-done:
				t.Fatalf("settlement did not wait for writer: %v", err)
			case <-time.After(40 * time.Millisecond):
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if err = <-done; err != nil {
				t.Fatal("settlement failed after writer released", err)
			}
			b, err := a.researchBudgetForUser(user.ID, s.now())
			if err != nil {
				t.Fatal(err)
			}
			used := 0
			if status == 200 {
				used = 1
			}
			if b.Used != used || b.Reserved != 0 {
				t.Fatal("wrong definitive settlement", b)
			}
		})
	}
}
func TestResearchBudgetClockSampledAfterWriterWait(t *testing.T) {
	dir := t.TempDir()
	a, err := newAccountStore(filepath.Join(dir, "everyoneagent.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	user, _, err := a.register("clock-boundary-user", "synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 12, 23, 59, 59, 0, time.UTC)
	s := newAccountResearchService(a)
	s.now = func() time.Time { return now }
	if _, err = a.researchBudgetForUser(user.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.Exec(`UPDATE research_budget SET enabled=1,maximum=1,period='day' WHERE user_id=?`, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.Exec(`INSERT INTO research_budget_events(id,user_id,started_ns,state) VALUES('previous-day',?,?,'used')`, user.ID, now.UnixNano()); err != nil {
		t.Fatal(err)
	}
	gated, gate := passwordGatedStore(t, dir, "PRAGMA busy_timeout")
	s.settings.accounts = gated
	web := &researchWeb{apiKey: settingsTestKey, searchTransport: budgetTransportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"web":{"results":[]}}`)), Request: r}, nil
	})}
	s.settings.trackRequests(user.ID, web)
	done := make(chan error, 1)
	go func() { _, err := web.search(context.Background(), "after writer wait"); done <- err }()
	select {
	case <-gate.reached:
	case <-time.After(5 * time.Second):
		close(gate.release)
		t.Fatal("reservation never reached writer gate")
	}
	now = now.Add(time.Second)
	close(gate.release)
	if err = <-done; err != nil {
		t.Fatal("reservation enforced stale pre-wait interval", err)
	}
	b, err := a.researchBudgetForUser(user.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if b.Used != 1 || b.Reserved != 0 {
		t.Fatal("request recorded at stale pre-wait timestamp", b)
	}
}
