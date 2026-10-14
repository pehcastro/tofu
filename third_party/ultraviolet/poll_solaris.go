//go:build solaris

package uv

import "io"

func newPollReader(reader io.Reader) (pollReader, error) {
	return newSelectPollReader(reader)
}
