package main

import (
	"strings"
	"testing"
	"time"

	"github.com/revytechai/cloudtop/internal/model"
)

func TestRenderFitsTheTerminal(t *testing.T) {
	snapshot := model.Snapshot{
		At: time.Now(),
		CPU: model.CPUStat{
			Total: model.Measured("/proc/stat", "percent", 12.5),
			Cores: []model.Value{model.Measured("/proc/stat", "percent", 1)},
		},
		GPUs: []model.GPUDevice{
			{
				Vendor:   "nvidia",
				Model:    "NVIDIA GeForce RTX 5070 Laptop GPU",
				Util:     model.Measured("nvml", "percent", 3),
				MemUsed:  model.Measured("nvml", "bytes", 364*1024*1024),
				MemTotal: model.Measured("nvml", "bytes", 8*1024*1024*1024),
				Temp:     model.Measured("nvml", "celsius", 35),
				Power:    model.Measured("nvml", "watt", 5.2),
			},
		},
	}
	for _, line := range strings.Split(render(snapshot, true), "\n") {
		if len(line) > terminalWidth {
			t.Errorf("line is %d columns, want at most %d: %q", len(line), terminalWidth, line)
		}
	}
}

func TestRenderKeepsUnmeasuredValuesAsNA(t *testing.T) {
	snapshot := model.Snapshot{
		At:  time.Now(),
		CPU: model.CPUStat{Total: model.Missing("/proc/stat", "percent")},
		GPUs: []model.GPUDevice{{
			Vendor: "amd",
			BusID:  "0000:c3:00.0",
			Util:   model.Missing("amdgpu sysfs", "percent"),
			Temp:   model.Missing("amdgpu hwmon", "celsius"),
		}},
	}
	out := render(snapshot, false)
	if !strings.Contains(out, "N/A") {
		t.Errorf("output does not show N/A for an unmeasured value: %q", out)
	}
	if strings.Contains(out, "util 0.0%") {
		t.Errorf("an unmeasured utilisation was rendered as zero: %q", out)
	}
}

func TestWrapFieldsKeepsEveryField(t *testing.T) {
	fields := []string{"util 12.0%", "mem 497.3MiB/512.0MiB", "temp 48C", "power 18.0W"}
	lines := wrapFields(fields, deviceIndent)
	joined := strings.Join(lines, " ")
	for _, field := range fields {
		if !strings.Contains(joined, field) {
			t.Errorf("field %q was dropped: %q", field, joined)
		}
	}
	for _, line := range lines {
		if len(line) > terminalWidth {
			t.Errorf("line is %d columns, want at most %d: %q", len(line), terminalWidth, line)
		}
	}
}
