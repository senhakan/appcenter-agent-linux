package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type RegisterRequest struct {
	UUID          string `json:"uuid"`
	Hostname      string `json:"hostname"`
	OSVersion     string `json:"os_version,omitempty"`
	Platform      string `json:"platform,omitempty"`
	Arch          string `json:"arch,omitempty"`
	Distro        string `json:"distro,omitempty"`
	DistroVersion string `json:"distro_version,omitempty"`
	AgentVersion  string `json:"agent_version,omitempty"`
	CPUModel      string `json:"cpu_model,omitempty"`
	RAMGB         int    `json:"ram_gb,omitempty"`
	DiskFreeGB    int    `json:"disk_free_gb,omitempty"`
}

type RegisterResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	SecretKey string `json:"secret_key"`
}

type HeartbeatRequest struct {
	Hostname      string `json:"hostname"`
	IPAddress     string `json:"ip_address,omitempty"`
	OSUser        string `json:"os_user,omitempty"`
	AgentVersion  string `json:"agent_version,omitempty"`
	DiskFreeGB    int    `json:"disk_free_gb,omitempty"`
	CurrentStatus string `json:"current_status,omitempty"`
	AppsChanged   bool   `json:"apps_changed"`
	InstalledApps []any  `json:"installed_apps"`
	Platform      string `json:"platform,omitempty"`
}

type HeartbeatConfig struct {
	HeartbeatIntervalSec int `json:"heartbeat_interval_sec"`
}

type HeartbeatResponse struct {
	Status string          `json:"status"`
	Config HeartbeatConfig `json:"config"`
}

func (c *Client) Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error) {
	var out RegisterResponse
	if err := c.postJSON(ctx, "/api/v1/agent/register", req, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Heartbeat(ctx context.Context, uuid, secret string, req HeartbeatRequest) (*HeartbeatResponse, error) {
	headers := map[string]string{
		"X-Agent-UUID":   uuid,
		"X-Agent-Secret": secret,
	}
	var out HeartbeatResponse
	if err := c.postJSON(ctx, "/api/v1/agent/heartbeat", req, headers, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) postJSON(ctx context.Context, path string, body any, headers map[string]string, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	resBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s failed: HTTP %d: %s", http.MethodPost, path, resp.StatusCode, strings.TrimSpace(string(resBody)))
	}
	if out != nil {
		if err := json.Unmarshal(resBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
