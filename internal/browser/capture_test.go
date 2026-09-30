package browser

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"tofu/internal/browser/motion"
)

const (
	pageOriginMs  = 1.7e12
	triggerPageMs = 1000.0
	triggerWallMs = pageOriginMs + triggerPageMs
	headerSel     = ".faq .item:nth-child(1) .header"
)

type motionPage struct {
	mu      sync.Mutex
	deaf    bool
	clicked bool
	steps   []string
}

func (p *motionPage) did(step string) {
	if len(p.steps) == 0 || p.steps[len(p.steps)-1] != step {
		p.steps = append(p.steps, step)
	}
}

func (p *motionPage) answer(method string, params map[string]any) any {
	script, _ := params["functionDeclaration"].(string)
	expression, _ := params["expression"].(string)
	switch {
	case method == "Emulation.clearDeviceMetricsOverride", method == "Emulation.setEmulatedMedia" && len(params["features"].([]any)) == 0:
		p.did("clear")
		return map[string]any{}
	case strings.HasPrefix(method, "Emulation.set"):
		if method == "Emulation.setDeviceMetricsOverride" && params["width"] != 960.0 {
			return nil
		}
		p.did("emulate")
		return map[string]any{}
	case strings.Contains(expression, "__tofuMotionReload = true"):
		p.did("reload")
		return byValue(true)
	case strings.Contains(expression, "window.__tofuMotionReload ||"):
		p.did("ready")
		return byValue(slices.Contains(p.steps, "reload"))
	case strings.Contains(expression, "requestAnimationFrame(tick)"):
		p.did("install")
		return byValue(true)
	case strings.Contains(expression, "running = false"):
		p.did("read")
		var trigger any
		if p.clicked && !p.deaf {
			trigger = map[string]any{"event": "pointerdown", "timeStamp": triggerPageMs, "wallMs": triggerWallMs}
		}
		sample := func(ts, height float64) map[string]any {
			return map[string]any{"ts": ts, "elements": map[string]any{"first-answer": map[string]any{"height": height, "display": "block", "attributes": map[string]string{"data-state": "closed"}}, "second-question": nil}}
		}
		return byValue(map[string]any{"trigger": trigger, "browser": "Chrome/140", "samples": []any{sample(990, 120), sample(1016.7, 0.5), sample(1253.4, 76.78)}})
	case expression == "document.querySelector(\""+strings.ReplaceAll(headerSel, `"`, `\"`)+"\")":
		return map[string]any{"result": map[string]any{"type": "object", "objectId": "header"}}
	case method == "DOM.describeNode" && params["objectId"] == "header":
		return map[string]any{"node": map[string]any{"backendNodeId": 25}}
	case method == "Page.getFrameTree":
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F", "loaderId": "L", "url": "http://localhost:4173/"}}}
	case method == "Accessibility.getFullAXTree":
		return map[string]any{"nodes": []any{
			map[string]any{"nodeId": "1", "role": map[string]any{"value": "RootWebArea"}, "name": map[string]any{"value": "Field Notes"}, "childIds": []string{"2"}},
			map[string]any{"nodeId": "2", "role": map[string]any{"value": "button"}, "name": map[string]any{"value": "What comes with a Field Notes membership?"}, "backendDOMNodeId": 25},
		}}
	case strings.Contains(expression, cursorTag):
		return byValue([]any{})
	case method == "DOM.scrollIntoViewIfNeeded":
		return map[string]any{}
	case method == "DOM.getBoxModel":
		return map[string]any{"model": map[string]any{"content": []float64{20, 20, 33, 20, 33, 33, 20, 33}}}
	case method == "DOM.resolveNode":
		return map[string]any{"object": map[string]any{"objectId": "target"}}
	case method == "Input.dispatchMouseEvent":
		if params["type"] == "mousePressed" {
			p.did("click")
			p.clicked = true
		}
		return map[string]any{}
	case script == blockerAt:
		return map[string]any{"result": map[string]any{"type": "object", "subtype": "null", "value": nil}}
	case script == submitsOrLinks:
		return byValue(false)
	case strings.Contains(script, "aria-checked"), strings.Contains(script, "this.click()"):
		return byValue(nil)
	case strings.Contains(expression, "MutationObserver"):
		return byValue(0)
	case expression == pageStateScript:
		return byValue(map[string]any{"url": "http://localhost:4173/", "count": 10, "text": strings.Repeat("x", len(p.steps))})
	}
	return nil
}

