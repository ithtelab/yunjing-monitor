//go:build windows

package agent

import "testing"

func TestDetectWindowsVirtualization(t *testing.T) {
	tests := []struct {
		name         string
		manufacturer string
		model        string
		hypervisor   bool
		want         string
	}{
		{name: "vmware", manufacturer: "VMware, Inc.", model: "VMware Virtual Platform", want: "VMware"},
		{name: "virtualbox", manufacturer: "innotek GmbH", model: "VirtualBox", want: "VirtualBox"},
		{name: "kvm", manufacturer: "QEMU", model: "Standard PC (Q35 + ICH9, 2009)", want: "KVM"},
		{name: "hyper-v guest", manufacturer: "Microsoft Corporation", model: "Virtual Machine", want: "Hyper-V"},
		{name: "hyper-v enabled", manufacturer: "ASUS", model: "System Product Name", hypervisor: true, want: "Hyper-V"},
		{name: "physical", manufacturer: "Dell Inc.", model: "PowerEdge R740", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := detectWindowsVirtualization(test.manufacturer, test.model, test.hypervisor); got != test.want {
				t.Fatalf("detectWindowsVirtualization() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPageFileMemory(t *testing.T) {
	memory := pageFileMemory(12*1024, 953)
	if memory.Total != 12*1024*1024*1024 {
		t.Fatalf("total = %d", memory.Total)
	}
	if memory.Used != 953*1024*1024 || memory.Free != memory.Total-memory.Used {
		t.Fatalf("memory = %#v", memory)
	}

	clamped := pageFileMemory(1024, 2048)
	if clamped.Used != clamped.Total || clamped.Free != 0 {
		t.Fatalf("clamped memory = %#v", clamped)
	}
}

func TestFilterWindowsGPUs(t *testing.T) {
	got := filterWindowsGPUs([]string{
		"GameViewer Virtual Display Adapter",
		"Microsoft Basic Display Adapter",
		"NVIDIA GeForce RTX 5070",
		"AMD Radeon(TM) Graphics",
		"nvidia geforce rtx 5070",
		" ",
	})
	want := []string{"NVIDIA GeForce RTX 5070", "AMD Radeon(TM) Graphics"}
	if len(got) != len(want) {
		t.Fatalf("gpus = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("gpus[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
