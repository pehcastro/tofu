//go:build !windows

package console

func EnableVT(uintptr) error { return nil }
