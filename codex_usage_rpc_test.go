package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// The test binary is a real subprocess, speaking the public stdio protocol.
func TestCodexUsageRPCHelper(t *testing.T) {
	if os.Getenv("GO_USAGE_RPC_HELPER") != "1" {
		return
	}
	defer os.Exit(0)
	scanner := bufio.NewScanner(os.Stdin)
	read := func() map[string]json.RawMessage {
		if !scanner.Scan() {
			os.Exit(2)
		}
		var message map[string]json.RawMessage
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			os.Exit(3)
		}
		return message
	}
	mode := os.Getenv("GO_USAGE_RPC_MODE")
	first := read()
	if mode == "nohandshake" {
		time.Sleep(time.Hour)
		return
	}
	if mode == "badhandshake" {
		fmt.Fprintf(os.Stdout, "{\"id\":%s,\"error\":{\"message\":\"secret-device-code\"}}\n", first["id"])
		time.Sleep(time.Hour)
		return
	}
	var method string
	_ = json.Unmarshal(first["method"], &method)
	var params struct {
		ClientInfo struct {
			Name    string
			Version string
		} `json:"clientInfo"`
	}
	_ = json.Unmarshal(first["params"], &params)
	if method != "initialize" || params.ClientInfo.Name == "" || params.ClientInfo.Version == "" {
		os.Exit(4)
	}
	fmt.Fprintf(os.Stdout, "{\"id\":%s,\"result\":{\"userAgent\":\"fixture\"}}\n", first["id"])
	second := read()
	_ = json.Unmarshal(second["method"], &method)
	if method != "initialized" || second["id"] != nil {
		os.Exit(5)
	}
	if mode == "blocked" {
		time.Sleep(time.Hour)
		return
	}
	var held []map[string]json.RawMessage
	for {
		message := read()
		switch mode {
		case "silent":
			continue
		case "eof":
			return
		case "rpcerror":
			fmt.Fprintf(os.Stdout, "{\"id\":%s,\"error\":{\"message\":\"secret-device-code\"}}\n", message["id"])
			continue
		case "partial":
			fmt.Fprintf(os.Stdout, "{\"id\":%s,\"result\":{}}", message["id"])
			os.Stdout.Close()
			time.Sleep(time.Hour)
			return
		case "oversize":
			fmt.Fprintln(os.Stdout, strings.Repeat("x", 2*1024*1024))
			continue
		case "fragment":
			fmt.Fprintln(os.Stderr, "secret-device-code")
			fmt.Fprintln(os.Stdout, "discard diagnostic secret-device-code")
			fmt.Fprintln(os.Stdout, `{"method":"account/login/completed","params":{"secret":"secret-device-code"}}`)
			payload := fmt.Sprintf("{\"id\":%s,\"result\":{\"value\":%q}}\n", message["id"], strings.Repeat("z", 256*1024))
			for len(payload) > 0 {
				n := min(len(payload), 137)
				fmt.Fprint(os.Stdout, payload[:n])
				payload = payload[n:]
			}
			continue
		case "tool":
			fmt.Fprintln(os.Stdout, `{"id":"server-tool","method":"item/tool/call","params":{"token":"secret-device-code"}}`)
			answer := read()
			var rpcError struct{ Code int }
			_ = json.Unmarshal(answer["error"], &rpcError)
			if string(answer["id"]) != `"server-tool"` || rpcError.Code != -32601 {
				os.Exit(6)
			}
		case "concurrent":
			held = append(held, message)
			if len(held) < 16 {
				continue
			}
			for i := len(held) - 1; i >= 0; i-- {
				fmt.Fprintf(os.Stdout, "{\"id\":%s,\"result\":%s}\n", held[i]["id"], held[i]["params"])
			}
			held = nil
			continue
		}
		fmt.Fprintf(os.Stdout, "{\"id\":%s,\"result\":{\"account\":null,\"requiresOpenaiAuth\":true}}\n", message["id"])
	}
}

func usageRPCTestCommand(t *testing.T) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestCodexUsageRPCHelper$")
	command.Env = append(os.Environ(), "GO_USAGE_RPC_HELPER=1")
	return command
}

