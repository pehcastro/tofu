package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"runtime"
	"slices"
	"sync"
	"time"

	"tofu/internal/browser/extension"
	"tofu/internal/konst"
	"tofu/internal/safe"
	"tofu/internal/sys"
)

type direct struct {
	cdp      *cdpConn
	profile  string
	build    string
	snapshot string
	sessions map[string]string
	mu       sync.Mutex
	casts    map[string]*[]ScreencastFrame
	report   safe.Report
}

type pageTarget struct {
	ID    string `json:"targetId"`
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

const (
	cdpModifierCtrl = 2
	cdpModifierMeta = 4
)

func serveDirect(cdp *cdpConn, profile string) (*Client, error) {
	build, err := Build()
	var source []byte
	if err == nil {
		source, err = extension.Files.ReadFile("snapshot.js")
	}
	if err != nil {
		_ = cdp.ws.Close()
		return nil, err
	}
	near, far := net.Pipe()
	d := &direct{cdp: cdp, profile: profile, build: build, snapshot: string(source), sessions: map[string]string{}, casts: map[string]*[]ScreencastFrame{}, report: sys.Crashes(".")}
	safe.Go(d.report, "browser direct", func() { d.serve(far) })
	return &Client{conn: near, out: json.NewEncoder(near), in: json.NewDecoder(near)}, nil
}

func (d *direct) serve(conn net.Conn) {
	defer func() { _ = d.cdp.ws.Close() }()
	requests, answers := make(chan request), json.NewEncoder(conn)
	safe.Go(d.report, "browser direct reader", func() {
		defer close(requests)
		for decoder := json.NewDecoder(conn); ; {
			var req request
			if decoder.Decode(&req) != nil {
				return
			}
			requests <- req
		}
	})
	for req := range requests {
		var value json.RawMessage
		err := safe.Call(d.report, "browser request", func() (err error) {
			value, err = d.handle(req)
			return err
		})
		answer := result{ID: req.ID, OK: err == nil, Value: value}
		if err != nil {
			answer.Error = err.Error()
		}
		if answers.Encode(answer) != nil {
			_ = conn.Close()
		}
	}
}

func (d *direct) handle(req request) (json.RawMessage, error) {
	switch req.Op {
	case opHello:
		return nil, nil
	case opBuilds:
		return json.Marshal(Builds{Extension: "none, profile " + d.profile, Tofu: d.build})
	case opTabs:
		pages, err := d.pages()
		if err != nil {
			return nil, err
		}
		tabs := []Tab{}
		for _, page := range pages {
			if reachable(page.URL) {
				tabs = append(tabs, Tab{ID: tabID(page.ID), URL: page.URL, Title: page.Title, Opened: true})
			}
		}
		return json.Marshal(tabs)
	case opOpen:
		url, err := openable(req.Args)
		var page pageTarget
		if err == nil {
			page, err = d.newPage()
		}
		if err == nil {
			err = d.navigate(page.ID, url)
		}
		if err != nil {
			return nil, err
		}
		return json.Marshal(tabID(page.ID))
	}
	page, err := d.page(req.Tab)
	if err != nil {
		return nil, err
	}
	switch req.Op {
	case opClose:
		delete(d.sessions, page.ID)
		_, err := d.cdp.send("", "Target.closeTarget", map[string]any{"targetId": page.ID})
		return nil, err
	case opNavigate:
		url, err := openable(req.Args)
		if err == nil {
			err = d.navigate(page.ID, url)
		}
		if err != nil {
			return nil, err
		}
		return json.Marshal(req.Tab)
	case opBack:
		return d.back(page.ID, req.Tab)
	}
	if _, err := opStatus(req); err != nil {
		return nil, err
	}
	if !reachable(page.URL) {
		return nil, fmt.Errorf("tab %d shows %s, which tofu never reads or drives", req.Tab, page.URL)
	}
	session, err := d.attach(page.ID)
	if err != nil {
		return nil, err
	}
	switch req.Op {
	case opScreencast:
		return d.screencast(session, req)
	case opCDP:
		var args cdpArgs
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return nil, err
		}
		answers := make([]cdpAnswer, len(args.Calls))
		for i, call := range args.Calls {
			answers[i].Result, err = d.cdp.send(session, call.Method, call.Params)
			if err != nil {
				answers[i].Error = err.Error()
			}
		}
		return json.Marshal(answers)
	}
	return d.perform(session, req)
}

func (d *direct) pages() ([]pageTarget, error) {
	raw, err := d.cdp.send("", "Target.getTargets", nil)
	var got struct {
		TargetInfos []pageTarget `json:"targetInfos"`
	}
	if err == nil {
		err = json.Unmarshal(raw, &got)
	}
	return slices.DeleteFunc(got.TargetInfos, func(target pageTarget) bool { return target.Type != "page" }), err
}

