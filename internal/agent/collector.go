package agent

import (
	"context"
	"math"
	"os"
	"runtime"
	"sync"
	"time"

	"vps-agent/internal/config"
)

type Collector struct {
	cfg config.Config

	mu         sync.Mutex
	lastCPU    cpuTimes
	lastNet    netCounters
	lastDiskIO diskCounters
	lastTime   time.Time

	disks                []Disk
	conns                *Connections
	lastDisk             time.Time
	lastConn             time.Time
	staticHost           string
	staticCores          int
	staticPhysicalCores  int
	staticOS             string
	staticOSName         string
	staticArch           string
	staticKernel         string
	staticVirtualization string
	staticCPUModel       string
	staticGPUs           []string
	windowsLoad          Load
	windowsLoadReady     bool
}

func NewCollector(cfg config.Config) *Collector {
	host, _ := os.Hostname()
	hostInfo := readHostInfo()
	return &Collector{
		cfg:                  cfg,
		staticHost:           host,
		staticCores:          runtime.NumCPU(),
		staticPhysicalCores:  hostInfo.PhysicalCores,
		staticOS:             runtime.GOOS,
		staticOSName:         hostInfo.OSName,
		staticArch:           runtime.GOARCH,
		staticKernel:         hostInfo.Kernel,
		staticVirtualization: hostInfo.Virtualization,
		staticCPUModel:       hostInfo.CPUModel,
		staticGPUs:           append([]string(nil), hostInfo.GPUs...),
	}
}

func (c *Collector) Collect(ctx context.Context) (Metrics, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	cpuNow, err := readCPUTimes()
	if err != nil {
		return Metrics{}, err
	}
	mem, swap, err := readMemory()
	if err != nil {
		return Metrics{}, err
	}
	load, _ := readLoad()
	uptime, _ := readUptime()
	netNow, _ := readNetwork(c.cfg.NetworkExclude)
	diskIONow, _ := readDiskCounters()

	if c.lastDisk.IsZero() || now.Sub(c.lastDisk) >= c.cfg.DiskInterval {
		if disks, err := readDisks(c.cfg.Mounts, c.cfg.DiskExcludeFS); err == nil {
			c.disks = disks
			c.lastDisk = now
		}
	}
	if c.lastConn.IsZero() || now.Sub(c.lastConn) >= c.cfg.ConnectionInterval {
		if conns, err := readConnections(); err == nil {
			c.conns = &conns
			c.lastConn = now
		}
	}

	cpuUsage := 0.0
	rxRate := uint64(0)
	txRate := uint64(0)
	diskReadRate := uint64(0)
	diskWriteRate := uint64(0)
	elapsed := 0.0
	if !c.lastTime.IsZero() {
		elapsed = now.Sub(c.lastTime).Seconds()
		cpuUsage = cpuNow.usageSince(c.lastCPU)
		if elapsed > 0 {
			if netNow.rx >= c.lastNet.rx {
				rxRate = uint64(float64(netNow.rx-c.lastNet.rx) / elapsed)
			}
			if netNow.tx >= c.lastNet.tx {
				txRate = uint64(float64(netNow.tx-c.lastNet.tx) / elapsed)
			}
			if diskIONow.read >= c.lastDiskIO.read {
				diskReadRate = uint64(float64(diskIONow.read-c.lastDiskIO.read) / elapsed)
			}
			if diskIONow.write >= c.lastDiskIO.write {
				diskWriteRate = uint64(float64(diskIONow.write-c.lastDiskIO.write) / elapsed)
			}
		}
	}
	if runtime.GOOS == "windows" && elapsed > 0 {
		load = c.updateWindowsLoad(cpuUsage, elapsed)
	}

	c.lastCPU = cpuNow
	c.lastNet = netNow
	c.lastDiskIO = diskIONow
	c.lastTime = now

	select {
	case <-ctx.Done():
		return Metrics{}, ctx.Err()
	default:
	}

	return Metrics{
		NodeID:         c.cfg.NodeID,
		Timestamp:      now.Unix(),
		OS:             c.staticOS,
		Arch:           c.staticArch,
		Hostname:       c.staticHost,
		Kernel:         c.staticKernel,
		OSName:         c.staticOSName,
		Virtualization: c.staticVirtualization,
		GPUs:           append([]string(nil), c.staticGPUs...),
		CPU:            CPU{UsagePercent: round2(cpuUsage), Cores: c.staticCores, PhysicalCores: c.staticPhysicalCores, ModelName: c.staticCPUModel},
		Memory:         mem,
		Swap:           swap,
		Load:           load,
		Uptime:         uptime,
		Disks:          c.disks,
		Network:        Network{RxBytes: netNow.rx, TxBytes: netNow.tx, RxRate: rxRate, TxRate: txRate},
		DiskIO:         DiskIO{ReadRate: diskReadRate, WriteRate: diskWriteRate},
		Conns:          c.conns,
		Processes:      readProcessCount(),
	}, nil
}

// Windows has no native Unix-style load average. Approximate it with the
// number of busy logical CPUs, then apply the same 1/5/15 minute exponential
// decay used by traditional load averages. This stays lightweight and makes
// the value comparable across machines with different core counts.
func (c *Collector) updateWindowsLoad(cpuUsage, elapsedSeconds float64) Load {
	cores := c.staticCores
	if cores < 1 {
		cores = 1
	}
	instant := math.Max(0, math.Min(100, cpuUsage)) / 100 * float64(cores)
	if !c.windowsLoadReady {
		c.windowsLoad = Load{Load1: instant, Load5: instant, Load15: instant}
		c.windowsLoadReady = true
	} else {
		c.windowsLoad.Load1 = exponentialLoad(c.windowsLoad.Load1, instant, elapsedSeconds, 60)
		c.windowsLoad.Load5 = exponentialLoad(c.windowsLoad.Load5, instant, elapsedSeconds, 5*60)
		c.windowsLoad.Load15 = exponentialLoad(c.windowsLoad.Load15, instant, elapsedSeconds, 15*60)
	}
	return Load{
		Load1:  round2(c.windowsLoad.Load1),
		Load5:  round2(c.windowsLoad.Load5),
		Load15: round2(c.windowsLoad.Load15),
	}
}

func exponentialLoad(previous, current, elapsedSeconds, windowSeconds float64) float64 {
	if elapsedSeconds <= 0 || windowSeconds <= 0 {
		return previous
	}
	alpha := 1 - math.Exp(-elapsedSeconds/windowSeconds)
	return previous + alpha*(current-previous)
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

type cpuTimes struct {
	idle  uint64
	total uint64
}

func (c cpuTimes) usageSince(prev cpuTimes) float64 {
	if c.total <= prev.total || c.idle < prev.idle {
		return 0
	}
	total := c.total - prev.total
	idle := c.idle - prev.idle
	if total == 0 || idle > total {
		return 0
	}
	return (1 - float64(idle)/float64(total)) * 100
}

type netCounters struct {
	rx uint64
	tx uint64
}

type diskCounters struct {
	read  uint64
	write uint64
}

type HostStaticInfo struct {
	Kernel         string
	OSName         string
	Virtualization string
	CPUModel       string
	PhysicalCores  int
	GPUs           []string
}
