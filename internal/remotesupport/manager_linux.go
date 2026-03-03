package remotesupport

import (
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

type Status struct {
	Installed     bool   `json:"installed"`
	X11VNCPath    string `json:"x11vnc_path,omitempty"`
	Running       bool   `json:"running"`
	PID           int    `json:"pid,omitempty"`
	Display       string `json:"display,omitempty"`
	Port          int    `json:"port,omitempty"`
	StartedAtUnix int64  `json:"started_at_unix,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}

type Manager struct {
	mu     sync.Mutex
	status Status
	cmd    *exec.Cmd
	logger *log.Logger
}

func NewManager(logger *log.Logger, display string, port int) *Manager {
	env := ProbeEnv()
	return &Manager{
		status: Status{
			Installed:  env.Installed,
			X11VNCPath: env.X11VNCPath,
			Display:    display,
			Port:       port,
		},
		logger: logger,
	}
}

func (m *Manager) Snapshot() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *Manager) Start() (Status, error) {
	m.mu.Lock()
	if m.status.Running {
		st := m.status
		m.mu.Unlock()
		return st, nil
	}
	if !m.status.Installed || m.status.X11VNCPath == "" {
		st := m.status
		m.mu.Unlock()
		return st, fmt.Errorf("x11vnc is not installed")
	}
	display := m.status.Display
	port := m.status.Port
	path := m.status.X11VNCPath
	cmd := exec.Command(
		path,
		"-display", display,
		"-rfbport", strconv.Itoa(port),
		"-forever",
		"-shared",
		"-nopw",
		"-noxrecord",
		"-noxfixes",
		"-noxdamage",
	)
	if err := cmd.Start(); err != nil {
		m.status.LastError = err.Error()
		st := m.status
		m.mu.Unlock()
		return st, err
	}
	m.cmd = cmd
	m.status.Running = true
	m.status.PID = cmd.Process.Pid
	m.status.StartedAtUnix = time.Now().Unix()
	m.status.LastError = ""
	st := m.status
	m.mu.Unlock()

	m.logger.Printf("remote support x11vnc started: pid=%d display=%s port=%d", st.PID, st.Display, st.Port)
	go m.wait(cmd)
	return st, nil
}

func (m *Manager) wait(cmd *exec.Cmd) {
	err := cmd.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != cmd {
		return
	}
	m.cmd = nil
	m.status.Running = false
	m.status.PID = 0
	m.status.StartedAtUnix = 0
	if err != nil {
		m.status.LastError = err.Error()
		m.logger.Printf("remote support x11vnc exited with error: %v", err)
		return
	}
	m.status.LastError = ""
	m.logger.Printf("remote support x11vnc exited")
}

func (m *Manager) Stop() (Status, error) {
	m.mu.Lock()
	cmd := m.cmd
	st := m.status
	m.mu.Unlock()
	if cmd == nil || !st.Running {
		return st, nil
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		if killErr := cmd.Process.Kill(); killErr != nil {
			return m.Snapshot(), fmt.Errorf("stop x11vnc failed: signal=%v kill=%v", err, killErr)
		}
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		cur := m.Snapshot()
		if !cur.Running {
			return cur, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if killErr := cmd.Process.Kill(); killErr != nil {
		return m.Snapshot(), killErr
	}
	time.Sleep(200 * time.Millisecond)
	return m.Snapshot(), nil
}
