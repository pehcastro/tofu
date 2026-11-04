//go:build !windows && !linux

package process

import "syscall"

func endWithParent(*syscall.SysProcAttr) {}
