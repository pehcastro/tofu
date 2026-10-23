package host

import (
	"bytes"
	"cmp"
	"net"
	"os"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

func peerProcess(conn net.Conn) (int, error) {
	raw, err := conn.(syscall.Conn).SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var credErr error
	err = raw.Control(func(fd uintptr) { cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	if err = cmp.Or(err, credErr); err != nil {
		return 0, err
	}
	return int(cred.Pid), nil
}

func parentOf(pid int) (int, error) {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, err
	}
	afterName := bytes.Fields(stat[bytes.LastIndexByte(stat, ')')+1:])
	if len(afterName) < 2 {
		return 0, os.ErrNotExist
	}
	return strconv.Atoi(string(afterName[1]))
}
