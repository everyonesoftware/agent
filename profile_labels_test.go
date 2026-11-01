package main

import "testing"

func TestNextProfileLabelUsesDistinctStableRegistrationLabels(t *testing.T) {
	profiles := []credentialProfile{
		{ID: "profile_abcdefghijklmnop", Label: "ChatGPT account 1"},
		{ID: "profile_bcdefghijklmnopq", Label: "ChatGPT account 3"},
	}
	if got, want := nextProfileLabel(profiles), "ChatGPT account 2"; got != want {
		t.Fatalf("next profile label = %q, want %q", got, want)
	}
}
