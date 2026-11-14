package main

import (
	"net/http"
	"testing"
	"time"
)

func TestCodexHistoryOwnerIsolationEvenWithSameProfileConnectionAndIdentity(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	s.now = func() time.Time { return time.Unix(1900000000, 0) }
	first := codexHistoryConnect(t, f, rpc)
	other, token, err := f.accounts.register("history-second-user", "second-user-password")
	if err != nil {
		t.Fatal(err)
	}
	otherStore := f.accounts.siwcForUser(s.base, other).store
	profile, err := f.store.LoadProfile(f.id)
	if err != nil {
		t.Fatal(err)
	}
	if err = otherStore.SaveProfile(profile); err != nil {
		t.Fatal(err)
	}
	if err = otherStore.SetActiveProfile(f.id); err != nil {
		t.Fatal(err)
	}
	entry, err := s.session(other, f.id)
	if err != nil {
		t.Fatal(err)
	}
	entry.mu.Lock()
	err = s.saveRecord(entry, usageRecord{ConnectionID: first.ConnectionID})
	entry.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	rpc.mu.Lock()
	rpc.fail = "account/rateLimits/read"
	rpc.mu.Unlock()
	f.user = other
	f.cookie = &http.Cookie{Name: authCookieName, Value: token}
	h := codexHistoryBody(t, f, "")
	if len(h.Points) != 0 || h.RemainingPercent != nil {
		t.Fatalf("another owner's observations leaked: %+v", h)
	}
}
