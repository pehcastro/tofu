package jsonrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/textproto"
	"strconv"
	"sync"
)

var ErrGone = errors.New("the other end exited")

type Wire struct {
	writing sync.Mutex
	to      io.Writer
}

func NewWire(to io.Writer) *Wire { return &Wire{to: to} }

func (w *Wire) Send(body any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	w.writing.Lock()
	defer w.writing.Unlock()
	_, err = io.WriteString(w.to, "Content-Length: "+strconv.Itoa(len(encoded))+"\r\n\r\n"+string(encoded))
	return err
}

func Read(from io.Reader, each func(body []byte)) {
	frames := textproto.NewReader(bufio.NewReader(from))
	for {
		header, err := frames.ReadMIMEHeader()
		if err != nil {
			return
		}
		length, err := strconv.Atoi(header.Get("Content-Length"))
		if err != nil {
			return
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(frames.R, body); err != nil {
			return
		}
		each(body)
	}
}

type Pending[R any] struct {
	mutex   sync.Mutex
	last    int
	waiting map[int]chan R
}

func (p *Pending[R]) Number() int {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.last++
	return p.last
}

func (p *Pending[R]) Open() (int, chan R) {
	id, answer := p.Number(), make(chan R, 1)
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.waiting == nil {
		p.waiting = map[int]chan R{}
	}
	p.waiting[id] = answer
	return id, answer
}

func (p *Pending[R]) Answer(id int, reply R) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if waiter := p.waiting[id]; waiter != nil {
		delete(p.waiting, id)
		waiter <- reply
	}
}

func (p *Pending[R]) Forget(id int) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	delete(p.waiting, id)
}

func (p *Pending[R]) Await(ctx context.Context, id int, answer <-chan R, gone <-chan struct{}) (R, error) {
	var none R
	select {
	case got := <-answer:
		return got, nil
	case <-ctx.Done():
		p.Forget(id)
		return none, ctx.Err()
	case <-gone:
		return none, ErrGone
	}
}
