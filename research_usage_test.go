package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func usageHeaders(limit, remaining, reset, policy string) http.Header {
	h := http.Header{}
	h.Set("X-RateLimit-Limit", limit)
	h.Set("X-RateLimit-Remaining", remaining)
	h.Set("X-RateLimit-Reset", reset)
	h.Set("X-RateLimit-Policy", policy)
	return h
}
func TestResearchUsageParsing(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 123, time.UTC)
	valid := usageHeaders("1, 15000", "0, 14950", "1, 123456", "1;w=1, 15000;w=2592000")
	got := parseResearchUsage(valid, now)
	if got == nil || len(got.Windows) != 2 || got.ObservedAt != now.Format(time.RFC3339Nano) || got.Windows[1].Remaining != 14950 || got.Windows[1].WindowSeconds != 2592000 || got.Windows[1].ResetAt != now.Add(123456*time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("bad snapshot: %+v", got)
	}
	unlimited := parseResearchUsage(usageHeaders("1,0", "0,0", "1,10", "1;w=1,0;w=2592000"), now)
	if unlimited == nil || unlimited.Windows[1].Limit != 0 {
		t.Fatal("zero means unlimited")
	}
	for _, tc := range []struct{ name, limit, remaining, reset, policy string }{
		{"missing", "1", "0", "", "1;w=1"}, {"mismatch", "1,2", "0", "1,2", "1;w=1,2;w=2"},
		{"negative", "1", "-1", "1", "1;w=1"}, {"overflow", "999999999999999999999", "0", "1", "1;w=1"},
		{"decimal", "1", "0.5", "1", "1;w=1"}, {"overlimit", "1", "2", "1", "1;w=1"},
		{"policy disagreement", "1", "0", "1", "2;w=1"}, {"zero window", "1", "0", "1", "1;w=0"},
		{"huge reset", "1", "0", "999999999999", "1;w=1"}, {"unknown policy", "1", "0", "1", "1;w=1;foo=2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if parseResearchUsage(usageHeaders(tc.limit, tc.remaining, tc.reset, tc.policy), now) != nil {
				t.Fatal("invented quota")
			}
		})
	}
	repeated := valid.Clone()
	repeated.Add("X-RateLimit-Limit", "1")
	if parseResearchUsage(repeated, now) != nil {
		t.Fatal("ambiguous headers accepted")
	}
}
func TestResearchUsageLifecycle(t *testing.T) {
	var calls atomic.Int32
	var status atomic.Int32
	status.Store(200)
	var headers atomic.Bool
	headers.Store(true)
	a, s, h, c, path := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if headers.Load() {
			for k, v := range usageHeaders("1,15000", "0,14999", "1,100", "1;w=1,15000;w=2592000") {
				w.Header()[k] = v
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(int(status.Load()))
		fmt.Fprint(w, `{"web":{"results":[]}}`)
	})
	initial := settingsResult(t, researchRequest(h, c, "GET", "/api/research/settings", ""), 200)
	if initial.Usage != nil || calls.Load() != 0 {
		t.Fatal("GET contacted provider")
	}
	saved := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	if saved.Usage == nil {
		t.Fatal("verification missed usage")
	}
	user := mustSettingsUser(t, a, c)
	reopened, e := newAccountStore(path)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.db.Close()
	persisted, e := reopened.researchCredentialForUser(user.ID)
	if e != nil || persisted.Usage == nil {
		t.Fatal("snapshot not persisted", e)
	}
	_, token, e := a.register("usage-other", "synthetic-password")
	if e != nil {
		t.Fatal(e)
	}
	other := settingsResult(t, researchRequest(h, &http.Cookie{Name: authCookieName, Value: token}, "GET", "/api/research/settings", ""), 200)
	if other.Usage != nil {
		t.Fatal("owner leak")
	}
	s.mu.Lock()
	web, _, e := s.webForUserLocked(user.ID)
	s.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	payload, e := web.search(context.Background(), "ordinary search")
	if e != nil || strings.Contains(payload, "usage") || strings.Contains(payload, "14999") {
		t.Fatal("tool contract changed", e, payload)
	}
	after := settingsResult(t, researchRequest(h, c, "GET", "/api/research/settings", ""), 200)
	if after.Usage == nil || after.Usage.ObservedAt == saved.Usage.ObservedAt || calls.Load() != 2 {
		t.Fatal("ordinary search not captured or GET made call")
	}
	status.Store(429)
	checked := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/check", settingsRevision(saved.Revision)), 429)
	if checked.Usage == nil || checked.Usage.ObservedAt == after.Usage.ObservedAt {
		t.Fatal("429 missed usage")
	}
	status.Store(200)
	headers.Store(false)
	replaced := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody("synthetic-replacement-9999", saved.Revision)), 200)
	if replaced.Usage != nil {
		t.Fatal("replacement inherited old quota")
	}
	// A previously constructed old-key adapter must not resurrect usage.
	headers.Store(true)
	_, _ = web.search(context.Background(), "late old key")
	fresh := settingsResult(t, researchRequest(h, c, "GET", "/api/research/settings", ""), 200)
	if fresh.Usage != nil {
		t.Fatal("old key overwrote replacement")
	}
	removed := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/remove", settingsRevision(replaced.Revision)), 200)
	if removed.Usage != nil {
		t.Fatal("removed quota retained")
	}
	_, _ = web.search(context.Background(), "late removed key")
	fresh = settingsResult(t, researchRequest(h, c, "GET", "/api/research/settings", ""), 200)
	if fresh.Usage != nil {
		t.Fatal("removed quota resurrected")
	}
}
func TestResearchUsageMonotonicAndStaleSettingsPersist(t *testing.T) {
	a, s, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"web":{"results":[]}}`)
	})
	saved := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	user := mustSettingsUser(t, a, c)
	stale, e := a.researchCredentialForUser(user.ID)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	newer := parseResearchUsage(usageHeaders("1", "0", "1", "1;w=1"), now)
	older := parseResearchUsage(usageHeaders("1", "1", "1", "1;w=1"), now.Add(-time.Second))
	if e := a.saveResearchUsage(user.ID, saved.Revision, newer); e != nil {
		t.Fatal(e)
	}
	if e := a.saveResearchUsage(user.ID, saved.Revision, older); e != nil {
		t.Fatal(e)
	}
	s.mu.Lock()
	e = s.settings.persist(user.ID, c.Value, saved.Revision, stale)
	s.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	got, e := a.researchCredentialForUser(user.ID)
	if e != nil || got.Usage == nil || got.Usage.ObservedAt != newer.ObservedAt {
		t.Fatal("stale settings or snapshot overwrote latest", e)
	}
}
