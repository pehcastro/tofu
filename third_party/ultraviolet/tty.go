package uv

import (
	"context"
	"os"
)

func OpenTTY() (inTty, outTty *os.File, err error) {
	return openTTY()
}

func Suspend() error {
	return suspend()
}

func NotifyWinch(c chan os.Signal, sigs ...os.Signal) {
	notifyWinch(c, sigs...)
}

func NotifyWinchContext(ctx context.Context, sigs ...os.Signal) (context.Context, context.CancelFunc) {
	return notifyWinchContext(ctx, sigs...)
}
