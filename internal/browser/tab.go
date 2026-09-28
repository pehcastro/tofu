package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type SharedTab struct {
	Client *Client
	ID     int
}

type opArgs struct {
	Fingerprint string `json:"fingerprint"`
	Element     int    `json:"element,omitempty"`
	Value       string `json:"value,omitempty"`
	Direction   string `json:"direction,omitempty"`
}

func (t SharedTab) Snapshot(context.Context) (Page, error) {
	raw, err := t.Client.Call(t.ID, "snapshot", nil)
	if err != nil {
		return Page{}, err
	}
	return ParsePage(raw)
}

func (t SharedTab) Fresh(_ context.Context, page Page) (bool, error) {
	raw, err := t.call("fresh", opArgs{Fingerprint: page.Fingerprint})
	if err != nil {
		return false, err
	}
	var fresh *bool
	if err := json.Unmarshal(raw, &fresh); err != nil || fresh == nil {
		return false, fmt.Errorf("the extension answered fresh on tab %d with %q", t.ID, raw)
	}
	return *fresh, nil
}

func (t SharedTab) Act(_ context.Context, page Page, action Action) (fresh bool, err error) {
	args := opArgs{Fingerprint: page.Fingerprint}
	var op string
	switch action.Op {
	case OpClick:
		op, args.Element = "click", action.Element
	case OpTypeText:
		op, args.Element, args.Value = "fill", action.Element, action.Value
	case OpSelect:
		op, args.Element, args.Value = "select", action.Element, action.Value
	case OpScrollUp:
		op, args.Direction = "scroll", "up"
	case OpScrollDown:
		op, args.Direction = "scroll", "down"
	case OpWait:
		op = "wait"
	case OpDone, OpBlocked:
		return false, fmt.Errorf("%s is not an action a tab can run", action.Op)
	}
	_, err = t.call(op, args)
	if err != nil && strings.HasPrefix(err.Error(), "stale") {
		return false, nil
	}
	return err == nil, err
}

func (t SharedTab) call(op string, args opArgs) (json.RawMessage, error) {
	raw, _ := json.Marshal(args)
	return t.Client.Call(t.ID, op, raw)
}
