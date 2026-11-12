package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Only these allowlisted values leave the provider boundary. No raw headers,
// credentials, plan guesses, or locally decremented counters are retained.
type researchUsage struct {
	ObservedAt string                `json:"observed_at"`
	Windows    []researchUsageWindow `json:"windows"`
}
type researchUsageWindow struct {
	Limit         int64  `json:"limit"`
	Remaining     int64  `json:"remaining"`
	ResetAt       string `json:"reset_at"`
	WindowSeconds int64  `json:"window_seconds"`
}

// Brave's rate-limiting guide defines parallel header lists, reset as seconds
// from receipt, and policy N;w=seconds. Zero limit means unlimited, not missing.
// Refuse ambiguous/incomplete lists rather than mislabeling an RPS window as a
// monthly allowance. Bounds also keep date arithmetic and JSON integers safe.
func parseResearchUsage(h http.Header, observed time.Time) *researchUsage {
	names := []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset", "X-RateLimit-Policy"}
	lists := make([][]string, 4)
	for i, name := range names {
		values := h.Values(name)
		if len(values) != 1 || len(values[0]) > 4096 {
			return nil
		}
		lists[i] = strings.Split(values[0], ",")
		if len(lists[i]) == 0 || len(lists[i]) > 16 || len(lists[i]) != len(lists[0]) {
			return nil
		}
	}
	number := func(raw string, max int64) (int64, bool) {
		raw = strings.TrimSpace(raw)
		if raw == "" || len(raw) > 16 {
			return 0, false
		}
		for _, c := range raw {
			if c < '0' || c > '9' {
				return 0, false
			}
		}
		n, e := strconv.ParseInt(raw, 10, 64)
		return n, e == nil && n <= max
	}
	const maxPeriod int64 = 10 * 366 * 24 * 60 * 60
	out := &researchUsage{ObservedAt: observed.UTC().Format(time.RFC3339Nano), Windows: make([]researchUsageWindow, 0, len(lists[0]))}
	for i := range lists[0] {
		limit, ok := number(lists[0][i], 1_000_000_000_000)
		if !ok {
			return nil
		}
		remaining, ok := number(lists[1][i], 1_000_000_000_000)
		if !ok || (limit != 0 && remaining > limit) {
			return nil
		}
		reset, ok := number(lists[2][i], maxPeriod)
		if !ok {
			return nil
		}
		policy := strings.Split(lists[3][i], ";")
		if len(policy) != 2 {
			return nil
		}
		policyLimit, ok := number(policy[0], 1_000_000_000_000)
		if !ok || policyLimit != limit {
			return nil
		}
		window := strings.TrimSpace(policy[1])
		if !strings.HasPrefix(window, "w=") {
			return nil
		}
		seconds, ok := number(strings.TrimPrefix(window, "w="), maxPeriod)
		if !ok || seconds == 0 {
			return nil
		}
		out.Windows = append(out.Windows, researchUsageWindow{Limit: limit, Remaining: remaining, ResetAt: observed.UTC().Add(time.Duration(reset) * time.Second).Format(time.RFC3339Nano), WindowSeconds: seconds})
	}
	return out
}

func (a *accountStore) saveResearchUsage(userID, revision string, usage *researchUsage) error {
	if usage == nil {
		return nil
	}
	observed, err := time.Parse(time.RFC3339Nano, usage.ObservedAt)
	if err != nil {
		return err
	}
	data, err := json.Marshal(usage)
	if err != nil {
		return err
	}
	// Revision guard and monotonic comparison occur in the same SQL statement:
	// old-key responses and out-of-order persistence cannot overwrite new data.
	_, err = a.db.Exec(`INSERT INTO research_usage(user_id,revision,observed_ns,snapshot)
 SELECT user_id,revision,?,? FROM research_settings WHERE user_id=? AND revision=? AND api_key<>''
 ON CONFLICT(user_id) DO UPDATE SET revision=excluded.revision,observed_ns=excluded.observed_ns,snapshot=excluded.snapshot
 WHERE research_usage.revision<>excluded.revision OR research_usage.observed_ns<excluded.observed_ns`, observed.UnixNano(), string(data), userID, revision)
	return err
}

func (s *researchSettingsService) webWithUsage(userID string, credential researchCredential) *researchWeb {
	w := s.web(credential.apiKey)
	// Copy factory-owned adapters so callbacks never cross owner/revision scopes.
	copy := *w
	copy.observeUsage = func(usage *researchUsage) { _ = s.accounts.saveResearchUsage(userID, credential.Revision, usage) }
	return &copy
}
