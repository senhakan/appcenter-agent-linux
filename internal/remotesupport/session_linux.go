package remotesupport

import (
	"fmt"
	"sync"
	"time"
)

const (
	StateIdle      = "idle"
	StatePending   = "pending_approval"
	StateApproved  = "approved"
	StateActive    = "active"
	StateRejected  = "rejected"
	StateEnded     = "ended"
	StateError     = "error"
	DefaultMessage = ""
)

type SessionStatus struct {
	State           string `json:"state"`
	SessionID       int    `json:"session_id,omitempty"`
	AdminName       string `json:"admin_name,omitempty"`
	Reason          string `json:"reason,omitempty"`
	RequestedAtUnix int64  `json:"requested_at_unix,omitempty"`
	DecisionAtUnix  int64  `json:"decision_at_unix,omitempty"`
	Message         string `json:"message,omitempty"`
	LastError       string `json:"last_error,omitempty"`
}

type SessionManager struct {
	mu     sync.Mutex
	status SessionStatus
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		status: SessionStatus{
			State:   StateIdle,
			Message: DefaultMessage,
		},
	}
}

func (m *SessionManager) Restore(s SessionStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.State == "" {
		s.State = StateIdle
	}
	m.status = s
}

func (m *SessionManager) Snapshot() SessionStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *SessionManager) InProgress() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status.State == StatePending || m.status.State == StateApproved || m.status.State == StateActive
}

func (m *SessionManager) Clear() SessionStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = SessionStatus{
		State:   StateIdle,
		Message: DefaultMessage,
	}
	return m.status
}

func (m *SessionManager) Begin(sessionID int, adminName, reason string) (SessionStatus, error) {
	if sessionID <= 0 {
		return m.Snapshot(), fmt.Errorf("session_id must be positive")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.State == StatePending || m.status.State == StateApproved || m.status.State == StateActive {
		if m.status.SessionID == sessionID {
			return m.status, nil
		}
		return m.status, fmt.Errorf("another remote support session is already in progress")
	}
	m.status = SessionStatus{
		State:           StatePending,
		SessionID:       sessionID,
		AdminName:       adminName,
		Reason:          reason,
		RequestedAtUnix: time.Now().Unix(),
		Message:         "waiting for approval",
	}
	return m.status, nil
}

func (m *SessionManager) Approve() (SessionStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.State != StatePending {
		return m.status, fmt.Errorf("session is not pending approval")
	}
	m.status.State = StateApproved
	m.status.DecisionAtUnix = time.Now().Unix()
	m.status.Message = "approved"
	m.status.LastError = ""
	return m.status, nil
}

func (m *SessionManager) Activate() SessionStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.State = StateActive
	m.status.Message = "active"
	m.status.LastError = ""
	return m.status
}

func (m *SessionManager) Reject(reason string) (SessionStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.State != StatePending {
		return m.status, fmt.Errorf("session is not pending approval")
	}
	m.status.State = StateRejected
	m.status.DecisionAtUnix = time.Now().Unix()
	m.status.Message = "rejected"
	m.status.LastError = reason
	return m.status, nil
}

func (m *SessionManager) End(message string) SessionStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.State = StateEnded
	m.status.DecisionAtUnix = time.Now().Unix()
	m.status.Message = message
	m.status.LastError = ""
	return m.status
}

func (m *SessionManager) Error(err error) SessionStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.State = StateError
	m.status.DecisionAtUnix = time.Now().Unix()
	m.status.Message = "remote support failed"
	if err != nil {
		m.status.LastError = err.Error()
	}
	return m.status
}
