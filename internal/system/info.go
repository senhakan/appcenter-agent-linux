package system

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

type HostInfo struct {
	Hostname      string
	IPAddress     string
	IPAddresses   []string
	UptimeSec     int
	OSVersion     string
	CPUModel      string
	RAMGB         int
	DiskFreeGB    int
	Platform      string
	Arch          string
	Distro        string
	DistroVersion string
}

func CollectHostInfo() HostInfo {
	hostname, _ := os.Hostname()
	osr := readOSRelease()
	ips := allNonLoopbackIPv4()
	primaryIP := ""
	if len(ips) > 0 {
		primaryIP = ips[0]
	}
	return HostInfo{
		Hostname:      hostname,
		IPAddress:     primaryIP,
		IPAddresses:   ips,
		UptimeSec:     readUptimeSec(),
		OSVersion:     osr.prettyName,
		CPUModel:      readFirstCPUModel(),
		RAMGB:         readMemTotalGB(),
		DiskFreeGB:    readDiskFreeGB("/"),
		Platform:      "linux",
		Arch:          normalizeArch(runtime.GOARCH),
		Distro:        osr.id,
		DistroVersion: osr.versionID,
	}
}

type osReleaseInfo struct {
	prettyName string
	id         string
	versionID  string
}

func readOSRelease() osReleaseInfo {
	out := osReleaseInfo{}
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return out
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := kv[0]
		v := strings.Trim(kv[1], "\"")
		switch k {
		case "PRETTY_NAME":
			out.prettyName = v
		case "ID":
			out.id = strings.ToLower(v)
		case "VERSION_ID":
			out.versionID = v
		}
	}
	return out
}

func allNonLoopbackIPv4() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]string, 0, 8)
	seen := make(map[string]struct{})
	for _, iface := range ifaces {
		if (iface.Flags&net.FlagUp) == 0 || (iface.Flags&net.FlagLoopback) != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP == nil {
				continue
			}
			ip := ipNet.IP.To4()
			if ip != nil && !ip.IsLoopback() {
				s := ip.String()
				if _, ok := seen[s]; ok {
					continue
				}
				seen[s] = struct{}{}
				out = append(out, s)
			}
		}
	}
	return out
}

func readFirstCPUModel() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(strings.ToLower(line), "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func readMemTotalGB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.Atoi(fields[1])
				if kb > 0 {
					return kb / (1024 * 1024)
				}
			}
		}
	}
	return 0
}

func readDiskFreeGB(path string) int {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	free := st.Bavail * uint64(st.Bsize)
	return int(free / (1024 * 1024 * 1024))
}

func readUptimeSec() int {
	f, err := os.Open("/proc/uptime")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0
	}
	parts := strings.Fields(strings.TrimSpace(sc.Text()))
	if len(parts) < 1 {
		return 0
	}
	secFloat, err := strconv.ParseFloat(parts[0], 64)
	if err != nil || secFloat < 0 {
		return 0
	}
	return int(secFloat)
}

func normalizeArch(arch string) string {
	switch strings.ToLower(arch) {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	default:
		return arch
	}
}

func CurrentOSUser() string {
	if u := currentLoggedInUser(); u != "" {
		return u
	}
	v := os.Getenv("USER")
	if v != "" {
		return v
	}
	return fmt.Sprintf("uid:%d", os.Getuid())
}

func currentLoggedInUser() string {
	out, err := exec.Command("who").Output()
	if err != nil {
		return ""
	}
	return parseWhoOutput(string(out))
}

func parseWhoOutput(out string) string {
	lines := strings.Split(out, "\n")
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		fields := strings.Fields(ln)
		if len(fields) == 0 {
			continue
		}
		user := strings.TrimSpace(fields[0])
		if user == "" {
			continue
		}
		if strings.EqualFold(user, "reboot") {
			continue
		}
		return user
	}
	return ""
}
