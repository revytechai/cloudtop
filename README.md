# cloudtop

A terminal system monitor with first-class GPU telemetry.

One Go binary. No cgo, no Python, no helper daemons. Every number it shows
comes from the kernel's own interfaces or from the vendor's own management
library, and `--verbose` names the source of every value.

The binary is dynamically linked: reading the NVIDIA management library at
run time needs libc, so purego cannot produce a fully static build.

## What it shows

- Aggregate CPU utilisation and the core count, from `/proc/stat` deltas.
- GPU telemetry per vendor: utilisation, memory, temperature, power.

## Not yet

- Per-core CPU utilisation, which is collected and not displayed.
- Per-process GPU usage.
- The display layer: this build prints a summary line and a block of lines per GPU.
- Configuration and themes.
- FreeBSD collectors.

## GPU support

| Vendor | Telemetry from | Needs |
|---|---|---|
| AMD | kernel sysfs (`gpu_busy_percent`, `mem_info_vram_*`, hwmon) | nothing |
| Intel | kernel sysfs, identity only for now | nothing |
| NVIDIA | `libnvidia-ml.so.1`, called through purego | the driver |

There is no ROCm-SMI, no `intel_gpu_top`, no cgo, and no process forked per
sample. NVIDIA's driver publishes the GPU's identity without NVML at
`/proc/driver/nvidia/gpus/<address>/information`, so an NVIDIA card is still
listed, with its model and UUID, on a machine where the management library is
absent.

A quantity the driver does not publish is shown as `N/A`, never as zero. On a
unified-memory GPU such as the GB10 there is no separate VRAM to report, and
cloudtop shows whatever the management library reports rather than guessing at
what that figure means.

## Building

```sh
make build      # binary in dist/
make test       # go test ./...
make check      # fmt, vet and test
```

Cross-compile for the supported platforms (Linux and FreeBSD, on amd64 and
arm64):

```sh
make build-all  # linux and freebsd, amd64 and arm64
```

## Status

Early. It prints a summary line for the machine and a block of lines for each
GPU, and nothing else yet. The display layer, the process list, the
configuration and the FreeBSD collectors are not here.

## Licence

BSD-3-Clause. See [LICENSE](LICENSE).
