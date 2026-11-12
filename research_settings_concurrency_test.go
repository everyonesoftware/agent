package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestResearchSettingsBlockedVerifyResponsiveAndNoResurrection(t *testing.T) {
	for _, action := range []string{"remove", "logout"} {
		t.Run(action, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var released atomic.Bool
			defer func() {
				if released.CompareAndSwap(false, true) {
					close(release)
				}
			}()
			a, _, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-release
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"web":{"results":[]}}`)
			})
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, ""))
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("verify not entered")
			}
			getDone := make(chan *httptest.ResponseRecorder, 1)
			go func() { getDone <- researchRequest(h, c, "GET", "/api/research/settings", "") }()
			select {
			case w := <-getDone:
				settingsResult(t, w, 200)
			case <-time.After(time.Second):
				t.Fatal("GET blocked by Brave I/O")
			}
			duplicate := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 429)
			if duplicate.Configured {
				t.Fatal("duplicate saved")
			}
			want := 409
			if action == "remove" {
				v := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/remove", settingsRevision("")), 200)
				if v.Revision == "" {
					t.Fatal("missing tombstone")
				}
			} else {
				if e := a.logout(c.Value); e != nil {
					t.Fatal(e)
				}
				want = 401
			}
			released.Store(true)
			close(release)
			select {
			case w := <-done:
				settingsResult(t, w, want)
			case <-time.After(2 * time.Second):
				t.Fatal("verification did not finish")
			}
			uID := ""
			if e := a.db.QueryRow(`SELECT id FROM users WHERE username='settings-user'`).Scan(&uID); e != nil {
				t.Fatal(e)
			}
			after, e := a.researchCredentialForUser(uID)
			if e != nil || after.Configured {
				t.Fatal("stale or signed-out verification resurrected key", e)
			}
		})
	}
}

func TestResearchSettingsRuntimeStoredKeyTraceAndInvalidation(t *testing.T) {
	var searches, models atomic.Int32
	a, s, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != settingsTestKey {
			t.Error("runtime token not stored literal")
		}
		if r.URL.Query().Get("q") != "Agent connection test" {
			searches.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"web":{"results":[{"title":"Evidence","url":"https://example.com/","description":"fixture evidence"}]}}`)
	})
	v := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), settingsTestKey) {
			t.Error("key disclosed to model")
		}
		if models.Add(1) == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"tool_calls":[{"id":"search-1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"public evidence\"}"}}]}}]}`)
		} else {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"Fixture evidence https://example.com/"}}]}`)
		}
	}))
	defer model.Close()
	mux := h.(*http.ServeMux)
	mux.Handle("POST /api/chat", a.middleware(chatHandlerWithResearch(&openAICompatibleProvider{baseURL: model.URL, model: "fixture", client: model.Client()}, a, s)))
	mux.Handle("GET /api/runs/{id}", a.middleware(http.HandlerFunc(a.runHandler)))
	payload := `{"messages":[{"role":"user","content":"Find public evidence"}],"run_id":"00000000-0000-0000-0000-000000000121"}`
	id := researchApproved(t, h, c, payload)
	s.mu.Lock()
	task := s.tasks[id]
	snapshot := task.web.(*researchWeb)
	s.mu.Unlock()
	if snapshot.apiKey != settingsTestKey || task.configRevision != v.Revision {
		t.Fatal("missing immutable key snapshot")
	}
	w := researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id))
	if w.Code != 200 || searches.Load() != 1 || models.Load() != 2 {
		t.Fatal(w.Code, w.Body.String(), searches.Load(), models.Load())
	}
	w = researchRequest(h, c, "GET", "/api/runs/00000000-0000-0000-0000-000000000121", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), settingsTestKey) || strings.Contains(w.Body.String(), v.Revision) {
		t.Fatal("trace credential exposure", w.Code)
	}
	pending := researchApproved(t, h, c, `{"messages":[{"role":"user","content":"Another question"}]}`)
	_, otherToken, e := a.register("runtime-other", "synthetic-password")
	if e != nil {
		t.Fatal(e)
	}
	other := &http.Cookie{Name: authCookieName, Value: otherToken}
	sibling := researchApproved(t, h, other, `{"messages":[{"role":"user","content":"Other question"}]}`)
	// A running task's context is cancelled in the same serialized local commit.
	var req chatRequest
	json.Unmarshal([]byte(`{"messages":[{"role":"user","content":"Another question"}]}`), &req)
	req.ResearchTaskID = pending
	ctx, running, e := s.consume(context.Background(), mustSettingsUser(t, a, c), req)
	if e != nil {
		t.Fatal(e)
	}
	settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/remove", settingsRevision(v.Revision)), 200)
	if ctx.Err() != context.Canceled || s.check(ctx, running) == nil {
		t.Fatal("running task survived removal")
	}
	s.mu.Lock()
	status, otherStatus := s.tasks[pending].Status, s.tasks[sibling].Status
	s.mu.Unlock()
	if status != "revoked" || otherStatus != "approved" {
		t.Fatal("owner invalidation", status, otherStatus)
	}
	if e = s.commit(context.Background(), running, func() error { t.Error("revoked final save"); return nil }); e == nil {
		t.Fatal("revoked task saved")
	}
}
