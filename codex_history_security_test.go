package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCodexHistorySamplerRejectsSymlinkedProfileFile(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	now := time.Unix(1900000000, 0)
	s.now = func() time.Time { return now }
	codexHistoryConnect(t, f, rpc)
	s.close()
	dir := filepath.Join(f.accounts.dataDir, "account-data", f.user.ID, "oauth")
	path := filepath.Join(dir, "siwc-profiles.json")
	moved := filepath.Join(t.TempDir(), "profiles.json")
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, path); err != nil {
		t.Fatal(err)
	}
	restored := newUsageService(f.accounts, s.base)
	defer restored.close()
	restored.now = func() time.Time { return now.Add(2 * time.Minute) }
	restored.executable = s.executable
	starts := 0
	restored.factory = func(context.Context, string, string) (usageRPC, error) {
		starts++
		return &fakeUsageRPC{signedIn: true, email: "fixture@example.com"}, nil
	}
	restored.sampleScopes(context.Background())
	if starts != 0 {
		t.Fatal("sampler followed symlinked profile state")
	}
}

func TestCodexHistoryAuthScopeAndReconnectIsolation(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	s.now = func() time.Time { return time.Unix(1900000000, 0) }
	connected := codexHistoryConnect(t, f, rpc)
	out := httptest.NewRecorder()
	f.server.ServeHTTP(out, httptest.NewRequest("GET", "/api/usage/history?profile_id="+f.id+"&range=week", nil))
	if out.Code != 401 || out.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unauthorized history: %d", out.Code)
	}
	req := httptest.NewRequest("GET", "/api/usage/history?profile_id="+f.id+"&range=week", nil)
	req.AddCookie(f.cookie)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	out = httptest.NewRecorder()
	f.server.ServeHTTP(out, req)
	if out.Code != 403 {
		t.Fatalf("cross-site history: %d", out.Code)
	}
	other, _ := newProfileID()
	if out := f.request("GET", "/api/usage/history?profile_id="+other+"&range=week", "", "agent.example.test"); out.Code != 409 {
		t.Fatalf("other profile history: %d", out.Code)
	}
	entry, err := s.session(f.user, f.id)
	if err != nil {
		t.Fatal(err)
	}
	entry.mu.Lock()
	id, _ := randomURLToken(32)
	if err = s.saveRecord(entry, usageRecord{ConnectionID: id}); err != nil {
		t.Fatal(err)
	}
	entry.mu.Unlock()
	h := codexHistoryBody(t, f, "")
	if h.RemainingPercent != nil || len(h.Points) != 0 {
		t.Fatalf("prior connection history leaked %s: %+v", connected.ConnectionID, h)
	}
}
