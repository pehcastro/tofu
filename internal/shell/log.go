package shell

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"tofu/internal/konst"
	"tofu/internal/safe"
)

type keptLog struct {
	mu      sync.Mutex
	file    *os.File
	size    int64
	head    int64
	body    int64
	dropped int64
	closed  bool
}

func (l *keptLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return len(p), nil
	}
	n, err := l.file.WriteAt(p, l.size)
	l.size += int64(n)
	if err == nil && l.size > konst.ShellLogBytes {
		err = l.compact()
	}
	l.closed = err != nil
	return len(p), nil
}

func (l *keptLog) compact() error {
	if l.head == 0 {
		l.head = cmp.Or(l.afterNewline(konst.ShellLogHeadBytes-konst.ShellLogLineSeekBytes, bytes.LastIndexByte), konst.ShellLogHeadBytes)
		l.body = l.head
	}
	tail := l.size - konst.ShellLogTailBytes
	tail = cmp.Or(l.afterNewline(tail, bytes.IndexByte), tail)
	l.dropped += tail - l.body
	marker := fmt.Sprintf("\ntofu: %d bytes dropped here, between the start above and the newest output below\n", l.dropped)
	if _, err := l.file.WriteAt([]byte(marker), l.head); err != nil {
		return err
	}
	l.body = l.head + int64(len(marker))
	moved, err := io.Copy(io.NewOffsetWriter(l.file, l.body), io.NewSectionReader(l.file, tail, l.size-tail))
	l.size = l.body + moved
	return errors.Join(err, l.file.Truncate(l.size))
}

func (l *keptLog) afterNewline(from int64, find func([]byte, byte) int) int64 {
	window := make([]byte, konst.ShellLogLineSeekBytes)
	n, _ := l.file.ReadAt(window, from)
	if at := find(window[:n], '\n'); at >= 0 {
		return from + int64(at) + 1
	}
	return 0
}

func (l *keptLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	return l.file.Close()
}

func startLogged(cmd *exec.Cmd, lifetime Lifetime, logFile *os.File, log io.Writer, report safe.Report) (tree, <-chan struct{}, error) {
	if lifetime == OutlivesTofu {
		cmd.Stdout, cmd.Stderr = logFile, logFile
		spawned, err := startTree(cmd, lifetime)
		return spawned, nil, err
	}
	read, write, err := os.Pipe()
	if err != nil {
		return tree{}, nil, err
	}
	cmd.Stdout, cmd.Stderr = write, write
	spawned, err := startTree(cmd, lifetime)
	_ = write.Close()
	if err != nil {
		_ = read.Close()
		return tree{}, nil, err
	}
	drained := make(chan struct{})
	safe.Go(report, "shell output", func() {
		defer close(drained)
		defer func() { _ = read.Close() }()
		pipeOut(report, log, read)
	})
	return spawned, drained, nil
}

func (p *live) closeLog() {
	if p.drained != nil {
		select {
		case <-p.drained:
		case <-time.After(killWait):
		}
	}
	_ = p.log.Close()
}

func (r *Registry) keptFull(counted int) error {
	listed, _ := r.List()
	others := -counted
	for _, one := range listed {
		if one.Kept != "" {
			others++
		}
	}
	if others < konst.KeptShellsRunningMax {
		return nil
	}
	return fmt.Errorf("shell: %d other kept shells are running and %d is the most at once: stop one on the shells screen or with tofu shells stop NAME, then run this again", others, konst.KeptShellsRunningMax)
}
