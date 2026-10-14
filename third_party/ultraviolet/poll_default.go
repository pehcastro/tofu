//go:build !linux && !windows && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly && !solaris

package uv

import "io"

func newPollReader(reader io.Reader) (pollReader, error) {
	return newFallbackReader(reader)
}
