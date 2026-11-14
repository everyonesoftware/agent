package main

import (
	"errors"
	"time"
)

type usageHistoryPoint struct {
	At               int64   `json:"at"`
	RemainingPercent float64 `json:"remaining_percent"`
	ResetAt          int64   `json:"reset_at,omitempty"`
}
type usageHistoryWindow struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type usageHistory struct {
	Provider          string               `json:"provider"`
	Scope             string               `json:"scope"`
	ObservedAt        int64                `json:"observed_at"`
	TrackingStartedAt int64                `json:"tracking_started_at"`
	PeriodStart       int64                `json:"period_start"`
	ResetAt           int64                `json:"reset_at"`
	RemainingPercent  *float64             `json:"remaining_percent"`
	RemainingCount    *int64               `json:"remaining_count,omitempty"`
	WindowID          string               `json:"window_id,omitempty"`
	Windows           []usageHistoryWindow `json:"windows,omitempty"`
	Points            []usageHistoryPoint  `json:"points"`
	Stale             bool                 `json:"stale"`
	GapSeconds        int64                `json:"gap_seconds"`
	Warning           string               `json:"warning,omitempty"`
}

func historyRangeStart(value string, now time.Time, periodStart int64) (int64, error) {
	switch value {
	case "24h":
		return now.Add(-24 * time.Hour).Unix(), nil
	case "week":
		return now.Add(-7 * 24 * time.Hour).Unix(), nil
	case "period":
		// Unknown periods do not invent a provider reset boundary.
		if periodStart <= 0 {
			return now.Add(-90 * 24 * time.Hour).Unix(), nil
		}
		floor := now.Add(-90 * 24 * time.Hour).Unix()
		if periodStart < floor {
			return floor, nil
		}
		return periodStart, nil
	default:
		return 0, errors.New("Select a valid history range: 24h, week, or period.")
	}
}
