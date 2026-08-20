package server

import "testing"

func TestInferRegion(t *testing.T) {
	name, code := inferRegion("香港 CN2 4C8G")
	if name != "香港" || code != "HK" {
		t.Fatalf("hongkong = %q %q", name, code)
	}
	name, code = inferRegion("US-node-001")
	if code != "US" {
		t.Fatalf("us prefix code = %q name=%q", code, name)
	}
	name, code = inferRegion("Tokyo-edge")
	if code != "JP" {
		t.Fatalf("tokyo = %q %q", name, code)
	}
}

func TestResolveNodeMeta(t *testing.T) {
	display, region, code := resolveNodeMeta("香港 CN2", "", "n_abc")
	if display != "香港 CN2" || region != "香港" || code != "HK" {
		t.Fatalf("meta = %q %q %q", display, region, code)
	}
	display, region, code = resolveNodeMeta("", "日本", "n_abc")
	if display != "n_abc" || code != "JP" {
		t.Fatalf("region only meta = %q %q %q", display, region, code)
	}
}
