package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

func TestResearchBudgetApprovedResearchContinuesWithEvidence(t *testing.T) {
	var braveCalls, pageCalls, modelCalls atomic.Int32
	a, s, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) {
		braveCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"web":{"results":[{"title":"Collected evidence","url":"http://example.com/evidence","description":"Evidence from the first allowed search"}]}}`)
	})
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageCalls.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "Retrieved public-page evidence after the search cap")
	}))
	defer page.Close()
	factory := s.settings.webFactory
	s.settings.webFactory = func(key string) *researchWeb {
		web := factory(key)
		web.lookupIP = func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}
		web.dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(page.URL, "http://"))
		}
		return web
	}
	saved, b := budgetResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		messages := string(body["messages"])
		switch modelCalls.Add(1) {
		case 1:
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"search1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"first evidence\"}"}}]}}]}`)
		case 2:
			if !strings.Contains(messages, "Collected evidence") {
				t.Error("allowed evidence missing", messages)
			}
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"search2","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"another search\"}"}}]}}]}`)
		case 3:
			for _, text := range []string{"Your Agent search request limit has been reached.", "No search was sent.", "Continue with existing evidence or public-page retrieval", "change the cap in Settings or wait until reset", "Collected evidence"} {
				if !strings.Contains(messages, text) {
					t.Error("actionable cap result/evidence missing", text, messages)
				}
			}
			for _, private := range []string{settingsTestKey, "tracking_started_at", "maximum", "reserved", "X-RateLimit"} {
				if strings.Contains(messages, private) {
					t.Error("budget/provider metadata leaked", private)
				}
			}
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"fetch1","type":"function","function":{"name":"web_fetch","arguments":"{\"url\":\"http://example.com/evidence\"}"}}]}}]}`)
		default:
			if !strings.Contains(messages, "Retrieved public-page evidence after the search cap") {
				t.Error("page retrieval did not continue", messages)
			}
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Answer based on collected evidence despite the search cap"}}]}`)
		}
	}))
	defer model.Close()
	p := &openAICompatibleProvider{baseURL: model.URL, model: "fixture", client: model.Client()}
	h.(*http.ServeMux).Handle("POST /api/chat", a.middleware(chatHandlerWithResearch(p, a, s)))
	payload := `{"messages":[{"role":"user","content":"Research with public evidence"}],"run_id":"00000000-0000-0000-0000-000000000082"}`
	id := researchApproved(t, h, c, payload)
	// Changing the cap after approval must not change credential consent or revoke the task.
	b.Enabled = true
	b.Maximum = 2
	budgetSave(t, h, c, saved.Revision, b)
	rr := researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Answer based on collected evidence") {
		t.Fatal(rr.Code, rr.Body.String())
	}
	_, b = budgetRead(t, h, c)
	if braveCalls.Load() != 2 || pageCalls.Load() != 1 || modelCalls.Load() != 4 || b.Used != 2 || b.Reserved != 0 {
		t.Fatalf("wrong request accounting: Brave=%d page=%d model=%d budget=%+v", braveCalls.Load(), pageCalls.Load(), modelCalls.Load(), b)
	}
}
