//go:build !windows

package sys

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	clipboardToolTimeout   = 3 * time.Second
	clipboardToolWaitDelay = 500 * time.Millisecond
	clipboardToolLocale    = "LANG=en_US.UTF-8"
)

type cappedOutput struct {
	body  bytes.Buffer
	limit int
	over  bool
}

func (o *cappedOutput) Write(chunk []byte) (int, error) {
	if o.body.Len()+len(chunk) > o.limit {
		o.over = true
		return 0, io.ErrShortWrite
	}
	return o.body.Write(chunk)
}

func runClipboardTool(pkg string, stdin io.Reader, stdout io.Writer, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), clipboardToolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), clipboardToolLocale)
	cmd.WaitDelay = clipboardToolWaitDelay
	cmd.Stdin, cmd.Stdout = stdin, stdout
	var said bytes.Buffer
	if stdout != nil {
		cmd.Stderr = &said
	}
	err := cmd.Run()
	switch {
	case err == nil:
		return nil
	case errors.Is(err, exec.ErrNotFound):
		return fmt.Errorf("sys: %s is not on PATH, install %s: %w", name, pkg, ErrNoLocalClipboard)
	case ctx.Err() != nil:
		return errors.New("sys: " + name + " did not answer within " + clipboardToolTimeout.String())
	case said.Len() > 0:
		return errors.New("sys: " + name + ": " + strings.TrimSpace(said.String()))
	}
	return errors.New("sys: " + name + ": " + err.Error())
}

func readClipboardTool(limit int, pkg, name string, args ...string) ([]byte, error) {
	out := &cappedOutput{limit: limit}
	err := runClipboardTool(pkg, nil, out, name, args...)
	if out.over {
		return nil, errors.New("sys: " + name + " offered more than " + strconv.Itoa(limit) + " bytes, past the paste ceiling")
	}
	return out.body.Bytes(), err
}

func textClipboard(text []byte) Clipboard {
	if len(text) == 0 {
		return Clipboard{Kind: ClipboardEmpty}
	}
	return Clipboard{Kind: ClipboardText, Text: string(text)}
}
