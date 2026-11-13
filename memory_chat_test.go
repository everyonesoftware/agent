package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMemoryExplicitRememberDoesNotCallProvider(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"choices":[{"message":{"content":"not a real save"}}]}`))
	}))
	defer upstream.Close()
	t.Setenv("APP_DATA_DIR", t.TempDir())
	t.Setenv("LLM_BASE_URL", upstream.URL)
	t.Setenv("LLM_MODEL", "fixture")
	h := newServer()
	cookie := runRequest(h, "POST", "/api/auth/register", `{"username":"memory-chat","password":"correct-horse-battery"}`, nil).Result().Cookies()[0]
	response := runRequest(h, "POST", "/api/chat", `{"messages":[{"role":"user","content":"/remember I prefer concise replies."}]}`, cookie)
	if response.Code != 200 {
		t.Fatalf("remember: %d %s", response.Code, response.Body)
	}
	if calls.Load() != 0 {
		t.Fatal("Explicit remember must persist locally without model inference")
	}
	snapshot := runRequest(h, "GET", "/api/memories", "", cookie)
	if snapshot.Code != 200 || !strings.Contains(snapshot.Body.String(), "I prefer concise replies.") {
		t.Fatalf("save not persisted: %d %s", snapshot.Code, snapshot.Body)
	}
	run := decodeRunMap(t, runRequest(h, "GET", "/api/runs/"+response.Header().Get("X-Agent-Run-ID"), "", cookie))
	if run["state"] != "completed" || run["request"] != nil {
		t.Fatalf("memory-only run invented model work: %#v", run)
	}
	events, _ := json.Marshal(run["events"])
	if !strings.Contains(string(events), "memory_save") {
		t.Fatalf("missing actual tool execution: %s", events)
	}
}

func TestMemoryFreshChatWireAndTraceFollowDeletionAndDisable(t *testing.T) {
	var wire string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		wire = string(body)
		w.Write([]byte(`{"choices":[{"message":{"content":"fixture answer"}}]}`))
	}))
	defer upstream.Close()
	t.Setenv("APP_DATA_DIR", t.TempDir())
	t.Setenv("LLM_BASE_URL", upstream.URL)
	t.Setenv("LLM_MODEL", "fixture")
	h := newServer()
	cookie := runRequest(h, "POST", "/api/auth/register", `{"username":"memory-wire","password":"correct-horse-battery"}`, nil).Result().Cookies()[0]
	saved := runRequest(h, "POST", "/api/memories", `{"category":"user","content":"Unique preference emerald-482."}`, cookie)
	if saved.Code != 201 {
		t.Fatalf("save: %d %s", saved.Code, saved.Body)
	}
	var entry struct{ Entry struct{ ID string } }
	if err := json.Unmarshal(saved.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	chat := func(want bool) map[string]any {
		t.Helper()
		response := runRequest(h, "POST", "/api/chat", `{"messages":[{"role":"user","content":"What preferences apply?"}]}`, cookie)
		if response.Code != 200 {
			t.Fatalf("chat: %d %s", response.Code, response.Body)
		}
		if strings.Contains(wire, "emerald-482") != want {
			t.Fatalf("memory wire inclusion=%v want=%v: %s", !want, want, wire)
		}
		run := decodeRunMap(t, runRequest(h, "GET", "/api/runs/"+response.Header().Get("X-Agent-Run-ID"), "", cookie))
		contextBytes, _ := json.Marshal(run["context"])
		if strings.Contains(string(contextBytes), "emerald-482") != want {
			t.Fatalf("context mismatch: %s", contextBytes)
		}
		requestBytes, _ := json.Marshal(run["request"])
		var expected any
		json.Unmarshal([]byte(wire), &expected)
		expectedBytes, _ := json.Marshal(expected)
		if string(requestBytes) != string(expectedBytes) {
			t.Fatal("trace differs from actual model request")
		}
		return run
	}
	chat(true)
	disabled := runRequest(h, "PUT", "/api/memory-settings", `{"enabled":false}`, cookie)
	if disabled.Code != 200 {
		t.Fatalf("disable: %d %s", disabled.Code, disabled.Body)
	}
	run := chat(false)
	memory := run["context"].(map[string]any)["memory"].(map[string]any)
	if memory["enabled"] != false {
		t.Fatal("disabled state not inspectable")
	}
	runRequest(h, "PUT", "/api/memory-settings", `{"enabled":true}`, cookie)
	if deleted := runRequest(h, "DELETE", "/api/memories/"+entry.Entry.ID, "", cookie); deleted.Code != 200 {
		t.Fatal(deleted.Code, deleted.Body)
	}
	chat(false)
}

func TestMemoryRememberRequiresSameSiteAndNoResearchGrant(t *testing.T) {
	t.Setenv("APP_DATA_DIR", t.TempDir())
	t.Setenv("LLM_MODEL", "fixture")
	h := newServer()
	cookie := runRequest(h, "POST", "/api/auth/register", `{"username":"memory-scope","password":"correct-horse-battery"}`, nil).Result().Cookies()[0]
	r := httptest.NewRequest("POST", "/api/chat", strings.NewReader(`{"messages":[{"role":"user","content":"/remember no cross site"}]}`))
	r.AddCookie(cookie)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-site remember: %d %s", w.Code, w.Body)
	}
	w = runRequest(h, "POST", "/api/chat", `{"research_task_id":"nonexistent","messages":[{"role":"user","content":"/remember no research writes"}]}`, cookie)
	if w.Code != 403 {
		t.Fatalf("research remember: %d %s", w.Code, w.Body)
	}
	snapshot := runRequest(h, "GET", "/api/memories", "", cookie)
	if strings.Contains(snapshot.Body.String(), "no research") || strings.Contains(snapshot.Body.String(), "no cross") {
		t.Fatal("unapproved save persisted")
	}
}

func TestMemoryResearchWireExcludesSavedFacts(t *testing.T) {
	var wire string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		wire = string(body)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"No public source was retrieved."},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()
	t.Setenv("APP_DATA_DIR", t.TempDir())
	t.Setenv("LLM_BASE_URL", upstream.URL)
	t.Setenv("LLM_MODEL", "fixture")
	h := newServer()
	cookie := runRequest(h, "POST", "/api/auth/register", `{"username":"memory-research","password":"correct-horse-battery"}`, nil).Result().Cookies()[0]
	saved := runRequest(h, "POST", "/api/memories", `{"category":"project","content":"PRIVATE-MEMORY-cobalt-729"}`, cookie)
	if saved.Code != 201 {
		t.Fatalf("save: %d %s", saved.Code, saved.Body)
	}
	payload := `{"messages":[{"role":"user","content":"Read https://example.com"}]}`
	created := runRequest(h, "POST", "/api/research/tasks", payload, cookie)
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body)
	}
	var task struct{ Task struct{ ID string } }
	json.Unmarshal(created.Body.Bytes(), &task)
	approved := runRequest(h, "POST", "/api/research/tasks/"+task.Task.ID+"/approve", `{}`, cookie)
	if approved.Code != 200 {
		t.Fatal(approved.Code, approved.Body)
	}
	response := runRequest(h, "POST", "/api/chat", strings.TrimSuffix(payload, "}")+`,"research_task_id":"`+task.Task.ID+`"}`, cookie)
	if response.Code != 200 {
		t.Fatalf("research: %d %s", response.Code, response.Body)
	}
	if wire == "" || strings.Contains(wire, "PRIVATE-MEMORY") {
		t.Fatalf("memory disclosed to research: %s", wire)
	}
	trace := runRequest(h, "GET", "/api/runs/"+response.Header().Get("X-Agent-Run-ID"), "", cookie)
	run := decodeRunMap(t, trace)
	if run["context"].(map[string]any)["memory"] != nil || strings.Contains(trace.Body.String(), "PRIVATE-MEMORY") {
		t.Fatal("research trace included memory")
	}
}

func TestMemoryRememberWithoutProviderAndDuplicateRunSafety(t *testing.T) {
	t.Setenv("APP_DATA_DIR", t.TempDir())
	t.Setenv("LLM_MODEL", "")
	h := newServer()
	cookie := runRequest(h, "POST", "/api/auth/register", `{"username":"memory-offline","password":"correct-horse-battery"}`, nil).Result().Cookies()[0]
	body := `{"run_id":"51734567-1234-4567-890a-123456789abc","messages":[{"role":"user","content":"/remember offline-persisted"}]}`
	if w := runRequest(h, "POST", "/api/chat", body, cookie); w.Code != 200 {
		t.Fatalf("offline save: %d %s", w.Code, w.Body)
	}
	if w := runRequest(h, "POST", "/api/chat", body, cookie); w.Code != 409 {
		t.Fatalf("duplicate run: %d %s", w.Code, w.Body)
	}
	empty := runRequest(h, "POST", "/api/chat", `{"messages":[{"role":"user","content":"/remember"}]}`, cookie)
	if empty.Code != 400 {
		t.Fatalf("empty memory: %d %s", empty.Code, empty.Body)
	}
	snapshot := runRequest(h, "GET", "/api/memories", "", cookie)
	var result struct{ Entries []json.RawMessage }
	if err := json.Unmarshal(snapshot.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("duplicate/empty save changed memory count: %s", snapshot.Body)
	}
}
