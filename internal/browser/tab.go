package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"tofu/internal/konst"
)

type SharedTab struct {
	Client *Client
	ID     int
}

type Stale string

const (
	StaleNone     Stale = ""
	StaleCovered  Stale = "covered"
	StaleHidden   Stale = "hidden"
	StaleNoSize   Stale = "no size"
	StaleDetached Stale = "detached"
	StaleChanged  Stale = "changed"
	StaleNoBody   Stale = "no body"
)

type opArgs struct {
	Element   int    `json:"element,omitempty"`
	Guard     string `json:"guard,omitempty"`
	Value     string `json:"value,omitempty"`
	Direction string `json:"direction,omitempty"`
}

func (t SharedTab) Snapshot(ctx context.Context) (Page, error) {
	for attempt := 1; ; attempt++ {
		raw, stale, err := t.call("snapshot", nil)
		switch {
		case err != nil:
			return Page{}, err
		case stale == StaleNone:
			return ParsePage(raw)
		case attempt == konst.BrowserSnapshotAttempts:
			return Page{}, fmt.Errorf("tab %d answered %s on %d snapshots in a row", t.ID, stale, attempt)
		}
		select {
		case <-ctx.Done():
			return Page{}, ctx.Err()
		case <-time.After(konst.BrowserSnapshotGapMillis * time.Millisecond):
		}
	}
}

func (t SharedTab) Act(_ context.Context, page Page, action Action) (Stale, error) {
	args := opArgs{Element: action.Element, Guard: page.Guards[action.Element], Value: action.Value}
	var op string
	switch action.Op {
	case OpClick:
		op = "click"
	case OpTypeText:
		op = "fill"
	case OpSelect:
		op = "select"
	case OpScrollUp:
		op, args.Direction = "scroll", "up"
	case OpScrollDown:
		op, args.Direction = "scroll", "down"
	case OpWait:
		op = "wait"
	case OpDone, OpBlocked:
		return StaleNone, fmt.Errorf("%s is not an action a tab can run", action.Op)
	}
	raw, _ := json.Marshal(args)
	_, stale, err := t.call(op, raw)
	return stale, err
}

func (t SharedTab) call(op string, args json.RawMessage) (json.RawMessage, Stale, error) {
	raw, err := t.Client.Call(t.ID, op, args)
	if err != nil {
		return nil, StaleNone, err
	}
	var answer struct {
		Stale Stale `json:"stale"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, StaleNone, fmt.Errorf("the extension answered %s on tab %d with %q", op, t.ID, raw)
	}
	switch answer.Stale {
	case StaleNone, StaleCovered, StaleHidden, StaleNoSize, StaleDetached, StaleChanged, StaleNoBody:
		return raw, answer.Stale, nil
	}
	return nil, StaleNone, fmt.Errorf("the extension answered %s on tab %d with the unknown stale kind %q", op, t.ID, answer.Stale)
}
