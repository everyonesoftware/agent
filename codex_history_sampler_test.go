package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodexHistoryBackgroundResumesPersistedScopeWithoutUI(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	var clock atomic.Int64
	clock.Store(1900000000)
	s.now = func() time.Time { return time.Unix(clock.Load(), 0) }
	codexHistoryConnect(t, f, rpc)
	s.close()
	clock.Add(120)
	restored := newUsageService(f.accounts, s.base)
	restored.executable = s.executable
	restored.now = s.now
	var created atomic.Int64
	restored.factory = func(context.Context, string, string) (usageRPC, error) {
		created.Add(1)
		return &fakeUsageRPC{signedIn: true, email: "fixture@example.com"}, nil
	}
	defer restored.close()
	restored.startSampler(5 * time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	count := 0
	for time.Now().Before(deadline) {
		if err := f.accounts.db.QueryRow(`SELECT COUNT(DISTINCT at) FROM usage_observations WHERE user_id=? AND profile_id=?`, f.user.ID, f.id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if count != 2 || created.Load() == 0 {
		t.Fatalf("closed Settings stopped collection: points=%d workers=%d", count, created.Load())
	}
	restored.close()
	before := created.Load()
	clock.Add(120)
	time.Sleep(25 * time.Millisecond)
	if created.Load() != before || len(restored.slots) != 0 {
		t.Fatal("sampler or worker survived close")
	}
}
