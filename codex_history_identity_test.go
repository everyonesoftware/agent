package main

import (
	"testing"
	"time"
)

func TestCodexHistoryFailedPollIsThrottled(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	now := time.Unix(1900000000, 0)
	s.now = func() time.Time { return now }
	codexHistoryConnect(t, f, rpc)
	now = now.Add(61 * time.Second)
	rpc.mu.Lock()
	rpc.fail = "account/read"
	rpc.mu.Unlock()
	usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	rpc.mu.Lock()
	before := len(rpc.calls)
	rpc.mu.Unlock()
	for i := 0; i < 3; i++ {
		usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	}
	rpc.mu.Lock()
	after := len(rpc.calls)
	rpc.mu.Unlock()
	if after != before {
		t.Fatalf("failed poll hammered provider: before=%d after=%d", before, after)
	}
}

func TestCodexHistoryIdentitySwitchNeverReusesSnapshot(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	now := time.Unix(1900000000, 0)
	s.now = func() time.Time { return now }
	codexHistoryConnect(t, f, rpc)
	p, _ := f.store.LoadProfile(f.id)
	p.Email = "new@example.com"
	if err := f.store.SaveProfile(p); err != nil {
		t.Fatal(err)
	}
	rpc.mu.Lock()
	rpc.email = p.Email
	rpc.mu.Unlock()
	h := codexHistoryBody(t, f, "")
	if len(h.Points) != 1 || h.RemainingPercent == nil {
		t.Fatalf("new identity did not get its own observation: %+v", h)
	}
	var identities int
	if err := f.accounts.db.QueryRow(`SELECT COUNT(DISTINCT identity_key) FROM usage_observations WHERE user_id=?`, f.user.ID).Scan(&identities); err != nil {
		t.Fatal(err)
	}
	if identities != 2 {
		t.Fatalf("identity switch reused old snapshot: identities=%d", identities)
	}
}
