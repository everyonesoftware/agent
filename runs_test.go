package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type contextProbe struct{ messages []chatMessage }

func (p *contextProbe) configured() bool                              { return true }
func (p *contextProbe) models(context.Context) ([]modelOption, error) { return nil, nil }
func (p *contextProbe) complete(_ context.Context, m []chatMessage, _ string) (string, error) {
	p.messages = m
	return "hello", nil
}
func TestTrustedDateContext(t *testing.T) {
	p := &contextProbe{}
	rec := httptest.NewRecorder()
	chatHandler(p).ServeHTTP(rec, httptest.NewRequest("POST", "/api/chat", strings.NewReader(`{"messages":[{"role":"user","content":"what day?"}]}`)))
	if len(p.messages) != 2 || p.messages[0].Role != "system" {
		t.Fatalf("missing trusted context: %#v", p.messages)
	}
	for _, want := range []string{time.Now().Format("2006-01-02"), "timezone", "knowledge", "verification"} {
		if !strings.Contains(p.messages[0].Content, want) {
			t.Errorf("context missing %q", want)
		}
	}
}

func TestRunLifecycleOwnerScopeAndCapture(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"answer"}}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6},"private":"not retained"}`))
	}))
	defer upstream.Close()
	t.Setenv("APP_DATA_DIR", t.TempDir())
	t.Setenv("LLM_BASE_URL", upstream.URL)
	t.Setenv("LLM_MODEL", "actual-model")
	t.Setenv("LLM_API_KEY", "never-retain-key")
	h := newServer()
	reg := runRequest(h, "POST", "/api/auth/register", `{"username":"run-owner","password":"correct-horse-battery"}`, nil)
	cookie := reg.Result().Cookies()[0]
	const id = "16334567-1234-4567-890a-123456789abc"
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- runRequest(h, "POST", "/api/chat", `{"run_id":"`+id+`","model":"browser-claim","messages":[{"role":"user","content":"question"}]}`, cookie)
	}()
	select {
	case <-entered:
	case response := <-done:
		t.Fatalf("provider not reached: %d %s", response.Code, response.Body)
	case <-time.After(5 * time.Second):
		t.Fatal("provider not reached")
	}
	running := runRequest(h, "GET", "/api/runs/"+id, "", cookie)
	run := decodeRunMap(t, running)
	if run["state"] != "running" || run["model"] != "actual-model" || run["request"] == nil {
		t.Errorf("running trace=%#v", run)
	}
	close(release)
	reply := <-done
	if reply.Code != 200 || reply.Header().Get("X-Agent-Run-ID") != id {
		t.Fatalf("chat=%d %s", reply.Code, reply.Body)
	}
	got := runRequest(h, "GET", "/api/runs/"+id, "", cookie)
	run = decodeRunMap(t, got)
	if run["state"] != "completed" || run["usage"].(map[string]any)["total_tokens"] != float64(6) {
		t.Errorf("completed trace=%#v", run)
	}
	for _, secret := range []string{"never-retain-key", "not retained", "browser-claim"} {
		if strings.Contains(got.Body.String(), secret) {
			t.Errorf("retained %q", secret)
		}
	}
	if got.Header().Get("Cache-Control") != "no-store" || got.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("unsafe headers")
	}
	other := runRequest(h, "POST", "/api/auth/register", `{"username":"other-owner","password":"correct-horse-battery"}`, nil).Result().Cookies()[0]
	if w := runRequest(h, "GET", "/api/runs/"+id, "", other); w.Code != 404 {
		t.Errorf("owner leak: %d", w.Code)
	}
	if w := runRequest(h, "GET", "/api/runs/"+id, "", nil); w.Code != 401 {
		t.Errorf("unauthenticated: %d", w.Code)
	}
	var result map[string]string
	json.Unmarshal(reply.Body.Bytes(), &result)
	history := runRequest(h, "GET", "/api/conversations/"+result["conversation_id"], "", cookie)
	if !strings.Contains(history.Body.String(), `"run_id":"`+id+`"`) {
		t.Errorf("missing message run link: %s", history.Body)
	}
	runRequest(h, "DELETE", "/api/conversations/"+result["conversation_id"], "", cookie)
	if w := runRequest(h, "GET", "/api/runs/"+id, "", cookie); w.Code != 404 {
		t.Error("deleted conversation retained trace")
	}
}

func decodeRunMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	run, ok := result["run"].(map[string]any)
	if !ok {
		t.Fatalf("no run: %s", rec.Body)
	}
	return run
}
func runRequest(h http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
