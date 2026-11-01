package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fixedIdentityVerifier struct {
	identity verifiedIdentity
}

func (v fixedIdentityVerifier) Verify(context.Context, string, string, string) (verifiedIdentity, error) {
	return v.identity, nil
}

func TestAddAccountCallbackSavesSeparateProfileWithoutChangingActiveSelection(t *testing.T) {
	tokenEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"second-access","refresh_token":"second-refresh","id_token":"second-id-token","token_type":"Bearer","scope":"openid profile email offline_access resource.invoke chatgpt.tokens.use.direct","expires_in":3600}`))
	}))
	defer tokenEndpoint.Close()

	store := newFileCredentialStore(t.TempDir())
	firstID, err := newProfileID()
	if err != nil {
		t.Fatalf("generate first profile ID: %v", err)
	}
	first := testOAuthCredentials("first-access", "first-refresh", time.Now().Add(time.Hour))
	first.ClientID, first.Subject = "first-client", "first-subject"
	if err := store.SaveProfile(credentialProfile{ID: firstID, Label: "ChatGPT account 1", Email: "first@example.test", Credentials: first}); err != nil {
		t.Fatalf("save first account: %v", err)
	}
	if err := store.SetActiveProfile(firstID); err != nil {
		t.Fatalf("select first account: %v", err)
	}

	service := newSIWCService(siwcConfig{
		redirectURI:  "http://127.0.0.1:8080/auth/callback",
		authorizeURL: siwcAuthorizeURL,
		tokenURL:     tokenEndpoint.URL,
	}, store)
	service.verifier = fixedIdentityVerifier{identity: verifiedIdentity{subject: "second-subject", email: "second@example.test", name: "Second Account"}}
	startRequest := httptest.NewRequest(http.MethodGet, "/auth/start?mode=add", nil)
	startRequest.Host = "127.0.0.1:8080"
	startRecorder := httptest.NewRecorder()
	service.startHandler(startRecorder, startRequest)
	if startRecorder.Code != http.StatusFound {
		t.Fatalf("add account start status = %d, want %d; body=%s", startRecorder.Code, http.StatusFound, startRecorder.Body.String())
	}
	location, err := url.Parse(startRecorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	if location.Query().Get("client_id") != siwcClientID || location.Query().Get("agent_name_hint") != siwcAgentName || location.Query().Has("id_token_hint") {
		t.Fatalf("add account did not begin a fresh registration: %s", location.String())
	}
	attempt := service.pending
	if attempt == nil || !attempt.newRegistration || attempt.profileID == "" || attempt.profileID == firstID {
		t.Fatalf("add account flow was not bound to a distinct new profile: %#v", attempt)
	}
	activeID, err := store.ActiveProfileID()
	if err != nil || activeID != firstID {
		t.Fatalf("starting add account changed active profile to %q, %v", activeID, err)
	}

	callbackRequest := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+url.QueryEscape(attempt.state)+"&code=authorization-code&client_id=second-client", nil)
	callbackRequest.Host = "127.0.0.1:8080"
	callbackRecorder := httptest.NewRecorder()
	service.callbackHandler(callbackRecorder, callbackRequest)
	if callbackRecorder.Code != http.StatusOK {
		t.Fatalf("add account callback status = %d, want %d; body=%s", callbackRecorder.Code, http.StatusOK, callbackRecorder.Body.String())
	}
	activeID, err = store.ActiveProfileID()
	if err != nil || activeID != firstID {
		t.Fatalf("completing add account changed active profile to %q, %v", activeID, err)
	}
	profiles, err := store.ListProfiles()
	if err != nil || len(profiles) != 2 {
		t.Fatalf("profiles after add account = %d, %v; want 2", len(profiles), err)
	}
	secondProfile, err := store.LoadProfile(attempt.profileID)
	if err != nil {
		t.Fatalf("load added account: %v", err)
	}
	if secondProfile.Label != "ChatGPT account 2" || secondProfile.Email != "second@example.test" || secondProfile.Credentials.ClientID != "second-client" || secondProfile.Credentials.Subject != "second-subject" || secondProfile.Credentials.RefreshToken != "second-refresh" {
		t.Fatalf("new account profile was not saved independently: %#v", secondProfile)
	}
	if strings.Contains(callbackRecorder.Body.String(), "second-refresh") || strings.Contains(callbackRecorder.Body.String(), "second-client") {
		t.Fatal("callback page exposed account credentials")
	}
}
