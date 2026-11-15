package main

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestCodexPinnedProtocolWithoutAuthorization(t *testing.T) {
	binary := os.Getenv("AGENT_CODEX_SMOKE_BINARY")
	if binary == "" {
		t.Skip("set AGENT_CODEX_SMOKE_BINARY to exercise pinned isolated runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rpc, err := startCodexInferenceRPC(ctx, binary, t.TempDir())
	if err != nil {
		t.Fatalf("isolated initialize: %v", err)
	}
	defer rpc.Close()
	var account struct {
		Account any `json:"account"`
	}
	if err = rpc.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &account); err != nil {
		t.Fatal(err)
	}
	if account.Account != nil {
		t.Fatal("isolated home imported an account")
	}
	var result struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err = rpc.call(ctx, "thread/start", map[string]any{"ephemeral": true, "environments": []any{}, "dynamicTools": []any{}, "sandbox": "read-only", "approvalPolicy": "never"}, &result); err != nil {
		t.Fatalf("no-auth thread schema smoke: %v", err)
	}
	if result.Thread.ID == "" {
		t.Fatal("thread id absent")
	}
	items := []any{map[string]any{"type": "message", "role": "user", "content": []any{map[string]string{"type": "input_text", "text": "Prior user context (protocol smoke only)."}}}, map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]string{"type": "output_text", "text": "Prior assistant context (protocol smoke only)."}}}}
	if err = rpc.call(ctx, "thread/inject_items", map[string]any{"threadId": result.Thread.ID, "items": items}, nil); err != nil {
		t.Fatalf("no-auth role-history schema smoke: %v", err)
	}
	t.Log("pinned Codex initialize/account-read/ephemeral thread-start/native role-history passed without login or inference")
}
