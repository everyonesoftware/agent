package main

import (
	"testing"
	"time"
)

func TestUsageFreshnessToleratesMinutePollingDelays(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	observed := time.Unix(1900000000, 0)
	now := observed
	s.now = func() time.Time { return now }
	codexHistoryConnect(t, f, rpc)
	entry, err := s.session(f.user, f.id)
	if err != nil {
		t.Fatal(err)
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	for _, age := range []int64{60, 61, 90, 120, 180, 181} {
		now = observed.Add(time.Duration(age) * time.Second)
		h, err := s.history(entry, "24h", "")
		if err != nil {
			t.Fatal(err)
		}
		wantStale := age > 180
		if h.Stale != wantStale {
			t.Errorf("history at age %ds: stale=%v, want %v", age, h.Stale, wantStale)
		}
		v := s.view(entry)
		if v.Snapshot == nil || v.Snapshot.Stale != wantStale {
			t.Errorf("snapshot at age %ds: %+v, want stale=%v", age, v.Snapshot, wantStale)
		}
		if h.ObservedAt != observed.Unix() || h.RemainingPercent == nil || *h.RemainingPercent != 68 {
			t.Fatal("freshness grace changed the recorded observation")
		}
	}
	// An actual provider error or disconnected session is not a polling delay.
	now = observed.Add(61 * time.Second)
	entry.view.Error = "Usage provider unavailable."
	h, err := s.history(entry, "24h", "")
	if err != nil || !h.Stale || h.Warning != entry.view.Error {
		t.Fatalf("provider error must remain stale: %+v, %v", h, err)
	}
	entry.view.Error = ""
	entry.view.State = "unavailable"
	h, err = s.history(entry, "24h", "")
	if err != nil || !h.Stale {
		t.Fatalf("disconnected allowance must remain stale: %+v, %v", h, err)
	}
	entry.view.State = "connected"
	entry.view.Snapshot.Windows[0].ResetAt = now.Unix()
	if !s.view(entry).Snapshot.Stale {
		t.Fatal("an elapsed reset must invalidate the snapshot within the grace period")
	}
}
