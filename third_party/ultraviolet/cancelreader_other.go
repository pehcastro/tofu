//go:build !windows

package uv

import (
	"io"

	"github.com/muesli/cancelreader"
)

func NewCancelReader(r io.Reader) (cancelreader.CancelReader, error) {
	return cancelreader.NewReader(r)
}
