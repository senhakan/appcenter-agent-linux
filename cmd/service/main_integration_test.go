//go:build linux

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestServiceBinaryWithMockAPIAndIPC(t *testing.T) {
	t.Parallel()

	var registerCount atomic.Int32
	var heartbeatCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/agent/register":
			registerCount.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":     "ok",
				"message":    "registered",
				"secret_key": "mock-secret",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/agent/heartbeat":
			heartbeatCount.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok",
				"config": map[string]any{
					"heartbeat_interval_sec":      1,
					"inventory_sync_required":     false,
					"inventory_scan_interval_min": 60,
					"remote_support_enabled":      true,
				},
				"commands": []any{},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/agent/signal"):
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "timeout"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/agent/inventory":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "message": "not found"})
		}
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	ipcSock := filepath.Join(tmpDir, "ipc.sock")
	logPath := filepath.Join(tmpDir, "agent.log")
	statePath := filepath.Join(tmpDir, "state.json")
	downloadDir := filepath.Join(tmpDir, "downloads")

	cfg := fmt.Sprintf(`server:
  url: %q
agent:
  version: "0.1.0"
heartbeat:
  interval_sec: 1
download:
  temp_dir: %q
  max_size_bytes: 104857600
install:
  timeout_sec: 30
  queue_capacity: 2
  worker_count: 1
logging:
  file: %q
paths:
  state_file: %q
ipc:
  socket_path: %q
remote_support:
  enabled: true
  approval_timeout_sec: 120
  display: ":0"
  port: 5900
`, srv.URL, downloadDir, logPath, statePath, ipcSock)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	binPath := filepath.Join(tmpDir, "service-bin")
	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/service")
	buildCmd.Dir = repoRoot
	if out, buildErr := buildCmd.CombinedOutput(); buildErr != nil {
		t.Fatalf("build service: %v output=%s", buildErr, string(out))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	runCmd := exec.CommandContext(ctx, binPath, "-config", cfgPath)
	runCmd.Dir = repoRoot
	if err := runCmd.Start(); err != nil {
		t.Fatalf("start service: %v", err)
	}
	defer func() {
		_ = runCmd.Process.Signal(syscall.SIGTERM)
		_ = runCmd.Wait()
	}()

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if _, statErr := os.Stat(ipcSock); statErr == nil {
			break
		}
		time.Sleep(80 * time.Millisecond)
	}
	if _, statErr := os.Stat(ipcSock); statErr != nil {
		b, _ := os.ReadFile(logPath)
		t.Fatalf("ipc socket not ready: %v log=%s", statErr, string(b))
	}

	pingResp := mustCallIPC(t, ipcSock, map[string]any{"action": "ping"})
	if pingResp.Status != "ok" || pingResp.Code != "ok" {
		t.Fatalf("unexpected ping response: %+v", pingResp)
	}
	unsupportedResp := mustCallIPC(t, ipcSock, map[string]any{"action": "nope_action"})
	if unsupportedResp.Status != "error" || unsupportedResp.Code != "unsupported_action" {
		t.Fatalf("unexpected unsupported response: %+v", unsupportedResp)
	}

	waitDeadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(waitDeadline) {
		if registerCount.Load() > 0 && heartbeatCount.Load() > 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("expected register/heartbeat calls, got register=%d heartbeat=%d", registerCount.Load(), heartbeatCount.Load())
}

type ipcTestResponse struct {
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func mustCallIPC(t *testing.T, socketPath string, payload map[string]any) ipcTestResponse {
	t.Helper()
	conn, err := net.DialTimeout("unix", socketPath, 3*time.Second)
	if err != nil {
		t.Fatalf("dial ipc: %v", err)
	}
	defer conn.Close()

	b, _ := json.Marshal(payload)
	if _, err = conn.Write(append(b, '\n')); err != nil {
		t.Fatalf("write ipc: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read ipc: %v", err)
	}
	var out ipcTestResponse
	if err = json.Unmarshal(line, &out); err != nil {
		t.Fatalf("decode ipc response: %v body=%s", err, string(line))
	}
	return out
}
