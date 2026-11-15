package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestMemoryResponsesActualWireContext(t *testing.T) {
	f, provider, _, audit := codexInferenceFixture(t, "chat")
	store, user := f.accounts, f.user
	// Use the same explicit-save service as the authenticated Memory API.
	if _, err := store.saveMemory(user, "user", "I prefer terse explanations. oauth-jade-735"); err != nil {
		t.Fatal(err)
	}
	handler := store.middleware(chatHandlerWithStore(provider, store))
	response := runRequest(handler, "POST", "/api/chat", `{"messages":[{"role":"user","content":"What style should you use?"}]}`, f.cookie)
	if response.Code != 200 {
		t.Fatalf("chat: %d %s", response.Code, response.Body)
	}
	wireBytes, err := os.ReadFile(audit)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(wireBytes)
	if !strings.Contains(wire, "oauth-jade-735") {
		t.Fatalf("missing memory in OAuth wire: %s", wire)
	}
	r := httptest.NewRequest("GET", "/api/runs/"+response.Header().Get("X-Agent-Run-ID"), nil)
	r.SetPathValue("id", response.Header().Get("X-Agent-Run-ID"))
	r.AddCookie(f.cookie)
	trace := httptest.NewRecorder()
	store.middleware(http.HandlerFunc(store.runHandler)).ServeHTTP(trace, r)
	run := decodeRunMap(t, trace)
	if !strings.Contains(trace.Body.String(), "oauth-jade-735") || run["provider"] != "chatgpt" {
		t.Fatal("OAuth trace missing memory/provider")
	}
	var instructions, turnText string
	for _, line := range strings.Split(strings.TrimSpace(wire), "\n") {
		var call struct {
			Method string
			Params json.RawMessage
		}
		if json.Unmarshal([]byte(line), &call) != nil {
			t.Fatal("invalid RPC audit")
		}
		if call.Method == "thread/start" {
			var p struct{ BaseInstructions string }
			if err := json.Unmarshal(call.Params, &p); err != nil {
				t.Fatal(err)
			}
			instructions = p.BaseInstructions
		}
		if call.Method == "turn/start" {
			var p struct{ Input []struct{ Text string } }
			if err := json.Unmarshal(call.Params, &p); err != nil || len(p.Input) != 1 {
				t.Fatalf("invalid turn input: %s", call.Params)
			}
			turnText = p.Input[0].Text
		}
	}
	requestBytes, _ := json.Marshal(run["request"])
	var request struct {
		Model    string
		Messages []chatMessage
	}
	if json.Unmarshal(requestBytes, &request) != nil || request.Model != "codex-fixture" || len(request.Messages) < 2 {
		t.Fatalf("invalid normalized trace: %s", requestBytes)
	}
	var history []chatMessage
	for _, message := range request.Messages {
		if message.Role == "system" || message.Role == "developer" {
			if !strings.Contains(instructions, message.Content) {
				t.Fatal("trace instructions differ from RPC")
			}
		} else {
			history = append(history, message)
		}
	}
	if len(history) != 1 || history[0].Role != "user" || turnText != history[0].Content {
		t.Fatal("normalized trace history differs from actual RPC request")
	}
}
