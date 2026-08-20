package agent

import "testing"

func TestUpdateWindowsLoadTracksBusyLogicalCPUs(t *testing.T) {
	collector := &Collector{staticCores: 8}
	first := collector.updateWindowsLoad(25, 2)
	if first.Load1 != 2 || first.Load5 != 2 || first.Load15 != 2 {
		t.Fatalf("first load = %#v", first)
	}

	second := collector.updateWindowsLoad(100, 60)
	if !(second.Load1 > second.Load5 && second.Load5 > second.Load15 && second.Load15 > first.Load15) {
		t.Fatalf("load windows did not decay in expected order: first=%#v second=%#v", first, second)
	}
}

func TestExponentialLoadKeepsPreviousWithoutElapsedTime(t *testing.T) {
	if got := exponentialLoad(3, 10, 0, 60); got != 3 {
		t.Fatalf("exponentialLoad() = %v, want 3", got)
	}
}
