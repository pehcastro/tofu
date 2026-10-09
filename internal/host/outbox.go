package host

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sync"

	"tofu/internal/konst"
)

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *Refusal        `json:"error,omitempty"`
}

type outgoing struct {
	msg   message
	merge string
	kept  bool
}

func notify(method string, params any) outgoing {
	if identified, numbered := params.(stamped); numbered {
		identified.refer()
	}
	return outgoing{msg: message{JSONRPC: rpcVersion, Method: method, Params: params}}
}

func kept(method string, params any) outgoing {
	out := notify(method, params)
	out.kept = true
	return out
}

func merged(method, key string, params any) outgoing {
	out := notify(method, params)
	out.merge = method + " " + key
	return out
}

type stamped interface {
	stamp(seq int64)
	refer()
	about() Identity
}

func (i *Identity) about() Identity { return *i }

type outbox struct {
	mu     sync.Mutex
	more   *sync.Cond
	queue  []outgoing
	closed bool
}

func newOutbox() *outbox {
	box := &outbox{}
	box.more = sync.NewCond(&box.mu)
	return box
}

func (o *outbox) push(out outgoing) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	defer o.more.Signal()
	if out.merge != "" {
		if at := slices.IndexFunc(o.queue, func(queued outgoing) bool { return queued.merge == out.merge }); at >= 0 {
			if absorbing, absorbs := out.msg.Params.(interface{ absorb(any) }); absorbs {
				absorbing.absorb(o.queue[at].msg.Params)
			}
			o.queue[at] = out
			return
		}
	}
	if out.kept || len(o.queue) < konst.ServeQueue {
		o.queue = append(o.queue, out)
		return
	}
	if at := slices.IndexFunc(o.queue, func(queued outgoing) bool { return queued.msg.Method == resyncMethod }); at >= 0 {
		o.queue[at].msg.Params.(*Resync).Dropped++
		return
	}
	lost := out.msg.Params.(stamped).about()
	lost.Item, lost.Agent = resyncMethod, ""
	o.queue = append(o.queue, kept(resyncMethod, &Resync{Identity: lost, Dropped: 1}))
}

func (o *outbox) take() (next outgoing, more, open bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for len(o.queue) == 0 && !o.closed {
		o.more.Wait()
	}
	if len(o.queue) == 0 {
		return outgoing{}, false, false
	}
	next, o.queue = o.queue[0], o.queue[1:]
	return next, len(o.queue) > 0, true
}

func (o *outbox) close() {
	o.mu.Lock()
	o.closed = true
	o.mu.Unlock()
	o.more.Broadcast()
}

func (o *outbox) drain(out io.Writer) error {
	buffered := bufio.NewWriter(out)
	encoder := json.NewEncoder(buffered)
	encoder.SetEscapeHTML(false)
	var seq int64
	var failed error
	for {
		next, more, open := o.take()
		if !open {
			return errors.Join(failed, buffered.Flush())
		}
		if params, numbered := next.msg.Params.(stamped); numbered {
			seq++
			params.stamp(seq)
		}
		if failed == nil {
			failed = encoder.Encode(next.msg)
		}
		if !more && failed == nil {
			failed = buffered.Flush()
		}
	}
}
