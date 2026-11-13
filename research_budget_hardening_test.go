package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestResearchBudgetCancellationBeforeDispatch(t *testing.T) {
	a, s, h, c, _ := settingsHarness(t, budgetSuccess)
	saved, b := budgetResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	user := mustSettingsUser(t, a, c)
	var calls atomic.Int32
	s.settings.webFactory = func(key string) *researchWeb {
		return &researchWeb{apiKey: key, searchTransport: budgetTransportFunc(func(r *http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, errors.New("unexpected dispatch")
		})}
	}
	credential, err := a.researchCredentialForUser(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	web := s.settings.webWithUsage(user.ID, credential)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = web.search(ctx, "already cancelled"); err == nil {
		t.Fatal("cancelled request succeeded")
	}
	// Cancellation while waiting for reservation must release it without dispatch.
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	reserve := web.reserveRequest
	web.reserveRequest = func() (func(int) error, error) { settle, err := reserve(); cancel(); return settle, err }
	if _, err = web.search(ctx, "cancel during reservation"); err == nil {
		t.Fatal("cancelled request succeeded")
	}
	_, b = budgetRead(t, h, c)
	if b.Used != 1 || b.Reserved != 0 || calls.Load() != 0 {
		t.Fatal("cancelled request consumed capacity", b, calls.Load())
	}
	if saved.Status != "connected" {
		t.Fatal("fixture not configured")
	}
}
func TestResearchBudgetSettlementFailureRetainsReservation(t *testing.T) {
	a, s, h, c, _ := settingsHarness(t, budgetSuccess)
	saved, b := budgetResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	b.Enabled = true
	b.Maximum = 2
	budgetSave(t, h, c, saved.Revision, b)
	user := mustSettingsUser(t, a, c)
	if _, err := a.db.Exec(`CREATE TRIGGER fail_budget_settlement BEFORE UPDATE ON research_budget_events BEGIN SELECT RAISE(ABORT,'private persistence detail'); END`); err != nil {
		t.Fatal(err)
	}
	web, _, err := s.webForUserLocked(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = web.search(context.Background(), "settlement cannot be recorded"); !errors.Is(err, errResearchBudgetUnavailable) {
		t.Fatal("settlement failure not safe", err)
	}
	_, b = budgetRead(t, h, c)
	if b.Used != 1 || b.Reserved != 1 {
		t.Fatal("settlement failure freed capacity", b)
	}
	if _, err = web.search(context.Background(), "blocked after settlement failure"); !errors.Is(err, errResearchBudgetExhausted) {
		t.Fatal("settlement failure allowed overshoot", err)
	}
}
func TestResearchBudgetAtomicAcrossDatabaseHandles(t *testing.T) {
	a, s, h, c, path := settingsHarness(t, budgetSuccess)
	saved, b := budgetResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	b.Enabled = true
	b.Maximum = 5
	budgetSave(t, h, c, saved.Revision, b)
	other, err := newAccountStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.db.Close()
	owner := mustSettingsUser(t, a, c)
	var wg sync.WaitGroup
	ids := make(chan string, 24)
	failures := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store := a
			if i%2 == 0 {
				store = other
			}
			id, err := store.reserveResearchRequest(owner.ID, s.now)
			if err != nil {
				failures <- err
			} else {
				ids <- id
			}
		}(i)
	}
	wg.Wait()
	close(ids)
	close(failures)
	count := 0
	for range ids {
		count++
	}
	for err := range failures {
		if !errors.Is(err, errResearchBudgetExhausted) {
			t.Error("unexpected writer failure", err)
		}
	}
	_, b = budgetRead(t, h, c)
	if count != 4 || b.Reserved != 4 || b.Used != 1 {
		t.Fatalf("cross-handle overshoot/underfill success=%d budget=%+v", count, b)
	}
}
func TestResearchBudgetSessionExpiryAfterWriterWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		a, err := newAccountStore(filepath.Join(dir, "everyoneagent.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		defer a.db.Close()
		user, token, err := a.register("budget-writer-user", "synthetic-password")
		if err != nil {
			t.Fatal(err)
		}
		srv := newAccountResearchService(a)
		b, err := a.researchBudgetForUser(user.ID, srv.now())
		if err != nil {
			t.Fatal(err)
		}
		enabled, maximum, period, zone, credential := true, 12, "day", "UTC", ""
		req := researchBudgetRequest{ExpectedRevision: &credential, ExpectedBudgetRevision: &b.Revision, Enabled: &enabled, Maximum: &maximum, Period: &period, Timezone: &zone}
		expiry := time.Now().Add(time.Minute).Unix()
		if _, err = a.db.Exec(`UPDATE sessions SET expires_at=? WHERE token_hash=?`, expiry, hashSession(token)); err != nil {
			t.Fatal(err)
		}
		gated, gate := passwordGatedStore(t, dir, "UPDATE sessions SET expires_at")
		srv.settings.accounts = gated
		done := make(chan error, 1)
		go func() { done <- srv.settings.saveBudget(user.ID, token, req) }()
		var once sync.Once
		unblock := func() { once.Do(func() { close(gate.release) }) }
		defer unblock()
		select {
		case <-gate.reached:
		case <-time.After(10 * time.Second):
			t.Fatal("budget did not acquire writer lock")
		}
		time.Sleep(time.Until(time.Unix(expiry, 0)))
		unblock()
		if err = <-done; !errors.Is(err, errResearchSettingsSignedOut) {
			t.Fatal("budget save accepted expired session", err)
		}
		after, err := a.researchBudgetForUser(user.ID, srv.now())
		if err != nil {
			t.Fatal(err)
		}
		if after.Enabled || after.Revision != b.Revision || after.Maximum != 1000 {
			t.Fatal("expired budget mutation committed", after)
		}
	})
}
func TestResearchBudgetAllSuccessfulHTTPStatusesCount(t *testing.T) {
	for _, status := range []int{200, 201, 204, 206, 299} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			_, _, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(status)
			})
			rr := researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, ""))
			_, b := budgetResult(t, rr, 503)
			if b.Used != 1 || b.Reserved != 0 {
				t.Fatal("successful HTTP not counted before parse", b)
			}
		})
	}
}
func TestResearchBudgetUnauthenticatedNoStore(t *testing.T) {
	_, _, h, _, _ := settingsHarness(t, budgetSuccess)
	rr := researchRequest(h, nil, "POST", "/api/research/settings/budget", `{}`)
	if rr.Code != 401 || rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("budget auth/no-store", rr.Code, rr.Header())
	}
	req := httptest.NewRequest("GET", "/api/research/settings?extra=1", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 401 || rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("settings auth/no-store")
	}
}

func TestResearchBudgetExhaustionPreservesCredentialStatus(t *testing.T) {
	var calls atomic.Int32
	a, _, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		budgetSuccess(w, r)
	})
	saved, b := budgetResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	b.Enabled = true
	b.Maximum = 1
	budgetSave(t, h, c, saved.Revision, b)
	user := mustSettingsUser(t, a, c)
	for _, status := range []string{"invalid", "unavailable", "connected"} {
		if _, err := a.db.Exec(`UPDATE research_settings SET status=? WHERE user_id=?`, status, user.ID); err != nil {
			t.Fatal(err)
		}
		after, budget := budgetResult(t, researchRequest(h, c, "POST", "/api/research/settings/check", settingsRevision(saved.Revision)), 429)
		if after.Status != status || after.Revision != saved.Revision || budget.Used != 1 || calls.Load() != 1 {
			t.Fatal("budget rejection changed credential state or dispatched", after, budget, calls.Load())
		}
	}
}
