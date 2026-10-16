package sys

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"time"

	"tofu/internal/safe"
)

const (
	profileDirEnv      = "TOFU_PPROF"
	profileMemoryRate  = 4096
	profileSampleEvery = 200 * time.Millisecond
	profilePeakStep    = 1 << 20
)

type memoryPoint struct {
	AtMS       int64  `json:"at_ms"`
	HeapInuse  uint64 `json:"heap_inuse"`
	StackInuse uint64 `json:"stack_inuse"`
	Sys        uint64 `json:"sys"`
	GCSys      uint64 `json:"gc_sys"`
	OtherSys   uint64 `json:"other_sys"`
	TotalAlloc uint64 `json:"total_alloc"`
	NumGC      uint32 `json:"num_gc"`
	Goroutines int    `json:"goroutines"`
}

func readMemoryPoint(started time.Time) memoryPoint {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return memoryPoint{AtMS: time.Since(started).Milliseconds(), HeapInuse: m.HeapInuse, StackInuse: m.StackInuse, Sys: m.Sys, GCSys: m.GCSys,
		OtherSys: m.OtherSys, TotalAlloc: m.TotalAlloc, NumGC: m.NumGC, Goroutines: runtime.NumGoroutine()}
}

func writeHeapProfile(path string) {
	file, err := os.Create(path)
	if err != nil {
		return
	}
	_ = pprof.Lookup("heap").WriteTo(file, 0)
	_ = file.Close()
}

func StartProfile() (stop func()) {
	dir := os.Getenv(profileDirEnv)
	if dir == "" || os.MkdirAll(dir, 0o755) != nil {
		return func() {}
	}
	cpu, err := os.Create(filepath.Join(dir, "cpu.pprof"))
	if err != nil {
		return func() {}
	}
	runtime.MemProfileRate = profileMemoryRate
	_ = pprof.StartCPUProfile(cpu)
	started, done, sampled := time.Now(), make(chan struct{}), make(chan []memoryPoint, 1)
	safe.Go(Crashes("."), "profile sampler", func() {
		var points []memoryPoint
		defer func() { sampled <- points }()
		var peak uint64
		tick := time.NewTicker(profileSampleEvery)
		defer tick.Stop()
		for {
			at := readMemoryPoint(started)
			points = append(points, at)
			if at.HeapInuse > peak+profilePeakStep {
				peak = at.HeapInuse
				writeHeapProfile(filepath.Join(dir, "heap-peak.pprof"))
			}
			select {
			case <-done:
				return
			case <-tick.C:
			}
		}
	})
	return func() {
		close(done)
		points := <-sampled
		pprof.StopCPUProfile()
		_ = cpu.Close()
		writeHeapProfile(filepath.Join(dir, "heap-end.pprof"))
		body, _ := json.MarshalIndent(append(points, readMemoryPoint(started)), "", " ")
		_ = os.WriteFile(filepath.Join(dir, "memstats.json"), body, 0o644)
	}
}
