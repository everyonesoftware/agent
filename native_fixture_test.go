package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in real-server target for native UI acceptance. The fixture is never
// compiled into the shipped executable, listens only on loopback, and uses
// isolated account storage plus a local inference fixture, never live accounts.
func TestNativeClientAcceptanceFixture(t *testing.T) {
	if os.Getenv("AGENT_NATIVE_FIXTURE") != "1" {
		t.Skip("opt-in native acceptance server")
	}
	var completions atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var request struct {
			Messages []chatMessage `json:"messages"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&request) != nil {
			http.Error(w, "invalid fixture request", 400)
			return
		}
		completions.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": "Native acceptance fixture reply. This response traversed the real Agent server and local test provider."}}}, "usage": map[string]int{"prompt_tokens": 24, "completion_tokens": 20, "total_tokens": 44}})
	}))
	defer upstream.Close()
	t.Setenv("APP_DATA_DIR", t.TempDir())
	t.Setenv("LLM_BASE_URL", upstream.URL)
	t.Setenv("LLM_MODEL", "native-fixture-model")
	t.Setenv("LLM_API_KEY", "")
	router := newServer()
	if app, ok := router.(*applicationServer); ok {
		t.Cleanup(app.close)
	}
	port := os.Getenv("AGENT_NATIVE_FIXTURE_PORT")
	if port == "" {
		port = "19370"
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: router, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Error(err)
		}
	}()
	fmt.Printf("NATIVE_ACCEPTANCE_FIXTURE=http://%s (isolated storage, local test provider, no live credentials)\n", listener.Addr())
	// Callers create ordinary test accounts with /api/auth/register. No fixture
	// auth bypass or production test route exists.
	timer := time.NewTimer(20 * time.Minute)
	defer timer.Stop()
	<-timer.C
	t.Logf("Fixture completed: %d actual local provider requests", completions.Load())
}
