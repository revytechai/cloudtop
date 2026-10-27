// Command cloudtop shows what the machine is doing.
//
// It prints a summary line for the machine and a block of lines for each
// GPU, refreshing on an interval:
//
//	00:22:42  cpu 4.6% (24 cores)
//	  nvidia  NVIDIA GeForce RTX 5070 Laptop GPU
//	          util 0.0%  mem 364.4MiB/8.0GiB  temp 35C  power 5.2W
//	  amd     0000:c3:00.0
//	          util 6.0%  mem 497.3MiB/512.0MiB  temp 48C  power 18.0W
//
// Each block is laid out to fit an 80-column terminal. A quantity the driver
// does not publish is shown as N/A rather than zero. The -verbose flag names
// the source of every value.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/revytechai/cloudtop/internal/collect"
	"github.com/revytechai/cloudtop/internal/gpu"
	"github.com/revytechai/cloudtop/internal/model"
)

// The output is laid out for a terminal of this width. It is not queried
// from the terminal, so the output stays the same whether it is on a screen
// or in a pipe.
const (
	terminalWidth = 80
	deviceIndent  = 10
)

func main() {
	interval := flag.Duration("interval", time.Second, "refresh interval")
	once := flag.Bool("once", false, "print one sample and exit")
	verbose := flag.Bool("verbose", false, "name the source of every value")
	flag.Parse()

	if err := run(*interval, *once, *verbose); err != nil {
		fmt.Fprintln(os.Stderr, "cloudtop:", err)
		os.Exit(1)
	}
}

// run samples the machine and prints what it finds.
func run(interval time.Duration, once, verbose bool) error {
	cpu := collect.CPUReader{}
	providers := []gpu.Provider{gpu.SysfsProvider{}, &gpu.NVMLProvider{}}
	warned := map[string]string{}

	var times *collect.CPUTimes
	for {
		// The first reading has no previous one to compare against, so a
		// rate cannot be computed from it. Take a baseline, wait, and read
		// again.
		if times == nil {
			baseline, err := takeBaseline(cpu)
			if err != nil {
				return err
			}
			times = baseline
			time.Sleep(interval)
			continue
		}

		snapshot, next, err := sample(cpu, times, providers, warned)
		if err != nil {
			return err
		}
		times = next

		fmt.Println(render(snapshot, verbose))
		if once {
			return nil
		}
		time.Sleep(interval)
	}
}

// takeBaseline reads the counters the first rate is measured against.
func takeBaseline(cpu collect.CPUReader) (*collect.CPUTimes, error) {
	_, baseline, err := cpu.Sample(nil)
	if err != nil {
		return nil, fmt.Errorf("%w. Check that /proc is mounted and that this platform is supported", err)
	}
	return baseline, nil
}

// sample reads one snapshot: the CPU rate and every GPU.
func sample(cpu collect.CPUReader, times *collect.CPUTimes, providers []gpu.Provider, warned map[string]string) (model.Snapshot, *collect.CPUTimes, error) {
	snapshot := model.Snapshot{At: time.Now()}
	stat, next, err := cpu.Sample(times)
	if err != nil {
		return snapshot, nil, fmt.Errorf("%w. Check that /proc is mounted and that this platform is supported", err)
	}
	snapshot.CPU = stat

	var seen [][]model.GPUDevice
	for _, provider := range providers {
		devices, err := provider.Sample()
		if err != nil {
			// A provider that cannot read its devices loses its rows.
			// It does not stop the rest of the monitor, and the reason
			// stays on screen: a missing row with no reason reads as "no
			// GPU here".
			warn(provider, warned, err)
			snapshot.Notes = append(snapshot.Notes, provider.Name()+": "+err.Error())
			continue
		}
		seen = append(seen, devices)
	}
	// Several providers may see the same GPU. One GPU is one row.
	snapshot.GPUs = gpu.Merge(seen...)

	return snapshot, next, nil
}

// warn reports a provider failure once, so the reason is on screen without
// being repeated on every refresh.
func warn(provider gpu.Provider, warned map[string]string, err error) {
	if warned[provider.Name()] == err.Error() {
		return
	}
	warned[provider.Name()] = err.Error()
	fmt.Fprintf(os.Stderr, "cloudtop: %s: %v\n", provider.Name(), err)
}

// render formats one snapshot as a block of lines: the machine on the first,
// then a block for each GPU.
func render(s model.Snapshot, verbose bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  cpu %s", s.At.Format("15:04:05"), formatValue(s.CPU.Total, verbose))
	if len(s.CPU.Cores) > 0 {
		fmt.Fprintf(&b, " (%d cores)", len(s.CPU.Cores))
	}
	lines := []string{b.String()}
	for _, d := range s.GPUs {
		lines = append(lines, renderDevice(d, verbose)...)
	}
	for _, note := range s.Notes {
		lines = append(lines, "  ! "+note)
	}
	return strings.Join(lines, "\n")
}

// renderDevice lays one GPU out over as many lines as it needs.
//
// A GPU row carries a model name and four measurements, which does not fit
// on one line of an 80-column terminal, let alone on a machine with a dozen
// GPUs. The name goes on its own line and the measurements wrap under it.
func renderDevice(d model.GPUDevice, verbose bool) []string {
	name := d.Model
	if name == "" {
		name = d.BusID
	}
	if len(name) > terminalWidth-10 {
		name = name[:terminalWidth-13] + "..."
	}
	fields := []string{"util " + formatValue(d.Util, verbose)}
	if d.MemTotal.Ok || d.MemUsed.Ok {
		fields = append(fields, fmt.Sprintf("mem %s/%s",
			formatValue(d.MemUsed, verbose), formatValue(d.MemTotal, verbose)))
	}
	fields = append(fields,
		"temp "+formatValue(d.Temp, verbose),
		"power "+formatValue(d.Power, verbose),
	)
	lines := []string{fmt.Sprintf("  %-7s %s", d.Vendor, name)}
	return append(lines, wrapFields(fields, deviceIndent)...)
}

// wrapFields lays fields out over lines no wider than the terminal.
func wrapFields(fields []string, indent int) []string {
	prefix := strings.Repeat(" ", indent)
	var lines []string
	current := ""
	for _, field := range fields {
		candidate := field
		if current != "" {
			candidate = current + "  " + field
		}
		if indent+len(candidate) > terminalWidth && current != "" {
			lines = append(lines, prefix+current)
			current = field
			continue
		}
		current = candidate
	}
	if current != "" {
		lines = append(lines, prefix+current)
	}
	return lines
}

// formatValue renders a measured value, or N/A when it was not measured.
//
// In verbose mode the value names its own source, because a merged row can
// have numbers from two different providers.
func formatValue(v model.Value, verbose bool) string {
	if !v.Ok {
		return "N/A"
	}
	var text string
	switch v.Unit {
	case "percent":
		text = fmt.Sprintf("%.1f%%", v.V)
	case "bytes":
		text = formatBytes(v.V)
	case "celsius":
		text = fmt.Sprintf("%.0fC", v.V)
	case "watt":
		text = fmt.Sprintf("%.1fW", v.V)
	default:
		text = fmt.Sprintf("%.2f", v.V)
	}
	if verbose && v.Src != "" {
		text += "(" + v.Src + ")"
	}
	return text
}

// formatBytes renders a byte count in binary units.
func formatBytes(v float64) string {
	const unit = 1024
	if v < unit {
		return fmt.Sprintf("%dB", int64(v))
	}
	div, exp := float64(unit), 0
	for n := v / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", v/div, "KMGTPE"[exp])
}
