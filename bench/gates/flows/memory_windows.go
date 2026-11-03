//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const memoryKind = "private"

type memoryCounters struct {
	size                             uint32
	pageFaults                       uint32
	peakWorkingSet, workingSet       uintptr
	quotaPeakPaged, quotaPaged       uintptr
	quotaPeakNonPaged, quotaNonPaged uintptr
	pagefileUsage, peakPagefileUsage uintptr
	privateUsage, privateWorkingSet  uintptr
	sharedCommit                     uint64
}

var processMemory = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

func memoryMB(pid int) (float64, error) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(process) }()
	counters := memoryCounters{size: uint32(unsafe.Sizeof(memoryCounters{}))}
	if ok, _, err := processMemory.Call(uintptr(process), uintptr(unsafe.Pointer(&counters)), uintptr(counters.size)); ok == 0 {
		return 0, err
	}
	return float64(counters.privateUsage) / (1 << 20), nil
}