func TestCodexUsageRPCCommandIsolation(t *testing.T) {
	for _, key := range []string{"OPENAI_API_KEY", "CODEX_ACCESS_TOKEN", "HERMES_TOKEN", "RUST_LOG", "CODEX_HOME", "HOME", "USERPROFILE", "XDG_CONFIG_HOME", "APPDATA", "LOCALAPPDATA"} {
		t.Setenv(key, "untrusted-secret")
	}
	home := t.TempDir()
	cmd, err := codexUsageCommand(os.Args[0], home)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{os.Args[0], "app-server", "--listen", "stdio://", "-c", `cli_auth_credentials_store="file"`}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %q", cmd.Args)
	}
	if cmd.Dir != home {
		t.Fatalf("directory = %q", cmd.Dir)
	}
	env := map[string]string{}
	for _, entry := range cmd.Env {
		key, value, _ := strings.Cut(entry, "=")
		env[key] = value
		if strings.Contains(entry, "untrusted-secret") {
			t.Fatalf("inherited unsafe environment: %s", key)
		}
	}
	for _, key := range []string{"CODEX_HOME", "HOME", "USERPROFILE", "XDG_CONFIG_HOME", "APPDATA", "LOCALAPPDATA", "TMPDIR", "TMP", "TEMP"} {
		if env[key] != home {
			t.Errorf("%s not isolated", key)
		}
	}
	for _, pair := range [][2]string{{"codex", home}, {os.Args[0], "relative"}} {
		if _, err := codexUsageCommand(pair[0], pair[1]); err == nil {
			t.Error("accepted relative executable/home")
		}
	}
	if _, err := startCodexUsageRPC(context.Background(), filepath.Join(home, "absent"), home); err == nil || strings.Contains(err.Error(), home) {
		t.Fatalf("unsafe startup error: %v", err)
	}
}

func TestCodexUsageRPCRejectsUnsupportedOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rpc, err := startCodexUsageCommand(ctx, usageRPCTestCommand(t))
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	for _, test := range []struct {
		method string
		params any
	}{
		{"thread/start", map[string]any{}}, {"initialize", nil}, {"account/login/start", map[string]any{"type": "apiKey", "apiKey": "secret"}},
		{"account/login/start", map[string]any{"type": "chatgptDeviceCode", "accessToken": "secret"}},
		{"account/read", map[string]any{"refreshToken": true}}, {"account/login/cancel", map[string]any{"loginId": 1}},
		{"account/logout", map[string]any{"token": "secret"}}, {"account/rateLimits/read", []string{}},
	} {
		if err := rpc.Call(ctx, test.method, test.params, nil); err == nil {
			t.Errorf("accepted %s with %#v", test.method, test.params)
		}
	}
	for _, test := range []struct {
		method string
		params any
	}{
		{"account/read", map[string]any{"refreshToken": false}}, {"account/login/start", map[string]any{"type": "chatgptDeviceCode"}},
		{"account/login/cancel", map[string]any{"loginId": "login-fixture"}}, {"account/rateLimits/read", map[string]any{}}, {"account/logout", map[string]any{}},
	} {
		if err := rpc.Call(ctx, test.method, test.params, nil); err != nil {
			t.Errorf("rejected %s: %v", test.method, err)
		}
	}
}

func usageRPCTestMode(t *testing.T, mode string) (usageRPC, *exec.Cmd) {
	t.Helper()
	cmd := usageRPCTestCommand(t)
	cmd.Env = append(cmd.Env, "GO_USAGE_RPC_MODE="+mode)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rpc, err := startCodexUsageCommand(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rpc.Close() })
	return rpc, cmd
}

func TestCodexUsageRPCFraming(t *testing.T) {
	t.Run("fragmented-large-response-and-ignored-diagnostics", func(t *testing.T) {
		rpc, _ := usageRPCTestMode(t, "fragment")
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var result struct{ Value string }
		if err := rpc.Call(ctx, "account/rateLimits/read", map[string]any{}, &result); err != nil {
			t.Fatal(err)
		}
		if result.Value != strings.Repeat("z", 256*1024) {
			t.Fatalf("fragmented response length = %d", len(result.Value))
		}
	})
	t.Run("reject-server-tool-request", func(t *testing.T) {
		rpc, _ := usageRPCTestMode(t, "tool")
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := rpc.Call(ctx, "account/read", map[string]any{"refreshToken": false}, nil); err != nil {
			t.Fatal(err)
		}
	})
	for _, mode := range []string{"partial", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			rpc, _ := usageRPCTestMode(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := rpc.Call(ctx, "account/rateLimits/read", map[string]any{}, nil); err == nil {
				t.Fatal("accepted invalid frame")
			}
		})
	}
}

