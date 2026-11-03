//go:build windows

package keymap

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func ancestorNames() []string {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()
	type process struct {
		parent uint32
		name   string
	}
	processes := map[uint32]process{}
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		processes[entry.ProcessID] = process{entry.ParentProcessID, windows.UTF16ToString(entry.ExeFile[:])}
	}
	var names []string
	seen := map[uint32]bool{}
	pid := uint32(os.Getpid())
	for len(names) < maxAncestors {
		current, ok := processes[pid]
		if !ok || current.parent == 0 || seen[current.parent] {
			break
		}
		seen[current.parent] = true
		pid = current.parent
		parent, ok := processes[pid]
		if !ok {
			break
		}
		names = append(names, parent.name)
	}
	return names
}
