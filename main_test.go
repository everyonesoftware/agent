package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthzCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/healthz" {
			t.Errorf("unexpected health request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PORT", port)
	// The probe must not initialize account storage.
	t.Setenv("APP_DATA_DIR", "")
	if err := run([]string{"healthz"}); err != nil {
		t.Fatalf("healthz failed: %v", err)
	}
}

func TestHealthzCommandRejectsUnhealthyResponses(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusFound, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "/healthy")
				w.WriteHeader(status)
			}))
			defer server.Close()
			_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("PORT", port)
			if err := run([]string{"healthz"}); err == nil || !strings.Contains(err.Error(), "HTTP") {
				t.Fatalf("expected unhealthy HTTP response, got %v", err)
			}
		})
	}
}

func TestHealthzCommandRejectsUnavailableServer(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	server.Close()
	t.Setenv("PORT", port)
	if err := run([]string{"healthz"}); err == nil {
		t.Fatal("expected unavailable server to fail")
	}
}

func TestHealthzCommandTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PORT", port)
	start := time.Now()
	if err := run([]string{"healthz"}); err == nil {
		t.Fatal("expected timeout")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("health probe took too long: %v", elapsed)
	}
}

func TestUnknownCommand(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"healthz", "extra"}} {
		if err := run(args); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Fatalf("run(%q) = %v, want usage error", args, err)
		}
	}
}
