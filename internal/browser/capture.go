package browser

import (
	"cmp"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/browser/motion"
	"tofu/internal/konst"
)

//go:embed sampler.js
var sampler string

const (
	motionReload = `(url => { window.__tofuMotionReload = true; if (location.href === url) location.reload(); else location.assign(url); return true; })(%s)`
	motionReady  = `(selector => {
  if (window.__tofuMotionReload || document.readyState !== 'complete') return false;
  const el = selector ? document.querySelector(selector) : document.body;
  const rect = el ? el.getBoundingClientRect() : {width: 0, height: 0};
  return rect.width > 0 && rect.height > 0 && getComputedStyle(el).visibility !== 'hidden';
})(%s)`
	motionRead = `(() => { window.__tofuMotion.running = false; return {trigger: window.__tofuMotion.trigger, samples: window.__tofuMotion.samples, browser: navigator.userAgent}; })()`
)

type pageTake struct {
	Trigger *motion.Trigger `json:"trigger"`
	Samples []struct {
		TS       float64                    `json:"ts"`
		Elements map[string]*motion.Element `json:"elements"`
	} `json:"samples"`
	Browser string `json:"browser"`
}

func ReadScenario(path string) (motion.Scenario, error) {
	var sc motion.Scenario
	file, err := os.Open(path)
	if err == nil {
		decoder := json.NewDecoder(file)
		decoder.DisallowUnknownFields()
		err = errors.Join(decoder.Decode(&sc), file.Close())
	}
	if err == nil {
		sc, err = sc.Checked()
	}
	if err != nil {
		return sc, fmt.Errorf("scenario %s: %w", path, err)
	}
	return sc, nil
}

func Capture(d *Driver, sc motion.Scenario, takes int, label, root string) ([]motion.Take, error) {
	defer func() {
		_ = d.send(cdpCall{Method: "Emulation.clearDeviceMetricsOverride"}, cdpCall{Method: "Emulation.setEmulatedMedia", Params: map[string]any{"features": []any{}}})
	}()
	var saved []motion.Take
	for n := 1; n <= takes; n++ {
		take, jpegs, err := d.take(sc, motion.NewID(time.Now(), label, n))
		if err == nil {
			take, err = motion.Save(root, take, jpegs)
		}
		if err != nil {
			return saved, fmt.Errorf("take %d of %d: %w", n, takes, err)
		}
		saved = append(saved, take)
	}
	return saved, nil
}

func (d *Driver) take(sc motion.Scenario, id string) (motion.Take, [][]byte, error) {
	err := d.send(cdpCall{Method: "Emulation.setDeviceMetricsOverride", Params: map[string]any{
		"width": sc.Viewport.Width, "height": sc.Viewport.Height, "deviceScaleFactor": sc.Viewport.DeviceScaleFactor, "mobile": false}},
		cdpCall{Method: "Emulation.setEmulatedMedia", Params: map[string]any{"features": []map[string]string{{"name": "prefers-reduced-motion", "value": sc.ReducedMotion}}}})
	var done bool
	if err == nil {
		err = d.run(fmt.Sprintf(motionReload, quoted(sc.URL)), &done)
	}
	if err == nil {
		err = d.awaitReady(sc.Ready.Selector)
	}
	if err != nil {
		return motion.Take{}, nil, err
	}
	time.Sleep(time.Duration(cmp.Or(sc.Ready.SettleMs, konst.MotionSettleMillisDefault)) * time.Millisecond)
	for i, setup := range sc.Setup {
		if err := d.perform(setup, fmt.Sprintf("setup[%d]", i)); err != nil {
			return motion.Take{}, nil, err
		}
		time.Sleep(konst.MotionSettleMillisDefault * time.Millisecond)
	}
	events := []string{"pointerdown", "click"}
	if sc.Trigger.Action == "press" {
		events = []string{"keydown"}
	}
	config, _ := json.Marshal(map[string]any{"watch": sc.Watch, "events": events})
	if err := d.run("("+sampler+")("+string(config)+")", &done); err != nil {
		return motion.Take{}, nil, err
	}
	if err := d.Client.StartScreencast(d.Tab); err != nil {
		return motion.Take{}, nil, err
	}
	time.Sleep(time.Duration(sc.RecordBeforeMs) * time.Millisecond)
	clicked := time.Now()
	err = d.perform(sc.Trigger, "trigger")
	time.Sleep(time.Until(clicked.Add(time.Duration(sc.RecordAfterMs) * time.Millisecond)))
	frames, stopped := d.Client.StopScreencast(d.Tab)
	var page pageTake
	if err = errors.Join(err, stopped); err == nil {
		err = d.run(motionRead, &page)
	}
	if err == nil && page.Trigger == nil {
		err = fmt.Errorf("no %s event fired on %s: the element may be covered or disabled", strings.Join(events, " or "), target(sc.Trigger))
	}
	if err != nil {
		return motion.Take{}, nil, err
	}
	slices.SortFunc(frames, func(a, b ScreencastFrame) int { return cmp.Compare(a.ChromeSeconds, b.ChromeSeconds) })
	take := motion.Take{Manifest: motion.Manifest{TakeID: id, Scenario: sc, Trigger: page.Trigger, Browser: page.Browser}}
	jpegs := make([][]byte, len(frames))
	for i, frame := range frames {
		ms := math.Round((frame.ChromeSeconds*1000-page.Trigger.WallMs)*10) / 10
		take.Frames = append(take.Frames, motion.Frame{ChromeTimestampS: frame.ChromeSeconds, MsFromTrigger: &ms})
		jpegs[i] = frame.Data
	}
	for _, sample := range page.Samples {
		ms := math.Round((sample.TS-page.Trigger.TimeStamp)*10) / 10
		take.Trace = append(take.Trace, motion.Sample{MsFromTrigger: &ms, Elements: sample.Elements})
	}
	return take, jpegs, nil
}

