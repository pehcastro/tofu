//go:build unix

package jobs

import "syscall"

func treeOf() func(root, pid int) bool {
	return func(root, pid int) bool {
		group, err := syscall.Getpgid(pid)
		return err == nil && group == root
	}
}
