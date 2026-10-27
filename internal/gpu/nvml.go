package gpu

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/revytechai/cloudtop/internal/model"
)

// NVML return codes we care about.
const (
	nvmlSuccess          = 0
	nvmlErrorAlreadyInit = 5
	nvmlTemperatureGPU   = 0
)

// nvmlUtilization is nvmlUtilization_t.
type nvmlUtilization struct {
	GPU    uint32
	Memory uint32
}

// nvmlMemory is nvmlMemory_t.
type nvmlMemory struct {
	Total uint64
	Free  uint64
	Used  uint64
}

// nvmlLib holds the NVML entry points we call.
type nvmlLib struct {
	initv2                    func() uint32
	shutdown                  func() uint32
	deviceGetCount            func(count *uint32) uint32
	deviceGetHandleByIndex    func(index uint32, device *unsafe.Pointer) uint32
	deviceGetUtilizationRates func(device unsafe.Pointer, util *nvmlUtilization) uint32
	deviceGetMemoryInfo       func(device unsafe.Pointer, mem *nvmlMemory) uint32
	deviceGetName             func(device unsafe.Pointer, name *byte, length uint32) uint32
	deviceGetUUID             func(device unsafe.Pointer, uuid *byte, length uint32) uint32
	deviceGetTemperature      func(device unsafe.Pointer, sensor int32, temp *uint32) uint32
	deviceGetPowerUsage       func(device unsafe.Pointer, milliwatts *uint32) uint32
}

// NVMLProvider reads NVIDIA GPUs through libnvidia-ml with purego.
//
// purego calls the library directly, so there is no cgo in the build and no
// process is forked per sample. When the library is absent the provider
// reports nothing rather than failing: a machine without an NVIDIA driver is
// a normal machine, not an error.
type NVMLProvider struct {
	// LibPath overrides the library location. Empty means discover it.
	LibPath string
	// ProcRoot is where the kernel publishes the GPU identity, used to map
	// a UUID to its PCI address. Empty means DefaultProcRoot.
	ProcRoot string

	once    sync.Once
	lib     *nvmlLib
	loadErr error
}

// Name identifies the provider.
func (p *NVMLProvider) Name() string { return "nvml" }

// procRoot is where the kernel publishes the GPU identity.
func (p *NVMLProvider) procRoot() string {
	if p.ProcRoot != "" {
		return p.ProcRoot
	}
	return DefaultProcRoot
}

// nvmlRemediation is what to do when the management library is missing.
const nvmlRemediation = "Install the NVIDIA driver, or point CLOUDTOP_NVML_LIB at the library"

// nvmlOverrideIgnored records an override that was not used.
var nvmlOverrideIgnored string

// nvmlCandidates lists the places the NVIDIA management library lives.
func nvmlCandidates() []string {
	if v := strings.TrimSpace(os.Getenv("CLOUDTOP_NVML_LIB")); v != "" {
		// The override chooses a library that is loaded into this process
		// and its constructors run there. An environment that is not the
		// login user's is therefore not allowed to choose it.
		if os.Geteuid() == os.Getuid() {
			return []string{v}
		}
		// The override is ignored, and the caller is told so rather than
		// left to wonder why the named library was not used.
		nvmlOverrideIgnored = v
	}
	candidates := []string{"libnvidia-ml.so.1"}
	switch runtime.GOARCH {
	case "amd64":
		candidates = append(candidates,
			"/usr/lib/x86_64-linux-gnu/libnvidia-ml.so.1",
			"/lib/x86_64-linux-gnu/libnvidia-ml.so.1")
	case "arm64":
		candidates = append(candidates,
			"/usr/lib/aarch64-linux-gnu/libnvidia-ml.so.1",
			"/lib/aarch64-linux-gnu/libnvidia-ml.so.1")
	}
	candidates = append(candidates,
		"/usr/lib64/libnvidia-ml.so.1",
		"/usr/local/lib/libnvidia-ml.so.1",
		"/usr/lib/libnvidia-ml.so.1")
	return candidates
}

// loadNVML binds the NVML entry points from the first library that loads.
//
// A library that cannot be found or bound is an error, not an empty result:
// "NVML is missing" and "NVML sees no GPUs" are different facts, and a
// caller that cannot tell them apart will report a healthy machine.
func loadNVML(path string) (*nvmlLib, error) {
	candidates := nvmlCandidates()
	if path != "" {
		candidates = []string{path}
	}
	var lastErr error
	for _, candidate := range candidates {
		lib := &nvmlLib{}
		if err := bindNVML(lib, candidate); err != nil {
			lastErr = err
			continue
		}
		return lib, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf(
			"no usable NVIDIA management library: %w. "+
				nvmlRemediation,
			lastErr,
		)
	}
	if nvmlOverrideIgnored != "" {
		return nil, fmt.Errorf(
			"no NVIDIA management library found (looked in %s). "+
				"CLOUDTOP_NVML_LIB=%s was ignored because the effective user "+
				"differs from the login user. "+
				nvmlRemediation,
			strings.Join(candidates, ", "),
			nvmlOverrideIgnored,
		)
	}
	return nil, fmt.Errorf(
		"no NVIDIA management library found (looked in %s). "+
			nvmlRemediation,
		strings.Join(candidates, ", "),
	)
}

