package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Explicit opt-in only: normal unit tests remain deterministic and offline.
func TestResearchLiveFetch(t *testing.T) {
	if os.Getenv("RESEARCH_WEB_LIVE") != "1" {
		t.Skip("set RESEARCH_WEB_LIVE=1 to exercise public network retrieval")
	}
	// No search credentials are read or used by this live retrieval check.
	out, err := (&researchWeb{}).fetch(context.Background(), "https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ URL, Title, Content string }
	if err = json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://example.com/" || !strings.Contains(got.Title, "Example Domain") || !strings.Contains(got.Content, "documentation") {
		t.Fatalf("unexpected actual example.com page: %s", out)
	}
	t.Logf("Actual public HTTPS retrieval verified: %s (%d content bytes)", got.URL, len(got.Content))
}
