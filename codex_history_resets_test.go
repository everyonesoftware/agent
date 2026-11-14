package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type codexHistoryWindowRPC struct {
	account *fakeUsageRPC
	windows usageRateLimits
	reads   int
}

func (f *codexHistoryWindowRPC) Call(ctx context.Context, method string, params, result any) error {
	if method != "account/rateLimits/read" {
		return f.account.Call(ctx, method, params, result)
	}
	f.reads++
	b, _ := json.Marshal(f.windows)
	return json.Unmarshal(b, result)
}
func (f *codexHistoryWindowRPC) Close() error { return f.account.Close() }
func historyRateWindow(used float64, minutes, reset int64) *usageRateWindow {
	return &usageRateWindow{UsedPercent: &used, Minutes: &minutes, ResetAt: &reset}
}

func TestCodexHistoryResetRefreshAndWindowSelection(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	now := time.Unix(1900000000, 0)
	s.now = func() time.Time { return now }
	custom := &codexHistoryWindowRPC{account: rpc, windows: usageRateLimits{RateLimits: &usageRateBucket{LimitID: "codex", Primary: historyRateWindow(30, 300, now.Unix()+30), Secondary: historyRateWindow(50, 10080, now.Unix()+600)}}}
	s.factory = func(context.Context, string, string) (usageRPC, error) { return custom, nil }
	codexHistoryConnect(t, f, rpc)
	h := codexHistoryBody(t, f, "")
	if h.WindowID != "codex:secondary" || *h.RemainingPercent != 50 || len(h.Windows) != 2 {
		t.Fatalf("longest window not selected: %+v", h)
	}
	now = now.Add(31 * time.Second)
	custom.windows.RateLimits.Primary = historyRateWindow(0, 300, now.Unix()+18000)
	h = codexHistoryBody(t, f, "codex:primary")
	if h.RemainingPercent == nil || *h.RemainingPercent != 100 || len(h.Points) != 2 || h.Points[0].ResetAt == h.Points[1].ResetAt {
		t.Fatalf("expired reset was cached or interpolated: %+v", h)
	}
}

func TestCodexHistoryChangedProfileAndFailedIdentityReadIsUnknown(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	s.now = func() time.Time { return time.Unix(1900000000, 0) }
	codexHistoryConnect(t, f, rpc)
	p, _ := f.store.LoadProfile(f.id)
	p.Email = "other@example.com"
	if err := f.store.SaveProfile(p); err != nil {
		t.Fatal(err)
	}
	rpc.mu.Lock()
	rpc.fail = "account/read"
	rpc.mu.Unlock()
	h := codexHistoryBody(t, f, "")
	if len(h.Points) != 0 || h.RemainingPercent != nil {
		t.Fatalf("failed identity check leaked previous account: %+v", h)
	}
}
