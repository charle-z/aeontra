//go:build !windows

package buildspike

import "testing"

func TestParseCgroupMetricsCapturesBoundedCPUAndMemoryEvidence(t *testing.T) {
	metrics, err := ParseCgroupMetrics(map[string][]byte{
		"cpu.stat":       []byte("usage_usec 120000\nuser_usec 90000\nsystem_usec 30000\nnr_periods 20\nnr_throttled 5\nthrottled_usec 20000\n"),
		"memory.current": []byte("104857600\n"),
		"memory.peak":    []byte("209715200\n"),
		"memory.events":  []byte("low 0\nhigh 2\nmax 0\noom 0\noom_kill 0\n"),
		"cpu.pressure":   []byte("some avg10=12.34 avg60=5.00 avg300=1.00 total=123456\nfull avg10=0.50 avg60=0.20 avg300=0.10 total=100\n"),
		"io.pressure":    []byte("some avg10=1.25 avg60=0.50 avg300=0.10 total=1000\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.CPUUsageUsec != 120000 || metrics.CPUThrottledUsec != 20000 || metrics.NrThrottled != 5 || metrics.MemoryCurrentBytes != 104857600 || metrics.MemoryPeakBytes != 209715200 || metrics.MemoryHighEvents != 2 || metrics.OOMKills != 0 || metrics.CPUPressureSomeAvg10 != 12.34 || metrics.IOPressureSomeAvg10 != 1.25 {
		t.Fatalf("metrics=%+v", metrics)
	}
}

func TestParseCgroupMetricsRejectsMissingMalformedAndOversizedEvidence(t *testing.T) {
	valid := map[string][]byte{
		"cpu.stat":       []byte("usage_usec 1\nnr_throttled 0\nthrottled_usec 0\n"),
		"memory.current": []byte("1\n"),
		"memory.peak":    []byte("1\n"),
		"memory.events":  []byte("high 0\noom_kill 0\n"),
		"cpu.pressure":   []byte("some avg10=0.00 avg60=0.00 avg300=0.00 total=0\n"),
		"io.pressure":    []byte("some avg10=0.00 avg60=0.00 avg300=0.00 total=0\n"),
	}
	cases := []map[string][]byte{
		{},
		func() map[string][]byte { value := cloneMetricFiles(valid); delete(value, "cpu.stat"); return value }(),
		func() map[string][]byte {
			value := cloneMetricFiles(valid)
			value["memory.current"] = []byte("-1\n")
			return value
		}(),
		func() map[string][]byte {
			value := cloneMetricFiles(valid)
			value["cpu.pressure"] = []byte("some avg10=NaN total=0\n")
			return value
		}(),
		func() map[string][]byte {
			value := cloneMetricFiles(valid)
			value["cpu.stat"] = make([]byte, 70<<10)
			return value
		}(),
	}
	for index, files := range cases {
		if _, err := ParseCgroupMetrics(files); err == nil {
			t.Fatalf("invalid metrics case %d accepted", index)
		}
	}
}

func cloneMetricFiles(input map[string][]byte) map[string][]byte {
	output := make(map[string][]byte, len(input))
	for key, value := range input {
		output[key] = append([]byte(nil), value...)
	}
	return output
}
