package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResearchSettingsRemovalCancelsBlockedModelHTTP(t *testing.T) {
	a, s, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"web":{"results":[]}}`)
	})
	saved := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	entered, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(entered)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer upstream.Close()
	defer close(release)
	h.(*http.ServeMux).Handle("POST /api/chat", a.middleware(chatHandlerWithResearch(&openAICompatibleProvider{baseURL: upstream.URL, model: "fixture", client: upstream.Client()}, a, s)))
	payload := `{"messages":[{"role":"user","content":"Public question"}]}`
	id := researchApproved(t, h, c, payload)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id)) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("model not entered")
	}
	settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/remove", settingsRevision(saved.Revision)), 200)
	select {
	case w := <-done:
		if w.Code == 200 {
			t.Fatal("cancelled research reported success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked model call not cancelled")
	}
	w := researchRequest(h, c, "GET", "/api/research/tasks/"+id, "")
	if !strings.Contains(w.Body.String(), `"status":"revoked"`) {
		t.Fatal(w.Body.String())
	}
}
