package browser

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"tofu/internal/browser/motion"
	"tofu/internal/konst"
)

const (
	pageOriginMs  = 1.7e12
	triggerPageMs = 1000.0
	triggerWallMs = pageOriginMs + triggerPageMs
	headerSel     = ".faq .item:nth-child(1) .header"
)

type motionPage struct {
	mu    sync.Mutex
	deaf  bool
	armed []string
	fired string
	steps []string
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
		var config struct{ Events []string }
		_ = json.Unmarshal([]byte(expression[strings.LastIndex(expression, ")(")+2:len(expression)-1]), &config)
		p.armed = config.Events
		return byValue(true)
	case strings.Contains(expression, "running = false"):
		p.did("read")
		var trigger any
		if p.fired != "" && !p.deaf {
			trigger = map[string]any{"event": p.fired, "timeStamp": triggerPageMs, "wallMs": triggerWallMs}
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
		fires := map[any]string{"mouseMoved": "pointerover", "mousePressed": "pointerdown"}[params["type"]]
		if params["type"] == "mousePressed" {
			p.did("click")
		}
		if p.fired == "" && slices.Contains(p.armed, fires) {
			p.fired = fires
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
	wantSteps := []string{"hide", "emulate", "reload", "ready", "install", "start", "click", "stop", "read", "clear", "show"}
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
		!slices.Contains(page.steps, "click") || page.steps[0] != "hide" || page.steps[len(page.steps)-1] != "show" {
		t.Fatalf("a deaf trigger gave %d takes, %d saved, steps %v and %v", len(takes), len(entries), page.steps, err)
	}
	t.Log(err)
}

func TestAHoverTakeRecordsItsTriggerFromPointerover(t *testing.T) {
	page := &motionPage{}
	sc := fieldNotes(t)
	sc.Trigger = motion.Action{Action: "hover", Selector: headerSel}
	takes, err := Capture(page.relay(t), sc, 1, "hover", t.TempDir())
	if err != nil || len(takes) != 1 || takes[0].Manifest.Trigger.Event != "pointerover" || !slices.Equal(page.armed, []string{"pointerover", "mouseover"}) {
		t.Fatalf("a hover take armed %v and gave %d takes, %v", page.armed, len(takes), err)
	}
}

const onePaintPage = `
const vm = require('node:vm');
const VSYNC_MS = 1000 / 144, WORK_MS = 0.5, SPIN_MS = 0.01, CAPTURE_MS = 10, BUCKET_MS = 15, STAMP_MS = 3, TRIGGER_MS = 100;
const take = (withSampler, closeMs) => {
  let clock = 0, now = 0, main = 0, rafs = [], hidden = false, reopen = null, tokens = 0, lastDamage = 0, shown = {seen: ''};
  const commits = [], frames = [];
  const look = () => {
    if (hidden) return {height: 0, display: 'none'};
    const left = 1 - (now - TRIGGER_MS) / closeMs;
    return {height: left > 1 || left <= 0 ? 76.78 : 76.78 * left ** 3, display: 'block'};
  };
  const el = {getBoundingClientRect: () => ({x: 0, y: 0, width: 100, height: look().height}), getAttribute: () => '', hidden: false};
  const page = {
    window: {}, performance: {now: () => (clock += SPIN_MS), timeOrigin: 0}, requestAnimationFrame: tick => rafs.push(tick),
    getComputedStyle: () => ({opacity: '1', display: look().display, visibility: 'visible', getPropertyValue: () => ''}),
    document: {querySelector: () => el, addEventListener() {}},
  };
  if (withSampler) vm.runInContext('(' + sampler + ')(' + JSON.stringify({watch: [{name: 'answer', selector: '.content'}], events: ['pointerdown']}) + ')', vm.createContext(page));
  for (let v = 0; v * VSYNC_MS < TRIGGER_MS + 600; v++) {
    const t = v * VSYNC_MS;
    let drawn = null;
    while (commits.length && commits[0].drawAt <= t + 1e-6) drawn = commits.shift();
    if (drawn && drawn.seen !== shown.seen) {
      tokens = Math.min(BUCKET_MS, tokens + t - lastDamage);
      lastDamage = t;
      if (tokens >= CAPTURE_MS) {
        tokens -= CAPTURE_MS;
        frames.push({stamp: t + STAMP_MS, main: drawn.main});
      }
    }
    shown = drawn || shown;
    if (clock > t) continue;
    now = t;
    clock = t + WORK_MS;
    main++;
    const due = rafs;
    rafs = [];
    for (const tick of due) tick(t);
    commits.push({main, seen: JSON.stringify(look()), drawAt: Math.ceil((clock + VSYNC_MS) / VSYNC_MS) * VSYNC_MS});
    if (!hidden && now - TRIGGER_MS >= closeMs) {
      reopen = {main, row: t};
      hidden = true;
    }
  }
  const after = frames.find(frame => frame.stamp >= reopen.row);
  return {
    row: reopen.row - TRIGGER_MS, frames: frames.length, lateMs: after.stamp - reopen.row, lateShowsOlder: after.main < reopen.main,
    framedMs: frames.filter(frame => frame.main === reopen.main).map(frame => frame.stamp - reopen.row),
    sampled: withSampler && page.window.__tofuMotion.samples.some(sample => sample.ts === reopen.row && sample.elements.answer.height === 76.78),
  };
};
console.log(JSON.stringify(Array.from({length: 9}, (_, phase) => {
  const closeMs = 250 + phase * VSYNC_MS / 3;
  return {phase, page: take(false, closeMs), sampled: take(true, closeMs)};
})));
`

func TestAOnePaintStateBetweenTwoScreencastFramesGetsAFrame(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on the PATH to run sampler.js")
	}
	out, err := exec.Command(node, "-e", "const sampler = "+quoted(sampler)+";"+onePaintPage).Output()
	if err != nil {
		t.Fatalf("the fake page under node: %v\n%s", err, out)
	}
	type run struct {
		Row, LateMs    float64
		Frames         int
		LateShowsOlder bool
		FramedMs       []float64
		Sampled        bool
	}
	var phases []struct {
		Phase         int
		Page, Sampled run
	}
	if err := json.Unmarshal(out, &phases); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, p := range phases {
		t.Logf("phase %d: page alone %+v; sampled %+v", p.Phase, p.Page, p.Sampled)
		framed := slices.ContainsFunc(p.Sampled.FramedMs, func(ms float64) bool { return ms >= 0 && ms <= konst.MotionFrameLagMillis })
		if !p.Page.LateShowsOlder || p.Page.LateMs > 10 {
			t.Errorf("phase %d: the fake's first frame after the reopen row is +%.1f ms and shows the old paint %v; the real takes have one 2 to 10 ms after that does", p.Phase, p.Page.LateMs, p.Page.LateShowsOlder)
		}
		if !p.Sampled.Sampled || !framed || p.Sampled.Row != p.Page.Row || p.Sampled.Frames < p.Page.Frames {
			t.Errorf("phase %d: sampler read the reopen %v, framed at %v ms after the row (want one within %d), row +%.1f against +%.1f without the sampler, %d frames against %d",
				p.Phase, p.Sampled.Sampled, p.Sampled.FramedMs, konst.MotionFrameLagMillis, p.Sampled.Row, p.Page.Row, p.Sampled.Frames, p.Page.Frames)
		}
	}
}
