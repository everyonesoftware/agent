package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func researchHarness(t *testing.T, p modelProvider, web researchWebClient) (http.Handler, *http.Cookie, *researchService) {
	t.Helper()
	accounts, e := newAccountStore(filepath.Join(t.TempDir(), "research.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { accounts.db.Close() })
	s := newResearchService(web)
	mux := http.NewServeMux()
	s.register(mux, accounts)
	mux.HandleFunc("POST /api/auth/register", accounts.registerHandler)
	mux.Handle("POST /api/chat", accounts.middleware(chatHandlerWithResearch(p, accounts, s)))
	mux.Handle("GET /api/conversations", accounts.middleware(http.HandlerFunc(accounts.listConversations)))
	mux.Handle("GET /api/runs/{id}", accounts.middleware(http.HandlerFunc(accounts.runHandler)))
	w := researchRequest(mux, nil, "POST", "/api/auth/register", `{"username":"research-fixture","password":"correct-horse-battery"}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	return mux, w.Result().Cookies()[0], s
}
func researchApproved(t *testing.T, h http.Handler, c *http.Cookie, payload string) string {
	t.Helper()
	w := researchRequest(h, c, "POST", "/api/research/tasks", payload)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var v struct{ Task struct{ ID string } }
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Task.ID == "" {
		t.Fatal(w.Body.String())
	}
	w = researchRequest(h, c, "POST", "/api/research/tasks/"+v.Task.ID+"/approve", `{}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	return v.Task.ID
}
func researchWithTask(payload, id string) string {
	return strings.TrimSuffix(payload, "}") + `,"research_task_id":"` + id + `"}`
}
func TestResearchStructuredRetrievalLoop(t *testing.T) {
	var modelCalls, webCalls atomic.Int32
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webCalls.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "Fixture public evidence")
	}))
	defer page.Close()
	web := newResearchWeb()
	web.lookupIP = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	web.dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(page.URL, "http://"))
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := modelCalls.Add(1)
		var body map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/chat/completions" {
			t.Error(r.URL.Path)
		}
		if strings.Contains(string(body["messages"]), "private old secret") {
			t.Error("private history disclosed")
		}
		if !strings.Contains(string(body["tools"]), "web_fetch") || strings.Contains(string(body["tools"]), "web_search") {
			t.Error("incorrect retrieval-only capabilities", string(body["tools"]))
		}
		if n == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"web_fetch","arguments":"{\"url\":\"http://example.com/\"}"},"private_reasoning":"PRIVATE-CALL-DETAIL"}]}}]}`)
			return
		}
		if strings.Contains(string(body["messages"]), "PRIVATE-CALL-DETAIL") {
			t.Error("private provider fields propagated")
		}
		if !strings.Contains(string(body["messages"]), "Fixture public evidence") || !strings.Contains(string(body["messages"]), `"tool_call_id":"call_1"`) {
			t.Error("tool output missing", string(body["messages"]))
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Verified fixture answer"}}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`)
	}))
	defer upstream.Close()
	p := &openAICompatibleProvider{baseURL: upstream.URL, model: "fixture", client: upstream.Client()}
	h, c, _ := researchHarness(t, p, web)
	payload := `{"messages":[{"role":"user","content":"private old secret"},{"role":"assistant","content":"old reply"},{"role":"user","content":"Read http://example.com/"}],"run_id":"00000000-0000-0000-0000-000000000002"}`
	id := researchApproved(t, h, c, payload)
	w := researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Verified fixture answer") {
		t.Fatal(w.Code, w.Body.String())
	}
	if modelCalls.Load() != 2 || webCalls.Load() != 1 {
		t.Fatal("calls", modelCalls.Load(), webCalls.Load())
	}
	w = researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id))
	if w.Code != 403 {
		t.Fatal("replay", w.Code, w.Body.String())
	}
	w = researchRequest(h, c, "GET", "/api/research/tasks/"+id, "")
	if !strings.Contains(w.Body.String(), `"status":"completed"`) {
		t.Fatal(w.Body.String())
	}
	w = researchRequest(h, c, "GET", "/api/runs/00000000-0000-0000-0000-000000000002", "")
	var trace struct {
		Run struct {
			ModelCalls []json.RawMessage `json:"model_calls"`
			ToolCalls  []json.RawMessage `json:"tool_calls"`
			Research   map[string]any    `json:"research"`
			Usage      RunUsage          `json:"usage"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &trace)
	if len(trace.Run.ModelCalls) != 2 || len(trace.Run.ToolCalls) != 1 || trace.Run.Research["task_id"] != id || trace.Run.Usage.TotalTokens != 13 {
		t.Fatal("incomplete research trace", w.Body.String())
	}
}
