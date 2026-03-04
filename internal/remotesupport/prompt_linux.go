package remotesupport

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func PromptApproval(display, adminName, reason string, timeoutSec int, logger *log.Logger) (approved bool, decided bool, err error) {
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	display = strings.TrimSpace(display)
	if display == "" {
		display = ":0"
	}
	title := "AppCenter Uzak Destek"
	text := "Bir yonetici uzak destek baglantisi istiyor.\n\n"
	if strings.TrimSpace(adminName) != "" {
		text += "Yonetici: " + strings.TrimSpace(adminName) + "\n"
	}
	if strings.TrimSpace(reason) != "" {
		text += "Neden: " + strings.TrimSpace(reason) + "\n"
	}
	text += "\nOnayliyor musunuz?"

	zenityPath, err := exec.LookPath("zenity")
	if err != nil {
		return false, false, fmt.Errorf("zenity is not installed")
	}
	args := []string{
		"--question",
		"--title", title,
		"--text", text,
		"--ok-label", "Onayla",
		"--cancel-label", "Reddet",
		"--width", "460",
		"--timeout", strconv.Itoa(timeoutSec),
	}
	cmd := exec.Command(zenityPath, args...)
	cmd.Env = withDisplayEnv(display)
	out, runErr := cmd.CombinedOutput()
	if runErr == nil {
		return true, true, nil
	}
	output := strings.ToLower(strings.TrimSpace(string(out)))
	if ee, ok := runErr.(*exec.ExitError); ok {
		// zenity exit-code: 1 cancel/reject, 5 timeout
		if ee.ExitCode() == 5 {
			return false, false, nil
		}
		if ee.ExitCode() == 1 {
			if strings.Contains(output, "cannot open display") || strings.Contains(output, "gtk-warning") || strings.Contains(output, "dbus") {
				return false, false, fmt.Errorf("zenity ui unavailable: %s", strings.TrimSpace(string(out)))
			}
			return false, true, nil
		}
	}
	if logger != nil {
		logger.Printf("remote support approval prompt error: %v output=%s", runErr, strings.TrimSpace(string(out)))
	}
	return false, false, runErr
}

func withDisplayEnv(display string) []string {
	env := append([]string{}, os.Environ()...)
	env = append(env, "DISPLAY="+display)
	if auth := discoverXAuthority(); auth != "" {
		env = append(env, "XAUTHORITY="+auth)
	}
	uid := os.Getuid()
	runtimeDir := fmt.Sprintf("/run/user/%d", uid)
	if _, err := os.Stat(runtimeDir); err == nil {
		env = append(env, "XDG_RUNTIME_DIR="+runtimeDir)
		env = append(env, "DBUS_SESSION_BUS_ADDRESS=unix:path="+runtimeDir+"/bus")
	}
	return env
}
