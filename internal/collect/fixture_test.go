package collect

import (
	"os"
	"path/filepath"
	"testing"
)

// The fixtures in testdata were recorded from a live /proc/stat on the
// development machine, one second apart. They are what the kernel actually
// wrote, not a hand-written sample.

func fixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture %s: %v", path, err)
	}
	return path
}

func TestRecordedProcStatParses(t *testing.T) {
	f, err := os.Open(fixture(t, "proc_stat"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	times, err := ParseCPUTimes(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if times.Total.Total() == 0 {
		t.Error("recorded counters are all zero, the fixture is wrong")
	}
	if len(times.Cores) != 24 {
		t.Errorf("cores = %d, want the 24 the machine has", len(times.Cores))
	}
	if times.Total.User == 0 {
		t.Error("aggregate user jiffies are zero, the fixture is wrong")
	}
}

func TestRecordedProcStatGivesAPlausibleRate(t *testing.T) {
	reader := CPUReader{Path: fixture(t, "proc_stat")}

	first, times, err := reader.Sample(nil)
	if err != nil {
		t.Fatalf("first sample: %v", err)
	}
	if first.Total.Ok {
		t.Error("the first sample has no baseline and must report nothing")
	}

	// The second fixture was recorded a second later.
	reader.Path = fixture(t, "proc_stat_2")
	second, _, err := reader.Sample(times)
	if err != nil {
		t.Fatalf("second sample: %v", err)
	}
	if !second.Total.Ok {
		t.Fatal("the second sample reports no rate, want a measurement")
	}
	if second.Total.V < 0 || second.Total.V > 100 {
		t.Errorf("busy = %v%%, want a percentage between 0 and 100", second.Total.V)
	}
	if len(second.Cores) != 24 {
		t.Errorf("cores = %d, want 24", len(second.Cores))
	}
	for i, core := range second.Cores {
		if !core.Ok {
			t.Errorf("core %d reports no rate", i)
			continue
		}
		if core.V < 0 || core.V > 100 {
			t.Errorf("core %d busy = %v%%, want a percentage between 0 and 100", i, core.V)
		}
	}
}
