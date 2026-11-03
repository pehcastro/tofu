package shell

import "syscall"

func endWithParent(attr *syscall.SysProcAttr) { attr.Pdeathsig = syscall.SIGKILL }