// bindNVML binds one library's entry points.
//
// The symbols are resolved with Dlsym rather than purego's RegisterLibFunc,
// which panics when a symbol is missing. A driver without a given entry
// point must leave the provider unavailable, not take the process down.
func bindNVML(lib *nvmlLib, path string) error {
	handle, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("dlopen %s: %w", path, err)
	}
	symbols := []struct {
		name string
		ptr  any
	}{
		{"nvmlInit_v2", &lib.initv2},
		{"nvmlShutdown", &lib.shutdown},
		{"nvmlDeviceGetCount_v2", &lib.deviceGetCount},
		{"nvmlDeviceGetHandleByIndex_v2", &lib.deviceGetHandleByIndex},
		{"nvmlDeviceGetUtilizationRates", &lib.deviceGetUtilizationRates},
		{"nvmlDeviceGetMemoryInfo", &lib.deviceGetMemoryInfo},
		{"nvmlDeviceGetName", &lib.deviceGetName},
		{"nvmlDeviceGetUUID", &lib.deviceGetUUID},
		{"nvmlDeviceGetTemperature", &lib.deviceGetTemperature},
		{"nvmlDeviceGetPowerUsage", &lib.deviceGetPowerUsage},
	}
	for _, symbol := range symbols {
		addr, err := purego.Dlsym(handle, symbol.name)
		if err != nil {
			return fmt.Errorf("symbol %s: %w", symbol.name, err)
		}
		purego.RegisterFunc(symbol.ptr, addr)
	}
	return nil
}

// load binds the library once per process.
func (p *NVMLProvider) load() (*nvmlLib, error) {
	p.once.Do(func() {
		lib, err := loadNVML(p.LibPath)
		if err != nil {
			p.loadErr = err
			return
		}
		switch ret := lib.initv2(); ret {
		case nvmlSuccess, nvmlErrorAlreadyInit:
			p.lib = lib
		default:
			p.loadErr = fmt.Errorf("nvmlInit_v2 failed: %s", nvmlErrorText(ret))
		}
	})
	return p.lib, p.loadErr
}

// Sample reads every NVIDIA GPU NVML knows about.
//
// A quantity NVML cannot report is missing. On a unified-memory GPU such as
// the GB10, there is no separate VRAM to report, and the honest answer is
// "no figure", not "zero bytes".
func (p *NVMLProvider) Sample() ([]model.GPUDevice, error) {
	lib, err := p.load()
	if err != nil {
		return nil, err
	}
	var count uint32
	if ret := lib.deviceGetCount(&count); ret != nvmlSuccess {
		return nil, fmt.Errorf("nvmlDeviceGetCount failed: %s", nvmlErrorText(ret))
	}
	devices := make([]model.GPUDevice, 0, count)
	for i := uint32(0); i < count; i++ {
		var handle unsafe.Pointer
		if lib.deviceGetHandleByIndex(i, &handle) != nvmlSuccess {
			continue
		}
		devices = append(devices, p.readDevice(lib, handle))
	}
	return devices, nil
}

// readDevice reads one GPU.
func (p *NVMLProvider) readDevice(lib *nvmlLib, handle unsafe.Pointer) model.GPUDevice {
	d := model.GPUDevice{
		Vendor:   "nvidia",
		Source:   "nvml",
		Util:     model.Missing("nvml", "percent"),
		MemUsed:  model.Missing("nvml", "bytes"),
		MemTotal: model.Missing("nvml", "bytes"),
		Temp:     model.Missing("nvml", "celsius"),
		Power:    model.Missing("nvml", "watt"),
	}

	var name [96]byte
	if lib.deviceGetName(handle, &name[0], uint32(len(name))) == nvmlSuccess {
		d.Model = trimCString(name[:])
	}
	var uuid [96]byte
	if lib.deviceGetUUID(handle, &uuid[0], uint32(len(uuid))) == nvmlSuccess {
		d.UUID = trimCString(uuid[:])
		d.ID = d.UUID
		// NVML reports the UUID, not the PCI address. The kernel publishes
		// the mapping, so the device can be identified the same way the
		// sysfs provider identifies it.
		if busID, ok := nvidiaBusIDForUUID(p.procRoot(), d.UUID); ok {
			d.BusID = busID
			d.ID = busID + "/" + d.UUID
		}
	}

	var util nvmlUtilization
	if lib.deviceGetUtilizationRates(handle, &util) == nvmlSuccess {
		d.Util = model.Measured("nvml", "percent", float64(util.GPU))
	}
	var mem nvmlMemory
	if lib.deviceGetMemoryInfo(handle, &mem) == nvmlSuccess {
		d.MemTotal = model.Measured("nvml", "bytes", float64(mem.Total))
		d.MemUsed = model.Measured("nvml", "bytes", float64(mem.Used))
	}
	var temp uint32
	if lib.deviceGetTemperature(handle, nvmlTemperatureGPU, &temp) == nvmlSuccess {
		d.Temp = model.Measured("nvml", "celsius", float64(temp))
	}
	var power uint32
	if lib.deviceGetPowerUsage(handle, &power) == nvmlSuccess {
		d.Power = model.Measured("nvml", "watt", float64(power)/1000.0)
	}
	return d
}

// nvmlErrorText names the NVML return codes that matter here.
func nvmlErrorText(code uint32) string {
	switch code {
	case nvmlSuccess:
		return "success"
	case 1:
		return "uninitialized"
	case 2:
		return "invalid argument"
	case 3:
		return "not supported"
	case 4:
		return "no permission"
	case 5:
		return "already initialized"
	case 6:
		return "not found"
	case 7:
		return "insufficient size"
	default:
		return fmt.Sprintf("code %d", code)
	}
}

// trimCString cuts a C string at its NUL terminator.
func trimCString(b []byte) string {
	if i := indexByte(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}