func TestCodexUsageRPCConcurrentOutOfOrder(t *testing.T) {
	rpc, _ := usageRPCTestMode(t, "concurrent")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			id := fmt.Sprintf("login-%d", i)
			var result struct {
				LoginID string `json:"loginId"`
			}
			if err := rpc.Call(ctx, "account/login/cancel", map[string]any{"loginId": id}, &result); err != nil {
				t.Errorf("call: %v", err)
				return
			}
			if result.LoginID != id {
				t.Errorf("misrouted response: got %q, want %q", result.LoginID, id)
			}
		}(i)
	}
	workers.Wait()
}

func TestCodexUsageRPCBoundsPendingCalls(t *testing.T) {
	rpc, _ := usageRPCTestMode(t, "blocked")
	r := rpc.(*codexUsageRPC)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	for i := 0; i < 64; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_ = rpc.Call(ctx, "account/login/cancel", map[string]any{"loginId": strings.Repeat("x", 1024)}, nil)
		}()
	}
	defer workers.Wait()
	defer cancel()
	deadline := time.Now().Add(time.Second)
	for {
		r.mu.Lock()
		count := len(r.pending)
		r.mu.Unlock()
		if count == 64 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending calls did not arrive: %d", count)
		}
		time.Sleep(time.Millisecond)
	}
	extraCtx, extraCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer extraCancel()
	if err := rpc.Call(extraCtx, "account/logout", map[string]any{}, nil); err != errCodexUsageRPC {
		t.Fatalf("excess call = %v; want immediate safe rejection", err)
	}
}

func TestCodexUsageRPCLifecycle(t *testing.T) {
	for _, mode := range []string{"silent", "eof", "rpcerror"} {
		t.Run(mode, func(t *testing.T) {
			rpc, cmd := usageRPCTestMode(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			err := rpc.Call(ctx, "account/logout", map[string]any{}, nil)
			if err == nil || strings.Contains(err.Error(), "secret-device-code") {
				t.Fatalf("unsafe error: %v", err)
			}
			if mode == "silent" {
				select {
				case <-rpc.(*codexUsageRPC).done:
				default:
					t.Fatal("timeout left process usable")
				}
			}
			var closers sync.WaitGroup
			for i := 0; i < 8; i++ {
				closers.Add(1)
				go func() { defer closers.Done(); _ = rpc.Close() }()
			}
			closers.Wait()
			if cmd.ProcessState == nil {
				t.Fatal("child not reaped")
			}
			if err := rpc.Call(context.Background(), "account/logout", map[string]any{}, nil); err == nil {
				t.Fatal("call succeeded after close")
			}
		})
	}
	for _, mode := range []string{"nohandshake", "badhandshake"} {
		t.Run(mode, func(t *testing.T) {
			cmd := usageRPCTestCommand(t)
			cmd.Env = append(cmd.Env, "GO_USAGE_RPC_MODE="+mode)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			rpc, err := startCodexUsageCommand(ctx, cmd)
			if rpc != nil || err != errCodexUsageRPC {
				t.Fatalf("startup = %v, %v", rpc, err)
			}
			if cmd.ProcessState == nil {
				t.Fatal("initialization failure did not reap child")
			}
		})
	}
}

func TestCodexUsageRPCHandshakeAndCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rpc, err := startCodexUsageCommand(ctx, usageRPCTestCommand(t))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer rpc.Close()
	var result struct {
		Account            any
		RequiresOpenaiAuth bool `json:"requiresOpenaiAuth"`
	}
	if err := rpc.Call(ctx, "account/read", map[string]any{"refreshToken": false}, &result); err != nil {
		t.Fatal(err)
	}
	if result.Account != nil || !result.RequiresOpenaiAuth {
		t.Fatalf("unexpected result: %#v", result)
	}
}
