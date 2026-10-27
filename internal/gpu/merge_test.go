package gpu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/revytechai/cloudtop/internal/model"
)

func TestMergeKeepsOneRowPerDevice(t *testing.T) {
	// The same GPU seen by two providers: sysfs knows its identity, NVML
	// knows its numbers. Merged, it is one row with both.
	sysfs := []model.GPUDevice{{
		ID:     "0000:c2:00.0/GPU-46ad",
		Vendor: "nvidia",
		Model:  "NVIDIA GeForce RTX 5070 Laptop GPU",
		BusID:  "0000:c2:00.0",
		UUID:   "GPU-46ad",
		Source: "sysfs",
		Util:   model.Missing("sysfs", "percent"),
		Temp:   model.Missing("sysfs", "celsius"),
	}}
	nvml := []model.GPUDevice{{
		ID:       "0000:c2:00.0/GPU-46ad",
		Vendor:   "nvidia",
		Model:    "NVIDIA GeForce RTX 5070 Laptop GPU",
		BusID:    "0000:c2:00.0",
		UUID:     "GPU-46ad",
		Source:   "nvml",
		Util:     model.Measured("nvml", "percent", 12),
		MemUsed:  model.Measured("nvml", "bytes", 1024),
		MemTotal: model.Measured("nvml", "bytes", 4096),
		Temp:     model.Measured("nvml", "celsius", 44),
		Power:    model.Measured("nvml", "watt", 5.5),
	}}

	merged := Merge(sysfs, nvml)
	if len(merged) != 1 {
		t.Fatalf("merged = %d devices, want 1", len(merged))
	}
	d := merged[0]
	if !d.Util.Ok || d.Util.V != 12 {
		t.Errorf("util = %+v, want the nvml measurement", d.Util)
	}
	if !d.Temp.Ok || d.Temp.V != 44 {
		t.Errorf("temp = %+v, want the nvml measurement", d.Temp)
	}
	if d.Model != "NVIDIA GeForce RTX 5070 Laptop GPU" {
		t.Errorf("model = %q, want the identity the sysfs provider found", d.Model)
	}
	if d.Source != "sysfs,nvml" {
		t.Errorf("source = %q, want both providers named", d.Source)
	}
}

func TestMergeKeepsDistinctDevicesApart(t *testing.T) {
	a := []model.GPUDevice{{ID: "0000:c3:00.0", Vendor: "amd", Source: "sysfs"}}
	b := []model.GPUDevice{{ID: "0000:c2:00.0", Vendor: "nvidia", Source: "nvml"}}
	merged := Merge(a, b)
	if len(merged) != 2 {
		t.Fatalf("merged = %d devices, want 2", len(merged))
	}
}

func TestMergeFillsMissingFromLaterProvider(t *testing.T) {
	first := []model.GPUDevice{{
		ID:     "0000:c3:00.0",
		Vendor: "amd",
		Source: "sysfs",
		Util:   model.Measured("sysfs", "percent", 7),
		Temp:   model.Missing("sysfs", "celsius"),
	}}
	second := []model.GPUDevice{{
		ID:     "0000:c3:00.0",
		Vendor: "amd",
		Source: "other",
		Util:   model.Missing("other", "percent"),
		Temp:   model.Measured("other", "celsius", 40),
	}}
	merged := Merge(first, second)
	if len(merged) != 1 {
		t.Fatalf("merged = %d devices, want 1", len(merged))
	}
	if merged[0].Util.V != 7 {
		t.Errorf("util = %v, want the first provider's value kept", merged[0].Util.V)
	}
	if !merged[0].Temp.Ok || merged[0].Temp.V != 40 {
		t.Errorf("temp = %+v, want the later provider's value filled in", merged[0].Temp)
	}
}

func TestMergeKeepsIdenticalDevicesApart(t *testing.T) {
	// Two identical cards whose UUID could not be read are still two cards.
	// Merging them would silently drop the second card's numbers.
	first := []model.GPUDevice{{
		Vendor: "nvidia",
		Model:  "NVIDIA GeForce RTX 5070 Laptop GPU",
		Source: "nvml",
		Util:   model.Measured("nvml", "percent", 10),
	}}
	second := []model.GPUDevice{{
		Vendor: "nvidia",
		Model:  "NVIDIA GeForce RTX 5070 Laptop GPU",
		Source: "nvml",
		Util:   model.Measured("nvml", "percent", 90),
	}}
	merged := Merge(first, second)
	if len(merged) != 2 {
		t.Fatalf("merged = %d devices, want 2", len(merged))
	}
	if merged[0].Util.V != 10 || merged[1].Util.V != 90 {
		t.Errorf("util values = %v and %v, want each card to keep its own", merged[0].Util.V, merged[1].Util.V)
	}
	for i, d := range merged {
		if !strings.Contains(d.Source, "unidentified") {
			t.Errorf("device %d source = %q, want it to say it has no identity", i, d.Source)
		}
	}
}

func TestNvidiaBusIDForUUID(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "driver", "nvidia", "gpus", "0000:c2:00.0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "Model: \t\t NVIDIA GeForce RTX 5070 Laptop GPU\n" +
		"GPU UUID: \t GPU-46ad4ec0-4e45-fd9b-0ace-61bf9f36bc79\n" +
		"Bus Location: \t\t 0000:c2:00.0\n"
	if err := os.WriteFile(filepath.Join(dir, "information"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok := nvidiaBusIDForUUID(root, "GPU-46ad4ec0-4e45-fd9b-0ace-61bf9f36bc79")
	if !ok {
		t.Fatal("bus address not found for a uuid the kernel published")
	}
	if got != "0000:c2:00.0" {
		t.Errorf("bus address = %q, want 0000:c2:00.0", got)
	}
	if _, ok := nvidiaBusIDForUUID(root, "GPU-does-not-exist"); ok {
		t.Error("a uuid the kernel does not publish resolved to a bus address")
	}
}
