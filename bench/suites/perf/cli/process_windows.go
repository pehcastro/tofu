package main

import (
	"os"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type processMemoryCounters struct {
	size                       uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
}

type meter struct {
	cyclesPerSecond float64
	processCycles   *windows.LazyProc
	memory          *windows.LazyProc
}

const cpuMethod = "CPU is QueryProcessCycleTime at a cycle rate calibrated by spinning one locked thread, because Windows user and kernel times move in 15.6 ms ticks; RSS is the peak working set"

func newMeter() meter {
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	threadCycles := kernel.NewProc("QueryThreadCycleTime")
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var best float64
	for range 5 {
		var before, after uint64
		_, _, _ = threadCycles.Call(uintptr(windows.CurrentThread()), uintptr(unsafe.Pointer(&before)))
		start := time.Now()
		for time.Since(start) < 20*time.Millisecond {
		}
		_, _, _ = threadCycles.Call(uintptr(windows.CurrentThread()), uintptr(unsafe.Pointer(&after)))
		best = max(best, float64(after-before)/time.Since(start).Seconds())
	}
	return meter{cyclesPerSecond: best, processCycles: kernel.NewProc("QueryProcessCycleTime"), memory: kernel.NewProc("K32GetProcessMemoryInfo")}
}

func (m meter) watch(pid int) (func(*os.ProcessState) usage, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return nil, err
	}
	return func(*os.ProcessState) usage {
		defer func() { _ = windows.CloseHandle(handle) }()
		var cycles uint64
		_, _, _ = m.processCycles.Call(uintptr(handle), uintptr(unsafe.Pointer(&cycles)))
		counters := processMemoryCounters{size: uint32(unsafe.Sizeof(processMemoryCounters{}))}
		_, _, _ = m.memory.Call(uintptr(handle), uintptr(unsafe.Pointer(&counters)), uintptr(counters.size))
		return usage{cpu: time.Duration(float64(cycles) / m.cyclesPerSecond * float64(time.Second)), peakRSS: uint64(counters.peakWorkingSetSize)}
	}, nil
}
