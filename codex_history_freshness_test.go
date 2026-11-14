package main

import (
	"testing"
	"time"
)

func TestCodexHistoryStatusAutomaticallyRefreshesStaleQuota(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	now := time.Unix(1900000000, 0)
	s.now = func() time.Time { return now }
	codexHistoryConnect(t, f, rpc)
	for i := 0; i < 3; i++ {
		usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	}
	rpc.mu.Lock()
	before := len(rpc.calls)
	rpc.mu.Unlock()
	if before != 3 {
		t.Fatalf("fresh status made RPC calls: %d", before)
	}
	now = now.Add(61 * time.Second)
	v := usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	if v.Snapshot == nil || v.Snapshot.FetchedAt != now.Unix() || v.Snapshot.Stale {
		t.Fatalf("stale quota was not refreshed: %+v", v.Snapshot)
	}
	if h := codexHistoryBody(t, f, ""); len(h.Points) != 2 {
		t.Fatalf("automatic refresh not collected: %+v", h)
	}
}

func TestCodexHistoryProfileIdentityChangeInvalidatesCachedHistory(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	now := time.Unix(1900000000, 0)
	s.now = func() time.Time { return now }
	codexHistoryConnect(t, f, rpc)
	p, _ := f.store.LoadProfile(f.id)
	p.Email = "different@example.com"
	if err := f.store.SaveProfile(p); err != nil {
		t.Fatal(err)
	}
	h := codexHistoryBody(t, f, "")
	if h.RemainingPercent != nil || len(h.Points) != 0 {
		t.Fatalf("cached other identity leaked: %+v", h)
	}
}
