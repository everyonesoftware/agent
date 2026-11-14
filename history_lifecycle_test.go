package main

import (
	"net/http/httptest"
	"testing"
)

func TestServerUsageSamplingHasExplicitLifecycle(t *testing.T) {
	t.Setenv("APP_DATA_DIR", t.TempDir())
	handler := newServer()
	managed, ok := handler.(interface {
		startBackground()
		close()
	})
	if !ok {
		t.Fatal("server has no lifecycle for stopping background quota sampling")
	}
	server := handler.(*applicationServer)
	if server.usage == nil || server.usage.samplerCancel != nil {
		t.Fatal("handler construction unexpectedly started background collection")
	}
	// Constructing isolated handlers must not create orphan workers. The actual
	// server command starts background collection and closes it on exit.
	managed.startBackground()
	managed.startBackground()
	if server.usage.samplerCancel == nil {
		t.Fatal("server did not start background collection")
	}
	managed.close()
	managed.close()
	select {
	case <-server.usage.samplerDone:
	default:
		t.Fatal("sampler survived server close")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "http://127.0.0.1/healthz", nil))
	if recorder.Code != 200 {
		t.Fatalf("health status %d", recorder.Code)
	}
}
