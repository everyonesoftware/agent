package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResearchTraceHardEncodedLimit(t *testing.T) {
	store, err := newAccountStore(filepath.Join(t.TempDir(), "accounts.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	user, _, err := store.register("bound-owner", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	c, err := store.createRun(user, "", "", []chatMessage{{Role: "system", Content: runtimeInstructions()}, {Role: "user", Content: strings.Repeat("\x01", 5320)}}, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	c.researchBudget = 16 << 10
	c.run.Research = &RunResearch{TaskID: "fixture-bound"}
	ctx := context.WithValue(context.Background(), runContextKey{}, c)
	body, _ := json.Marshal(map[string]string{"input": strings.Repeat("\x01", 5400)})
	for i := 0; i < 8; i++ {
		traceRequest(ctx, "openai-compatible", strings.Repeat("\x01", 256), body)
		traceResearchModelStart(ctx, "openai-compatible", strings.Repeat("\x01", 256), body)
		traceResearchModelEnd(ctx, researchTurn{Text: strings.Repeat("\x01", 600)}, nil)
		if i < 6 {
			call := researchCall{ID: strings.Repeat("\x01", 128), Name: "web_fetch", Arguments: `{"url":"https://example.com/"}`}
			traceResearchTool(ctx, call, "", "running")
			traceResearchTool(ctx, call, strings.Repeat("\x01", 600), "completed")
		}
	}
	c.finish(strings.Repeat("\x01", 5400), nil)
	var b []byte
	if err = store.db.QueryRow(`SELECT data FROM runs WHERE id=?`, c.run.ID).Scan(&b); err != nil {
		t.Fatal(err)
	}
	if len(b) > 128<<10 {
		t.Fatalf("encoded research trace=%d exceeds 128KiB", len(b))
	}
}

func TestResearchTraceWithinBudgetPreservesCompleteRequest(t *testing.T) {
	c := &runCapture{run: Run{Research: &RunResearch{TaskID: "small-task"}, Request: map[string]any{"model": "fixture", "messages": []chatMessage{{Role: "user", Content: "Read https://example.com/"}}}}}
	data := c.boundResearchSnapshot()
	var got Run
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Request == nil || got.RequestTruncated || got.CaptureOmittedFields != 0 {
		t.Fatal("a trace within its budget must not omit the request or falsely report capture truncation")
	}
}
