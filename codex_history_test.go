package main

import (
	"encoding/json"
	"testing"
)

func TestCodexHistoryUnknownAndRangeValidation(t *testing.T) {
	f, _, _ := newUsageFixture(t)
	out := f.request("GET", "/api/usage/history?profile_id="+f.id+"&range=24h", "", "agent.example.test")
	if out.Code != 200 {
		t.Fatalf("history HTTP %d: %s", out.Code, out.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["remaining_percent"] != nil || body["provider"] != "chatgpt" {
		t.Fatalf("unknown history: %s", out.Body.String())
	}
	if points, ok := body["points"].([]any); !ok || len(points) != 0 {
		t.Fatalf("points must be empty array: %s", out.Body.String())
	}
	if out.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
	for _, value := range []string{"", "month", "all", "24H"} {
		out = f.request("GET", "/api/usage/history?profile_id="+f.id+"&range="+value, "", "agent.example.test")
		if out.Code != 400 {
			t.Fatalf("range %q accepted: %d", value, out.Code)
		}
	}
}
