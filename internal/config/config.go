package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	defaultConfigPath = "/etc/appcenter-agent/config.yaml"
)

type Config struct {
	Server struct {
		URL string `yaml:"url"`
	} `yaml:"server"`
	Agent struct {
		UUID      string `yaml:"uuid"`
		SecretKey string `yaml:"secret_key"`
		Version   string `yaml:"version"`
	} `yaml:"agent"`
	Heartbeat struct {
		IntervalSec int `yaml:"interval_sec"`
	} `yaml:"heartbeat"`
	Download struct {
		TempDir      string `yaml:"temp_dir"`
		MaxSizeBytes int64  `yaml:"max_size_bytes"`
	} `yaml:"download"`
	Install struct {
		TimeoutSec    int `yaml:"timeout_sec"`
		QueueCapacity int `yaml:"queue_capacity"`
		WorkerCount   int `yaml:"worker_count"`
	} `yaml:"install"`
	Logging struct {
		File string `yaml:"file"`
	} `yaml:"logging"`
	Paths struct {
		StateFile string `yaml:"state_file"`
	} `yaml:"paths"`
	IPC struct {
		SocketPath string `yaml:"socket_path"`
	} `yaml:"ipc"`
	RemoteSupport struct {
		Enabled            bool   `yaml:"enabled"`
		ApprovalTimeoutSec int    `yaml:"approval_timeout_sec"`
		Display            string `yaml:"display"`
		Port               int    `yaml:"port"`
	} `yaml:"remote_support"`
}

func ResolvePath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv("APPCENTER_CONFIG"); v != "" {
		return v
	}
	return defaultConfigPath
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Server.URL == "" {
		return nil, fmt.Errorf("server.url is required")
	}
	if cfg.Agent.Version == "" {
		cfg.Agent.Version = "0.1.0"
	}
	if cfg.Heartbeat.IntervalSec <= 0 {
		cfg.Heartbeat.IntervalSec = 60
	}
	if cfg.Download.TempDir == "" {
		cfg.Download.TempDir = "/var/lib/appcenter-agent/downloads"
	}
	if cfg.Install.TimeoutSec <= 0 {
		cfg.Install.TimeoutSec = 1800
	}
	if cfg.Install.QueueCapacity <= 0 {
		cfg.Install.QueueCapacity = 32
	}
	if cfg.Install.WorkerCount <= 0 {
		cfg.Install.WorkerCount = 1
	}
	if cfg.Logging.File == "" {
		cfg.Logging.File = "/var/log/appcenter-agent/agent.log"
	}
	if cfg.Paths.StateFile == "" {
		cfg.Paths.StateFile = "/var/lib/appcenter-agent/state.json"
	}
	if cfg.IPC.SocketPath == "" {
		cfg.IPC.SocketPath = "/var/run/appcenter-agent/ipc.sock"
	}
	if cfg.RemoteSupport.ApprovalTimeoutSec <= 0 {
		cfg.RemoteSupport.ApprovalTimeoutSec = 120
	}
	if cfg.RemoteSupport.Display == "" {
		cfg.RemoteSupport.Display = ":0"
	}
	if cfg.RemoteSupport.Port <= 0 {
		cfg.RemoteSupport.Port = 5900
	}
	return &cfg, nil
}

func EnsureDirs(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	for _, p := range []string{cfg.Logging.File, cfg.IPC.SocketPath, cfg.Paths.StateFile, cfg.Download.TempDir + "/.keep"} {
		d := filepath.Dir(p)
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	return nil
}
