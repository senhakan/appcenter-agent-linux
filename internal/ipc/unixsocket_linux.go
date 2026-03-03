//go:build linux

package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

type Request struct {
	Action       string `json:"action"`
	AppID        int    `json:"app_id,omitempty"`
	SessionID    int    `json:"session_id,omitempty"`
	AdminName    string `json:"admin_name,omitempty"`
	Reason       string `json:"reason,omitempty"`
	MonitorCount int    `json:"monitor_count,omitempty"`
}

type Response struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
	Data    any    `json:"data,omitempty"`
}

type HandlerFunc func(req Request) Response

func Start(ctx context.Context, socketPath string, logger *log.Logger, handler HandlerFunc) error {
	if strings.TrimSpace(socketPath) == "" {
		return fmt.Errorf("socket path is empty")
	}
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen unix socket: %w", err)
	}
	if err := os.Chmod(socketPath, 0o666); err != nil {
		logger.Printf("ipc chmod warning: %v", err)
	}
	logger.Printf("ipc server listening: %s", socketPath)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
		_ = os.Remove(socketPath)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			logger.Printf("ipc accept warning: %v", err)
			continue
		}
		go handleConn(conn, logger, handler)
	}
}

func handleConn(conn net.Conn, logger *log.Logger, handler HandlerFunc) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	line, err := r.ReadBytes('\n')
	if err != nil {
		writeResp(conn, Response{Status: "error", Error: "read request failed"})
		return
	}
	var req Request
	if err := json.Unmarshal(bytesTrimSpace(line), &req); err != nil {
		writeResp(conn, Response{Status: "error", Error: "invalid json"})
		return
	}
	if handler != nil {
		writeResp(conn, handler(req))
		return
	}
	writeResp(conn, defaultHandler(req))
}

func writeResp(conn net.Conn, resp Response) {
	b, _ := json.Marshal(resp)
	_, _ = conn.Write(append(b, '\n'))
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func defaultHandler(req Request) Response {
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "ping":
		return Response{Status: "ok", Message: "pong"}
	default:
		return Response{Status: "error", Error: "unsupported action"}
	}
}
