//go:build !windows

package shell

func Decode(output []byte) string { return string(output) }
