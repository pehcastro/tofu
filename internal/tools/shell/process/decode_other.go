//go:build !windows

package process

func Decode(output []byte) string { return string(output) }
