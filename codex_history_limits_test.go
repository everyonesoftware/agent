package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestCodexHistoryRestoredObservationsRemainStaleOnQuotaFailure(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	now := time.Unix(1900000000, 0)
	s.now = func() time.Time { return now }
	codexHistoryConnect(t, f, rpc)
	s.close()
	now = now.Add(10 * time.Minute)
	restored := newUsageService(f.accounts, s.base)
	defer restored.close()
	restored.now = s.now
	restored.executable = s.executable
	restored.factory = func(context.Context, string, string) (usageRPC, error) {
		return &fakeUsageRPC{signedIn: true, email: "fixture@example.com", fail: "account/rateLimits/read"}, nil
	}
	mux := http.NewServeMux()
	restored.register(mux)
	f.server = mux
	h := codexHistoryBody(t, f, "")
	if len(h.Points) != 1 || h.RemainingPercent == nil || !h.Stale || h.Warning == "" || h.ObservedAt != 1900000000 {
		t.Fatalf("lost real stale history: %+v", h)
	}
}

func TestCodexHistoryStorageFailureDoesNotReturnSyntheticSuccess(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	codexHistoryConnect(t, f, rpc)
	s.historyErr = errors.New("private storage diagnostic")
	out := f.request("GET", "/api/usage/history?profile_id="+f.id+"&range=week", "", "agent.example.test")
	if out.Code != 503 || out.Body.String() == "private storage diagnostic" {
		t.Fatalf("unsafe migration failure: %d %s", out.Code, out.Body.String())
	}
}

func TestCodexHistoryRetentionAndBoundedRealPoints(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	now := time.Unix(1900000000, 0)
	s.now = func() time.Time { return now }
	codexHistoryConnect(t, f, rpc)
	entry, _ := s.session(f.user, f.id)
	tx, err := f.accounts.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO usage_observations(user_id,profile_id,connection_id,identity_key,window_id,label,minutes,at,remaining,reset_at) VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5000; i++ {
		if _, err = stmt.Exec(f.user.ID, f.id, entry.record.ConnectionID, "fixture@example.com", "codex:primary", "fixture", 300, now.Unix()-int64(i)*60, 68, 2000000000); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = stmt.Exec(f.user.ID, f.id, entry.record.ConnectionID, "fixture@example.com", "codex:primary", "fixture", 300, now.Add(-91*24*time.Hour).Unix(), 68, 2000000000); err != nil {
		t.Fatal(err)
	}
	stmt.Close()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	h := codexHistoryBody(t, f, "")
	if len(h.Points) > 4096 || len(h.Points) < 1000 {
		t.Fatalf("unbounded history: %d", len(h.Points))
	}
	for _, p := range h.Points {
		if p.RemainingPercent != 68 || p.At%60 != now.Unix()%60 {
			t.Fatalf("synthetic point: %+v", p)
		}
	}
	now = now.Add(61 * time.Second)
	usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	var old int
	if err = f.accounts.db.QueryRow(`SELECT COUNT(*) FROM usage_observations WHERE at<?`, now.Add(-90*24*time.Hour).Unix()).Scan(&old); err != nil {
		t.Fatal(err)
	}
	if old != 0 {
		t.Fatalf("retention left %d old observations", old)
	}
}

func TestCodexHistoryIncludesObservedRangeBaseline(t *testing.T) {
	for _, rangeID := range []string{"24h", "week", "period"} {
		t.Run(rangeID, func(t *testing.T) {
			f, s, rpc := newUsageFixture(t)
			now := time.Unix(2000000000-60, 0)
			s.now = func() time.Time { return now }
			codexHistoryConnect(t, f, rpc)
			entry, err := s.session(f.user, f.id)
			if err != nil {
				t.Fatal(err)
			}
			initial, err := s.history(entry, rangeID, "codex:primary")
			if err != nil {
				t.Fatal(err)
			}
			start, err := historyRangeStart(rangeID, now, initial.PeriodStart)
			if err != nil {
				t.Fatal(err)
			}
			insert := func(at int64, profile, connection, identity, window string) {
				t.Helper()
				_, err := f.accounts.db.Exec(`INSERT INTO usage_observations(user_id,profile_id,connection_id,identity_key,window_id,label,minutes,at,remaining,reset_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, f.user.ID, profile, connection, identity, window, "fixture", 300, at, 73, 2000000000)
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, at := range []int64{start - 181, start - 180, start - 60, start + 30} {
				insert(at, f.id, entry.record.ConnectionID, entry.identityKey, "codex:primary")
			}
			// A nearby timestamp in another scope or window is not a baseline.
			insert(start-1, "other-profile", entry.record.ConnectionID, entry.identityKey, "codex:primary")
			insert(start-2, f.id, "other-connection", entry.identityKey, "codex:primary")
			insert(start-3, f.id, entry.record.ConnectionID, "other@example.com", "codex:primary")
			insert(start-4, f.id, entry.record.ConnectionID, entry.identityKey, "other-window")
			h, err := s.history(entry, rangeID, "codex:primary")
			if err != nil {
				t.Fatal(err)
			}
			want := []int64{start - 180, start - 60, start + 30, now.Unix()}
			if len(h.Points) != len(want) {
				t.Fatalf("missing bounded observed baseline: got %+v, want timestamps %v", h.Points, want)
			}
			for i, at := range want {
				p := h.Points[i]
				if p.At != at || (i < len(want)-1 && (p.RemainingPercent != 73 || p.ResetAt != 2000000000)) {
					t.Fatalf("observation %d was changed or leaked: %+v, want timestamp %d", i, p, at)
				}
			}
			if h.GapSeconds != 180 || h.ObservedAt != initial.ObservedAt || *h.RemainingPercent != *initial.RemainingPercent {
				t.Fatalf("baseline changed latest observation metadata: %+v", h)
			}
		})
	}
}

func TestCodexHistoryRangeBoundaries(t *testing.T) {
	now := time.Unix(1900000000, 0)
	for _, tc := range []struct {
		id           string
		period, want int64
	}{{"24h", 0, now.Add(-24 * time.Hour).Unix()}, {"week", 0, now.Add(-7 * 24 * time.Hour).Unix()}, {"period", now.Add(-3 * time.Hour).Unix(), now.Add(-3 * time.Hour).Unix()}, {"period", 0, now.Add(-90 * 24 * time.Hour).Unix()}, {"period", 1, now.Add(-90 * 24 * time.Hour).Unix()}} {
		got, err := historyRangeStart(tc.id, now, tc.period)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: got %d %v", tc, got, err)
		}
	}
}
