package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

func VerifySHA256(path string, expected string) error {
	exp := strings.TrimSpace(strings.ToLower(expected))
	exp = strings.TrimPrefix(exp, "sha256:")
	if exp == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash file: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != exp {
		return fmt.Errorf("sha256 mismatch: got=%s expected=%s", got, exp)
	}
	return nil
}
