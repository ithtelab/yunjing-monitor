package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"vps-agent/internal/agent"
)

const (
	metricsHistoryBucket    = int64(60)
	metricsHistoryRetention = 7 * 24 * time.Hour
)

type MetricsHistorySample struct {
	Timestamp   int64   `json:"timestamp"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryUsed  uint64  `json:"memory_used"`
	MemoryTotal uint64  `json:"memory_total"`
	DiskUsed    uint64  `json:"disk_used"`
	DiskTotal   uint64  `json:"disk_total"`
	NetInSpeed  uint64  `json:"net_in_speed"`
	NetOutSpeed uint64  `json:"net_out_speed"`
	Load1       float64 `json:"load1"`
}

type metricsHistoryFile struct {
	Version int                               `json:"version"`
	Nodes   map[string][]MetricsHistorySample `json:"nodes"`
}

type MetricsHistoryStore struct {
	mu        sync.RWMutex
	saveMu    sync.Mutex
	path      string
	nodes     map[string][]MetricsHistorySample
	lastSaved time.Time
}

func NewMetricsHistoryStore(path string) *MetricsHistoryStore {
	h := &MetricsHistoryStore{path: path, nodes: map[string][]MetricsHistorySample{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		return h
	}
	var file metricsHistoryFile
	if json.Unmarshal(raw, &file) == nil && file.Nodes != nil {
		h.nodes = file.Nodes
		h.pruneLocked(time.Now().Add(-metricsHistoryRetention).Unix())
	}
	return h
}

func metricsHistoryPath(dataPath string) string {
	return filepath.Join(filepath.Dir(dataPath), "metrics-history.json")
}

func (h *MetricsHistoryStore) Add(metrics agent.Metrics) {
	if h == nil || metrics.NodeID == "" || metrics.Timestamp <= 0 {
		return
	}
	sample := MetricsHistorySample{
		Timestamp:   metrics.Timestamp,
		CPUPercent:  metrics.CPU.UsagePercent,
		MemoryUsed:  metrics.Memory.Used,
		MemoryTotal: metrics.Memory.Total,
		NetInSpeed:  metrics.Network.RxRate,
		NetOutSpeed: metrics.Network.TxRate,
		Load1:       metrics.Load.Load1,
	}
	for _, disk := range metrics.Disks {
		sample.DiskUsed += disk.Used
		sample.DiskTotal += disk.Total
	}

	h.mu.Lock()
	items := h.nodes[metrics.NodeID]
	if len(items) > 0 && items[len(items)-1].Timestamp/metricsHistoryBucket == sample.Timestamp/metricsHistoryBucket {
		items[len(items)-1] = sample
	} else {
		items = append(items, sample)
	}
	h.nodes[metrics.NodeID] = items
	h.pruneLocked(time.Now().Add(-metricsHistoryRetention).Unix())
	shouldSave := time.Since(h.lastSaved) >= 30*time.Second
	if shouldSave {
		h.lastSaved = time.Now()
	}
	h.mu.Unlock()
	if shouldSave {
		_ = h.Save()
	}
}

func (h *MetricsHistoryStore) Query(nodeID string, since int64) []MetricsHistorySample {
	if h == nil {
		return []MetricsHistorySample{}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	items := h.nodes[nodeID]
	start := sort.Search(len(items), func(i int) bool { return items[i].Timestamp >= since })
	out := append([]MetricsHistorySample(nil), items[start:]...)
	if out == nil {
		return []MetricsHistorySample{}
	}
	return out
}

func (h *MetricsHistoryStore) Delete(nodeID string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	delete(h.nodes, nodeID)
	h.mu.Unlock()
	_ = h.Save()
}

func (h *MetricsHistoryStore) Save() error {
	if h == nil || h.path == "" {
		return nil
	}
	h.saveMu.Lock()
	defer h.saveMu.Unlock()
	h.mu.RLock()
	copyNodes := make(map[string][]MetricsHistorySample, len(h.nodes))
	for id, items := range h.nodes {
		copyNodes[id] = append([]MetricsHistorySample(nil), items...)
	}
	h.mu.RUnlock()
	return writeJSONAtomic(h.path, metricsHistoryFile{Version: 1, Nodes: copyNodes}, 0o600)
}

func (h *MetricsHistoryStore) pruneLocked(cutoff int64) {
	for id, items := range h.nodes {
		start := sort.Search(len(items), func(i int) bool { return items[i].Timestamp >= cutoff })
		if start >= len(items) {
			delete(h.nodes, id)
			continue
		}
		if start > 0 {
			h.nodes[id] = append([]MetricsHistorySample(nil), items[start:]...)
		}
	}
}

func historyRange(value string) (time.Duration, error) {
	switch value {
	case "", "1h":
		return time.Hour, nil
	case "6h":
		return 6 * time.Hour, nil
	case "24h":
		return 24 * time.Hour, nil
	case "7d":
		return 7 * 24 * time.Hour, nil
	default:
		return 0, errors.New("range must be one of 1h, 6h, 24h or 7d")
	}
}
