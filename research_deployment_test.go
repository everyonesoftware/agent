package main

import (
	"os"
	"strings"
	"testing"
)

func TestResearchComposeDoesNotRequireSearchEnvironment(t *testing.T) {
	data, err := os.ReadFile("compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "BRAVE_SEARCH_API_KEY") {
		t.Fatal("Brave Search must be configured per user in Settings, not through a shared environment credential")
	}
}
