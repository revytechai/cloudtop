// Package gpu reads GPU telemetry straight from the kernel and the vendor
// libraries.
//
// There is no helper binary, no ROCm-SMI and no intel_gpu_top. AMD and Intel
// GPUs are read from the kernel's own sysfs; NVIDIA GPUs are read through
// libnvidia-ml with purego, so the binary stays free of cgo.
//
// Every quantity a driver does not expose is reported as missing, never as
// zero.
package gpu

import "github.com/revytechai/cloudtop/internal/model"

// Provider reads GPU telemetry.
type Provider interface {
	// Name identifies the provider in the output.
	Name() string
	// Sample returns one reading per GPU the provider can see.
	Sample() ([]model.GPUDevice, error)
}
