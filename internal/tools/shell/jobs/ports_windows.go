//go:build windows

package jobs

import (
	"context"
	"encoding/binary"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	tcpTableOwnerPIDListener = 3
	tcpTableRowsAt           = 4
)

type tcpTable struct {
	family, rowBytes, portAt, pidAt int
}

func listeners(context.Context) ([]listener, error) {
	extended := windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")
	var found []listener
	for _, layout := range []tcpTable{{family: windows.AF_INET, rowBytes: 24, portAt: 8, pidAt: 20}, {family: windows.AF_INET6, rowBytes: 56, portAt: 20, pidAt: 52}} {
		table, size := make([]byte, 1), uint32(0)
		for {
			code, _, _ := extended.Call(uintptr(unsafe.Pointer(&table[0])), uintptr(unsafe.Pointer(&size)), 0, uintptr(layout.family), tcpTableOwnerPIDListener, 0)
			if code == 0 {
				break
			}
			if syscall.Errno(code) != windows.ERROR_INSUFFICIENT_BUFFER {
				return nil, fmt.Errorf("GetExtendedTcpTable: %w", syscall.Errno(code))
			}
			table = make([]byte, size)
		}
		for i := range int(binary.LittleEndian.Uint32(table)) {
			row := table[tcpTableRowsAt+i*layout.rowBytes:]
			found = append(found, listener{port: int(binary.BigEndian.Uint16(row[layout.portAt:])), pid: int(binary.LittleEndian.Uint32(row[layout.pidAt:]))})
		}
	}
	return found, nil
}

func commandOf(_ context.Context, pid int) string {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	name, size := make([]uint16, windows.MAX_LONG_PATH), uint32(windows.MAX_LONG_PATH)
	if windows.QueryFullProcessImageName(handle, 0, &name[0], &size) != nil {
		return ""
	}
	return windows.UTF16ToString(name[:size])
}

func treeOf() func(root, pid int) bool {
	parents := map[int]int{}
	if snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0); err == nil {
		defer func() { _ = windows.CloseHandle(snapshot) }()
		entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
		for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
			parents[int(entry.ProcessID)] = int(entry.ParentProcessID)
		}
	}
	return func(root, pid int) bool {
		for range len(parents) + 1 {
			if pid == root {
				return true
			}
			if pid = parents[pid]; pid == 0 {
				return false
			}
		}
		return false
	}
}
