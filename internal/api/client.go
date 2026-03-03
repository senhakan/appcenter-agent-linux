package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
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
	InventoryHash string `json:"inventory_hash,omitempty"`
	Platform      string `json:"platform,omitempty"`
}

type HeartbeatConfig struct {
	HeartbeatIntervalSec     int  `json:"heartbeat_interval_sec"`
	InventorySyncRequired    bool `json:"inventory_sync_required"`
	InventoryScanIntervalMin int  `json:"inventory_scan_interval_min"`
}

type HeartbeatResponse struct {
	Status   string          `json:"status"`
	Config   HeartbeatConfig `json:"config"`
	Commands []Command       `json:"commands"`
}

type SignalResponse struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type Command struct {
	TaskID        int    `json:"task_id"`
	Action        string `json:"action"`
	AppID         int    `json:"app_id"`
	AppName       string `json:"app_name,omitempty"`
	AppVersion    string `json:"app_version,omitempty"`
	DownloadURL   string `json:"download_url,omitempty"`
	FileHash      string `json:"file_hash,omitempty"`
	FileSizeBytes int64  `json:"file_size_bytes,omitempty"`
	InstallArgs   string `json:"install_args,omitempty"`
	ForceUpdate   bool   `json:"force_update,omitempty"`
	Priority      int    `json:"priority,omitempty"`
}

type TaskStatusRequest struct {
	Status              string `json:"status"`
	Progress            int    `json:"progress,omitempty"`
	Message             string `json:"message,omitempty"`
	ExitCode            *int   `json:"exit_code,omitempty"`
	InstalledVersion    string `json:"installed_version,omitempty"`
	DownloadDurationSec int    `json:"download_duration_sec,omitempty"`
	InstallDurationSec  int    `json:"install_duration_sec,omitempty"`
	Error               string `json:"error,omitempty"`
}

type SoftwareItem struct {
	Name            string `json:"name"`
	Version         string `json:"version,omitempty"`
	Publisher       string `json:"publisher,omitempty"`
	InstallDate     string `json:"install_date,omitempty"`
	EstimatedSizeKB int    `json:"estimated_size_kb,omitempty"`
	Architecture    string `json:"architecture,omitempty"`
}

type InventoryRequest struct {
	InventoryHash string         `json:"inventory_hash"`
	SoftwareCount int            `json:"software_count"`
	Items         []SoftwareItem `json:"items"`
}

func (c *Client) Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error) {
	var out RegisterResponse
	if err := c.postJSON(ctx, "/api/v1/agent/register", req, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SubmitInventory(ctx context.Context, agentUUID, secret string, req InventoryRequest) error {
	headers := map[string]string{
		"X-Agent-UUID":   agentUUID,
		"X-Agent-Secret": secret,
	}
	return c.postJSON(ctx, "/api/v1/agent/inventory", req, headers, nil)
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

func (c *Client) WaitForSignal(ctx context.Context, uuid, secret string, timeoutSec int) (*SignalResponse, error) {
	if timeoutSec <= 0 {
		timeoutSec = 55
	}
	q := url.Values{}
	q.Set("timeout", fmt.Sprintf("%d", timeoutSec))
	path := "/api/v1/agent/signal?" + q.Encode()
	headers := map[string]string{
		"X-Agent-UUID":   uuid,
		"X-Agent-Secret": secret,
	}
	var out SignalResponse
	if err := c.getJSON(ctx, path, headers, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DownloadToFile(ctx context.Context, agentUUID, secret, downloadURL, outDir, defaultName string) (string, int64, error) {
	if downloadURL == "" {
		return "", 0, fmt.Errorf("download url is empty")
	}
	u := downloadURL
	if strings.HasPrefix(downloadURL, "/") {
		u = c.baseURL + downloadURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Agent-UUID", agentUUID)
	req.Header.Set("X-Agent-Secret", secret)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("download failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", 0, fmt.Errorf("mkdir download dir: %w", err)
	}
	outName := defaultName
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if n := parseFilenameFromContentDisposition(cd); n != "" {
			outName = n
		}
	}
	outPath := filepath.Join(outDir, outName)
	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", 0, fmt.Errorf("open download file: %w", err)
	}
	defer f.Close()
	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("write download file: %w", err)
	}
	return outPath, n, nil
}

func (c *Client) ReportTaskStatus(ctx context.Context, agentUUID, secret string, taskID int, reqBody TaskStatusRequest) error {
	headers := map[string]string{
		"X-Agent-UUID":   agentUUID,
		"X-Agent-Secret": secret,
	}
	path := fmt.Sprintf("/api/v1/agent/task/%d/status", taskID)
	return c.postJSON(ctx, path, reqBody, headers, nil)
}

var cdFilenameRe = regexp.MustCompile(`(?i)filename=\"?([^\";]+)`)

func parseFilenameFromContentDisposition(v string) string {
	m := cdFilenameRe.FindStringSubmatch(v)
	if len(m) < 2 {
		return ""
	}
	name := strings.TrimSpace(m[1])
	name = strings.Trim(name, "\"")
	name = filepath.Base(name)
	if name == "." || name == "/" || name == "" {
		return ""
	}
	return name
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

func (c *Client) getJSON(ctx context.Context, path string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
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
		return fmt.Errorf("%s %s failed: HTTP %d: %s", http.MethodGet, path, resp.StatusCode, strings.TrimSpace(string(resBody)))
	}
	if out != nil {
		if err := json.Unmarshal(resBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
