package mutate

import (
	"context"
	"os/exec"
)

func run(ctx context.Context, cmd *exec.Cmd, memoryCeilingBytes uintptr) error {
	b, err := startBounded(cmd, memoryCeilingBytes)
	if err != nil {
		return err
	}
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = b.kill()
		case <-stopped:
		}
	}()
	waitErr := cmd.Wait()
	close(stopped)
	_ = b.release()
	return waitErr
}
