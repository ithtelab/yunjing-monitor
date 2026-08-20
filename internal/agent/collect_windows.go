//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	getSystemTimes       = kernel32.NewProc("GetSystemTimes")
	globalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	getTickCount64       = kernel32.NewProc("GetTickCount64")
	getDiskFreeSpaceExW  = kernel32.NewProc("GetDiskFreeSpaceExW")
	getLogicalDrives     = kernel32.NewProc("GetLogicalDrives")
	iphlpapi             = syscall.NewLazyDLL("iphlpapi.dll")
	getIfTable           = iphlpapi.NewProc("GetIfTable")
)

var windowsSwapCache struct {
	sync.Mutex
	value     Memory
	updatedAt time.Time
	valid     bool
}

type filetime struct {
	LowDateTime  uint32
	HighDateTime uint32
}

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

type mibIfRow struct {
	Name            [256]uint16
	Index           uint32
	Type            uint32
	Mtu             uint32
	Speed           uint32
	PhysAddrLen     uint32
	PhysAddr        [8]byte
	AdminStatus     uint32
	OperStatus      uint32
	LastChange      uint32
	InOctets        uint32
	InUcastPkts     uint32
	InNUcastPkts    uint32
	InDiscards      uint32
	InErrors        uint32
	InUnknownProtos uint32
	OutOctets       uint32
	OutUcastPkts    uint32
	OutNUcastPkts   uint32
	OutDiscards     uint32
	OutErrors       uint32
	OutQLen         uint32
	DescrLen        uint32
	Descr           [256]byte
}

func readCPUTimes() (cpuTimes, error) {
	var idle, kernel, user filetime
	r1, _, err := getSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if r1 == 0 {
		return cpuTimes{}, err
	}
	idleTicks := filetimeToUint64(idle)
	kernelTicks := filetimeToUint64(kernel)
	userTicks := filetimeToUint64(user)
	return cpuTimes{idle: idleTicks, total: kernelTicks + userTicks}, nil
}

func readMemory() (Memory, Memory, error) {
	var stat memoryStatusEx
	stat.Length = uint32(unsafe.Sizeof(stat))
	r1, _, err := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&stat)))
	if r1 == 0 {
		return Memory{}, Memory{}, err
	}
	memUsed := stat.TotalPhys - stat.AvailPhys
	return Memory{Total: stat.TotalPhys, Used: memUsed, Free: stat.AvailPhys}, readWindowsSwap(), nil
}

func readWindowsSwap() Memory {
	windowsSwapCache.Lock()
	defer windowsSwapCache.Unlock()
	if windowsSwapCache.valid && time.Since(windowsSwapCache.updatedAt) < 30*time.Second {
		return windowsSwapCache.value
	}
	value, err := queryWindowsSwap()
	if err == nil {
		windowsSwapCache.value = value
		windowsSwapCache.updatedAt = time.Now()
		windowsSwapCache.valid = true
	}
	return windowsSwapCache.value
}

func queryWindowsSwap() (Memory, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var record windowsPageFileRecord
	if err := runWindowsPowerShellJSON(ctx, windowsPageFileScript, &record); err != nil {
		return Memory{}, err
	}
	return pageFileMemory(record.AllocatedMB, record.UsedMB), nil
}

func pageFileMemory(allocatedMB, usedMB uint64) Memory {
	if usedMB > allocatedMB {
		usedMB = allocatedMB
	}
	const mib = uint64(1024 * 1024)
	total := allocatedMB * mib
	used := usedMB * mib
	return Memory{Total: total, Used: used, Free: total - used}
}

type windowsPageFileRecord struct {
	AllocatedMB uint64 `json:"AllocatedMB"`
	UsedMB      uint64 `json:"UsedMB"`
}

func readLoad() (Load, error) {
	return Load{}, nil
}

func readUptime() (uint64, error) {
	r1, _, _ := getTickCount64.Call()
	return uint64(r1) / 1000, nil
}

