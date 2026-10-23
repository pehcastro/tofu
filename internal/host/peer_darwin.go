package host

import (
	"cmp"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func peerProcess(conn net.Conn) (int, error) {
	raw, err := conn.(syscall.Conn).SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid int
	var pidErr error
	err = raw.Control(func(fd uintptr) { pid, pidErr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID) })
	return pid, cmp.Or(err, pidErr)
}

func parentOf(pid int) (int, error) {
	proc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, err
	}
	return int(proc.Eproc.Ppid), nil
}
