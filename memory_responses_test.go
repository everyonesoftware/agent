package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMemoryResponsesActualWireContext(t *testing.T) {
	var wire string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Write([]byte(`{"data":[{"id":"oauth-memory","visibility":"list"}]}`))
			return
		}
		var body json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		wire = string(body)
		w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"Fixture answer\"}\n\ndata: {\"type\":\"response.completed\"}\n\n"))
	}))
	defer upstream.Close()
	store, err := newAccountStore(t.TempDir() + "/memory.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	user, token, err := store.register("memory-oauth", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	// Use the same explicit-save service as the authenticated Memory API.
	if _, err = store.saveMemory(user, "user", "I prefer terse explanations. oauth-jade-735"); err != nil {
		t.Fatal(err)
	}
	creds := newFileCredentialStore(t.TempDir())
	if err = creds.Save(testOAuthCredentials("TEST-ACCESS", "TEST-REFRESH", time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	service := newSIWCService(siwcConfig{apiBaseURL: upstream.URL}, creds)
	service.client = upstream.Client()
	provider := &responsesProvider{auth: service}
	handler := store.middleware(chatHandlerWithStore(provider, store))
	response := runRequest(handler, "POST", "/api/chat", `{"messages":[{"role":"user","content":"What style should you use?"}]}`, &http.Cookie{Name: authCookieName, Value: token})
	if response.Code != 200 {
		t.Fatalf("chat: %d %s", response.Code, response.Body)
	}
	if !strings.Contains(wire, "oauth-jade-735") {
		t.Fatalf("missing memory in OAuth wire: %s", wire)
	}
	r := httptest.NewRequest("GET", "/api/runs/"+response.Header().Get("X-Agent-Run-ID"), nil)
	r.SetPathValue("id", response.Header().Get("X-Agent-Run-ID"))
	r.AddCookie(&http.Cookie{Name: authCookieName, Value: token})
	trace := httptest.NewRecorder()
	store.middleware(http.HandlerFunc(store.runHandler)).ServeHTTP(trace, r)
	run := decodeRunMap(t, trace)
	if !strings.Contains(trace.Body.String(), "oauth-jade-735") || run["provider"] != "chatgpt" {
		t.Fatal("OAuth trace missing memory/provider")
	}
	var sent any
	json.Unmarshal([]byte(wire), &sent)
	gotBytes, _ := json.Marshal(run["request"])
	sentBytes, _ := json.Marshal(sent)
	if string(gotBytes) != string(sentBytes) {
		t.Fatal("OAuth trace not equal to actual request")
	}
}
