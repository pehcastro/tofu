package process

import "syscall"

func endWithParent(attr *syscall.SysProcAttr) { attr.Pdeathsig = syscall.SIGKILL }
