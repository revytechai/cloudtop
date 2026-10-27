package gpu

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/revytechai/cloudtop/internal/model"
)

// Defaults for the provider's roots.
const (
	DefaultDRMRoot  = "/sys/class/drm"
	DefaultProcRoot = "/proc"
)

// PCI vendor identifiers, as they appear in the first half of a PCI_ID.
const (
	pciVendorNVIDIA = "10de"
	pciVendorAMD    = "1002"
	pciVendorIntel  = "8086"
)

// SysfsProvider reads GPU state from the kernel's sysfs.
//
// It is the primary path for AMD and Intel GPUs, and it supplies the static
// identity of NVIDIA GPUs (model and UUID) that the kernel exposes without
// NVML. The dynamic NVIDIA numbers come from NVMLProvider.
type SysfsProvider struct {
	// Root is the DRM sysfs root. Empty means DefaultDRMRoot.
	Root string
	// ProcRoot is the proc root, used for the NVIDIA identity file.
	ProcRoot string
}

// Name identifies the provider.
func (p SysfsProvider) Name() string { return "sysfs" }

func (p SysfsProvider) drmRoot() string {
	if p.Root != "" {
		return p.Root
	}
	return DefaultDRMRoot
}

func (p SysfsProvider) procRoot() string {
	if p.ProcRoot != "" {
		return p.ProcRoot
	}
	return DefaultProcRoot
}

// Sample enumerates the GPUs the kernel exposes and reads what each driver
// publishes.
func (p SysfsProvider) Sample() ([]model.GPUDevice, error) {
	entries, err := p.enumerate()
	if err != nil {
		return nil, err
	}
	devices := make([]model.GPUDevice, 0, len(entries))
	for _, entry := range entries {
		device := entry.device
		switch device.Vendor {
		case "amd":
			p.fillAMD(entry.cardDir, &device)
		case "nvidia":
			p.fillNVIDIAIdentity(&device)
		}
		devices = append(devices, device)
	}
	return devices, nil
}

// cardEntry is one DRM card and the directory its attributes live in.
type cardEntry struct {
	device  model.GPUDevice
	cardDir string
}

// enumerate finds the DRM card devices.
//
// Connector nodes (card0-DP-1) and render nodes (renderD128) are not
// devices and are skipped.
func (p SysfsProvider) enumerate() ([]cardEntry, error) {
	entries, err := os.ReadDir(p.drmRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf(
				"no DRM devices at %s: load the GPU driver for the card "+
					"(amdgpu, i915, nvidia) and check that /sys is mounted",
				p.drmRoot(),
			)
		}
		return nil, err
	}
	var cards []cardEntry
	for _, entry := range entries {
		name := entry.Name()
		if !isCardName(name) {
			continue
		}
		cardDir := filepath.Join(p.drmRoot(), name, "device")
		uevent, ok := readFile(filepath.Join(cardDir, "uevent"))
		if !ok {
			return nil, fmt.Errorf(
				"the driver for %s publishes no identity, so the card cannot be "+
					"identified: check dmesg for the driver, and the permissions "+
					"on %s",
				name,
				filepath.Join(cardDir, "uevent"),
			)
		}
		fields := parseKeyValues(uevent)
		busID := fields["PCI_SLOT_NAME"]
		if busID == "" {
			continue
		}
		vendor := vendorFromPCIID(fields["PCI_ID"])
		cards = append(cards, cardEntry{
			device: model.GPUDevice{
				ID:     busID,
				Vendor: vendor,
				BusID:  busID,
				Source: p.Name(),
			},
			cardDir: cardDir,
		})
	}
	return cards, nil
}

