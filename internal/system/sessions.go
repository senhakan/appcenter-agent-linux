package system

import (
	"os/exec"
	"strings"
)

type LoggedInSession struct {
	Username    string
	SessionType string
	LogonID     string
}

func GetLoggedInSessions() []LoggedInSession {
	out, err := exec.Command("who").Output()
	if err != nil {
		user := CurrentOSUser()
		if strings.TrimSpace(user) == "" {
			return nil
		}
		return []LoggedInSession{{Username: user, SessionType: "local"}}
	}
	lines := strings.Split(string(out), "\n")
	items := make([]LoggedInSession, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		fields := strings.Fields(ln)
		if len(fields) < 1 {
			continue
		}
		u := strings.TrimSpace(fields[0])
		if u == "" || strings.EqualFold(u, "reboot") {
			continue
		}
		key := strings.ToLower(u) + "|local"
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, LoggedInSession{
			Username:    u,
			SessionType: "local",
		})
	}
	if len(items) == 0 {
		user := CurrentOSUser()
		if strings.TrimSpace(user) != "" {
			items = append(items, LoggedInSession{Username: user, SessionType: "local"})
		}
	}
	return items
}
