//go:build windows

package uv

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

var fileShareValidFlags uint32 = windows.FILE_SHARE_DELETE | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_READ

func newPollReader(reader io.Reader) (pollReader, error) {
	f, ok := reader.(File)
	if !ok || f.Fd() != os.Stdin.Fd() {
		return newFallbackReader(reader)
	}

	coninPath, err := windows.UTF16PtrFromString("CONIN$")
	if err != nil {
		return nil, fmt.Errorf("convert CONIN$ to UTF16: %w", err)
	}

	conin, err := windows.CreateFile(
		coninPath,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		fileShareValidFlags,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OVERLAPPED,
		0)
	if err != nil {
		return nil, fmt.Errorf("open CONIN$ in overlapping mode: %w", err)
	}

	resetConsole, err := preparePollConsole(conin)
	if err != nil {
		_ = windows.CloseHandle(conin)
		return nil, fmt.Errorf("prepare console: %w", err)
	}

	err = windows.FlushConsoleInputBuffer(conin)
	if err != nil {
		_ = windows.CloseHandle(conin)
		return nil, fmt.Errorf("flush console input buffer: %w", err)
	}

	cancelEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(conin)
		return nil, fmt.Errorf("create cancel event: %w", err)
	}

	return &conReader{
		reader:             reader,
		conin:              conin,
		cancelEvent:        cancelEvent,
		resetConsole:       resetConsole,
		blockingReadSignal: make(chan struct{}, 1),
	}, nil
}

type conReader struct {
	reader             io.Reader
	conin              windows.Handle
	cancelEvent        windows.Handle
	resetConsole       func() error
	blockingReadSignal chan struct{}
	mu                 sync.Mutex
	canceled           bool
}

func (r *conReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if r.canceled {
		r.mu.Unlock()
		return 0, ErrCanceled
	}
	r.mu.Unlock()

	return r.reader.Read(p)
}

func (r *conReader) Poll(timeout time.Duration) (bool, error) {
	r.mu.Lock()
	if r.canceled {
		r.mu.Unlock()
		return false, ErrCanceled
	}
	r.mu.Unlock()

	timeoutMs := uint32(windows.INFINITE)
	if timeout >= 0 {
		timeoutMs = uint32(timeout.Milliseconds())
	}

	event, err := windows.WaitForMultipleObjects([]windows.Handle{r.conin, r.cancelEvent}, false, timeoutMs)
	switch {
	case windows.WAIT_OBJECT_0 <= event && event < windows.WAIT_OBJECT_0+2:
		if event == windows.WAIT_OBJECT_0+1 {
			return false, ErrCanceled
		}

		if event == windows.WAIT_OBJECT_0 {
			return true, nil
		}

		return false, fmt.Errorf("unexpected wait object is ready: %d", event-windows.WAIT_OBJECT_0)
	case windows.WAIT_ABANDONED <= event && event < windows.WAIT_ABANDONED+2:
		return false, fmt.Errorf("abandoned")
	case event == uint32(windows.WAIT_TIMEOUT):
		return false, nil
	case event == windows.WAIT_FAILED:
		return false, fmt.Errorf("failed")
	default:
		return false, fmt.Errorf("unexpected error: %w", error(err))
	}
}

func (r *conReader) Cancel() bool {
	r.mu.Lock()
	r.canceled = true
	r.mu.Unlock()

	select {
	case r.blockingReadSignal <- struct{}{}:
		err := windows.SetEvent(r.cancelEvent)
		if err != nil {
			return false
		}
		<-r.blockingReadSignal
	case <-time.After(100 * time.Millisecond):

		return false
	}

	return true
}

func (r *conReader) Close() error {
	err := windows.CloseHandle(r.cancelEvent)
	if err != nil {
		return fmt.Errorf("closing cancel event handle: %w", err)
	}

	err = r.resetConsole()
	if err != nil {
		return err
	}

	err = windows.CloseHandle(r.conin)
	if err != nil {
		return fmt.Errorf("closing CONIN$: %w", err)
	}

	return nil
}

func preparePollConsole(input windows.Handle) (reset func() error, err error) {
	var originalMode uint32

	err = windows.GetConsoleMode(input, &originalMode)
	if err != nil {
		return nil, fmt.Errorf("get console mode: %w", err)
	}

	var newMode uint32
	newMode &^= windows.ENABLE_ECHO_INPUT
	newMode &^= windows.ENABLE_LINE_INPUT
	newMode &^= windows.ENABLE_MOUSE_INPUT
	newMode &^= windows.ENABLE_PROCESSED_INPUT

	newMode |= windows.ENABLE_EXTENDED_FLAGS
	newMode |= windows.ENABLE_INSERT_MODE
	newMode |= windows.ENABLE_QUICK_EDIT_MODE
	newMode |= windows.ENABLE_WINDOW_INPUT

	newMode |= windows.ENABLE_VIRTUAL_TERMINAL_INPUT

	err = windows.SetConsoleMode(input, newMode)
	if err != nil {
		return nil, fmt.Errorf("set console mode: %w", err)
	}

	return func() error {
		err := windows.SetConsoleMode(input, originalMode)
		if err != nil {
			return fmt.Errorf("reset console mode: %w", err)
		}

		return nil
	}, nil
}
