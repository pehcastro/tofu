//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package uv

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

func newPollReader(reader io.Reader) (pollReader, error) {
	file, ok := reader.(File)
	if !ok {
		return newFallbackReader(reader)
	}

	if file.Name() == "/dev/tty" {
		return newSelectPollReader(reader)
	}

	kQueue, err := unix.Kqueue()
	if err != nil {
		return nil, fmt.Errorf("create kqueue: %w", err)
	}

	r := &kqueueReader{
		reader: reader,
		file:   file,
		kQueue: kQueue,
	}

	r.cancelSignalReader, r.cancelSignalWriter, err = os.Pipe()
	if err != nil {
		_ = unix.Close(kQueue)
		return nil, err
	}

	unix.SetKevent(&r.kQueueEvents[0], int(file.Fd()), unix.EVFILT_READ, unix.EV_ADD)
	unix.SetKevent(&r.kQueueEvents[1], int(r.cancelSignalReader.Fd()), unix.EVFILT_READ, unix.EV_ADD)

	return r, nil
}

type kqueueReader struct {
	reader             io.Reader
	file               File
	cancelSignalReader *os.File
	cancelSignalWriter *os.File
	kQueue             int
	kQueueEvents       [2]unix.Kevent_t
	mu                 sync.Mutex
	canceled           bool
}

func (r *kqueueReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if r.canceled {
		r.mu.Unlock()
		return 0, ErrCanceled
	}
	r.mu.Unlock()

	return r.reader.Read(p)
}

func (r *kqueueReader) Poll(timeout time.Duration) (bool, error) {
	r.mu.Lock()
	if r.canceled {
		r.mu.Unlock()
		return false, ErrCanceled
	}
	r.mu.Unlock()

	events := make([]unix.Kevent_t, 1)

	var ts *unix.Timespec
	if timeout >= 0 {
		t := unix.NsecToTimespec(timeout.Nanoseconds())
		ts = &t
	}

	for {
		n, err := unix.Kevent(r.kQueue, r.kQueueEvents[:], events, ts)
		if errors.Is(err, unix.EINTR) {
			continue
		}

		if err != nil {
			return false, fmt.Errorf("kevent: %w", err)
		}

		if n == 0 {
			return false, nil
		}

		break
	}

	ident := uint64(events[0].Ident)
	switch ident {
	case uint64(r.file.Fd()):
		return true, nil
	case uint64(r.cancelSignalReader.Fd()):

		var b [1]byte
		_, errRead := r.cancelSignalReader.Read(b[:])
		if errRead != nil {
			return false, fmt.Errorf("reading cancel signal: %w", errRead)
		}
		return false, ErrCanceled
	}

	return false, fmt.Errorf("unknown error")
}

func (r *kqueueReader) Cancel() bool {
	r.mu.Lock()
	r.canceled = true
	r.mu.Unlock()

	_, err := r.cancelSignalWriter.Write([]byte{'c'})
	return err == nil
}

func (r *kqueueReader) Close() error {
	var errMsgs []string

	err := unix.Close(r.kQueue)
	if err != nil {
		errMsgs = append(errMsgs, fmt.Sprintf("closing kqueue: %v", err))
	}

	err = r.cancelSignalWriter.Close()
	if err != nil {
		errMsgs = append(errMsgs, fmt.Sprintf("closing cancel signal writer: %v", err))
	}

	err = r.cancelSignalReader.Close()
	if err != nil {
		errMsgs = append(errMsgs, fmt.Sprintf("closing cancel signal reader: %v", err))
	}

	if len(errMsgs) > 0 {
		return fmt.Errorf("%s", strings.Join(errMsgs, ", "))
	}

	return nil
}
