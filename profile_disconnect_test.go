package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDisconnectClearsOnlyTheProfileSelectedWhenRequestStarted(t *testing.T) {
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
	first.ClientID, first.Subject = "first-client", "first-subject"
	second := testOAuthCredentials("second-access", "second-refresh", time.Now().Add(time.Hour))
	second.ClientID, second.Subject = "second-client", "second-subject"
	if err := store.SaveProfile(credentialProfile{ID: firstID, Credentials: first}); err != nil {
		t.Fatalf("save first profile: %v", err)
	}
	if err := store.SaveProfile(credentialProfile{ID: secondID, Credentials: second}); err != nil {
		t.Fatalf("save second profile: %v", err)
	}
	if err := store.SetActiveProfile(firstID); err != nil {
		t.Fatalf("select first profile: %v", err)
	}
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(openIDMetadata{Issuer: issuer, AuthorizationEndpoint: issuer + "/authorize", TokenEndpoint: issuer + "/token", RevocationEndpoint: issuer + "/revoke"})
		case "/revoke":
			if err := store.SetActiveProfile(secondID); err != nil {
				t.Errorf("switch active profile during disconnect: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	issuer = server.URL
	service := newSIWCService(siwcConfig{issuerURL: issuer, redirectURI: "http://127.0.0.1:8080/auth/callback"}, store)
	service.client = server.Client()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/disconnect", nil)
	request.Host = "127.0.0.1:8080"
	recorder := httptest.NewRecorder()
	service.disconnectHandler(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("disconnect status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	storedFirst, err := store.LoadProfile(firstID)
	if err != nil || credentialsConnected(storedFirst.Credentials) || storedFirst.Credentials.OAuthPreferred {
		t.Fatalf("selected profile was not disconnected: %#v, %v", storedFirst, err)
	}
	storedSecond, err := store.LoadProfile(secondID)
	if err != nil || storedSecond.Credentials.AccessToken != "second-access" || storedSecond.Credentials.RefreshToken != "second-refresh" || storedSecond.Credentials.Subject != "second-subject" {
		t.Fatalf("disconnect changed the other profile: %#v, %v", storedSecond, err)
	}
}
