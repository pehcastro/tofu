package lsp

import (
	"errors"
	"sync"
	"time"

	"tofu/internal/konst"
)

type Pool struct {
	mutex   sync.Mutex
	running map[string]*entry
	closed  bool
}

type entry struct {
	ready  chan struct{}
	client *Client
	err    error
	busy   int
	idle   *time.Timer
}

func NewPool() *Pool { return &Pool{running: map[string]*entry{}} }

func (p *Pool) Use(server Server, root string, work func(*Client) error) error {
	key := server.Program + "\x00" + root
	held, err := p.acquire(key, server, root)
	if err != nil {
		return err
	}
	defer p.release(key, held)
	return work(held.client)
}

func (p *Pool) acquire(key string, server Server, root string) (*entry, error) {
	p.mutex.Lock()
	if p.closed {
		p.mutex.Unlock()
		return nil, errors.New("the language servers were stopped with the host")
	}
	held := p.running[key]
	starting := held == nil
	if starting {
		held = &entry{ready: make(chan struct{})}
		p.running[key] = held
	}
	held.busy++
	if held.idle != nil {
		held.idle.Stop()
	}
	p.mutex.Unlock()
	if starting {
		held.client, held.err = start(server, root)
		close(held.ready)
	}
	<-held.ready
	if held.err != nil || !held.client.alive() {
		p.mutex.Lock()
		held.busy--
		if p.running[key] == held {
			delete(p.running, key)
		}
		p.mutex.Unlock()
		if held.err != nil {
			return nil, held.err
		}
		return p.acquire(key, server, root)
	}
	return held, nil
}

func (p *Pool) release(key string, held *entry) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if held.busy--; held.busy > 0 || p.closed {
		return
	}
	held.idle = time.AfterFunc(konst.LspIdleMillis*time.Millisecond, func() {
		p.mutex.Lock()
		retire := held.busy == 0 && p.running[key] == held
		if retire {
			delete(p.running, key)
		}
		p.mutex.Unlock()
		if retire {
			held.client.Shutdown()
		}
	})
}

func (p *Pool) Close() {
	if p == nil {
		return
	}
	p.mutex.Lock()
	p.closed = true
	running := p.running
	p.running = map[string]*entry{}
	p.mutex.Unlock()
	var stopping sync.WaitGroup
	for _, held := range running {
		stopping.Go(func() {
			<-held.ready
			if held.idle != nil {
				held.idle.Stop()
			}
			if held.err == nil {
				held.client.Shutdown()
			}
		})
	}
	stopping.Wait()
}
