package inventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"appcenter-agent-linux/internal/api"
)

// Collect reads installed package inventory from dpkg-query.
// On Debian-based systems this is the most stable package source.
func Collect(ctx context.Context) ([]api.SoftwareItem, error) {
	cmd := exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${binary:Package}\t${Version}\t${Architecture}\n")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("dpkg-query failed: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	items := make([]api.SoftwareItem, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		parts := strings.Split(ln, "\t")
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		item := api.SoftwareItem{Name: name}
		if len(parts) > 1 {
			item.Version = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			item.Architecture = strings.TrimSpace(parts[2])
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].Version < items[j].Version
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func Hash(items []api.SoftwareItem) string {
	h := sha256.New()
	for _, it := range items {
		h.Write([]byte(it.Name))
		h.Write([]byte{0})
		h.Write([]byte(it.Version))
		h.Write([]byte{0})
		h.Write([]byte(it.Architecture))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}
