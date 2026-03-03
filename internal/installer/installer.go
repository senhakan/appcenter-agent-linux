package installer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func Install(ctx context.Context, path string, installArgs string) (string, int, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if strings.HasSuffix(strings.ToLower(path), ".tar.gz") {
		ext = ".tar.gz"
	}
	switch ext {
	case ".deb":
		return run(ctx, "dpkg", []string{"-i", path})
	case ".sh":
		_ = os.Chmod(path, 0o755)
		args := []string{path}
		if strings.TrimSpace(installArgs) != "" {
			args = append(args, strings.Fields(installArgs)...)
		}
		return run(ctx, "/bin/bash", args)
	case ".tar.gz":
		targetDir := strings.TrimSuffix(path, ".tar.gz") + "_extract"
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return "", -1, fmt.Errorf("mkdir extract dir: %w", err)
		}
		if out, code, err := run(ctx, "tar", []string{"-xzf", path, "-C", targetDir}); err != nil {
			return out, code, err
		}
		installScript := filepath.Join(targetDir, "install.sh")
		if _, err := os.Stat(installScript); err == nil {
			_ = os.Chmod(installScript, 0o755)
			args := []string{installScript}
			if strings.TrimSpace(installArgs) != "" {
				args = append(args, strings.Fields(installArgs)...)
			}
			return run(ctx, "/bin/bash", args)
		}
		return "", 0, nil
	default:
		_ = os.Chmod(path, 0o755)
		args := []string{path}
		if strings.TrimSpace(installArgs) != "" {
			args = append(args, strings.Fields(installArgs)...)
		}
		return run(ctx, "/bin/bash", args)
	}
}

func run(ctx context.Context, exe string, args []string) (string, int, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return string(out), ee.ExitCode(), fmt.Errorf("command failed: %s %v (exit=%d)", exe, args, ee.ExitCode())
	}
	return string(out), -1, fmt.Errorf("run command: %w", err)
}