func (d *Driver) awaitReady(selector string) error {
	for until := time.Now().Add(konst.MotionReadyTimeoutMillis * time.Millisecond); time.Now().Before(until); time.Sleep(konst.BrowserSettleTickMillis * time.Millisecond) {
		var ready bool
		if d.run(fmt.Sprintf(motionReady, quoted(selector)), &ready) == nil && ready {
			return nil
		}
	}
	what := "the page"
	if selector != "" {
		what = "ready.selector " + strconv.Quote(selector)
	}
	return fmt.Errorf("%s was not visible on tab %d within %d ms", what, d.Tab, konst.MotionReadyTimeoutMillis)
}

func (d *Driver) perform(action motion.Action, field string) error {
	move := Move{Kind: MovePress, Value: action.Key}
	var err error
	switch action.Action {
	case "hover":
		return fmt.Errorf("%s: tofu cannot hover yet, use click or press", field)
	case "click":
		move = Move{Kind: MoveClick}
		move.Ref, err = d.refOf(action)
	}
	if err == nil {
		_, err = d.Do(move)
	}
	if err != nil {
		return fmt.Errorf("%s %s on %s: %w", field, action.Action, target(action), err)
	}
	return nil
}

func (d *Driver) refOf(action motion.Action) (string, error) {
	if action.Role != "" {
		ref, err := d.Find(action.Role, action.Name, 1)
		if err == nil && (ref == "" || action.Exact && d.refs.entries[ref].name != action.Name) {
			err = fmt.Errorf("tab %d shows no such %s", d.Tab, action.Role)
		}
		return ref, err
	}
	deadline := time.Now().Add(konst.BrowserObserveTimeoutMillis * time.Millisecond)
	_, err := d.observe(deadline, true)
	var answer cdpAnswer
	if err == nil {
		answer, err = d.one(deadline, false, cdpCall{Method: "Runtime.evaluate", Params: map[string]any{"expression": "document.querySelector(" + quoted(action.Selector) + ")"}})
	}
	var element remoteObject
	if err == nil {
		element, err = answer.object()
	}
	var described struct {
		Node struct {
			Backend int `json:"backendNodeId"`
		} `json:"node"`
	}
	if err == nil && element.ObjectID != "" {
		if answer, err = d.one(deadline, false, cdpCall{Method: "DOM.describeNode", Params: map[string]any{"objectId": element.ObjectID}}); err == nil {
			err = answer.into(&described)
		}
	}
	for ref, entry := range d.refs.entries {
		if err == nil && described.Node.Backend != 0 && entry.backend == described.Node.Backend {
			return ref, nil
		}
	}
	return "", cmp.Or(err, fmt.Errorf("tab %d shows nothing there that tofu can click", d.Tab))
}

func (d *Driver) run(expression string, into any) error {
	return d.value(time.Now().Add(konst.BrowserActTimeoutMillis*time.Millisecond), false, evaluate(expression), into)
}

func (d *Driver) send(calls ...cdpCall) error {
	answers, err := d.cdp(time.Now().Add(konst.BrowserActTimeoutMillis*time.Millisecond), false, calls...)
	for _, answer := range answers {
		if err == nil && answer.Error != "" {
			err = errors.New(answer.Error)
		}
	}
	return err
}

func target(action motion.Action) string {
	switch {
	case action.Selector != "":
		return "selector " + strconv.Quote(action.Selector)
	case action.Role != "":
		return action.Role + " " + strconv.Quote(action.Name)
	}
	return "key " + action.Key
}

func quoted(text string) string {
	raw, _ := json.Marshal(text)
	return string(raw)
}
