package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type AgentState struct {
	UUID      string `json:"uuid"`
	SecretKey string `json:"secret_key"`
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
