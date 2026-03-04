package remotesupport

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func ResolveDisplay(preferred string, logger *log.Logger) string {
	preferred = strings.TrimSpace(preferred)
	if preferred != "" {
		if displaySocketExists(preferred) {
			return preferred
		}
	}
	glob, _ := filepath.Glob("/tmp/.X11-unix/X*")
	if len(glob) == 0 {
		if preferred != "" {
			return preferred
		}
		return ":0"
	}
	sort.Slice(glob, func(i, j int) bool {
		return xDisplayIndex(glob[i]) < xDisplayIndex(glob[j])
	})
	last := glob[len(glob)-1]
	idx := xDisplayIndex(last)
	if idx >= 0 {
		resolved := fmt.Sprintf(":%d", idx)
		if logger != nil && preferred != "" && preferred != resolved {
			logger.Printf("remote support display auto-resolved: requested=%s resolved=%s", preferred, resolved)
		}
		return resolved
	}
	if preferred != "" {
		return preferred
	}
	return ":0"
}

func displaySocketExists(display string) bool {
	display = strings.TrimSpace(display)
	if !strings.HasPrefix(display, ":") {
		return false
	}
	idx := strings.TrimPrefix(display, ":")
	_, err := os.Stat("/tmp/.X11-unix/X" + idx)
	return err == nil
}

func xDisplayIndex(path string) int {
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "X") {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimPrefix(base, "X"))
	if err != nil {
		return -1
	}
	return n
}
