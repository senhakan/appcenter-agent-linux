package utils

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

func NewLogger(logPath string) (*log.Logger, io.Closer, error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	l := log.New(io.MultiWriter(os.Stdout, f), "", log.LstdFlags|log.LUTC)
	return l, f, nil
}