func (d *direct) page(tab int) (pageTarget, error) {
	pages, err := d.pages()
	if err != nil {
		return pageTarget{}, err
	}
	for _, page := range pages {
		if tabID(page.ID) == tab {
			return page, nil
		}
	}
	return pageTarget{}, fmt.Errorf("there is no tab %d in the Chrome of the browser profile %q", tab, d.profile)
}

func tabID(target string) int {
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(target))
	return int(sum.Sum32()>>1) + 1
}

func (d *direct) newPage() (pageTarget, error) {
	before, err := d.pages()
	var raw json.RawMessage
	if err == nil {
		raw, err = d.cdp.send("", "Target.createTarget", map[string]any{"url": "about:blank"})
	}
	created := pageTarget{}
	if err == nil {
		err = json.Unmarshal(raw, &created)
	}
	for _, page := range before {
		if err == nil && page.URL == "about:blank" {
			_, err = d.cdp.send("", "Target.closeTarget", map[string]any{"targetId": page.ID})
		}
	}
	return created, err
}

func (d *direct) attach(target string) (string, error) {
	if session, attached := d.sessions[target]; attached {
		return session, nil
	}
	raw, err := d.cdp.send("", "Target.attachToTarget", map[string]any{"targetId": target, "flatten": true})
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err == nil {
		err = json.Unmarshal(raw, &attached)
	}
	if err == nil {
		_, err = d.cdp.send(attached.SessionID, "Emulation.setFocusEmulationEnabled", map[string]any{"enabled": true})
	}
	if err == nil {
		_, err = d.cdp.send(attached.SessionID, "Page.enable", nil)
	}
	if err != nil {
		return "", err
	}
	_, _ = d.cdp.send(attached.SessionID, "Page.setWebLifecycleState", map[string]any{"state": "active"})
	d.sessions[target] = attached.SessionID
	return attached.SessionID, nil
}

func (d *direct) navigate(target, url string) error {
	session, err := d.attach(target)
	if err != nil {
		return err
	}
	return d.loading(session, func() error {
		raw, err := d.cdp.send(session, "Page.navigate", map[string]any{"url": url})
		var went struct {
			ErrorText string `json:"errorText"`
		}
		if err == nil {
			err = json.Unmarshal(raw, &went)
		}
		if err == nil && went.ErrorText != "" {
			err = fmt.Errorf("chrome could not open %s: %s", url, went.ErrorText)
		}
		return err
	}, "Page.domContentEventFired")
}

func (d *direct) back(target string, tab int) (json.RawMessage, error) {
	session, err := d.attach(target)
	var raw json.RawMessage
	if err == nil {
		raw, err = d.cdp.send(session, "Page.getNavigationHistory", nil)
	}
	var history struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID  int    `json:"id"`
			URL string `json:"url"`
		} `json:"entries"`
	}
	if err == nil {
		err = json.Unmarshal(raw, &history)
	}
	if err == nil && (history.CurrentIndex < 1 || history.Entries[history.CurrentIndex-1].URL == "about:blank") {
		err = fmt.Errorf("tab %d has no page to go back to", tab)
	}
	if err != nil {
		return nil, err
	}
	err = d.loading(session, func() error {
		_, err := d.cdp.send(session, "Page.navigateToHistoryEntry", map[string]any{"entryId": history.Entries[history.CurrentIndex-1].ID})
		return err
	}, "Page.domContentEventFired", "Page.frameNavigated")
	if err != nil {
		return nil, err
	}
	return json.Marshal(tab)
}

func (d *direct) loading(session string, navigate func() error, ready ...string) error {
	fired := make(chan struct{}, 1)
	for _, event := range ready {
		d.cdp.on(session, event, func(json.RawMessage) {
			select {
			case fired <- struct{}{}:
			default:
			}
		})
		defer d.cdp.off(session, event)
	}
	if err := navigate(); err != nil {
		return err
	}
	select {
	case <-fired:
	case <-time.After(konst.BrowserWaitMaxMillis * time.Millisecond):
	}
	return nil
}

func (d *direct) inPage(session string, asked map[string]any) (json.RawMessage, error) {
	encoded, err := json.Marshal(asked)
	if err != nil {
		return nil, err
	}
	call := evaluate("(" + d.snapshot + ")(" + string(encoded) + ")")
	raw, err := d.cdp.send(session, call.Method, call.Params)
	if err != nil {
		return nil, err
	}
	object, err := cdpAnswer{Result: raw}.object()
	if err != nil {
		return json.RawMessage(`{"stale":"` + string(StaleChanged) + `"}`), nil
	}
	var failed struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(object.Value, &failed) == nil && failed.Error != "" {
		return nil, errors.New(failed.Error)
	}
	return object.Value, nil
}