func readNetwork(exclude []string) (netCounters, error) {
	var size uint32
	getIfTable.Call(0, uintptr(unsafe.Pointer(&size)), 0)
	if size == 0 {
		return netCounters{}, nil
	}

	buf := make([]byte, size)
	r1, _, err := getIfTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0)
	if r1 != 0 {
		return netCounters{}, err
	}

	base := unsafe.Pointer(&buf[0])
	count := *(*uint32)(base)
	rowSize := unsafe.Sizeof(mibIfRow{})
	if maxRows := (uintptr(len(buf)) - unsafe.Sizeof(count)) / rowSize; uintptr(count) > maxRows {
		count = uint32(maxRows)
	}
	var total netCounters
	for i := uint32(0); i < count; i++ {
		row := (*mibIfRow)(unsafe.Add(base, unsafe.Sizeof(count)+uintptr(i)*rowSize))
		total.rx += uint64(row.InOctets)
		total.tx += uint64(row.OutOctets)
	}
	return total, nil
}

func readDisks(mounts []string, excludeFS []string) ([]Disk, error) {
	if autoMounts(mounts) {
		mounts = windowsAutoMounts()
	}
	disks := make([]Disk, 0, len(mounts))
	for _, mount := range mounts {
		path, err := syscall.UTF16PtrFromString(mount)
		if err != nil {
			continue
		}
		var freeAvail, total, free uint64
		r1, _, _ := getDiskFreeSpaceExW.Call(uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(&freeAvail)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
		if r1 == 0 {
			continue
		}
		used := total - freeAvail
		percent := 0.0
		if total > 0 {
			percent = round2(float64(used) / float64(total) * 100)
		}
		disks = append(disks, Disk{Mount: mount, Total: total, Used: used, Free: freeAvail, UsedPercent: percent})
	}
	return disks, nil
}

func autoMounts(mounts []string) bool {
	return len(mounts) == 0 || (len(mounts) == 1 && strings.EqualFold(mounts[0], "auto"))
}

func windowsAutoMounts() []string {
	r1, _, _ := getLogicalDrives.Call()
	mask := uint32(r1)
	if mask == 0 {
		return []string{`C:\`}
	}
	mounts := []string{}
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		mounts = append(mounts, string(rune('A'+i))+`:\`)
	}
	return mounts
}

func readConnections() (Connections, error) {
	return Connections{TCP: windowsTCPCount(), UDP: windowsUDPCount()}, nil
}

func readDiskCounters() (diskCounters, error) {
	return diskCounters{}, nil
}

func readHostInfo() HostStaticInfo {
	info := HostStaticInfo{OSName: "Windows"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var record windowsHostInfoRecord
	if err := runWindowsPowerShellJSON(ctx, windowsHostInfoScript, &record); err != nil {
		return info
	}
	if value := strings.TrimSpace(record.OSName); value != "" {
		info.OSName = value
	}
	info.Kernel = strings.TrimSpace(record.Kernel)
	info.CPUModel = strings.TrimSpace(record.CPUModel)
	info.PhysicalCores = record.PhysicalCores
	info.Virtualization = detectWindowsVirtualization(record.Manufacturer, record.Model, record.HypervisorPresent)
	info.GPUs = filterWindowsGPUs(record.GPUs)
	return info
}

func runWindowsPowerShellJSON(ctx context.Context, script string, target any) error {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		return err
	}
	payload := strings.TrimPrefix(strings.TrimSpace(string(output)), "\ufeff")
	return json.Unmarshal([]byte(payload), target)
}

type windowsHostInfoRecord struct {
	OSName            string   `json:"OSName"`
	Kernel            string   `json:"Kernel"`
	CPUModel          string   `json:"CPUModel"`
	PhysicalCores     int      `json:"PhysicalCores"`
	Manufacturer      string   `json:"Manufacturer"`
	Model             string   `json:"Model"`
	HypervisorPresent bool     `json:"HypervisorPresent"`
	GPUs              []string `json:"GPUs"`
}

func filterWindowsGPUs(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		lower := strings.ToLower(value)
		if value == "" || strings.Contains(lower, "basic display") || strings.Contains(lower, "remote display") ||
			strings.Contains(lower, "virtual display") || strings.Contains(lower, "indirect display") ||
			strings.Contains(lower, "rdp display") || strings.Contains(lower, "citrix display") {
			continue
		}
		key := strings.ToLower(value)
		if !seen[key] {
			seen[key] = true
			out = append(out, value)
		}
	}
	return out
}

func detectWindowsVirtualization(manufacturer, model string, hypervisorPresent bool) string {
	identity := strings.ToLower(strings.TrimSpace(manufacturer + " " + model))
	switch {
	case strings.Contains(identity, "vmware"):
		return "VMware"
	case strings.Contains(identity, "virtualbox"), strings.Contains(identity, "innotek"):
		return "VirtualBox"
	case strings.Contains(identity, "kvm"), strings.Contains(identity, "qemu"):
		return "KVM"
	case strings.Contains(identity, "xen"):
		return "Xen"
	case strings.Contains(identity, "parallels"):
		return "Parallels"
	case strings.Contains(identity, "amazon ec2"):
		return "Amazon EC2"
	case strings.Contains(identity, "google compute engine"):
		return "Google Compute Engine"
	case strings.Contains(identity, "digitalocean"):
		return "DigitalOcean"
	case strings.Contains(identity, "openstack"):
		return "OpenStack"
	case strings.Contains(identity, "microsoft corporation virtual machine"),
		strings.EqualFold(strings.TrimSpace(model), "Virtual Machine"):
		return "Hyper-V"
	case hypervisorPresent:
		return "Hyper-V"
	default:
		return ""
	}
}

const windowsHostInfoScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.UTF8Encoding]::UTF8
$os = Get-CimInstance Win32_OperatingSystem
$cpus = @(Get-CimInstance Win32_Processor)
$computer = Get-CimInstance Win32_ComputerSystem
$gpus = @(Get-CimInstance Win32_VideoController | ForEach-Object { [string]$_.Name })
$physicalCores = ($cpus | Measure-Object -Property NumberOfCores -Sum).Sum
$kernel = [string]$os.Version
if ($os.BuildNumber) {
  $kernel += ' (Build ' + [string]$os.BuildNumber + ')'
}
[pscustomobject]@{
  OSName = [string]$os.Caption
  Kernel = $kernel
  CPUModel = [string]$cpus[0].Name
  PhysicalCores = [int]$physicalCores
  Manufacturer = [string]$computer.Manufacturer
  Model = [string]$computer.Model
  HypervisorPresent = [bool]$computer.HypervisorPresent
  GPUs = @($gpus)
} | ConvertTo-Json -Compress`

const windowsPageFileScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.UTF8Encoding]::UTF8
$pageFiles = @(Get-CimInstance Win32_PageFileUsage)
$allocated = ($pageFiles | Measure-Object -Property AllocatedBaseSize -Sum).Sum
$used = ($pageFiles | Measure-Object -Property CurrentUsage -Sum).Sum
[pscustomobject]@{
  AllocatedMB = [uint64]$(if ($null -eq $allocated) { 0 } else { $allocated })
  UsedMB = [uint64]$(if ($null -eq $used) { 0 } else { $used })
} | ConvertTo-Json -Compress`

func readProcessCount() int {
	return powershellCount("Get-Process | Measure-Object | Select-Object -ExpandProperty Count")
}

func filetimeToUint64(ft filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}

func windowsTCPCount() int {
	return powershellCount("Get-NetTCPConnection | Measure-Object | Select-Object -ExpandProperty Count")
}

func windowsUDPCount() int {
	return powershellCount("Get-NetUDPEndpoint | Measure-Object | Select-Object -ExpandProperty Count")
}

func powershellCount(script string) int {
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

func matchAny(value string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern == value {
			return true
		}
	}
	return false
}
