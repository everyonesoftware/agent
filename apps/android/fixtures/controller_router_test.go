//go:build android_controller_fixture

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Loaded into the root test package with a Go overlay, never into production.
// The real router, SQLite, cookies and password handlers own both mutations.
func TestAndroidControllerRealRouter(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "fixture makes no provider requests", 503) }))
	defer upstream.Close()
	t.Setenv("LLM_BASE_URL", upstream.URL)
	t.Setenv("LLM_MODEL", "fixture-model")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("CODEX_USAGE_BINARY", filepath.Join(t.TempDir(), "no-live-provider"))
	f := newConnectionFixture(t)
	if app, ok := f.server.(*applicationServer); ok {
		t.Cleanup(app.close)
	}
	original, err := f.store.LoadProfile(f.id)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newProfileID()
	if err != nil {
		t.Fatal(err)
	}
	original.ID = second
	original.Label = "Second fixture account"
	if err = f.store.SaveProfile(original); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(f.server)
	defer server.Close()
	cmd := exec.Command("node", "apps/android/controller-tests.mjs", "AgentControllerRouterTest")
	cmd.Env = append(os.Environ(), "AGENT_CONTROLLER_ROUTER="+server.URL+"/")
	output, err := cmd.CombinedOutput()
	fmt.Print(string(output))
	if err != nil {
		t.Fatalf("Android controller against real router: %v", err)
	}
	active, err := f.store.ActiveProfileID()
	if err != nil || active != second {
		t.Fatalf("switch readback: active=%q expected=%q error=%v", active, second, err)
	}
	profile, err := f.store.LoadProfile(second)
	if err != nil || profile.ID != second {
		t.Fatalf("disconnect must preserve saved profile: %v", err)
	}
}
