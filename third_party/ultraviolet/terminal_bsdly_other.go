//go:build !darwin && !linux && !aix && !windows

package uv

func supportsBackspace(uint64) bool {
	return false
}
