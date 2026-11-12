package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func researchRequest(h http.Handler, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func researchServer(t *testing.T) (http.Handler, *http.Cookie) {
	t.Helper()
	t.Setenv("APP_DATA_DIR", t.TempDir())
	h := newServer()
	w := researchRequest(h, nil, "POST", "/api/auth/register", `{"username":"researcher","password":"correct-horse-battery"}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	return h, w.Result().Cookies()[0]
}
func TestResearchConsentCreationAndApproval(t *testing.T) {
	h, c := researchServer(t)
	w := researchRequest(h, c, "GET", "/api/research", "")
	if w.Code != 200 {
		t.Fatalf("capability status=%d body=%s", w.Code, w.Body.String())
	}
	w = researchRequest(h, c, "POST", "/api/research/tasks", `{"messages":[{"role":"user","content":"Read https://example.com"}]}`)
	if w.Code != 201 {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var v struct {
		Task struct {
			ID, Status, Scope string
			SearchAvailable   bool `json:"search_available"`
		} `json:"task"`
	}
	json.Unmarshal(w.Body.Bytes(), &v)
	if v.Task.Status != "pending" || v.Task.Scope != "public_web" || v.Task.SearchAvailable {
		t.Fatal(w.Body.String())
	}
	w = researchRequest(h, c, "POST", "/api/research/tasks/"+v.Task.ID+"/approve", `{}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"approved"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestResearchDeniedBeforeProviderIO(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"choices":[{"message":{"content":"should not happen"}}]}`))
	}))
	defer upstream.Close()
	t.Setenv("LLM_BASE_URL", upstream.URL)
	t.Setenv("LLM_MODEL", "fixture")
	h, c := researchServer(t)
	payload := `{"messages":[{"role":"user","content":"research"}],"run_id":"00000000-0000-0000-0000-000000000001"}`
	created := researchRequest(h, c, "POST", "/api/research/tasks", payload)
	var v struct{ Task struct{ ID string } }
	json.Unmarshal(created.Body.Bytes(), &v)
	if calls.Load() != 0 {
		t.Fatal("creation performed network I/O")
	}
	for _, tc := range []struct{ name, id string }{{"pending", v.Task.ID}, {"missing", "missing"}} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.TrimSuffix(payload, "}") + `,"research_task_id":"` + tc.id + `"}`
			w := researchRequest(h, c, "POST", "/api/chat", body)
			if w.Code != 403 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	researchRequest(h, c, "POST", "/api/research/tasks/"+v.Task.ID+"/approve", `{}`)
	changed := strings.Replace(payload, "research", "changed", 1)
	changed = strings.TrimSuffix(changed, "}") + `,"research_task_id":"` + v.Task.ID + `"}`
	if w := researchRequest(h, c, "POST", "/api/chat", changed); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	if calls.Load() != 0 {
		t.Fatalf("denied requests made %d network calls", calls.Load())
	}
}
