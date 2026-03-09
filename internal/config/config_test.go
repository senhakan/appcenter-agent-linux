package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateAgentVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &Config{}
	cfg.Server.URL = "http://10.6.100.170:8000"
	cfg.Agent.Version = "0.1.47-live"
	cfg.Agent.UUID = "u-1"
	cfg.Agent.SecretKey = "sk-1"

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := UpdateAgentVersion(path, "0.1.48-live"); err != nil {
		t.Fatalf("UpdateAgentVersion() error = %v", err)
	}

	updated, err := Load(path)
	if err != nil {
		t.Fatalf("Load() after update error = %v", err)
	}
	if updated.Agent.Version != "0.1.48-live" {
		t.Fatalf("agent.version = %q, want %q", updated.Agent.Version, "0.1.48-live")
	}
	if updated.Agent.UUID != "u-1" {
		t.Fatalf("agent.uuid changed unexpectedly: %q", updated.Agent.UUID)
	}
	if updated.Agent.SecretKey != "sk-1" {
		t.Fatalf("agent.secret_key changed unexpectedly: %q", updated.Agent.SecretKey)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(b) == 0 {
		t.Fatal("config file is empty after update")
	}
}
