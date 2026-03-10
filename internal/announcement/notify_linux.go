package announcement

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// desktopSession holds the environment needed to show a GUI notification
// from a root service to a logged-in desktop user.
type desktopSession struct {
	User    string
	UID     string
	Display string
	DBus    string
}

// findDesktopSessions discovers active graphical sessions by scanning
// /proc for processes that have DISPLAY set.
func findDesktopSessions() []desktopSession {
	// Use loginctl to find graphical sessions
	out, err := exec.Command("loginctl", "list-sessions", "--no-legend", "--no-pager").Output()
	if err != nil {
		return findDesktopSessionsFallback()
	}

	var sessions []desktopSession
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		sessionID := fields[0]
		user := fields[2]
		if seen[user] {
			continue
		}

		// Get session details
		detail, err := exec.Command("loginctl", "show-session", sessionID, "--no-pager").Output()
		if err != nil {
			continue
		}
		props := parseLoginctlProps(string(detail))

		// Only graphical sessions
		stype := props["Type"]
		if stype != "x11" && stype != "wayland" && stype != "mir" {
			continue
		}
		if props["State"] != "active" && props["Active"] != "yes" {
			continue
		}

		uid := props["User"]
		display := props["Display"]
		if display == "" {
			display = ":0"
		}

		// Find DBUS address
		dbus := findDBusAddr(uid)

		seen[user] = true
		sessions = append(sessions, desktopSession{
			User:    user,
			UID:     uid,
			Display: display,
			DBus:    dbus,
		})
	}
	return sessions
}

func parseLoginctlProps(output string) map[string]string {
	props := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		if idx := strings.Index(line, "="); idx > 0 {
			props[strings.TrimSpace(line[:idx])] = strings.TrimSpace(line[idx+1:])
		}
	}
	return props
}

func findDBusAddr(uid string) string {
	// Try the well-known XDG runtime path
	runtimeDir := fmt.Sprintf("/run/user/%s", uid)
	busPath := filepath.Join(runtimeDir, "bus")
	if _, err := os.Stat(busPath); err == nil {
		return "unix:path=" + busPath
	}
	return ""
}

// findDesktopSessionsFallback uses the `who` command as a simple fallback.
func findDesktopSessionsFallback() []desktopSession {
	out, err := exec.Command("who").Output()
	if err != nil {
		return nil
	}

	seen := map[string]bool{}
	var sessions []desktopSession
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		user := fields[0]
		if seen[user] || user == "root" {
			continue
		}

		// Get UID
		uidOut, err := exec.Command("id", "-u", user).Output()
		if err != nil {
			continue
		}
		uid := strings.TrimSpace(string(uidOut))

		seen[user] = true
		sessions = append(sessions, desktopSession{
			User:    user,
			UID:     uid,
			Display: ":0",
			DBus:    findDBusAddr(uid),
		})
	}
	return sessions
}

func getUserName(uid string) string {
	out, err := exec.Command("id", "-nu", uid).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func ShowNotification(title, message, priority string) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Announcement"
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "(empty message)"
	}

	sessions := findDesktopSessions()
	if len(sessions) == 0 {
		log.Printf("announcement: no desktop sessions found, title=%q", title)
		return
	}

	shown := false
	for _, sess := range sessions {
		if showToSession(sess, title, message, priority) {
			shown = true
		}
	}
	if !shown {
		log.Printf("announcement: notification failed for all sessions, title=%q priority=%q", title, priority)
	}
}

func showToSession(sess desktopSession, title, message, priority string) bool {
	env := []string{
		"DISPLAY=" + sess.Display,
		fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%s", sess.UID),
	}
	if sess.DBus != "" {
		env = append(env, "DBUS_SESSION_BUS_ADDRESS="+sess.DBus)
	}
	// Also pass HOME for some desktop environments
	env = append(env, fmt.Sprintf("HOME=/home/%s", sess.User))

	// Try zenity first (blocking dialog — good for important messages)
	if tryExecAsUser(sess.User, env, "zenity", "--info", "--title="+title, "--text="+message, "--width=400") {
		log.Printf("announcement: zenity shown to user=%s display=%s title=%q", sess.User, sess.Display, title)
		return true
	}

	// Try notify-send (non-blocking toast)
	urgency := "normal"
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "important", "critical":
		urgency = "critical"
	}
	if tryExecAsUser(sess.User, env, "notify-send", "-u", urgency, title, message) {
		log.Printf("announcement: notify-send shown to user=%s display=%s title=%q", sess.User, sess.Display, title)
		return true
	}

	// Try xmessage as last resort
	if tryExecAsUser(sess.User, env, "xmessage", "-center", fmt.Sprintf("%s\n\n%s", title, message)) {
		log.Printf("announcement: xmessage shown to user=%s display=%s title=%q", sess.User, sess.Display, title)
		return true
	}

	log.Printf("announcement: all methods failed for user=%s display=%s", sess.User, sess.Display)
	return false
}

func tryExecAsUser(user string, env []string, name string, args ...string) bool {
	// Use sudo to run as the target user
	cmdArgs := append([]string{"-u", user, "--", name}, args...)
	cmd := exec.Command("sudo", cmdArgs...)
	cmd.Env = append(os.Environ(), env...)
	err := cmd.Run()
	return err == nil
}
