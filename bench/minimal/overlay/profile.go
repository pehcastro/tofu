//go:build minimal

package sys

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"slices"
	"strconv"
	"time"

	"tofu/internal/safe"
)

const (
	profileDirEnv      = "TOFU_PPROF"
	countFileEnv       = "TOFU_MINIMAL_COUNT"
	profileMemoryRate  = 4096
	profileSampleEvery = 20 * time.Millisecond
	countEvery         = 20 * time.Millisecond
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

type goroutineCount struct {
	GOMAXPROCS       int    `json:"gomaxprocs"`
	PeakGoroutines   uint64 `json:"peak_goroutines"`
	SteadyGoroutines uint64 `json:"steady_goroutines"`
	PeakHeapBytes    uint64 `json:"peak_heap_bytes"`
	PeakStackBytes   uint64 `json:"peak_stack_bytes"`
	PeakGoTotalBytes uint64 `json:"peak_go_total_bytes"`
	Samples          int    `json:"samples"`
}

func readMemoryPoint(started time.Time) memoryPoint {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return memoryPoint{AtMS: time.Since(started).Milliseconds(), HeapInuse: m.HeapInuse, StackInuse: m.StackInuse, Sys: m.Sys, GCSys: m.GCSys,
		OtherSys: m.OtherSys, TotalAlloc: m.TotalAlloc, NumGC: m.NumGC, Goroutines: runtime.NumGoroutine()}
}

func writeProfile(name, path string, debug int) {
	file, err := os.Create(path)
	if err != nil {
		return
	}
	_ = pprof.Lookup(name).WriteTo(file, debug)
	_ = file.Close()
}

func startCount(path string) (stop func()) {
	read := []metrics.Sample{{Name: "/sched/goroutines:goroutines"}, {Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/memory/classes/heap/stacks:bytes"}, {Name: "/memory/classes/total:bytes"}}
	done, counted := make(chan struct{}), make(chan goroutineCount)
	go func() {
		count := goroutineCount{GOMAXPROCS: runtime.GOMAXPROCS(0)}
		var seen []uint64
		tick := time.NewTicker(countEvery)
		defer tick.Stop()
		for {
			metrics.Read(read)
			seen = append(seen, read[0].Value.Uint64())
			count.PeakHeapBytes = max(count.PeakHeapBytes, read[1].Value.Uint64())
			count.PeakStackBytes = max(count.PeakStackBytes, read[2].Value.Uint64())
			count.PeakGoTotalBytes = max(count.PeakGoTotalBytes, read[3].Value.Uint64())
			select {
			case <-done:
				count.PeakGoroutines, count.Samples = slices.Max(seen), len(seen)
				slices.Sort(seen)
				count.SteadyGoroutines = seen[len(seen)/2]
				counted <- count
				return
			case <-tick.C:
			}
		}
	}()
	return func() {
		close(done)
		body, _ := json.Marshal(<-counted)
		_ = os.WriteFile(path, body, 0o644)
	}
}

func startPprof(dir string) (stop func()) {
	cpu, err := os.Create(filepath.Join(dir, "cpu.pprof"))
	if err != nil {
		return func() {}
	}
	runtime.MemProfileRate = profileMemoryRate
	_ = pprof.StartCPUProfile(cpu)
	started, done, sampled := time.Now(), make(chan struct{}), make(chan []memoryPoint)
	go func() {
		var points []memoryPoint
		var peak uint64
		var mostGoroutines int
		tick := time.NewTicker(profileSampleEvery)
		defer tick.Stop()
		for {
			at := readMemoryPoint(started)
			points = append(points, at)
			if at.HeapInuse > peak+profilePeakStep {
				peak = at.HeapInuse
				writeProfile("heap", filepath.Join(dir, "heap-peak.pprof"), 0)
			}
			if at.Goroutines > mostGoroutines {
				mostGoroutines = at.Goroutines
				writeProfile("goroutine", filepath.Join(dir, "goroutine-peak.txt"), 1)
			}
			select {
			case <-done:
				sampled <- points
				return
			case <-tick.C:
			}
		}
	}()
	return func() {
		close(done)
		points := <-sampled
		pprof.StopCPUProfile()
		_ = cpu.Close()
		writeProfile("heap", filepath.Join(dir, "heap-end.pprof"), 0)
		body, _ := json.MarshalIndent(append(points, readMemoryPoint(started)), "", " ")
		_ = os.WriteFile(filepath.Join(dir, "memstats.json"), body, 0o644)
	}
}

func StartProfile() (stop func()) {
	stopCount, stopPprof := func() {}, func() {}
	if path := os.Getenv(countFileEnv); path != "" {
		stopCount = startCount(path)
	}
	if dir := os.Getenv(profileDirEnv); dir != "" && os.MkdirAll(dir, 0o755) == nil {
		stopPprof = startPprof(dir)
	}
	return func() {
		stopPprof()
		stopCount()
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
		writeProfile("heap", named(n, "heap.pprof"), 0)
		writeProfile("goroutine", named(n, "goroutines.txt"), 1)
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
