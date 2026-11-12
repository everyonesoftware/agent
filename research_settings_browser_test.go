package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
)

// Opt-in production-router/browser integration. Only the Brave/model upstreams
// are local HTTP fixtures; no real credential, paid search or inference is used.
func TestResearchSettingsBrowser(t *testing.T) {
	if os.Getenv("AGENT_SETTINGS_BROWSER") != "1" {
		t.Skip("set AGENT_SETTINGS_BROWSER=1 to run the production settings browser check")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Fatal("Node is required for the explicit browser integration")
	}
	t.Setenv("APP_DATA_DIR", t.TempDir())
	t.Setenv("BRAVE_SEARCH_API_KEY", "synthetic-global-key-must-not-be-used")
	var searches, verifications, models atomic.Int32
	var rejectSaved atomic.Bool
	var lastSuffix atomic.Value
	lastSuffix.Store("")
	brave := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searches.Add(1)
		key := r.Header.Get("X-Subscription-Token")
		if r.Method != "GET" || r.URL.Path != "/res/v1/web/search" || !strings.HasPrefix(key, "synthetic-brave-") {
			t.Error("Brave fixture received an unexpected method, endpoint or credential source")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		lastSuffix.Store(key[len(key)-4:])
		if r.URL.Query().Get("q") == "Agent connection test" {
			verifications.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(key, "rejected") || rejectSaved.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			// Deliberately private upstream content must not reach UI or traces.
			fmt.Fprintf(w, `{"error":"PRIVATE-UPSTREAM-DETAIL %s"}`, key)
			return
		}
		io.WriteString(w, `{"web":{"results":[{"title":"Isolated search fixture","url":"https://example.com/","description":"Synthetic evidence from the isolated test upstream, not a real web search."}]}}`)
	}))
	defer brave.Close()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		models.Add(1)
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			t.Error("could not read isolated model request")
			w.WriteHeader(500)
			return
		}
		if strings.Contains(string(body), "synthetic-brave-") || strings.Contains(string(body), "PRIVATE-UPSTREAM-DETAIL") {
			t.Error("search credentials or private provider errors entered a model request")
		}
		var payload struct {
			Messages []struct{ Role, Content string }
			Tools    []json.RawMessage
		}
		if json.Unmarshal(body, &payload) != nil || len(payload.Messages) == 0 {
			t.Error("invalid isolated model payload")
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if len(payload.Tools) > 0 && payload.Messages[len(payload.Messages)-1].Role == "user" {
			io.WriteString(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"settings-search","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"public research fixture\"}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"A response from the isolated model fixture, not live inference."},"finish_reason":"stop"}]}`)
	}))
	defer model.Close()
	t.Setenv("LLM_BASE_URL", model.URL+"/v1")
	t.Setenv("LLM_MODEL", "settings-fixture-model")
	t.Setenv("LLM_API_KEY", "")
	factory := func(key string) *researchWeb {
		client := newResearchWeb()
		client.apiKey = key
		client.searchTransport = researchRoundTrip(func(r *http.Request) (*http.Response, error) {
			if r.URL.Scheme != "https" || r.URL.Host != "api.search.brave.com" {
				return nil, fmt.Errorf("search did not target the official fixed endpoint")
			}
			copy := r.Clone(r.Context())
			url := *r.URL
			copy.URL = &url
			copy.URL.Scheme = "http"
			copy.URL.Host = strings.TrimPrefix(brave.URL, "http://")
			return http.DefaultTransport.RoundTrip(copy)
		})
		return client
	}
	type rootHandler struct{ http.Handler }
	var application atomic.Value
	application.Store(rootHandler{newServerWithResearchWebFactory(factory)})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// These controls exist only in this test harness, never in Agent's router.
		switch r.URL.Path {
		case "/__fixture/state":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"search_calls": searches.Load(), "verify_calls": verifications.Load(), "model_calls": models.Load(), "last_suffix": lastSuffix.Load()})
			return
		case "/__fixture/reject-saved":
			if r.Method != "POST" {
				w.WriteHeader(405)
				return
			}
			rejectSaved.Store(r.URL.Query().Get("enabled") == "1")
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{}`)
			return
		case "/__fixture/reopen":
			if r.Method != "POST" {
				w.WriteHeader(405)
				return
			}
			application.Store(rootHandler{newServerWithResearchWebFactory(factory)})
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{}`)
			return
		}
		application.Load().(rootHandler).ServeHTTP(w, r)
	}))
	defer server.Close()
	command := exec.Command("node", "tests/research-settings-browser.mjs", server.URL)
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatalf("production settings browser check failed: %v", err)
	}
	if searches.Load() == 0 || verifications.Load() == 0 || models.Load() == 0 {
		t.Fatal("browser did not exercise the real verification and research request paths")
	}
}
