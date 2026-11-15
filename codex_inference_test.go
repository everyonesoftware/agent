package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCodexInferenceHelper(t *testing.T) {
	if os.Getenv("GO_CODEX_INFERENCE_HELPER") != "1" {
		return
	}
	defer os.Exit(0)
	mode := os.Getenv("GO_CODEX_INFERENCE_MODE")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	send := func(v any) { b, _ := json.Marshal(v); fmt.Fprintln(os.Stdout, string(b)) }
	event := func(method string, p any) { send(map[string]any{"method": method, "params": p}) }
	finish := func() {
		answer := "Codex fixture answer"
		if mode == "suggestions" {
			answer = `{"suggestions":["I prefer concise explanations."]}`
		}
		if mode == "large-chat" {
			answer = strings.Repeat("x", maxResearchOutputBytes+1)
		}
		event("thread/tokenUsage/updated", map[string]any{"threadId": "thread-fixture", "turnId": "turn-fixture", "tokenUsage": map[string]any{"last": map[string]int{"inputTokens": 17, "outputTokens": 5, "totalTokens": 22}, "total": map[string]int{"inputTokens": 27, "outputTokens": 9, "totalTokens": 36}}})
		event("item/completed", map[string]any{"threadId": "thread-fixture", "turnId": "turn-fixture", "item": map[string]any{"type": "agentMessage", "id": "answer", "text": answer, "phase": "final_answer"}})
		event("turn/completed", map[string]any{"threadId": "thread-fixture", "turn": map[string]any{"id": "turn-fixture", "status": "completed"}})
	}
	for scanner.Scan() {
		var m struct {
			ID     json.RawMessage
			Method string
			Params map[string]json.RawMessage
			Result json.RawMessage
			Error  json.RawMessage
		}
		if json.Unmarshal(scanner.Bytes(), &m) != nil {
			os.Exit(2)
		}
		if audit := os.Getenv("GO_CODEX_AUDIT"); audit != "" {
			f, _ := os.OpenFile(audit, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			f.Write(append(scanner.Bytes(), '\n'))
			f.Close()
		}
		response := func(v any) { send(map[string]any{"id": m.ID, "result": v}) }
		switch m.Method {
		case "initialize":
			response(map[string]any{"userAgent": "fixture"})
		case "initialized":
		case "account/read":
			response(map[string]any{"account": map[string]any{"type": "chatgpt", "email": "fixture@example.com", "planType": "plus"}})
		case "model/list":
			response(map[string]any{"data": []any{map[string]any{"id": "opaque-catalog-id", "model": "codex-fixture", "displayName": "Codex fixture", "isDefault": true, "hidden": false}, map[string]any{"model": "hidden-fixture", "hidden": true}}, "nextCursor": nil})
		case "thread/start":
			if string(m.Params["environments"]) != "[]" || string(m.Params["sandbox"]) != `"read-only"` || string(m.Params["approvalPolicy"]) != `"never"` || string(m.Params["ephemeral"]) != "true" {
				response(nil)
				os.Exit(4)
			}
			response(map[string]any{"thread": map[string]any{"id": "thread-fixture"}})
		case "thread/inject_items":
			response(map[string]any{})
		case "turn/start":
			response(map[string]any{"turn": map[string]any{"id": "turn-fixture", "status": "inProgress"}})
			switch mode {
			case "hang":
			case "large-delta":
				for i := 0; i < 2; i++ {
					event("item/agentMessage/delta", map[string]any{"threadId": "thread-fixture", "turnId": "turn-fixture", "itemId": "answer", "delta": strings.Repeat("x", 16<<10)})
				}
				finish()
			case "retry":
				event("error", map[string]any{"threadId": "thread-fixture", "turnId": "turn-fixture", "willRetry": true, "error": map[string]any{"message": "provider-secret", "codexErrorInfo": "serverOverloaded"}})
				finish()
			case "unauthorized":
				event("error", map[string]any{"threadId": "thread-fixture", "turnId": "turn-fixture", "willRetry": false, "error": map[string]any{"message": "provider-secret", "codexErrorInfo": "unauthorized"}})
			case "failure":
				event("turn/completed", map[string]any{"threadId": "thread-fixture", "turn": map[string]any{"id": "turn-fixture", "status": "failed", "error": map[string]any{"message": "provider-secret"}}})
			case "research":
				send(map[string]any{"id": "dynamic-request", "method": "item/tool/call", "params": map[string]any{"threadId": "thread-fixture", "turnId": "turn-fixture", "callId": "fetch-call", "tool": "web_fetch", "arguments": map[string]string{"url": "https://example.com/evidence"}}})
			case "malicious":
				send(map[string]any{"id": "dynamic-request", "method": "item/commandExecution/requestApproval", "params": map[string]any{"threadId": "thread-fixture", "turnId": "turn-fixture"}})
			case "builtin":
				event("item/started", map[string]any{"threadId": "thread-fixture", "turnId": "turn-fixture", "item": map[string]any{"type": "fileChange", "id": "forbidden-builtin"}})
				finish()
			default:
				finish()
			}
		case "":
			if mode == "research" {
				if len(m.Result) == 0 {
					os.Exit(5)
				}
				finish()
			} else if mode == "malicious" {
				if len(m.Error) == 0 {
					os.Exit(6)
				}
			}
		default:
			os.Exit(7)
		}
	}
}

func codexInferenceFixture(t *testing.T, mode string) (connectionFixture, *responsesProvider, *usageService, string) {
	t.Helper()
	f, u, _ := newUsageFixture(t)
	entry, err := u.session(f.user, f.id)
	if err != nil {
		t.Fatal(err)
	}
	entry.mu.Lock()
	id, _ := randomURLToken(32)
	if err = u.saveRecord(entry, usageRecord{ConnectionID: id}); err != nil {
		t.Fatal(err)
	}
	entry.mu.Unlock()
	if err = os.MkdirAll(filepath.Join(entry.dir, "codex-home"), 0700); err != nil {
		t.Fatal(err)
	}
	audit := filepath.Join(t.TempDir(), "rpc.jsonl")
	u.inferenceFactory = func(ctx context.Context, _, home string) (*codexUsageRPC, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCodexInferenceHelper$")
		cmd.Env = append(os.Environ(), "GO_CODEX_INFERENCE_HELPER=1", "GO_CODEX_INFERENCE_MODE="+mode, "GO_CODEX_AUDIT="+audit)
		cmd.Dir = home
		rpc, err := startCodexProtocolCommand(ctx, cmd, true)
		if err != nil {
			return nil, err
		}
		return rpc.(*codexUsageRPC), nil
	}
	service := f.accounts.siwcForUser(u.base, f.user)
	service.codex = u
	service.owner = f.user
	return f, &responsesProvider{auth: service}, u, audit
}
func TestCodexModelsAndChatUseRPC(t *testing.T) {
	_, p, _, audit := codexInferenceFixture(t, "chat")
	models, err := p.models(context.Background())
	if err != nil || len(models) != 1 || models[0].ID != "codex-fixture" {
		t.Fatalf("models %+v %v", models, err)
	}
	text, err := p.complete(context.Background(), []chatMessage{{Role: "system", Content: "private memory context"}, {Role: "user", Content: "hello"}}, "")
	if err != nil || text != "Codex fixture answer" {
		t.Fatalf("chat %q %v", text, err)
	}
	b, err := os.ReadFile(audit)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "private memory context") || strings.Contains(string(b), "access_token") {
		t.Fatalf("invalid trace %s", b)
	}
}
func TestCodexConversationRolesArePreserved(t *testing.T) {
	_, p, _, audit := codexInferenceFixture(t, "chat")
	messages := []chatMessage{{Role: "system", Content: "trusted server instructions"}, {Role: "user", Content: "earlier user request"}, {Role: "assistant", Content: "previous assistant answer"}, {Role: "user", Content: "final user request"}}
	if _, err := p.complete(context.Background(), messages, ""); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(audit)
	injected := false
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var m struct {
			Method string
			Params struct {
				Items []struct {
					Type    string
					Role    string
					Content []struct {
						Type string
						Text string
					}
				}
				Input            []struct{ Text string }
				BaseInstructions string
			}
		}
		if json.Unmarshal([]byte(line), &m) != nil {
			t.Fatal("invalid audit")
		}
		switch m.Method {
		case "thread/start":
			if m.Params.BaseInstructions != "trusted server instructions\n" {
				t.Fatalf("instructions %q", m.Params.BaseInstructions)
			}
		case "thread/inject_items":
			injected = true
			if len(m.Params.Items) != 2 || m.Params.Items[0].Role != "user" || m.Params.Items[0].Content[0].Type != "input_text" || m.Params.Items[1].Role != "assistant" || m.Params.Items[1].Content[0].Type != "output_text" {
				t.Fatalf("history role provenance %s", line)
			}
		case "turn/start":
			if len(m.Params.Input) != 1 || m.Params.Input[0].Text != "final user request" {
				t.Fatalf("final request flattened: %s", line)
			}
		}
	}
	if !injected {
		t.Fatal("role-bearing history was not injected")
	}
}

