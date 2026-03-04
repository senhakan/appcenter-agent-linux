package remotesupport

import (
	"errors"
	"testing"
)

func TestSessionConnectionInfoSetAndClearedAcrossTransitions(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()
	if _, err := m.Begin(9003, "qa-admin", "metadata test"); err != nil {
		t.Fatalf("Begin error: %v", err)
	}
	if _, err := m.Approve(); err != nil {
		t.Fatalf("Approve error: %v", err)
	}
	m.SetConnectionInfo("10.6.100.170", 4822, 5900, true)
	s := m.Activate()
	if s.GuacdHost != "10.6.100.170" || s.GuacdReversePort != 4822 || s.LocalVNCPort != 5900 || !s.ServerVNCPasswordSet {
		t.Fatalf("unexpected active connection info: %+v", s)
	}

	s = m.End("ended")
	if s.GuacdHost != "" || s.GuacdReversePort != 0 || s.LocalVNCPort != 0 || s.ServerVNCPasswordSet {
		t.Fatalf("expected metadata cleared on End: %+v", s)
	}
}

func TestSessionConnectionInfoClearedOnRejectAndError(t *testing.T) {
	t.Parallel()

	m := NewSessionManager()
	if _, err := m.Begin(9010, "qa-admin", "reject test"); err != nil {
		t.Fatalf("Begin error: %v", err)
	}
	if _, err := m.Approve(); err != nil {
		t.Fatalf("Approve error: %v", err)
	}
	m.SetConnectionInfo("10.6.100.170", 4822, 5900, true)

	if _, err := m.Reject("rejected"); err == nil {
		t.Fatalf("expected Reject to fail when not pending")
	}

	m2 := NewSessionManager()
	if _, err := m2.Begin(9011, "qa-admin", "reject pending"); err != nil {
		t.Fatalf("Begin error: %v", err)
	}
	if _, err := m2.Approve(); err != nil {
		t.Fatalf("Approve error: %v", err)
	}
	m2.SetConnectionInfo("10.6.100.170", 4822, 5900, true)
	m2.Error(errors.New("boom"))
	s := m2.Snapshot()
	if s.GuacdHost != "" || s.GuacdReversePort != 0 || s.LocalVNCPort != 0 || s.ServerVNCPasswordSet {
		t.Fatalf("expected metadata cleared on Error: %+v", s)
	}
}
