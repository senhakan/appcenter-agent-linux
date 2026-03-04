package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteApproveParsesResponseAndSendsMonitorCount(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAuthUUID string
	var gotAuthSecret string
	var gotBody RemoteApproveRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthUUID = r.Header.Get("X-Agent-UUID")
		gotAuthSecret = r.Header.Get("X-Agent-Secret")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":             "ok",
			"message":            "approved",
			"vnc_password":       "pw-from-server",
			"guacd_host":         "10.6.100.170",
			"guacd_reverse_port": 4822,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.RemoteApprove(context.Background(), "agent-uuid", "agent-secret", 9003, true, 2)
	if err != nil {
		t.Fatalf("RemoteApprove error: %v", err)
	}
	if gotPath != "/api/v1/agent/remote-support/9003/approve" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotAuthUUID != "agent-uuid" || gotAuthSecret != "agent-secret" {
		t.Fatalf("unexpected auth headers: uuid=%q secret=%q", gotAuthUUID, gotAuthSecret)
	}
	if !gotBody.Approved || gotBody.MonitorCount != 2 {
		t.Fatalf("unexpected approve body: %+v", gotBody)
	}
	if resp == nil {
		t.Fatalf("expected response")
	}
	if resp.GuacdHost != "10.6.100.170" || resp.GuacdReversePort != 4822 || resp.VNCPassword != "pw-from-server" {
		t.Fatalf("unexpected approve response: %+v", resp)
	}
}

func TestRemoteReadySendsLocalVNCPortWhenProvided(t *testing.T) {
	t.Parallel()

	var gotBody RemoteReadyRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/remote-support/42/ready" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.RemoteReady(context.Background(), "agent-uuid", "agent-secret", 42, 5900); err != nil {
		t.Fatalf("RemoteReady error: %v", err)
	}
	if !gotBody.VNCReady || gotBody.LocalVNCPort != 5900 {
		t.Fatalf("unexpected ready body: %+v", gotBody)
	}
}
