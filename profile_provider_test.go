package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLegacyProfilesCannotReuseTokensOrModelCache(t *testing.T) {
	var responseAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("legacy profile attempted removed provider transport")
		switch r.URL.Path {
		case "/models":
			w.Header().Set("Content-Type", "application/json")
			modelID, modelName := "model-a", "Account A model"
			if r.Header.Get("Authorization") == "Bearer second-access" {
				modelID, modelName = "model-b", "Account B model"
			} else if r.Header.Get("Authorization") != "Bearer first-access" {
				t.Errorf("unexpected model-list authorization header: %q", r.Header.Get("Authorization"))
			}
			_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"slug":%q,"display_name":%q,"visibility":"list"}]}`, modelID, modelID, modelName)
		case "/responses":
			responseAuthorization = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"second reply\"}\n\ndata: {\"type\":\"response.completed\"}\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

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
	if err := store.SaveProfile(credentialProfile{ID: firstID, Label: "Account A", Credentials: first}); err != nil {
		t.Fatalf("save first profile: %v", err)
	}
	if err := store.SaveProfile(credentialProfile{ID: secondID, Label: "Account B", Credentials: second}); err != nil {
		t.Fatalf("save second profile: %v", err)
	}
	if err := store.SetActiveProfile(firstID); err != nil {
		t.Fatalf("select first profile: %v", err)
	}
	service := newSIWCService(siwcConfig{apiBaseURL: server.URL}, store)
	service.client = server.Client()
	provider := &responsesProvider{auth: service, defaultModel: "model-a"}

	models, err := provider.models(context.Background())
	if err != errChatGPTNotConnected || len(models) != 0 {
		t.Fatalf("first legacy profile models = %#v, %v; want reconnect", models, err)
	}
	if err := store.SetActiveProfile(secondID); err != nil {
		t.Fatalf("select second profile: %v", err)
	}
	models, err = provider.models(context.Background())
	if err != errChatGPTNotConnected || len(models) != 0 {
		t.Fatalf("second legacy profile reused model cache = %#v, %v", models, err)
	}
	reply, err := provider.complete(context.Background(), []chatMessage{{Role: "user", Content: "hello"}}, "model-b")
	if err != errChatGPTNotConnected || reply != "" {
		t.Fatalf("second profile completion = %q, %v", reply, err)
	}
	if responseAuthorization != "" {
		t.Fatalf("removed Responses transport received authorization %q", responseAuthorization)
	}
}