// isCardName reports whether a DRM node names a card rather than a
// connector or a render node.
func isCardName(name string) bool {
	if !strings.HasPrefix(name, "card") {
		return false
	}
	rest := name[len("card"):]
	if rest == "" {
		return false
	}
	for _, c := range rest {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// vendorFromPCIID maps a PCI vendor identifier to a vendor name.
func vendorFromPCIID(pciID string) string {
	if len(pciID) < 4 {
		return "unknown"
	}
	switch strings.ToLower(pciID[:4]) {
	case pciVendorNVIDIA:
		return "nvidia"
	case pciVendorAMD:
		return "amd"
	case pciVendorIntel:
		return "intel"
	default:
		return "unknown"
	}
}

// The sources recorded with each measured value.
const (
	SourceAMDGpuSysfs = "amdgpu sysfs"
	SourceAMDGpuHwmon = "amdgpu hwmon"
)

// fillAMD reads the amdgpu attributes.
func (p SysfsProvider) fillAMD(cardDir string, d *model.GPUDevice) {
	if v, ok := readUint(filepath.Join(cardDir, "gpu_busy_percent")); ok {
		d.Util = model.Measured(SourceAMDGpuSysfs, "percent", float64(v))
	} else {
		d.Util = model.Missing(SourceAMDGpuSysfs, "percent")
	}
	if v, ok := readUint(filepath.Join(cardDir, "mem_info_vram_total")); ok {
		d.MemTotal = model.Measured(SourceAMDGpuSysfs, "bytes", float64(v))
	} else {
		d.MemTotal = model.Missing(SourceAMDGpuSysfs, "bytes")
	}
	if v, ok := readUint(filepath.Join(cardDir, "mem_info_vram_used")); ok {
		d.MemUsed = model.Measured(SourceAMDGpuSysfs, "bytes", float64(v))
	} else {
		d.MemUsed = model.Missing(SourceAMDGpuSysfs, "bytes")
	}

	hwmon := p.hwmonDir(cardDir)
	if hwmon == "" {
		d.Temp = model.Missing(SourceAMDGpuHwmon, "celsius")
		d.Power = model.Missing(SourceAMDGpuHwmon, "watt")
		return
	}
	if v, ok := readUint(filepath.Join(hwmon, "temp1_input")); ok {
		d.Temp = model.Measured(SourceAMDGpuHwmon, "celsius", float64(v)/1000.0)
	} else {
		d.Temp = model.Missing(SourceAMDGpuHwmon, "celsius")
	}
	if v, ok := readUint(filepath.Join(hwmon, "power1_average")); ok {
		d.Power = model.Measured(SourceAMDGpuHwmon, "watt", float64(v)/1e6)
	} else {
		d.Power = model.Missing(SourceAMDGpuHwmon, "watt")
	}
}

// hwmonDir finds the hwmon directory of a device.
func (p SysfsProvider) hwmonDir(cardDir string) string {
	matches, err := filepath.Glob(filepath.Join(cardDir, "hwmon", "hwmon*"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// fillNVIDIAIdentity reads the NVIDIA identity the kernel publishes without
// NVML. The dynamic numbers are not available here; NVMLProvider has them.
func (p SysfsProvider) fillNVIDIAIdentity(d *model.GPUDevice) {
	path := filepath.Join(p.procRoot(), "driver", "nvidia", "gpus", d.BusID, "information")
	body, ok := readFile(path)
	if !ok {
		return
	}
	fields := parseKeyValues(body)
	if name := strings.TrimSpace(fields["Model"]); name != "" {
		d.Model = name
	}
	if uuid := strings.TrimSpace(fields["GPU UUID"]); uuid != "" {
		d.UUID = uuid
		d.ID = d.BusID + "/" + uuid
	}
}

// nvidiaBusIDForUUID maps an NVIDIA GPU UUID to its PCI bus address using
// the identity the kernel publishes in /proc/driver/nvidia.
func nvidiaBusIDForUUID(procRoot, uuid string) (string, bool) {
	if uuid == "" {
		return "", false
	}
	dir := filepath.Join(procRoot, "driver", "nvidia", "gpus")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		body, ok := readFile(filepath.Join(dir, entry.Name(), "information"))
		if !ok {
			continue
		}
		fields := parseKeyValues(body)
		if strings.TrimSpace(fields["GPU UUID"]) != uuid {
			continue
		}
		if bus := strings.TrimSpace(fields["Bus Location"]); bus != "" {
			return bus, true
		}
		return entry.Name(), true
	}
	return "", false
}

// parseKeyValues parses the key/value lines the kernel writes.
//
// Two shapes appear in the files we read: sysfs uevent writes KEY=VALUE,
// and the NVIDIA information file writes "Key: value". The split character is
// whichever separator comes first, so a PCI id like 1002:150E stays intact.
func parseKeyValues(body string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(body, "\n") {
		colon := strings.IndexByte(line, ':')
		equals := strings.IndexByte(line, '=')
		var key, value string
		switch {
		case colon < 0 && equals < 0:
			continue
		case equals >= 0 && (colon < 0 || equals < colon):
			key, value = line[:equals], line[equals+1:]
		default:
			key, value = line[:colon], line[colon+1:]
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}

// readFile reads a whole file.
func readFile(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// readUint reads a file holding a single unsigned integer.
func readUint(path string) (uint64, bool) {
	body, ok := readFile(path)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseUint(strings.TrimSpace(body), 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
