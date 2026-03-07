package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"sort"
	"strings"

	"appcenter-agent-linux/internal/api"
)

func normStatus(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "active", "activating":
		return "running"
	case "inactive", "deactivating":
		return "stopped"
	case "failed":
		return "failed"
	default:
		return "unknown"
	}
}

func normStartup(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "enabled", "enabled-runtime":
		return "auto"
	case "disabled", "masked":
		return "disabled"
	case "static", "indirect", "generated":
		return "manual"
	default:
		return "unknown"
	}
}

func Collect(ctx context.Context) ([]api.ServiceItem, error) {
	unitState := map[string]string{}
	unitDesc := map[string]string{}
	{
		cmd := exec.CommandContext(ctx, "systemctl", "list-units", "--type=service", "--all", "--no-legend", "--no-pager", "--plain")
		out, err := cmd.Output()
		if err != nil {
			return nil, err
		}
		for _, ln := range strings.Split(string(out), "\n") {
			line := strings.TrimSpace(ln)
			if line == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			name := strings.TrimSpace(fields[0])
			active := strings.TrimSpace(fields[2])
			desc := ""
			if len(fields) > 4 {
				desc = strings.TrimSpace(strings.Join(fields[4:], " "))
			}
			if name == "" || !strings.HasSuffix(name, ".service") {
				continue
			}
			unitState[name] = normStatus(active)
			unitDesc[name] = desc
		}
	}
	startup := map[string]string{}
	{
		cmd := exec.CommandContext(ctx, "systemctl", "list-unit-files", "--type=service", "--no-legend", "--no-pager")
		out, err := cmd.Output()
		if err != nil {
			return nil, err
		}
		for _, ln := range strings.Split(string(out), "\n") {
			line := strings.TrimSpace(ln)
			if line == "" || strings.HasPrefix(line, "UNIT FILE") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			name := strings.TrimSpace(fields[0])
			if name == "" || !strings.HasSuffix(name, ".service") {
				continue
			}
			startup[name] = normStartup(fields[1])
		}
	}
	out := make([]api.ServiceItem, 0, len(unitState))
	seen := map[string]struct{}{}
	for name, status := range unitState {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, api.ServiceItem{
			Name:        name,
			DisplayName: strings.TrimSuffix(name, ".service"),
			Status:      status,
			StartupType: startup[name],
			Description: unitDesc[name],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func Hash(items []api.ServiceItem) string {
	h := sha256.New()
	for _, it := range items {
		h.Write([]byte(it.Name))
		h.Write([]byte{0})
		h.Write([]byte(it.Status))
		h.Write([]byte{0})
		h.Write([]byte(it.StartupType))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}
