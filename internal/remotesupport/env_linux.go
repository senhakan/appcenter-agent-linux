package remotesupport

import (
	"os"
	"os/exec"
	"strings"
)

type EnvInfo struct {
	X11VNCPath string `json:"x11vnc_path,omitempty"`
	Installed  bool   `json:"installed"`
	Display    string `json:"display,omitempty"`
}

func ProbeEnv() EnvInfo {
	info := EnvInfo{
		Display: strings.TrimSpace(os.Getenv("DISPLAY")),
	}
	if p, err := exec.LookPath("x11vnc"); err == nil {
		info.X11VNCPath = p
		info.Installed = true
	}
	return info
}