func (p *motionPage) relay(t *testing.T) *Driver {
	ours, relay := net.Pipe()
	t.Cleanup(func() { _ = ours.Close() })
	go func() {
		in, out := json.NewDecoder(relay), json.NewEncoder(relay)
		for {
			var req request
			if in.Decode(&req) != nil {
				return
			}
			p.mu.Lock()
			answer := result{ID: req.ID, OK: true, Value: json.RawMessage(`[{"id":7,"url":"http://localhost:4173/"}]`)}
			switch req.Op {
			case opScreencast:
				var args struct{ Action string }
				_ = json.Unmarshal(req.Args, &args)
				p.did(args.Action)
				answer.Value = json.RawMessage(`{}`)
				if args.Action == "stop" {
					answer.Value, _ = json.Marshal([]ScreencastFrame{
						{Data: []byte("late"), ChromeSeconds: (triggerWallMs + 253.4) / 1000},
						{Data: []byte("early"), ChromeSeconds: (triggerWallMs - 10) / 1000},
					})
				}
			case opCDP:
				var args cdpArgs
				_ = json.Unmarshal(req.Args, &args)
				answers := []cdpAnswer{}
				for _, call := range args.Calls {
					params, _ := call.Params.(map[string]any)
					raw, _ := json.Marshal(p.answer(call.Method, params))
					if string(raw) == "null" {
						answers = append(answers, cdpAnswer{Error: "the fake page has no " + call.Method})
						continue
					}
					answers = append(answers, cdpAnswer{Result: raw})
				}
				answer.Value, _ = json.Marshal(answers)
			}
			p.mu.Unlock()
			if out.Encode(answer) != nil {
				return
			}
		}
	}()
	return &Driver{Client: &Client{conn: ours, out: json.NewEncoder(ours), in: json.NewDecoder(ours)}, Tab: 7}
}

func fieldNotes(t *testing.T) motion.Scenario {
	sc, err := ReadScenario(filepath.Join("motion", "testdata", "field-notes-close.json"))
	if err != nil {
		t.Fatal(err)
	}
	sc.Ready.SettleMs, sc.RecordBeforeMs, sc.RecordAfterMs = 1, 1, 1
	return sc
}

func TestCaptureSavesFramesAndSamplesTimedFromTheTrigger(t *testing.T) {
	page := &motionPage{}
	root := t.TempDir()
	takes, err := Capture(page.relay(t), fieldNotes(t), 1, "before", root)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := motion.Load(root, takes[0].Manifest.TakeID)
	if err != nil {
		t.Fatal(err)
	}
	var frameMs, sampleMs []float64
	for _, frame := range saved.Frames {
		frameMs = append(frameMs, *frame.MsFromTrigger)
	}
	for _, sample := range saved.Trace {
		sampleMs = append(sampleMs, *sample.MsFromTrigger)
	}
	first, _ := os.ReadFile(filepath.Join(saved.Dir, saved.Frames[0].File))
	wantSteps := []string{"emulate", "reload", "ready", "install", "start", "click", "stop", "read", "clear"}
	if !slices.Equal(frameMs, []float64{-10, 253.4}) || !slices.Equal(sampleMs, []float64{-10, 16.7, 253.4}) || string(first) != "early" ||
		saved.Trace[2].Elements["first-answer"].Height != 76.78 || saved.Manifest.Trigger.Event != "pointerdown" || saved.Manifest.Browser != "Chrome/140" ||
		!slices.Equal(page.steps, wantSteps) {
		t.Fatalf("frames %v (first %q), samples %v, trigger %+v, steps %v; want frames [-10 253.4] early first, samples [-10 16.7 253.4], steps %v",
			frameMs, first, sampleMs, saved.Manifest.Trigger, page.steps, wantSteps)
	}
}

func TestCaptureWithNoTriggerEventFailsNamingTheSelector(t *testing.T) {
	page := &motionPage{deaf: true}
	sc := fieldNotes(t)
	sc.Trigger = motion.Action{Action: "click", Selector: headerSel}
	root := t.TempDir()
	takes, err := Capture(page.relay(t), sc, 2, "deaf", root)
	entries, _ := os.ReadDir(root)
	if err == nil || !strings.Contains(err.Error(), `selector "`+headerSel+`"`) || !strings.Contains(err.Error(), "no pointerdown or click event fired") || len(takes) != 0 || len(entries) != 0 ||
		!slices.Contains(page.steps, "click") || page.steps[len(page.steps)-1] != "clear" {
		t.Fatalf("a deaf trigger gave %d takes, %d saved, steps %v and %v", len(takes), len(entries), page.steps, err)
	}
	t.Log(err)
}
