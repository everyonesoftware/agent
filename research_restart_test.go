package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestResearchRestartTerminalizesCallTraces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.sqlite")
	accounts, err := newAccountStore(path)
	if err != nil {
		t.Fatal(err)
	}
	user, _, err := accounts.register("restart-research", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	c, err := accounts.createRun(user, "", "", []chatMessage{{Role: "system", Content: "research instructions"}, {Role: "user", Content: "public query"}}, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	c.run.Research = &RunResearch{TaskID: "fixture-task"}
	c.run.ModelCalls = []RunResearchModelCall{{Index: 1, State: "running"}}
	c.run.ToolCalls = []RunResearchToolCall{{ID: "call_1", Name: "web_fetch", State: "running"}}
	c.persist()
	accounts.db.Close()
	accounts, err = newAccountStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer accounts.db.Close()
	var b []byte
	if err = accounts.db.QueryRow(`SELECT data FROM runs WHERE id=?`, c.run.ID).Scan(&b); err != nil {
		t.Fatal(err)
	}
	var run Run
	json.Unmarshal(b, &run)
	if run.State != "interrupted" || run.ModelCalls[0].State != "interrupted" || run.ToolCalls[0].State != "interrupted" {
		t.Fatal("stale running call after restart", string(b))
	}
}
func TestResearchRestartDropsPermission(t *testing.T) {
	h, c := researchServer(t)
	payload := `{"messages":[{"role":"user","content":"Research"}]}`
	id := researchApproved(t, h, c, payload)
	// The same persistent authentication database, but a new server owns a new
	// in-memory research service. Previously approved grants must not survive.
	h = newServer()
	w := researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id))
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
}
