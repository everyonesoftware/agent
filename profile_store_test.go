package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCredentialStoreKeepsProfilesSeparateAndSelectionExplicit(t *testing.T) {
	store := newFileCredentialStore(t.TempDir())
	firstID, err := newProfileID()
	if err != nil {
		t.Fatalf("generate first profile ID: %v", err)
	}
	secondID, err := newProfileID()
	if err != nil {
		t.Fatalf("generate second profile ID: %v", err)
	}
	first := testOAuthCredentials("first-access", "first-refresh", time.Now().Add(time.Hour))
	first.ClientID = "first-client"
	first.Subject = "first-subject"
	second := testOAuthCredentials("second-access", "second-refresh", time.Now().Add(time.Hour))
	second.ClientID = "second-client"
	second.Subject = "second-subject"

	if err := store.SaveProfile(credentialProfile{ID: firstID, Label: "first@example.test", Credentials: first}); err != nil {
		t.Fatalf("save first profile: %v", err)
	}
	if err := store.SetActiveProfile(firstID); err != nil {
		t.Fatalf("select first profile: %v", err)
	}
	if err := store.SaveProfile(credentialProfile{ID: secondID, Label: "second@example.test", Credentials: second}); err != nil {
		t.Fatalf("save second profile: %v", err)
	}
	activeID, err := store.ActiveProfileID()
	if err != nil || activeID != firstID {
		t.Fatalf("active profile = %q, %v; want first profile %q", activeID, err, firstID)
	}
	storedFirst, err := store.LoadProfile(firstID)
	if err != nil || storedFirst.Credentials.RefreshToken != "first-refresh" || storedFirst.Credentials.Subject != "first-subject" {
		t.Fatalf("first profile was changed by adding second: %#v, %v", storedFirst, err)
	}
	storedSecond, err := store.LoadProfile(secondID)
	if err != nil || storedSecond.Credentials.RefreshToken != "second-refresh" || storedSecond.Credentials.Subject != "second-subject" {
		t.Fatalf("second profile was not preserved: %#v, %v", storedSecond, err)
	}
	profiles, err := store.ListProfiles()
	if err != nil || len(profiles) != 2 {
		t.Fatalf("profiles = %d, %v; want 2", len(profiles), err)
	}
	if err := store.SetActiveProfile(secondID); err != nil {
		t.Fatalf("select second profile: %v", err)
	}
	activeID, err = store.ActiveProfileID()
	if err != nil || activeID != secondID {
		t.Fatalf("active profile after selection = %q, %v; want %q", activeID, err, secondID)
	}
	if err := store.SetActiveProfile("profile-unknown"); err == nil {
		t.Fatal("selecting an unknown profile succeeded")
	}
}

func TestCredentialStoreMigratesLegacyTokenFileAtomically(t *testing.T) {
	dir := t.TempDir()
	legacy := testOAuthCredentials("legacy-access", "legacy-refresh", time.Now().Add(time.Hour))
	legacy.ClientID = "legacy-client"
	legacy.Subject = "legacy-subject"
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("encode legacy credentials: %v", err)
	}
	legacyPath := filepath.Join(dir, "siwc-token.json")
	if err := os.WriteFile(legacyPath, data, 0600); err != nil {
		t.Fatalf("write legacy credentials: %v", err)
	}

	store := newFileCredentialStore(dir)
	profiles, err := store.ListProfiles()
	if err != nil {
		t.Fatalf("load migrated profiles: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("migrated profiles = %d, want 1", len(profiles))
	}
	profile := profiles[0]
	if profile.ID == "" || profile.Credentials.ClientID != legacy.ClientID || profile.Credentials.Subject != legacy.Subject || profile.Credentials.RefreshToken != legacy.RefreshToken {
		t.Fatalf("legacy credentials were not preserved: %#v", profile)
	}
	activeID, err := store.ActiveProfileID()
	if err != nil || activeID != profile.ID {
		t.Fatalf("active profile after migration = %q, %v; want %q", activeID, err, profile.ID)
	}
	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy token file still exists after migration: %v", err)
	}
	statePath := filepath.Join(dir, "siwc-profiles.json")
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("stat migrated state: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("profile state permissions = %04o, want 0600", got)
	}
}

func TestCredentialProfileStateRejectsUnverifiedActiveAccount(t *testing.T) {
	id, err := newProfileID()
	if err != nil {
		t.Fatalf("generate profile ID: %v", err)
	}
	credentials := oauthCredentials{
		ClientID: "pending-client", HostID: testHostID, Issuer: siwcIssuer,
		RegistrationPending: true, OAuthPreferred: true,
	}
	state := credentialProfileState{
		Version: credentialProfileStateVersion, ActiveProfileID: id,
		Profiles: []credentialProfile{{ID: id, Credentials: credentials}},
	}
	if err := validateCredentialProfileState(state); err == nil {
		t.Fatal("profile state accepted an unverified active account")
	}
}
