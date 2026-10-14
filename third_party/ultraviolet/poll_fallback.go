package uv

import (
	"bufio"
	"io"
	"sync"
	"time"
)

func newFallbackReader(reader io.Reader) (pollReader, error) {
	return &fallbackReader{
		reader:     bufio.NewReader(reader),
		cancelChan: make(chan struct{}),
		dataChan:   make(chan struct{}, 1),
	}, nil
}

type fallbackReader struct {
	reader     *bufio.Reader
	cancelChan chan struct{}
	dataChan   chan struct{}
	mu         sync.Mutex
	canceled   bool
	started    bool
}

func (r *fallbackReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if r.canceled {
		r.mu.Unlock()
		return 0, ErrCanceled
	}
	r.mu.Unlock()

	n, err := r.reader.Read(p)

	if err != nil {
		r.mu.Lock()
		if r.canceled {
			r.mu.Unlock()
			return 0, ErrCanceled
		}
		r.mu.Unlock()
	}
	return n, err
}

func (r *fallbackReader) Poll(timeout time.Duration) (bool, error) {
	r.mu.Lock()
	if r.canceled {
		r.mu.Unlock()
		return false, ErrCanceled
	}

	if !r.started {
		r.started = true
		go r.checkBuffered()
	}
	r.mu.Unlock()

	if timeout < 0 {

		select {
		case <-r.dataChan:

			select {
			case r.dataChan <- struct{}{}:
			default:
			}
			return true, nil
		case <-r.cancelChan:
			return false, ErrCanceled
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-r.dataChan:

		select {
		case r.dataChan <- struct{}{}:
		default:
		}
		return true, nil
	case <-timer.C:
		return false, nil
	case <-r.cancelChan:
		return false, ErrCanceled
	}
}

func (r *fallbackReader) checkBuffered() {
	for {
		select {
		case <-r.cancelChan:
			return
		default:
		}

		r.mu.Lock()
		if r.canceled {
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()

		_, err := r.reader.Peek(1)
		if err != nil {

			return
		}

		select {
		case r.dataChan <- struct{}{}:
		case <-r.cancelChan:
			return
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func (r *fallbackReader) Cancel() bool {
	r.mu.Lock()
	if r.canceled {
		r.mu.Unlock()
		return false
	}
	r.canceled = true
	r.mu.Unlock()

	close(r.cancelChan)
	return true
}

func (r *fallbackReader) Close() error {
	r.Cancel()
	return nil
}
