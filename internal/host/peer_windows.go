package host

import (
	"cmp"
	"errors"
	"net"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const sioAfUnixGetPeerPID = 0x58000100

var errPeerUnknown = errors.New("the socket did not say which process dialled it")

func peerProcess(conn net.Conn) (int, error) {
	raw, err := conn.(syscall.Conn).SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid, returned uint32
	var pidErr error
	err = raw.Control(func(fd uintptr) {
		pidErr = windows.WSAIoctl(windows.Handle(fd), sioAfUnixGetPeerPID, nil, 0, (*byte)(unsafe.Pointer(&pid)), uint32(unsafe.Sizeof(pid)), &returned, nil, 0)
	})
	if err = cmp.Or(err, pidErr); err == nil && pid == 0 {
		err = errPeerUnknown
	}
	return int(pid), err
}

func parentOf(pid int) (int, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if int(entry.ProcessID) == pid {
			return int(entry.ParentProcessID), nil
		}
	}
	return 0, err
}
