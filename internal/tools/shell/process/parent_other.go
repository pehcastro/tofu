//go:build !windows && !linux

package shell

import "syscall"

func endWithParent(*syscall.SysProcAttr) {}
