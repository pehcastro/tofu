//go:build !windows

package sys

func EnableVT(uintptr) error { return nil }
