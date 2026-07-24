//go:build !windows

package buildspike

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

const maxMetricFileBytes = 64 << 10

type CgroupMetrics struct {
	CPUUsageUsec         int64
	CPUThrottledUsec     int64
	NrThrottled          int64
	MemoryCurrentBytes   int64
	MemoryPeakBytes      int64
	MemoryHighEvents     int64
	OOMKills             int64
	CPUPressureSomeAvg10 float64
	IOPressureSomeAvg10  float64
}

func ParseCgroupMetrics(files map[string][]byte) (CgroupMetrics, error) {
	required := []string{"cpu.stat", "memory.current", "memory.peak", "memory.events", "cpu.pressure", "io.pressure"}
	for _, name := range required {
		body, exists := files[name]
		if !exists || len(body) == 0 || len(body) > maxMetricFileBytes {
			return CgroupMetrics{}, errors.New("buildspike: cgroup metric evidence is missing or oversized")
		}
	}
	cpu, err := parseKeyValueIntegers(files["cpu.stat"])
	if err != nil {
		return CgroupMetrics{}, err
	}
	memoryEvents, err := parseKeyValueIntegers(files["memory.events"])
	if err != nil {
		return CgroupMetrics{}, err
	}
	usage, okUsage := cpu["usage_usec"]
	nrThrottled, okNr := cpu["nr_throttled"]
	throttledUsec, okThrottled := cpu["throttled_usec"]
	high, okHigh := memoryEvents["high"]
	oomKills, okOOM := memoryEvents["oom_kill"]
	if !okUsage || !okNr || !okThrottled || !okHigh || !okOOM {
		return CgroupMetrics{}, errors.New("buildspike: required cgroup metric is missing")
	}
	current, err := parseSingleInteger(files["memory.current"])
	if err != nil {
		return CgroupMetrics{}, err
	}
	peak, err := parseSingleInteger(files["memory.peak"])
	if err != nil {
		return CgroupMetrics{}, err
	}
	cpuPressure, err := parseSomeAvg10(files["cpu.pressure"])
	if err != nil {
		return CgroupMetrics{}, err
	}
	ioPressure, err := parseSomeAvg10(files["io.pressure"])
	if err != nil {
		return CgroupMetrics{}, err
	}
	return CgroupMetrics{
		CPUUsageUsec:         usage,
		CPUThrottledUsec:     throttledUsec,
		NrThrottled:          nrThrottled,
		MemoryCurrentBytes:   current,
		MemoryPeakBytes:      peak,
		MemoryHighEvents:     high,
		OOMKills:             oomKills,
		CPUPressureSomeAvg10: cpuPressure,
		IOPressureSomeAvg10:  ioPressure,
	}, nil
}

func parseKeyValueIntegers(body []byte) (map[string]int64, error) {
	result := make(map[string]int64)
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] == "" {
			return nil, errors.New("buildspike: cgroup key/value metric is invalid")
		}
		if _, exists := result[fields[0]]; exists {
			return nil, errors.New("buildspike: duplicate cgroup metric")
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || value < 0 || value > 1<<62 {
			return nil, errors.New("buildspike: cgroup integer metric is invalid")
		}
		result[fields[0]] = value
	}
	if len(result) == 0 {
		return nil, errors.New("buildspike: cgroup metric file is empty")
	}
	return result, nil
}

func parseSingleInteger(body []byte) (int64, error) {
	text := strings.TrimSpace(string(body))
	if text == "" || strings.ContainsAny(text, " \t\n\r") {
		return 0, errors.New("buildspike: scalar cgroup metric is invalid")
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil || value < 0 || value > 1<<62 {
		return 0, errors.New("buildspike: scalar cgroup metric is invalid")
	}
	return value, nil
}

func parseSomeAvg10(body []byte) (float64, error) {
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "some" {
			continue
		}
		for _, field := range fields[1:] {
			name, raw, found := strings.Cut(field, "=")
			if !found || name != "avg10" {
				continue
			}
			value, err := strconv.ParseFloat(raw, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
				return 0, errors.New("buildspike: pressure metric is invalid")
			}
			return value, nil
		}
	}
	return 0, errors.New("buildspike: pressure some avg10 is missing")
}
