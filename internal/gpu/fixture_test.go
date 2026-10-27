package gpu

import (
	"os"
	"path/filepath"
	"testing"
)

// The fixtures in testdata were copied from the live machine: the DRM
// attributes of the AMD card and the identity file the kernel publishes for
// the NVIDIA card.

func fixture(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"testdata"}, parts...)...)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture %s: %v", path, err)
	}
	return path
}

func TestRecordedAMDAttributesParse(t *testing.T) {
	p := SysfsProvider{
		Root:     fixture(t, "drm"),
		ProcRoot: fixture(t, "proc"),
	}
	devices, err := p.Sample()
	if err != nil {
		t.Fatalf("sample: %v", err)
	}

	var found bool
	for i := range devices {
		if devices[i].Vendor != "amd" {
			continue
		}
		found = true
		d := devices[i]
		if !d.Util.Ok {
			t.Error("util is missing, the recorded card reports one")
		}
		if !d.MemTotal.Ok || d.MemTotal.V != 536870912 {
			t.Errorf("mem total = %+v, want 536870912", d.MemTotal)
		}
		if !d.MemUsed.Ok {
			t.Error("mem used is missing, the recorded card reports one")
		}
		if !d.Temp.Ok {
			t.Error("temp is missing, the recorded card reports one")
		}
		if !d.Power.Ok {
			t.Error("power is missing, the recorded card reports one")
		}
	}
	if !found {
		t.Fatal("the recorded AMD card was not found")
	}
}

func TestRecordedNvidiaIdentityParses(t *testing.T) {
	body, err := os.ReadFile(fixture(t, "proc", "driver", "nvidia", "gpus", "0000:c2:00.0", "information"))
	if err != nil {
		t.Fatal(err)
	}
	fields := parseKeyValues(string(body))
	if got := fields["GPU UUID"]; got != "GPU-46ad4ec0-4e45-fd9b-0ace-61bf9f36bc79" {
		t.Errorf("uuid = %q", got)
	}
	if got := fields["Model"]; got != "NVIDIA GeForce RTX 5070 Laptop GPU" {
		t.Errorf("model = %q", got)
	}
	if got := fields["Bus Location"]; got != "0000:c2:00.0" {
		t.Errorf("bus location = %q", got)
	}
}

func TestRecordedNvidiaUUIDResolvesToItsBusAddress(t *testing.T) {
	bus, ok := nvidiaBusIDForUUID(fixture(t, "proc"), "GPU-46ad4ec0-4e45-fd9b-0ace-61bf9f36bc79")
	if !ok {
		t.Fatal("the recorded uuid did not resolve")
	}
	if bus != "0000:c2:00.0" {
		t.Errorf("bus address = %q, want 0000:c2:00.0", bus)
	}
}
