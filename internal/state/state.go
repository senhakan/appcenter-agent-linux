package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type ProcessedTask struct {
	TaskID         int   `json:"task_id"`
	ExecutedAtUnix int64 `json:"executed_at_unix"`
}

type AgentState struct {
	UUID                 string               `json:"uuid"`
	SecretKey            string               `json:"secret_key"`
	InventoryHash        string               `json:"inventory_hash,omitempty"`
	ProcessedTasks       []ProcessedTask      `json:"processed_tasks,omitempty"`
	RemoteSupportSession RemoteSupportSession `json:"remote_support_session,omitempty"`
}

type RemoteSupportSession struct {
	State           string `json:"state,omitempty"`
	SessionID       int    `json:"session_id,omitempty"`
	AdminName       string `json:"admin_name,omitempty"`
	Reason          string `json:"reason,omitempty"`
	RequestedAtUnix int64  `json:"requested_at_unix,omitempty"`
	DecisionAtUnix  int64  `json:"decision_at_unix,omitempty"`
	Message         string `json:"message,omitempty"`
	LastError       string `json:"last_error,omitempty"`
}

func Load(path string) (*AgentState, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &AgentState{}, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s AgentState
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	return &s, nil
}

func Save(path string, s *AgentState) error {
	if s == nil {
		return fmt.Errorf("state is nil")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}
