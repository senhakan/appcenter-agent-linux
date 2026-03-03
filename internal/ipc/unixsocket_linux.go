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

type request struct {
	Action string `json:"action"`
}

type response struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func Start(ctx context.Context, socketPath string, logger *log.Logger) error {
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
		go handleConn(conn, logger)
	}
}

func handleConn(conn net.Conn, logger *log.Logger) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	line, err := r.ReadBytes('\n')
	if err != nil {
		writeResp(conn, response{Status: "error", Error: "read request failed"})
		return
	}
	var req request
	if err := json.Unmarshal(bytesTrimSpace(line), &req); err != nil {
		writeResp(conn, response{Status: "error", Error: "invalid json"})
		return
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "ping":
		writeResp(conn, response{Status: "ok", Message: "pong"})
	default:
		writeResp(conn, response{Status: "error", Error: "unsupported action"})
	}
}

func writeResp(conn net.Conn, resp response) {
	b, _ := json.Marshal(resp)
	_, _ = conn.Write(append(b, '\n'))
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
