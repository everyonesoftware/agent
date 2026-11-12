package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var errCodexUsageRPC = errors.New("Codex usage connection failed")

type usageRPC interface {
	Call(context.Context, string, any, any) error
	Close() error
}

type codexUsageRPC struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	mu         sync.Mutex
	nextID     uint64
	pending    map[uint64]chan json.RawMessage
	writes     chan []byte
	done       chan struct{}
	reaped     chan struct{}
	readerDone chan struct{}
	writerDone chan struct{}
	once       sync.Once
}

// startCodexUsageRPC never uses a shell, the caller's request context as the
// process lifetime, or the user's default Codex credentials/configuration.
func startCodexUsageRPC(ctx context.Context, executable, home string) (usageRPC, error) {
	cmd, err := codexUsageCommand(executable, home)
	if err != nil {
		return nil, err
	}
	return startCodexUsageCommand(ctx, cmd)
}

func codexUsageCommand(executable, home string) (*exec.Cmd, error) {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(home) {
		return nil, errCodexUsageRPC
	}
	cmd := exec.Command(executable, "app-server", "--listen", "stdio://", "-c", `cli_auth_credentials_store="file"`)
	cmd.Dir = home
	// Only OS loader/path, CA trust and networking proxy settings are inherited.
	// In particular no API tokens, Rust logging, Hermes or Codex settings survive.
	allowed := map[string]bool{"PATH": true, "SYSTEMROOT": true, "WINDIR": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true}
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && allowed[strings.ToUpper(key)] {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for _, key := range []string{"CODEX_HOME", "HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "APPDATA", "LOCALAPPDATA", "TMPDIR", "TMP", "TEMP"} {
		cmd.Env = append(cmd.Env, key+"="+home)
	}
	return cmd, nil
}

// The command constructor is internal so tests can exercise a real child binary
// without adding a production environment switch or configurable CLI arguments.
func startCodexUsageCommand(ctx context.Context, cmd *exec.Cmd) (usageRPC, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, errCodexUsageRPC
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, errCodexUsageRPC
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, errCodexUsageRPC
	}
	r := &codexUsageRPC{cmd: cmd, stdin: stdin, stdout: stdout, pending: make(map[uint64]chan json.RawMessage), writes: make(chan []byte, 64), done: make(chan struct{}), reaped: make(chan struct{}), readerDone: make(chan struct{}), writerDone: make(chan struct{})}
	go func() { _ = cmd.Wait(); r.stop(); close(r.reaped) }()
	go r.read()
	go r.write()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := r.call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "everyoneagent_usage", "version": "1.0.0"}}, nil); err != nil {
		r.Close()
		return nil, errCodexUsageRPC
	}
	message, _ := json.Marshal(map[string]any{"method": "initialized", "params": map[string]any{}})
	select {
	case r.writes <- append(message, '\n'):
	case <-r.done:
		r.Close()
		return nil, errCodexUsageRPC
	case <-ctx.Done():
		r.Close()
		return nil, errCodexUsageRPC
	}
	return r, nil
}

func (r *codexUsageRPC) Call(ctx context.Context, method string, params, result any) error {
	// Re-encode the validated object, rather than invoking a caller's custom
	// marshaler twice; unknown keys cannot smuggle token-based authentication.
	data, err := json.Marshal(params)
	if err != nil || len(data) > 64*1024 {
		return errCodexUsageRPC
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil || object == nil {
		return errCodexUsageRPC
	}
	switch method {
	case "account/read":
		if len(object) != 1 || string(object["refreshToken"]) != "false" {
			return errCodexUsageRPC
		}
	case "account/login/start":
		var kind string
		if len(object) != 1 || json.Unmarshal(object["type"], &kind) != nil || kind != "chatgptDeviceCode" {
			return errCodexUsageRPC
		}
	case "account/login/cancel":
		var id string
		if len(object) != 1 || json.Unmarshal(object["loginId"], &id) != nil || id == "" || len(id) > 1024 {
			return errCodexUsageRPC
		}
	case "account/rateLimits/read", "account/logout":
		if len(object) != 0 {
			return errCodexUsageRPC
		}
	default:
		return errCodexUsageRPC
	}
	return r.call(ctx, method, object, result)
}

func (r *codexUsageRPC) call(ctx context.Context, method string, params, result any) error {
	r.mu.Lock()
	if len(r.pending) >= 64 {
		r.mu.Unlock()
		return errCodexUsageRPC
	}
	select {
	case <-r.done:
		r.mu.Unlock()
		return errCodexUsageRPC
	default:
	}
	r.nextID++
	id := r.nextID
	reply := make(chan json.RawMessage, 1)
	r.pending[id] = reply
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.pending, id); r.mu.Unlock() }()
	message, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return errCodexUsageRPC
	}
	select {
	case r.writes <- append(message, '\n'):
	case <-r.done:
		return errCodexUsageRPC
	case <-ctx.Done():
		r.stop()
		return ctx.Err()
	}
	select {
	case data := <-reply:
		var response struct {
			Result json.RawMessage
			Error  json.RawMessage
		}
		if json.Unmarshal(data, &response) != nil || len(response.Error) > 0 || len(response.Result) == 0 {
			return errCodexUsageRPC
		}
		if result != nil && json.Unmarshal(response.Result, result) != nil {
			return errCodexUsageRPC
		}
		return nil
	case <-r.done:
		return errCodexUsageRPC
	case <-ctx.Done():
		r.stop()
		return ctx.Err()
	}
}

func (r *codexUsageRPC) read() {
	defer close(r.readerDone)
	defer r.stop()
	scanner := bufio.NewScanner(r.stdout)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	// A final unterminated record is not a complete newline JSON message.
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if index := bytes.IndexByte(data, '\n'); index >= 0 {
			return index + 1, data[:index], nil
		}
		if atEOF && len(data) > 0 {
			return 0, nil, errCodexUsageRPC
		}
		return 0, nil, nil
	})
	for scanner.Scan() {
		var response struct {
			ID     json.RawMessage
			Method string
		}
		if json.Unmarshal(scanner.Bytes(), &response) != nil {
			continue
		}
		if response.Method != "" {
			if len(response.ID) > 0 && string(response.ID) != "null" {
				// No tools, approval, or token-refresh requests are supported.
				message, _ := json.Marshal(map[string]any{"id": response.ID, "error": map[string]any{"code": -32601, "message": "Method not supported"}})
				select {
				case r.writes <- append(message, '\n'):
				case <-r.done:
					return
				default:
					return
				}
			}
			continue
		}
		var id uint64
		if json.Unmarshal(response.ID, &id) != nil {
			continue
		}
		r.mu.Lock()
		reply := r.pending[id]
		if reply != nil {
			select {
			case reply <- append(json.RawMessage(nil), scanner.Bytes()...):
			default:
			}
		}
		r.mu.Unlock()
	}
}

func (r *codexUsageRPC) write() {
	defer close(r.writerDone)
	for {
		select {
		case message := <-r.writes:
			if _, err := r.stdin.Write(message); err != nil {
				r.stop()
				return
			}
		case <-r.done:
			return
		}
	}
}

func (r *codexUsageRPC) stop() {
	r.once.Do(func() { close(r.done); _ = r.cmd.Process.Kill(); _ = r.stdin.Close(); _ = r.stdout.Close() })
}

func (r *codexUsageRPC) Close() error {
	r.stop()
	<-r.reaped
	<-r.readerDone
	<-r.writerDone
	return nil
}