func (d *direct) perform(session string, req request) (json.RawMessage, error) {
	var args opArgs
	if len(req.Args) > 0 && json.Unmarshal(req.Args, &args) != nil {
		return nil, fmt.Errorf("the %s op carries %s, not an object", req.Op, req.Args)
	}
	if req.Op == "scroll" && args.Direction != "up" && args.Direction != "down" {
		return nil, fmt.Errorf("no scroll direction %q", args.Direction)
	}
	target, err := d.inPage(session, map[string]any{"op": req.Op, "element": args.Element, "guard": args.Guard, "value": args.Value, "direction": args.Direction})
	if err != nil || req.Op == "snapshot" {
		return target, err
	}
	var at struct {
		Stale    Stale   `json:"stale"`
		X        float64 `json:"x"`
		Y        float64 `json:"y"`
		Combobox bool    `json:"combobox"`
	}
	if err := json.Unmarshal(target, &at); err != nil || at.Stale != StaleNone {
		return target, err
	}
	if req.Op == "wait" {
		time.Sleep(konst.BrowserSettlePollMillis * time.Millisecond)
		return json.RawMessage(`{}`), nil
	}
	var calls []cdpCall
	if req.Op == "click" || req.Op == "fill" {
		for _, kind := range []string{"mousePressed", "mouseReleased"} {
			calls = append(calls, cdpCall{Method: "Input.dispatchMouseEvent", Params: map[string]any{"type": kind, "x": at.X, "y": at.Y, "button": "left", "clickCount": 1}})
		}
	}
	if req.Op == "fill" {
		modifier := cdpModifierCtrl
		if runtime.GOOS == "darwin" {
			modifier = cdpModifierMeta
		}
		calls = append(calls,
			cdpCall{Method: "Input.dispatchKeyEvent", Params: map[string]any{"type": "keyDown", "key": "a", "code": "KeyA", "modifiers": modifier, "commands": []string{"selectAll"}}},
			cdpCall{Method: "Input.dispatchKeyEvent", Params: map[string]any{"type": "keyUp", "key": "a", "code": "KeyA", "modifiers": modifier}},
			cdpCall{Method: "Input.insertText", Params: map[string]any{"text": args.Value}})
	}
	for _, call := range calls {
		if _, err := d.cdp.send(session, call.Method, call.Params); err != nil {
			return nil, err
		}
	}
	combobox, settle := req.Op == "fill" && at.Combobox, konst.BrowserSettleTickMillis
	if combobox {
		settle = konst.BrowserSettlePollMillis
	}
	_, _ = d.inPage(session, map[string]any{"op": "settle", "element": args.Element, "combobox": combobox, "wait": settle})
	return json.RawMessage(`{}`), nil
}

func (d *direct) screencast(session string, req request) (json.RawMessage, error) {
	var args struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return nil, err
	}
	frames, recording := d.casts[session]
	switch {
	case args.Action == "show" || args.Action == "hide":
		return json.RawMessage(`{}`), nil
	case args.Action == "start" && recording:
		return nil, fmt.Errorf("tab %d is already recording a screencast", req.Tab)
	case args.Action == "start":
		frames = &[]ScreencastFrame{}
		d.casts[session] = frames
		d.cdp.on(session, "Page.screencastFrame", func(params json.RawMessage) {
			var frame struct {
				Data     []byte `json:"data"`
				Session  int    `json:"sessionId"`
				Metadata struct {
					Timestamp float64 `json:"timestamp"`
				} `json:"metadata"`
			}
			if json.Unmarshal(params, &frame) != nil {
				return
			}
			d.mu.Lock()
			*frames = append(*frames, ScreencastFrame{Data: frame.Data, ChromeSeconds: frame.Metadata.Timestamp})
			d.mu.Unlock()
			safe.Go(d.report, "browser frame ack", func() {
				_, _ = d.cdp.send(session, "Page.screencastFrameAck", map[string]any{"sessionId": frame.Session})
			})
		})
		_, err := d.cdp.send(session, "Page.startScreencast", map[string]any{"format": "jpeg", "everyNthFrame": 1})
		if err != nil {
			d.cdp.off(session, "Page.screencastFrame")
			delete(d.casts, session)
		}
		return json.RawMessage(`{}`), err
	case args.Action == "stop" && recording:
		_, err := d.cdp.send(session, "Page.stopScreencast", nil)
		d.cdp.off(session, "Page.screencastFrame")
		delete(d.casts, session)
		if err != nil {
			return nil, err
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		return json.Marshal(*frames)
	case args.Action == "stop":
		return nil, fmt.Errorf("tab %d is not recording a screencast", req.Tab)
	}
	return nil, fmt.Errorf("no screencast action %q", args.Action)
}
