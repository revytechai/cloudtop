package collect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStringReader(s string) *strings.Reader { return strings.NewReader(s) }

const sample1 = `cpu  1000 20 300 40000 500 0 10 0 0 0
cpu0 500 10 150 20000 250 0 5 0 0 0
cpu1 500 10 150 20000 250 0 5 0 0 0
intr 123456
ctxt 98765
btime 1700000000
`

const sample2 = `cpu  1100 20 350 40400 520 0 10 0 0 0
cpu0 550 10 175 20200 260 0 5 0 0 0
cpu1 550 10 175 20200 260 0 5 0 0 0
intr 123556
ctxt 98865
btime 1700000000
`

func writeStat(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestParseCPUTimes(t *testing.T) {
	times, err := ParseCPUTimes(newStringReader(sample1))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got, want := times.Total.User, uint64(1000); got != want {
		t.Errorf("total user = %d, want %d", got, want)
	}
	if got, want := len(times.Cores), 2; got != want {
		t.Fatalf("cores = %d, want %d", got, want)
	}
	if got, want := times.Cores[1].Idle, uint64(20000); got != want {
		t.Errorf("cpu1 idle = %d, want %d", got, want)
	}
	// intr/ctxt/btime must not be mistaken for a core.
	for _, c := range times.Cores {
		if c.Total() == 0 {
			t.Errorf("a parsed core has no jiffies: %+v", c)
		}
	}
}

func TestBusyPercent(t *testing.T) {
	prev, err := ParseCPUTimes(newStringReader(sample1))
	if err != nil {
		t.Fatalf("parse sample1: %v", err)
	}
	cur, err := ParseCPUTimes(newStringReader(sample2))
	if err != nil {
		t.Fatalf("parse sample2: %v", err)
	}

	total := busyPercent(prev.Total, cur.Total)
	if !total.Ok {
		t.Fatal("total busy is missing, want a measurement")
	}
	// delta total = 100+0+50+400+20 = 570; delta busy = 100+0+50+0+0+0+0 = 150
	want := 100 * 150.0 / 570.0
	if diff := total.V - want; diff > 0.01 || diff < -0.01 {
		t.Errorf("total busy = %.3f%%, want %.3f%%", total.V, want)
	}
	if total.Unit != "percent" {
		t.Errorf("unit = %q, want percent", total.Unit)
	}

	cores := busyCores(prev.Cores, cur.Cores)
	if len(cores) != 2 {
		t.Fatalf("cores = %d, want 2", len(cores))
	}
	if !cores[0].Ok || !cores[1].Ok {
		t.Fatal("a core busy value is missing, want a measurement")
	}
}

func TestBusyPercentWithoutDelta(t *testing.T) {
	times, err := ParseCPUTimes(newStringReader(sample1))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Sampling the same counters twice means no elapsed time, so there is no
	// rate to report. Reporting zero would claim the CPU is idle.
	got := busyPercent(times.Total, times.Total)
	if got.Ok {
		t.Errorf("busy with no delta = %v, want missing", got.V)
	}
}

func TestBusyPercentAfterCounterReset(t *testing.T) {
	prev, err := ParseCPUTimes(newStringReader(sample1))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cur, err := ParseCPUTimes(newStringReader(`cpu  10 0 0 100 0 0 0 0 0 0
cpu0 10 0 0 100 0 0 0 0 0 0
cpu1 10 0 0 100 0 0 0 0 0 0
`))
	if err != nil {
		t.Fatalf("parse reset sample: %v", err)
	}
	if got := busyPercent(prev.Total, cur.Total); got.Ok {
		t.Errorf("busy after a counter reset = %v, want missing", got.V)
	}
}

func TestCPUReaderSample(t *testing.T) {
	dir := t.TempDir()
	path := writeStat(t, dir, "stat", sample1)

	r := CPUReader{Path: path}
	first, times, err := r.Sample(nil)
	if err != nil {
		t.Fatalf("first sample: %v", err)
	}
	if first.Total.Ok {
		t.Error("the first sample has no baseline, want missing")
	}
	if times == nil {
		t.Fatal("first sample returned no baseline times")
	}

	// Rewrite the file with the second sample and sample again.
	writeStat(t, dir, "stat", sample2)
	second, _, err := r.Sample(times)
	if err != nil {
		t.Fatalf("second sample: %v", err)
	}
	if !second.Total.Ok {
		t.Error("the second sample is missing, want a measurement")
	}
	if len(second.Cores) != 2 {
		t.Errorf("cores = %d, want 2", len(second.Cores))
	}
}
