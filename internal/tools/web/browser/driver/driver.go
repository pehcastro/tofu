package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Driver struct {
	Client *Client
	Tab    int
	refs   refMap
}

type remoteObject struct {
	ObjectID string          `json:"objectId"`
	Value    json.RawMessage `json:"value"`
}

func (a cdpAnswer) into(v any) error {
	if a.Error != "" {
		return errors.New(a.Error)
	}
	return json.Unmarshal(a.Result, v)
}

func (a cdpAnswer) object() (remoteObject, error) {
	var evaluated struct {
		Result           remoteObject `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := a.into(&evaluated); err != nil {
		return remoteObject{}, err
	}
	if evaluated.ExceptionDetails != nil {
		return remoteObject{}, fmt.Errorf("the page threw: %s", evaluated.ExceptionDetails.Text)
	}
	return evaluated.Result, nil
}

func evaluate(expression string) cdpCall {
	return cdpCall{Method: "Runtime.evaluate", Params: map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": true}}
}

func callOn(objectID, function string, byValue bool, args ...any) cdpCall {
	arguments := make([]map[string]any, len(args))
	for i, arg := range args {
		arguments[i] = map[string]any{"value": arg}
	}
	return cdpCall{Method: "Runtime.callFunctionOn", Params: map[string]any{"objectId": objectID, "functionDeclaration": function, "arguments": arguments, "returnByValue": byValue}}
}

func byBackend(method string, backend int) cdpCall {
	return cdpCall{Method: method, Params: map[string]any{"backendNodeId": backend}}
}

func (d *Driver) cdp(deadline time.Time, act bool, calls ...cdpCall) ([]cdpAnswer, error) {
	args, err := json.Marshal(cdpArgs{Calls: calls, Act: act})
	if err != nil {
		return nil, err
	}
	raw, err := d.Client.callBy(deadline, d.Tab, opCDP, args)
	if err != nil {
		return nil, err
	}
	var answers []cdpAnswer
	if err := json.Unmarshal(raw, &answers); err != nil || len(answers) != len(calls) {
		return nil, fmt.Errorf("the extension answered %d CDP calls with %.200s", len(calls), raw)
	}
	return answers, nil
}

func (d *Driver) one(deadline time.Time, act bool, call cdpCall) (cdpAnswer, error) {
	answers, err := d.cdp(deadline, act, call)
	if err != nil {
		return cdpAnswer{}, err
	}
	return answers[0], nil
}

func (d *Driver) act(deadline time.Time, calls ...cdpCall) error {
	answers, err := d.cdp(deadline, true, calls...)
	for _, answer := range answers {
		if err == nil {
			_, err = answer.object()
		}
	}
	return err
}

func (d *Driver) value(deadline time.Time, act bool, call cdpCall, into any) error {
	answer, err := d.one(deadline, act, call)
	var object remoteObject
	if err == nil {
		object, err = answer.object()
	}
	if err == nil {
		err = json.Unmarshal(object.Value, into)
	}
	return err
}

func (d *Driver) findAgain(deadline time.Time, ref string) (refEntry, error) {
	entry := d.refs.entries[ref]
	params := map[string]any{}
	if entry.frame != "" {
		params["frameId"] = entry.frame
	}
	answer, err := d.one(deadline, false, cdpCall{Method: "Accessibility.getFullAXTree", Params: params})
	var tree struct {
		Nodes []axNode `json:"nodes"`
	}
	if err == nil {
		err = answer.into(&tree)
	}
	if err != nil {
		return refEntry{}, err
	}
	match := 0
	for _, node := range tree.Nodes {
		if node.Ignored || node.Role.text() != entry.role || node.Name.text() != entry.name {
			continue
		}
		if match == entry.nth {
			entry.backend = node.Backend
			d.refs.entries[ref] = entry
			if doc := d.refs.documents[entry.frame]; doc != nil {
				doc.refs[node.Backend] = ref
			}
			return entry, nil
		}
		match++
	}
	return refEntry{}, fmt.Errorf("tab %d no longer shows %s %q, the ref %s: observe the page again", d.Tab, entry.role, entry.name, ref)
}

func (d *Driver) onNode(deadline time.Time, ref string, try func(backend int) error) (int, error) {
	entry, known := d.refs.entries[ref]
	if !known {
		return 0, fmt.Errorf("tab %d has no ref %q: observe the page again", d.Tab, ref)
	}
	var err error
	if try(entry.backend) != nil {
		if entry, err = d.findAgain(deadline, ref); err == nil {
			err = try(entry.backend)
		}
	}
	return entry.backend, err
}
