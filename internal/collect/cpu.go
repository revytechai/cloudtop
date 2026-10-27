// Package collect reads the machine's state and turns it into snapshots.
//
// Every collector here reads the kernel's own interfaces directly. There is
// no gopsutil, no psutil equivalent and no helper binary: the values come
// from /proc and /sys, and the source of each value is recorded with it.
package collect

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/revytechai/cloudtop/internal/model"
)

// DefaultProcStat is where Linux keeps the CPU counters.
const DefaultProcStat = "/proc/stat"

// SourceProcStat names where the CPU numbers came from.
const SourceProcStat = "/proc/stat"

// ErrNoCounters is returned when a /proc/stat file has no cpu line at all.
var ErrNoCounters = errors.New("no cpu counters found")

// Jiffies are the CPU time counters from /proc/stat.
type Jiffies struct {
	User    uint64
	Nice    uint64
	System  uint64
	Idle    uint64
	IOWait  uint64
	IRQ     uint64
	SoftIRQ uint64
	Steal   uint64
}

// Total is every jiffy counted.
func (j Jiffies) Total() uint64 {
	return j.User + j.Nice + j.System + j.Idle + j.IOWait + j.IRQ + j.SoftIRQ + j.Steal
}

// IdleTotal is the time the CPU was not doing work.
//
// IOWait counts as idle. A core blocked on I/O is not busy, and counting it
// as work makes a saturated disk look like a busy CPU.
func (j Jiffies) IdleTotal() uint64 { return j.Idle + j.IOWait }

// Busy is the time the CPU was doing work.
func (j Jiffies) Busy() uint64 {
	total := j.Total()
	if j.IdleTotal() > total {
		return 0
	}
	return total - j.IdleTotal()
}

// CPUTimes holds the aggregate counters and each core's counters.
type CPUTimes struct {
	Total Jiffies
	Cores []Jiffies
}

// ParseCPUTimes reads the cpu lines of a /proc/stat file.
//
// Lines that are not cpu lines (intr, ctxt, btime, process counts) are
// skipped rather than misread as a core.
func ParseCPUTimes(r io.Reader) (CPUTimes, error) {
	var (
		times CPUTimes
		found bool
	)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		switch {
		case fields[0] == "cpu":
			j, err := parseJiffies(fields[1:])
			if err != nil {
				return CPUTimes{}, err
			}
			times.Total = j
			found = true
		case isCoreName(fields[0]):
			j, err := parseJiffies(fields[1:])
			if err != nil {
				return CPUTimes{}, err
			}
			times.Cores = append(times.Cores, j)
		}
	}
	if err := sc.Err(); err != nil {
		return CPUTimes{}, fmt.Errorf("read cpu counters: %w", err)
	}
	if !found {
		return CPUTimes{}, ErrNoCounters
	}
	return times, nil
}

// isCoreName reports whether a /proc/stat label names a core (cpu0, cpu1).
func isCoreName(name string) bool {
	if len(name) < 4 || name[:3] != "cpu" {
		return false
	}
	for _, c := range name[3:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func parseJiffies(fields []string) (Jiffies, error) {
	values := make([]uint64, 0, 8)
	for _, f := range fields {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			// A field we do not know about is not an error: the kernel may
			// add counters. Stop at the first non-numeric field.
			break
		}
		values = append(values, v)
	}
	var j Jiffies
	set := func(dst *uint64, i int) {
		if i < len(values) {
			*dst = values[i]
		}
	}
	set(&j.User, 0)
	set(&j.Nice, 1)
	set(&j.System, 2)
	set(&j.Idle, 3)
	set(&j.IOWait, 4)
	set(&j.IRQ, 5)
	set(&j.SoftIRQ, 6)
	set(&j.Steal, 7)
	return j, nil
}

// busyPercent reports the busy share between two readings.
//
// It returns a missing value when there is no elapsed time to divide by, and
// when the counters went backwards, which happens when the kernel resets
// them. Both cases are "no rate", not "zero percent busy".
func busyPercent(prev, cur Jiffies) model.Value {
	if cur.Total() < prev.Total() {
		return model.Missing(SourceProcStat, "percent")
	}
	total := cur.Total() - prev.Total()
	if total == 0 {
		return model.Missing(SourceProcStat, "percent")
	}
	busy := cur.Busy() - prev.Busy()
	if cur.Busy() < prev.Busy() {
		return model.Missing(SourceProcStat, "percent")
	}
	return model.Measured(SourceProcStat, "percent", 100*float64(busy)/float64(total))
}

func busyCores(prev, cur []Jiffies) []model.Value {
	out := make([]model.Value, 0, len(cur))
	for i := range cur {
		if i >= len(prev) {
			out = append(out, model.Missing(SourceProcStat, "percent"))
			continue
		}
		out = append(out, busyPercent(prev[i], cur[i]))
	}
	return out
}

// CPUReader samples CPU utilisation from /proc/stat.
//
// The first sample has no previous reading to compare against and therefore
// reports nothing; every sample after it carries the baseline forward.
type CPUReader struct {
	// Path is the /proc/stat file to read. Empty means DefaultProcStat.
	Path string
}

func (r CPUReader) path() string {
	if r.Path != "" {
		return r.Path
	}
	return DefaultProcStat
}

// Sample reads the counters and, when prev is not nil, computes the busy
// share since that reading. It returns the new baseline in all cases.
func (r CPUReader) Sample(prev *CPUTimes) (model.CPUStat, *CPUTimes, error) {
	f, err := os.Open(r.path())
	if err != nil {
		return model.CPUStat{}, nil, fmt.Errorf("open %s: %w", r.path(), err)
	}
	defer f.Close()

	cur, err := ParseCPUTimes(f)
	if err != nil {
		return model.CPUStat{}, nil, fmt.Errorf("parse %s: %w", r.path(), err)
	}
	if prev == nil {
		return model.CPUStat{Total: model.Missing(SourceProcStat, "percent")}, &cur, nil
	}
	return model.CPUStat{
		Total: busyPercent(prev.Total, cur.Total),
		Cores: busyCores(prev.Cores, cur.Cores),
	}, &cur, nil
}
