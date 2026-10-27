package gpu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile writes body to path under dir, creating parent directories.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeSysfs builds a /sys/class/drm tree with one AMD card.
func fakeSysfs(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	card := filepath.Join(root, "card0", "device")

	writeFile(t, filepath.Join(card, "uevent"),
		"DRIVER=amdgpu\nPCI_CLASS=38000\nPCI_ID=1002:150E\nPCI_SUBSYS_ID=1002:150E\nPCI_SLOT_NAME=0000:c3:00.0\n")
	writeFile(t, filepath.Join(card, "gpu_busy_percent"), "6\n")
	writeFile(t, filepath.Join(card, "mem_info_vram_total"), "4294967296\n")
	writeFile(t, filepath.Join(card, "mem_info_vram_used"), "536870912\n")
	writeFile(t, filepath.Join(card, "mem_info_vis_vram_total"), "0\n")
	writeFile(t, filepath.Join(card, "mem_info_vis_vram_used"), "0\n")
	writeFile(t, filepath.Join(card, "mem_info_gtt_total"), "8589934592\n")
	writeFile(t, filepath.Join(card, "mem_info_gtt_used"), "1048576\n")

	hwmon := filepath.Join(card, "hwmon", "hwmon3")
	writeFile(t, filepath.Join(hwmon, "name"), "amdgpu\n")
	writeFile(t, filepath.Join(hwmon, "temp1_input"), "41000\n")
	writeFile(t, filepath.Join(hwmon, "temp1_label"), "edge\n")
	writeFile(t, filepath.Join(hwmon, "power1_average"), "3000000\n")
	writeFile(t, filepath.Join(hwmon, "power1_label"), "power\n")
	writeFile(t, filepath.Join(hwmon, "freq1_input"), "2400\n")

	return root
}

// fakeProcNvidia builds a /proc tree with one NVIDIA GPU's identity.
func fakeProcNvidia(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "driver", "nvidia", "gpus", "0000:c2:00.0")
	writeFile(t, filepath.Join(dir, "information"),
		"Model: \t\t NVIDIA GeForce RTX 5070 Laptop GPU\n"+
			"IRQ:    \t\t 55\n"+
			"GPU UUID: \t GPU-46ad4ec0-4e45-fd9b-0ace-61bf9f36bc79\n"+
			"Bus Location: \t\t 0000:c2:00.0\n"+
			"Device Minor: \t\t 0\n")
	return root
}

func TestSysfsProviderReadsAMD(t *testing.T) {
	sysRoot := fakeSysfs(t)
	p := SysfsProvider{Root: sysRoot, ProcRoot: t.TempDir()}

	devices, err := p.Sample()
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(devices))
	}
	d := devices[0]
	if d.Vendor != "amd" {
		t.Errorf("vendor = %q, want amd", d.Vendor)
	}
	if d.BusID != "0000:c3:00.0" {
		t.Errorf("bus id = %q, want 0000:c3:00.0", d.BusID)
	}
	if !d.Util.Ok || d.Util.V != 6 {
		t.Errorf("util = %+v, want 6 percent", d.Util)
	}
	if !d.MemTotal.Ok || d.MemTotal.V != 4294967296 {
		t.Errorf("mem total = %+v, want 4294967296 bytes", d.MemTotal)
	}
	if !d.MemUsed.Ok || d.MemUsed.V != 536870912 {
		t.Errorf("mem used = %+v, want 536870912 bytes", d.MemUsed)
	}
	if !d.Temp.Ok || d.Temp.V != 41 {
		t.Errorf("temp = %+v, want 41 celsius", d.Temp)
	}
	if !d.Power.Ok || d.Power.V != 3 {
		t.Errorf("power = %+v, want 3 watt", d.Power)
	}
	if d.ID == "" {
		t.Error("device has no stable id")
	}
}

func TestSysfsProviderWithoutAttributesReportsMissingNotZero(t *testing.T) {
	sysRoot := t.TempDir()
	card := filepath.Join(sysRoot, "card0", "device")
	// A card the driver exposes but whose telemetry attributes are absent:
	// the device must still be listed, with missing values rather than zeros.
	writeFile(t, filepath.Join(card, "uevent"),
		"DRIVER=amdgpu\nPCI_ID=1002:1638\nPCI_SLOT_NAME=0000:c1:00.0\n")

	p := SysfsProvider{Root: sysRoot, ProcRoot: t.TempDir()}
	devices, err := p.Sample()
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(devices))
	}
	d := devices[0]
	if d.Util.Ok {
		t.Error("util is present, want missing")
	}
	if d.Util.V != 0 {
		t.Errorf("missing util carries %v, want 0 with ok=false", d.Util.V)
	}
	if d.MemUsed.Ok {
		t.Error("mem used is present, want missing")
	}
	if d.Vendor != "amd" {
		t.Errorf("vendor = %q, want amd", d.Vendor)
	}
}

func TestSysfsProviderIgnoresConnectors(t *testing.T) {
	sysRoot := t.TempDir()
	for _, name := range []string{"card0", "card0-DP-1", "card0-eDP-2", "renderD128"} {
		writeFile(t, filepath.Join(sysRoot, name, "device", "uevent"),
			"DRIVER=amdgpu\nPCI_ID=1002:150E\nPCI_SLOT_NAME=0000:c3:00.0\n")
	}
	p := SysfsProvider{Root: sysRoot, ProcRoot: t.TempDir()}
	devices, err := p.Sample()
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices = %d, want 1: connectors and render nodes are not devices", len(devices))
	}
}

func TestSysfsProviderReadsNvidiaIdentityWithoutNVML(t *testing.T) {
	sysRoot := t.TempDir()
	writeFile(t, filepath.Join(sysRoot, "card0", "device", "uevent"),
		"DRIVER=nvidia\nPCI_ID=10DE:2D58\nPCI_SLOT_NAME=0000:c2:00.0\n")
	procRoot := fakeProcNvidia(t)

	p := SysfsProvider{Root: sysRoot, ProcRoot: procRoot}
	devices, err := p.Sample()
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(devices))
	}
	d := devices[0]
	if d.Vendor != "nvidia" {
		t.Errorf("vendor = %q, want nvidia", d.Vendor)
	}
	if d.Model != "NVIDIA GeForce RTX 5070 Laptop GPU" {
		t.Errorf("model = %q", d.Model)
	}
	if d.UUID != "GPU-46ad4ec0-4e45-fd9b-0ace-61bf9f36bc79" {
		t.Errorf("uuid = %q", d.UUID)
	}
	// Without NVML the dynamic numbers are not available. They must be
	// missing, not zero.
	if d.Util.Ok {
		t.Error("util is present, want missing without NVML")
	}
}

func TestNVMLProviderWithoutLibrarySaysWhy(t *testing.T) {
	// A missing library and a library that sees no GPUs are different facts.
	// The caller has to be able to tell them apart, so the reason is an
	// error rather than an empty list.
	p := NVMLProvider{LibPath: filepath.Join(t.TempDir(), "libnvidia-ml.so.1")}
	devices, err := p.Sample()
	if err == nil {
		t.Fatal("a missing library reported no error, want the reason")
	}
	if len(devices) != 0 {
		t.Errorf("devices = %d, want 0", len(devices))
	}
	if !strings.Contains(err.Error(), "libnvidia-ml.so.1") {
		t.Errorf("error = %v, want it to name the library it looked for", err)
	}
}
