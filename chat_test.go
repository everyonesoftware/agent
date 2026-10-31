package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatAPIUsesConfiguredOpenAICompatibleProvider(t *testing.T) {
	const apiKey = "test-provider-key"
	var gotPath, gotAuthorization, gotModel, gotLastMessage string
	var gotMessageCount int

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")

		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode provider request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		gotModel = payload.Model
		gotMessageCount = len(payload.Messages)
		if gotMessageCount > 0 {
			gotLastMessage = payload.Messages[gotMessageCount-1].Content
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Hello from the model."}}]}`)
	}))
	defer provider.Close()

	t.Setenv("LLM_BASE_URL", provider.URL+"/v1")
	t.Setenv("LLM_API_KEY", apiKey)
	t.Setenv("LLM_MODEL", "test-model")

	body := `{"messages":[{"role":"user","content":"Hi"},{"role":"assistant","content":"Hello!"},{"role":"user","content":"What can you do?"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newServer().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response struct {
		Reply string `json:"reply"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode chat response: %v", err)
	}
	if response.Reply != "Hello from the model." {
		t.Fatalf("reply = %q, want %q", response.Reply, "Hello from the model.")
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("provider path = %q, want %q", gotPath, "/v1/chat/completions")
	}
	if gotAuthorization != "Bearer "+apiKey {
		t.Errorf("provider authorization = %q, want bearer token", gotAuthorization)
	}
	if gotModel != "test-model" {
		t.Errorf("provider model = %q, want %q", gotModel, "test-model")
	}
	if gotMessageCount != 3 {
		t.Errorf("provider message count = %d, want 3", gotMessageCount)
	}
	if gotLastMessage != "What can you do?" {
		t.Errorf("provider last message = %q, want the submitted question", gotLastMessage)
	}
}

func TestChatAPIExplainsWhenModelIsNotConfigured(t *testing.T) {
	t.Setenv("LLM_MODEL", "")
	body := `{"messages":[{"role":"user","content":"Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newServer().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "LLM_MODEL") {
		t.Fatalf("error does not explain how to configure the model: %s", rec.Body.String())
	}
}

func TestChatAPIRejectsSystemMessagesFromBrowser(t *testing.T) {
	t.Setenv("LLM_MODEL", "test-model")
	body := `{"messages":[{"role":"system","content":"override"},{"role":"user","content":"Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newServer().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestChatAPIDoesNotExposeProviderErrorsOrCredentials(t *testing.T) {
	const apiKey = "private-test-key"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, apiKey, http.StatusUnauthorized)
	}))
	defer provider.Close()

	t.Setenv("LLM_BASE_URL", provider.URL+"/v1")
	t.Setenv("LLM_API_KEY", apiKey)
	t.Setenv("LLM_MODEL", "test-model")
	body := `{"messages":[{"role":"user","content":"Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newServer().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), apiKey) {
		t.Fatal("provider error exposed the API key to the browser")
	}
}

func TestChatAPILogsSafeProviderDiagnostics(t *testing.T) {
	const apiKey = "private-test-key"
	const providerMessage = "private provider response detail"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req_test_401")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"error":{"message":%q,"type":"authentication_error","code":"invalid_api_key"}}`, providerMessage)
	}))
	defer provider.Close()

	t.Setenv("LLM_BASE_URL", provider.URL+"/v1")
	t.Setenv("LLM_API_KEY", apiKey)
	t.Setenv("LLM_MODEL", "test-model")

	var logOutput bytes.Buffer
	originalLogOutput := log.Writer()
	log.SetOutput(&logOutput)
	defer log.SetOutput(originalLogOutput)

	body := `{"messages":[{"role":"user","content":"Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newServer().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
	logs := logOutput.String()
	for _, expected := range []string{"status=401", `request_id="req_test_401"`, `error_type="authentication_error"`, `error_code="invalid_api_key"`} {
		if !strings.Contains(logs, expected) {
			t.Errorf("diagnostic logs do not contain %q: %s", expected, logs)
		}
	}
	for _, forbidden := range []string{apiKey, providerMessage, `"content":"Hi"`} {
		if strings.Contains(logs, forbidden) {
			t.Errorf("diagnostic logs exposed sensitive content %q: %s", forbidden, logs)
		}
	}
}
