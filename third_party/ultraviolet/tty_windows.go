//go:build windows

package uv

import (
	"context"
	"os"
	"os/signal"
)

func openTTY() (inTty, outTty *os.File, err error) {

	inTty, err = os.OpenFile("CONIN$", os.O_RDWR, 0o644)
	if err != nil {
		return nil, nil, err
	}
	outTty, err = os.OpenFile("CONOUT$", os.O_RDWR, 0o644)
	if err != nil {
		return nil, nil, err
	}
	return inTty, outTty, nil
}

func suspend() (err error) {

	return nil
}

func notifyWinch(c chan os.Signal, sigs ...os.Signal) {
	signal.Notify(c, sigs...)
}

func notifyWinchContext(ctx context.Context, sigs ...os.Signal) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, sigs...)
}
