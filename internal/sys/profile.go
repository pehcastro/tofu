package sys

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"time"

	"tofu/internal/safe"
)

const (
	profileDirEnv      = "TOFU_PPROF"
	profileMemoryRate  = 4096
	profileSampleEvery = 200 * time.Millisecond
	profilePeakStep    = 1 << 20
	SnapshotEvery      = 3 * time.Minute
	SnapshotsKept      = 5
	profileRunsKept    = 5
	profileFolder      = "profile"
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

func writeProfile(path, name string, debug int) {
	file, err := os.Create(path)
	if err != nil {
		return
	}
	_ = pprof.Lookup(name).WriteTo(file, debug)
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
				writeProfile(filepath.Join(dir, "heap-peak.pprof"), "heap", 0)
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
		writeProfile(filepath.Join(dir, "heap-end.pprof"), "heap", 0)
		body, _ := json.MarshalIndent(append(points, readMemoryPoint(started)), "", " ")
		_ = os.WriteFile(filepath.Join(dir, "memstats.json"), body, 0o644)
	}
}

func StartSnapshots(state string) (dir string, stop func(), err error) {
	root := filepath.Join(state, profileFolder)
	runs, _ := os.ReadDir(root)
	for _, old := range runs[:max(0, len(runs)-profileRunsKept+1)] {
		_ = os.RemoveAll(filepath.Join(root, old.Name()))
	}
	dir = filepath.Join(root, time.Now().Format("20060102-150405.000")+"-"+strconv.Itoa(os.Getpid()))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	named := func(n int, kind string) string { return filepath.Join(dir, fmt.Sprintf("%04d-%s", n, kind)) }
	startCPU := func(n int) *os.File {
		cpu, err := os.Create(named(n, "cpu.pprof"))
		if err == nil {
			_ = pprof.StartCPUProfile(cpu)
		}
		return cpu
	}
	snapshot := func(n int, cpu *os.File) {
		pprof.StopCPUProfile()
		_ = cpu.Close()
		writeProfile(named(n, "heap.pprof"), "heap", 0)
		writeProfile(named(n, "goroutines.txt"), "goroutine", 1)
		stale, _ := filepath.Glob(named(n-SnapshotsKept, "*"))
		for _, path := range stale {
			_ = os.Remove(path)
		}
	}
	done, finished := make(chan struct{}), make(chan struct{})
	safe.Go(Crashes("."), "profile snapshots", func() {
		defer close(finished)
		tick := time.NewTicker(SnapshotEvery)
		defer tick.Stop()
		for n, cpu := 1, startCPU(1); ; n++ {
			select {
			case <-done:
				snapshot(n, cpu)
				return
			case <-tick.C:
				snapshot(n, cpu)
				cpu = startCPU(n + 1)
			}
		}
	})
	return dir, func() {
		close(done)
		<-finished
	}, nil
}
