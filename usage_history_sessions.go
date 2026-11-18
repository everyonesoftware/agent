package main

// A usage session is an estimated cluster of observed quota declines, not a chat.
type usageHistorySession struct {
	StartAt     int64   `json:"start_at"`
	EndAt       int64   `json:"end_at"`
	LastUsedAt  int64   `json:"last_used_at"`
	UsedPercent float64 `json:"used_percent"`
	Current     bool    `json:"current"`
	Estimated   bool    `json:"estimated"`
}

const usageSessionIdleSeconds int64 = 30 * 60
const usageSessionObservationLimit = 131072
const usageSessionLimit = 4096

// historySessions reads raw, scoped evidence independently of display buckets.
func (s *usageService) historySessions(h *usageHistory, scopeArgs []any, start int64) error {
	args := append(append([]any{}, scopeArgs...), start-usageSessionIdleSeconds-h.GapSeconds, s.now().Unix(), usageSessionObservationLimit)
	rows, err := s.accounts.db.Query(`SELECT at,remaining,reset_at,minutes FROM (SELECT at,remaining,reset_at,minutes FROM usage_observations WHERE user_id=? AND profile_id=? AND connection_id=? AND identity_key=? AND window_id=? AND at>=? AND at<=? ORDER BY at DESC LIMIT ?) ORDER BY at`, args...)
	if err != nil {
		return errUsageHistoryUnavailable
	}
	defer rows.Close()
	var points []usageHistoryPoint
	for rows.Next() {
		var p usageHistoryPoint
		if rows.Scan(&p.At, &p.RemainingPercent, &p.ResetAt, &p.WindowMinutes) != nil {
			return errUsageHistoryUnavailable
		}
		points = append(points, p)
	}
	if rows.Err() != nil {
		return errUsageHistoryUnavailable
	}
	h.Sessions, h.SessionsTruncated = estimateUsageSessions(points, start, s.now().Unix(), h)
	// The range lookback is bounded too: a cluster already active at its first
	// retained reading may have begun earlier, even when the row cap was not hit.
	if len(points) > 0 && points[0].At > h.TrackingStartedAt {
		for _, session := range h.Sessions {
			if session.StartAt == points[0].At {
				h.SessionsTruncated = true
				break
			}
		}
	}
	return nil
}

func estimateUsageSessions(points []usageHistoryPoint, start, now int64, h *usageHistory) ([]usageHistorySession, bool) {
	var sessions []usageHistorySession
	active := -1
	truncated := len(points) >= usageSessionObservationLimit
	// A bounded dense suffix can begin inside an existing cluster. Do not
	// invent its beginning: resume only after a witnessed boundary or idle.
	suppressLeading := truncated
	var lastPotentialUse int64
	if len(points) > 0 {
		lastPotentialUse = points[0].At
	}
	for i := 1; i < len(points); i++ {
		prev, p := points[i-1], points[i]
		// Missing coverage, replenishment and changed or elapsed resets are
		// boundaries, never evidence that the intervening decline was usage.
		if p.At <= prev.At || p.At-prev.At > h.GapSeconds ||
			(prev.ResetAt > 0 && p.At >= prev.ResetAt) ||
			(p.ResetAt > 0 && p.ResetAt <= p.At) ||
			(prev.ResetAt > 0 && p.ResetAt > 0 && prev.ResetAt != p.ResetAt) ||
			(prev.WindowMinutes > 0 && p.WindowMinutes > 0 && prev.WindowMinutes != p.WindowMinutes) ||
			p.RemainingPercent > prev.RemainingPercent {
			active = -1
			suppressLeading = false
			continue
		}
		decline := prev.RemainingPercent - p.RemainingPercent
		if suppressLeading {
			if decline > 0 {
				lastPotentialUse = p.At
			} else if p.At-lastPotentialUse > usageSessionIdleSeconds {
				suppressLeading = false
			}
			continue
		}
		if active >= 0 && prev.At-sessions[active].LastUsedAt > usageSessionIdleSeconds {
			active = -1
		}
		if decline <= 0 {
			continue
		}
		if active < 0 {
			sessions = append(sessions, usageHistorySession{StartAt: prev.At, Estimated: true})
			active = len(sessions) - 1
		}
		sessions[active].EndAt = p.At
		sessions[active].LastUsedAt = p.At
		sessions[active].UsedPercent += decline
	}
	if active >= 0 && len(points) > 0 && !h.Stale {
		last := points[len(points)-1]
		if last.At == h.ObservedAt && last.At <= now && now-last.At <= h.GapSeconds &&
			last.At-sessions[active].LastUsedAt < usageSessionIdleSeconds &&
			(last.ResetAt == 0 || last.ResetAt == h.ResetAt) && (h.ResetAt == 0 || h.ResetAt > now) {
			sessions[active].Current = true
			sessions[active].EndAt = last.At
		}
	}
	// Keep original evidence timestamps for overlapping clusters, rather than
	// clipping a session's beginning to the selected chart range.
	visible := sessions[:0]
	for _, session := range sessions {
		if session.EndAt >= start && session.StartAt <= now {
			visible = append(visible, session)
		}
	}
	truncated = truncated || len(visible) >= usageSessionLimit
	if len(visible) > usageSessionLimit {
		visible = visible[len(visible)-usageSessionLimit:]
	}
	return visible, truncated
}
