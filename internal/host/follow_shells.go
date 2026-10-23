package host

import (
	"encoding/json"
	"errors"
	"sync"

	"tofu/internal/safe"
	"tofu/internal/shell"
)

type heldShells struct {
	mu      sync.Mutex
	by      map[string]*heldShell
	pushing sync.Once
}

type heldShell struct {
	seat *seat
	next int64
}

func (h *Host) servesShells() bool { return h.shells != nil && !h.joins }

func (h *Host) useTerminals(a *audience) {
	if h.servesShells() {
		h.shells.Terminals.Store(a.wantsTerminals())
	}
}

func (h *Host) servedShell(s *seat, ask followAsk) followReply {
	reply := followReply{ID: ask.ID, OK: true}
	var err error
	switch {
	case !h.servesShells():
		err = shell.RefusedOtherProcess
	case ask.Op == followTakeOver:
		reply.Hold, err = h.takeShell(s, ask)
	case ask.Op == followInput:
		err = h.shells.Input(ask.Shell, ask.Data)
	case ask.Op == followResize:
		var size shell.Size
		if size, err = shell.NewSize(ask.Cols, ask.Rows); err == nil {
			err = h.shells.Resize(ask.Shell, size)
		}
	case ask.Op == followRelease:
		reply.Hold.Shell, err = h.shells.HandBack(ask.Shell)
		if err == nil {
			h.held.mu.Lock()
			delete(h.held.by, ask.Shell)
			h.held.mu.Unlock()
		}
	}
	var refused shell.Refused
	switch {
	case errors.As(err, &refused):
		reply.OK, reply.Shell = false, refused
	case err != nil:
		reply.OK, reply.Failed = false, err.Error()
	}
	return reply
}

func (h *Host) takeShell(s *seat, ask followAsk) (shell.Hold, error) {
	size, err := shell.NewSize(ask.Cols, ask.Rows)
	if err != nil {
		return shell.Hold{}, err
	}
	hold, err := h.shells.TakeOver(ask.Shell, size)
	if err != nil {
		return hold, err
	}
	h.held.mu.Lock()
	if h.held.by == nil {
		h.held.by = map[string]*heldShell{}
	}
	h.held.by[ask.Shell] = &heldShell{seat: s, next: hold.Next}
	h.held.mu.Unlock()
	h.held.pushing.Do(func() { safe.Go(h.report, "follow shell raw", h.pushRaw) })
	return hold, nil
}

func (h *Host) pushRaw() {
	for {
		select {
		case <-h.done:
			return
		case <-h.shells.Wrote():
		}
		h.held.mu.Lock()
		for name, held := range h.held.by {
			data, from := h.shells.Raw(name, held.next)
			if len(data) == 0 {
				continue
			}
			line, err := json.Marshal(followLine{Raw: &ShellRaw{Shell: name, Offset: from, Data: data}})
			if err == nil && held.seat.send(line) {
				held.next = from + int64(len(data))
			}
		}
		h.held.mu.Unlock()
	}
}

func (h *Host) handBackHeld(s *seat) {
	h.held.mu.Lock()
	var gone []string
	for name, held := range h.held.by {
		if held.seat == s {
			delete(h.held.by, name)
			gone = append(gone, name)
		}
	}
	h.held.mu.Unlock()
	for _, name := range gone {
		if _, err := h.shells.HandBack(name); err == nil {
			h.Steer("The person's client went away, so " + name + " is handed back to you. It is running. Its output while they held it is in shell logs.")
		}
	}
}

func (h *Host) heldShell(ask followAsk) (shell.Hold, error) {
	f := h.following.Load()
	if f == nil {
		return shell.Hold{}, shell.RefusedOtherProcess
	}
	reply := f.ask(ask)
	switch {
	case reply.Shell != "":
		return reply.Hold, reply.Shell
	case reply.Failed != "":
		return reply.Hold, errors.New(reply.Failed)
	case !reply.delivered():
		return reply.Hold, errHostSilent
	}
	return reply.Hold, nil
}
