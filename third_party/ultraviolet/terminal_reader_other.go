//go:build !windows

package uv

import (
	"context"
)

func (d *TerminalReader) streamData(ctx context.Context, readc chan []byte) error {
	return d.sendBytes(ctx, readc)
}
