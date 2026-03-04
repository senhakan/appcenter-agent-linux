package system

import (
	"bufio"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

type SystemDisk struct {
	Index   int
	SizeGB  int
	Model   string
	BusType string
}

type VirtualizationInfo struct {
	IsVirtual bool
	Vendor    string
	Model     string
}

type SystemProfile struct {
	OSFullName       string
	OSVersion        string
	BuildNumber      string
	Architecture     string
	Manufacturer     string
	Model            string
	CPUModel         string
	CPUCoresPhysical int
	CPUCoresLogical  int
	TotalMemoryGB    int
	DiskCount        int
	Disks            []SystemDisk
	Virtualization   *VirtualizationInfo
}

func CollectSystemProfile() SystemProfile {
	osr := readOSRelease()
	disks := readDisks()
	return SystemProfile{
		OSFullName:       osr.prettyName,
		OSVersion:        osr.versionID,
		BuildNumber:      "",
		Architecture:     normalizeArch(runtime.GOARCH),
		Manufacturer:     readDMIValue("/sys/class/dmi/id/sys_vendor"),
		Model:            readDMIValue("/sys/class/dmi/id/product_name"),
		CPUModel:         readFirstCPUModel(),
		CPUCoresPhysical: readPhysicalCores(),
		CPUCoresLogical:  runtime.NumCPU(),
		TotalMemoryGB:    readMemTotalGB(),
		DiskCount:        len(disks),
		Disks:            disks,
		Virtualization:   detectVirtualization(),
	}
}

func readDMIValue(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readPhysicalCores() int {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	seen := map[string]struct{}{}
	currentPhysicalID := ""
	currentCoreID := ""
	flush := func() {
		if currentPhysicalID != "" && currentCoreID != "" {
			seen[currentPhysicalID+":"+currentCoreID] = struct{}{}
		}
	}
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			flush()
			currentPhysicalID = ""
			currentCoreID = ""
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(strings.ToLower(parts[0]))
		v := strings.TrimSpace(parts[1])
		switch k {
		case "physical id":
			currentPhysicalID = v
		case "core id":
			currentCoreID = v
		}
	}
	flush()
	if len(seen) > 0 {
		return len(seen)
	}
	return runtime.NumCPU()
}

func readDisks() []SystemDisk {
	out, err := exec.Command("lsblk", "-b", "-dn", "-o", "NAME,SIZE,MODEL,TYPE,TRAN").Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	disks := make([]SystemDisk, 0, len(lines))
	idx := 0
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		fields := strings.Fields(ln)
		if len(fields) < 4 {
			continue
		}
		// layout: NAME SIZE MODEL... TYPE [TRAN]
		name := fields[0]
		sizeBytes, _ := strconv.ParseInt(fields[1], 10, 64)
		dtype := fields[len(fields)-2]
		if dtype != "disk" {
			continue
		}
		tran := fields[len(fields)-1]
		modelFields := fields[2 : len(fields)-2]
		model := strings.TrimSpace(strings.Join(modelFields, " "))
		if model == "" {
			model = name
		}
		disks = append(disks, SystemDisk{
			Index:   idx,
			SizeGB:  int(sizeBytes / (1024 * 1024 * 1024)),
			Model:   model,
			BusType: tran,
		})
		idx++
	}
	return disks
}

func detectVirtualization() *VirtualizationInfo {
	virt := &VirtualizationInfo{IsVirtual: false}
	out, err := exec.Command("systemd-detect-virt").Output()
	if err != nil {
		return virt
	}
	v := strings.TrimSpace(string(out))
	if v == "" || v == "none" {
		return virt
	}
	virt.IsVirtual = true
	virt.Vendor = v
	virt.Model = v
	return virt
}