func TestCodexResearchStructuredToolRoundtrip(t *testing.T) {
	_, p, _, audit := codexInferenceFixture(t, "research")
	session, err := newCodexModelSession(context.Background(), p, []chatMessage{{Role: "user", Content: "Read https://example.com/evidence"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer session.close()
	first, err := session.step(context.Background(), researchTools(false, false))
	if err != nil || len(first.Calls) != 1 || first.Calls[0].Name != "web_fetch" {
		t.Fatalf("tool %+v %v", first, err)
	}
	if _, err = researchArgument(first.Calls[0], false); err != nil {
		t.Fatal(err)
	}
	session.result(first.Calls[0], `{"url":"https://example.com/evidence","title":"Evidence","content":"public evidence"}`)
	final, err := session.step(context.Background(), researchTools(false, false))
	if err != nil || final.Text != "Codex fixture answer" || final.Usage == nil || final.Usage.TotalTokens != 36 {
		t.Fatalf("final %+v %v", final, err)
	}
	b, _ := os.ReadFile(audit)
	if !strings.Contains(string(b), "inputText") || !strings.Contains(string(b), "public evidence") {
		t.Fatalf("tool reply absent %s", b)
	}
}
func TestCodexNormalChatNotSubjectToResearchOutputCap(t *testing.T) {
	_, p, _, _ := codexInferenceFixture(t, "large-chat")
	text, err := p.complete(context.Background(), []chatMessage{{Role: "user", Content: "hello"}}, "")
	if err != nil || len(text) != maxResearchOutputBytes+1 {
		t.Fatalf("normal output bytes %d %v", len(text), err)
	}
}
func TestCodexResearchOutputCapAppliesToStreamingDeltas(t *testing.T) {
	_, p, _, _ := codexInferenceFixture(t, "large-delta")
	session, err := newCodexModelSession(context.Background(), p, []chatMessage{{Role: "user", Content: "hello"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer session.close()
	text, err := session.step(context.Background(), researchTools(false, false))
	if err == nil {
		t.Fatalf("oversized research stream accepted: %+v", text)
	}
}

func TestCodexNativeRetryAndExpiredConnection(t *testing.T) {
	t.Run("retry", func(t *testing.T) {
		_, p, _, _ := codexInferenceFixture(t, "retry")
		text, err := p.complete(context.Background(), []chatMessage{{Role: "user", Content: "hello"}}, "")
		if err != nil || text != "Codex fixture answer" {
			t.Fatalf("native retry %q %v", text, err)
		}
	})
	t.Run("unauthorized", func(t *testing.T) {
		_, p, _, _ := codexInferenceFixture(t, "unauthorized")
		_, err := p.complete(context.Background(), []chatMessage{{Role: "user", Content: "hello"}}, "")
		if err != errChatGPTNotConnected {
			t.Fatalf("expired connection %v", err)
		}
	})
}

func TestCodexFailureAndUndeclaredCapabilitiesFailClosed(t *testing.T) {
	for _, mode := range []string{"failure", "malicious", "builtin"} {
		t.Run(mode, func(t *testing.T) {
			_, p, _, _ := codexInferenceFixture(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if text, err := p.complete(ctx, []chatMessage{{Role: "user", Content: "hello"}}, ""); err == nil || text != "" || strings.Contains(err.Error(), "provider-secret") {
				t.Fatalf("unsafe result %q %v", text, err)
			}
		})
	}
}
func TestCodexUnifiedUsageConnectCannotReplaceCredentials(t *testing.T) {
	f, _, u, _ := codexInferenceFixture(t, "chat")
	f.accounts.codex = u
	entry, err := u.session(f.user, f.id)
	if err != nil {
		t.Fatal(err)
	}
	before := entry.record.ConnectionID
	marker := filepath.Join(entry.dir, "codex-home", "fixture-credential-marker")
	if err = os.WriteFile(marker, []byte("fixture-only"), 0600); err != nil {
		t.Fatal(err)
	}
	out := f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":"`+before+`"}`, "agent.example.test")
	if out.Code != 410 {
		t.Fatalf("obsolete connect still mutates unified credentials: %d %s", out.Code, out.Body)
	}
	after, err := readUsageRecord(filepath.Join(entry.dir, "connection.json"))
	if err != nil || after.ConnectionID != before {
		t.Fatalf("connection replaced: %+v %v", after, err)
	}
	if _, err = os.Stat(marker); err != nil {
		t.Fatalf("credential home erased: %v", err)
	}
}

func TestCodexReconnectDoesNotEraseUnverifiableLinkedConnection(t *testing.T) {
	f, _, u, _ := codexInferenceFixture(t, "chat")
	entry, err := u.session(f.user, f.id)
	if err != nil {
		t.Fatal(err)
	}
	before := entry.record.ConnectionID
	marker := filepath.Join(entry.dir, "codex-home", "fixture-credential-marker")
	if err = os.WriteFile(marker, []byte("fixture-only"), 0600); err != nil {
		t.Fatal(err)
	}
	u.factory = func(context.Context, string, string) (usageRPC, error) {
		return &fakeUsageRPC{fail: "account/read"}, nil
	}
	mux := http.NewServeMux()
	u.registerAuth(mux)
	f.server = mux
	out := f.request("POST", "/auth/start", `{"expected_active_profile_id":"`+f.id+`"}`, "agent.example.test")
	if out.Code != 503 {
		t.Fatalf("unverifiable account restarted sign-in: %d %s", out.Code, out.Body)
	}
	after, err := readUsageRecord(filepath.Join(entry.dir, "connection.json"))
	if err != nil || after.ConnectionID != before {
		t.Fatalf("connection was replaced: %+v %v", after, err)
	}
	if _, err = os.Stat(marker); err != nil {
		t.Fatalf("registered credential home erased: %v", err)
	}
}

func TestCodexUsageWorkerSaturationDoesNotStarveInference(t *testing.T) {
	_, p, u, _ := codexInferenceFixture(t, "chat")
	for i := 0; i < cap(u.slots); i++ {
		u.slots <- struct{}{}
	}
	defer func() {
		for len(u.slots) > 0 {
			<-u.slots
		}
	}()
	models, err := p.models(context.Background())
	if err != nil || len(models) != 1 {
		t.Fatalf("usage workers starved inference: %+v %v", models, err)
	}
}

func TestCodexCancellationReapsInference(t *testing.T) {
	_, p, u, _ := codexInferenceFixture(t, "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := p.complete(ctx, []chatMessage{{Role: "user", Content: "hello"}}, "")
	if err == nil {
		t.Fatal("cancelled inference succeeded")
	}
	if len(u.inferenceSlots) != 0 {
		t.Fatal("cancelled inference retained worker slot")
	}
}
func TestCodexTwoOwnerHomesCannotCross(t *testing.T) {
	f, p, u, _ := codexInferenceFixture(t, "chat")
	var mu sync.Mutex
	homes := []string{}
	factory := u.inferenceFactory
	u.inferenceFactory = func(ctx context.Context, binary, home string) (*codexUsageRPC, error) {
		mu.Lock()
		homes = append(homes, home)
		mu.Unlock()
		return factory(ctx, binary, home)
	}
	if _, err := p.models(context.Background()); err != nil {
		t.Fatal(err)
	}
	other := authUser{ID: "other-owner"}
	service := newSIWCService(siwcConfig{}, f.store)
	service.codex = u
	service.owner = other
	if _, err := service.availableModels(context.Background()); err != errChatGPTNotConnected {
		t.Fatalf("unregistered owner used another connection: %v", err)
	}
	if len(homes) != 1 || !strings.Contains(homes[0], f.user.ID) || strings.Contains(homes[0], other.ID) {
		t.Fatalf("homes %v", homes)
	}
}

func TestCodexChatMemoryAndRunUsage(t *testing.T) {
	f, p, _, audit := codexInferenceFixture(t, "chat")
	if _, err := f.accounts.saveMemory(f.user, "user", "private-memory-marker"); err != nil {
		t.Fatal(err)
	}
	h := f.accounts.middleware(chatHandlerWithStore(p, f.accounts))
	out := runRequest(h, "POST", "/api/chat", `{"messages":[{"role":"user","content":"hello"}]}`, f.cookie)
	if out.Code != 200 {
		t.Fatalf("chat %d %s", out.Code, out.Body)
	}
	b, _ := os.ReadFile(audit)
	if !strings.Contains(string(b), "private-memory-marker") {
		t.Fatal("memory was not sent to Codex")
	}
	runID := out.Header().Get("X-Agent-Run-ID")
	r := runRequest(f.accounts.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.SetPathValue("id", runID); f.accounts.runHandler(w, r) })), "GET", "/api/runs/"+runID, "", f.cookie)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"total_tokens":36`) || !strings.Contains(r.Body.String(), `"provider":"chatgpt"`) {
		t.Fatalf("usage trace %d %s", r.Code, r.Body)
	}
}

func TestCodexMemorySuggestionsUseManagedInference(t *testing.T) {
	f, p, _, audit := codexInferenceFixture(t, "suggestions")
	h := f.accounts.middleware(f.accounts.memorySuggestionsHandler(p))
	out := runRequest(h, "POST", "/api/memory-suggestions", `{"message":"I prefer concise explanations."}`, f.cookie)
	if out.Code != 200 || !strings.Contains(out.Body.String(), "I prefer concise explanations.") {
		t.Fatalf("suggestions %d %s", out.Code, out.Body)
	}
	b, _ := os.ReadFile(audit)
	if !strings.Contains(string(b), "Extract memory suggestions") {
		t.Fatal("memory instructions absent from Codex request")
	}
}

func TestCodexExistingUsageConnectionIsUnifiedProvider(t *testing.T) {
	f, _, u, _ := codexInferenceFixture(t, "chat")
	profile, err := f.store.LoadProfile(f.id)
	if err != nil {
		t.Fatal(err)
	}
	profile.Credentials.OAuthPreferred = false
	if err = f.store.SaveProfile(profile); err != nil {
		t.Fatal(err)
	}
	provider := &userScopedProvider{accounts: f.accounts, baseAuth: u.base, usage: u}
	ctx := context.WithValue(context.Background(), authContextKey{}, f.user)
	text, err := provider.complete(ctx, []chatMessage{{Role: "user", Content: "hello"}}, "")
	if err != nil || text != "Codex fixture answer" {
		t.Fatalf("existing Codex connection ignored: %q %v", text, err)
	}
}

func TestCodexPasswordDisconnectCancelsAndTombstonesInference(t *testing.T) {
	f, p, u, audit := codexInferenceFixture(t, "hang")
	mux := http.NewServeMux()
	u.registerAuth(mux)
	f.server = mux
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := p.complete(ctx, []chatMessage{{Role: "user", Content: "hello"}}, ""); done <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		b, _ := os.ReadFile(audit)
		if strings.Contains(string(b), "turn/start") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("turn did not start")
		}
		time.Sleep(time.Millisecond)
	}
	bad := f.request("POST", "/api/auth/disconnect", `{"profile_id":"`+f.id+`","password":"wrong-password"}`, "agent.example.test")
	if bad.Code != 401 {
		t.Fatalf("bad password %d", bad.Code)
	}
	out := f.request("POST", "/api/auth/disconnect", `{"profile_id":"`+f.id+`","password":"correct-horse-battery"}`, "agent.example.test")
	if out.Code != 200 {
		t.Fatalf("disconnect %d %s", out.Code, out.Body)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("disconnected inference succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("disconnect left inference running")
	}
	if _, err := p.models(context.Background()); err != errChatGPTNotConnected {
		t.Fatalf("disconnected model listing %v", err)
	}
	entry, err := u.session(f.user, f.id)
	if err != nil {
		t.Fatal(err)
	}
	record, err := readUsageRecord(filepath.Join(entry.dir, "connection.json"))
	if err != nil || record.ConnectionID != "" {
		t.Fatalf("tombstone %+v %v", record, err)
	}
}

func TestCodexServerCloseCancelsInference(t *testing.T) {
	_, p, u, audit := codexInferenceFixture(t, "hang")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := p.complete(ctx, []chatMessage{{Role: "user", Content: "hello"}}, ""); done <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		b, _ := os.ReadFile(audit)
		if strings.Contains(string(b), "turn/start") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("turn did not start")
		}
		time.Sleep(time.Millisecond)
	}
	u.close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("shutdown inference succeeded")
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("server close left inference running")
	}
}
