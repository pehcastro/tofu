package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"
)

type screenSize struct {
	columns, rows int16
}

type transcript struct {
	mu      sync.Mutex
	screen  *screen
	changed chan struct{}
	ended   bool
}

func watch(from io.Reader, size screenSize) *transcript {
	t := &transcript{screen: newScreen(size), changed: make(chan struct{}, 1)}
	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, err := from.Read(buf)
			t.mu.Lock()
			t.screen.feed(buf[:n])
			t.ended = err != nil
			t.mu.Unlock()
			select {
			case t.changed <- struct{}{}:
			default:
			}
			if err != nil {
				return
			}
		}
	}()
	return t
}

var errNeverDrawn = errors.New("the text never reached the screen")

func (t *transcript) text() (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.screen.String(), t.ended
}

func (t *transcript) await(texts []string, deadline time.Time) (time.Time, error) {
	for {
		seen, ended := t.text()
		if !slices.ContainsFunc(texts, func(text string) bool { return !strings.Contains(seen, text) }) {
			return time.Now(), nil
		}
		if ended {
			return time.Time{}, errNeverDrawn
		}
		select {
		case <-t.changed:
		case <-time.After(time.Until(deadline)):
			return time.Time{}, errNeverDrawn
		}
	}
}

func (t *transcript) failed(err error) error {
	text, _ := t.text()
	return fmt.Errorf("%w, the screen ended as:\n%s", err, text)
}
