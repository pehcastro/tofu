package browser

import (
	"encoding/json"
	"fmt"
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
	fakePaintMs   = 1000.0 / 144
)

type motionPage struct {
	mu        sync.Mutex
	deaf      bool
	armed     []string
	fired     string
	steps     []string
	taped     []string
	script    string
	installed string
	failed    error
	frames    []ScreencastFrame
}

func (p *motionPage) node(read string) any {
	run := exec.Command("node", "-e", p.script)
	run.Env = append(os.Environ(), "TOFU_INSTALL="+p.installed, "TOFU_READ="+read)
	out, err := run.CombinedOutput()
	var got any
	if err == nil {
		err = json.Unmarshal(out, &got)
	}
	if err != nil {
		p.failed = fmt.Errorf("the fake page under node: %w\n%s", err, out)
		return nil
	}
	return byValue(got)
}

func (p *motionPage) did(step string) {
	if len(p.steps) == 0 || p.steps[len(p.steps)-1] != step {
		p.steps = append(p.steps, step)
	}
}

func (p *motionPage) answer(method string, params map[string]any) any {
	script, _ := params["functionDeclaration"].(string)
	expression, _ := params["expression"].(string)
	if p.steps[len(p.steps)-1] == "start" || p.steps[len(p.steps)-1] == "click" {
		p.taped = append(p.taped, method)
	}
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
		p.armed, p.installed = config.Events, expression
		return byValue(true)
	case strings.Contains(expression, "running = false"):
		p.did("read")
		if p.script != "" {
			return p.node(expression)
		}
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
				if frames := p.frames; args.Action == "stop" {
					if frames == nil {
						frames = []ScreencastFrame{
							{Data: []byte("late"), ChromeSeconds: (triggerWallMs + 253.4) / 1000},
							{Data: []byte("early"), ChromeSeconds: (triggerWallMs - 10) / 1000},
						}
					}
					answer.Value, _ = json.Marshal(frames)
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

func TestWhileRecordingTheTriggerSendsOnlyItsInput(t *testing.T) {
	page := &motionPage{}
	if _, err := Capture(page.relay(t), fieldNotes(t), 1, "quiet", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if len(page.taped) == 0 || slices.ContainsFunc(page.taped, func(method string) bool { return method != "Input.dispatchMouseEvent" }) {
		t.Fatalf("between the screencast start and stop the page got %v; want only the trigger's mouse events", page.taped)
	}
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
const take = (withSampler, closeMs, symbolMs, shift) => {
  let clock = 0, now = 0, main = 0, rafs = [], hidden = false, reopen = null, tokens = 0, lastDamage = 0, shown = {seen: ''}, nextFrame = 0;
  let keeperMs = 0, keeperUntil = 0;
  const commits = [], frames = [], symbolFrom = TRIGGER_MS + 2 * VSYNC_MS;
  const look = () => {
    if (hidden) return {height: 0, display: 'none'};
    const left = 1 - (now - TRIGGER_MS) / closeMs;
    return {height: left > 1 || left <= 0 ? 76.78 : 76.78 * left ** 3, display: 'block'};
  };
  const el = {getBoundingClientRect: () => ({x: 0, y: 0, width: 100, height: look().height}), getAttribute: () => '', hidden: false};
  const page = {
    window: {}, performance: {now: () => (clock += SPIN_MS), timeOrigin: 0}, requestAnimationFrame: tick => rafs.push(tick),
    getComputedStyle: () => ({opacity: '1', display: look().display, visibility: 'visible', getPropertyValue: () => ''}),
    document: {
      querySelector: () => el, addEventListener() {}, documentElement: {append() {}},
      createElement: () => ({style: {}, remove() {}, animate: (keyframes, timing) => { keeperMs = timing.duration; }}),
    },
  };
  if (withSampler) vm.runInContext('(' + sampler + ')(' + JSON.stringify({watch: [{name: 'answer', selector: '.content'}], events: ['pointerdown']}) + ')', vm.createContext(page));
  for (let v = 0; v * VSYNC_MS < TRIGGER_MS + 600; v++) {
    const t = v * VSYNC_MS;
    let drawn = null;
    while (commits.length && commits[0].drawAt <= t + 1e-6) {
      drawn = commits.shift();
      if (drawn.keeperMs) keeperUntil = t + drawn.keeperMs;
    }
    const symbolTurning = t >= symbolFrom && t < symbolFrom + symbolMs;
    if ((drawn && drawn.seen !== shown.seen) || symbolTurning || t < keeperUntil) {
      tokens = Math.min(BUCKET_MS, tokens + t - lastDamage);
      lastDamage = t;
      if (screencastGaps ? t >= nextFrame : tokens >= CAPTURE_MS) {
        tokens -= CAPTURE_MS;
        nextFrame = screencastGaps && t + screencastGaps[(frames.length + shift) % screencastGaps.length] - 1;
        frames.push({stamp: t + STAMP_MS, main: (drawn || shown).main});
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
    commits.push({main, keeperMs, seen: JSON.stringify(look()), drawAt: Math.ceil((clock + VSYNC_MS) / VSYNC_MS) * VSYNC_MS});
    keeperMs = 0;
    if (!hidden && now - TRIGGER_MS >= closeMs) {
      reopen = {main, row: t};
      hidden = true;
    }
  }
  const after = frames.find(frame => frame.stamp >= reopen.row);
  return {
    near: frames.filter(frame => frame.stamp > reopen.row - 10 && frame.stamp < reopen.row + 50).map(frame => [+(frame.stamp - reopen.row).toFixed(1), frame.main - reopen.main]),
    row: reopen.row - TRIGGER_MS, frames: frames.length, lateMs: after.stamp - reopen.row, lateShowsOlder: after.main < reopen.main,
    framedMs: frames.filter(frame => frame.main === reopen.main).map(frame => frame.stamp - reopen.row),
    sampled: withSampler && page.window.__tofuMotion.samples.some(sample => sample.ts === reopen.row && sample.elements.answer.height === 76.78),
  };
};
console.log(JSON.stringify(Array.from({length: 54}, (_, phase) => {
  const closeMs = 250 + (phase % 9) * VSYNC_MS / 3, symbolMs = Math.floor(phase / 9) && closeMs + (Math.floor(phase / 9) - 3) * VSYNC_MS / 2;
  return {phase, closeMs, symbolMs, page: take(false, closeMs, symbolMs, phase), sampled: take(true, closeMs, symbolMs, phase)};
})));
`

type onePaintRun struct {
	Row, LateMs    float64
	Frames         int
	LateShowsOlder bool
	FramedMs       []float64
	Sampled        bool
	Near           [][2]float64
}

type onePaintPhase struct {
	Phase             int
	CloseMs, SymbolMs float64
	Page, Sampled     onePaintRun
}

func (p onePaintPhase) framed(windowMs float64) bool {
	return slices.ContainsFunc(p.Sampled.FramedMs, func(ms float64) bool { return ms >= 0 && ms <= windowMs })
}

func onePaintPhases(t *testing.T, screencastGaps string) []onePaintPhase {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on the PATH to run sampler.js")
	}
	out, err := exec.Command(node, "-e", "const sampler = "+quoted(sampler)+", screencastGaps = "+screencastGaps+";"+onePaintPage).Output()
	if err != nil {
		t.Fatalf("the fake page under node: %v\n%s", err, out)
	}
	var phases []onePaintPhase
	if err := json.Unmarshal(out, &phases); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return phases
}

func TestAOnePaintStateUnderTheRealScreencastGapsGetsAFrame(t *testing.T) {
	missed := 0
	for _, p := range onePaintPhases(t, "[6.5, 14.6, 8.3, 11.9, 6.7, 10.4, 11.1, 13.7, 3.6, 17.6, 3.1, 12.9, 6.9, 18.7, 4.1, 17.3, 5.6, 14.2, 6, 15.3, 5.7, 14.9, 6.6, 14.2, 7.2, 14.5, 2.4]") {
		if !p.framed(fakePaintMs+konst.MotionFrameLagMillis) || !p.Sampled.Sampled || p.Sampled.Row != p.Page.Row {
			missed++
			t.Logf("phase %d: framed at %v ms after the row, sampled %v, row +%.1f against +%.1f, near %v", p.Phase, p.Sampled.FramedMs, p.Sampled.Sampled, p.Sampled.Row, p.Page.Row, p.Sampled.Near)
		}
	}
	if missed > 0 {
		t.Fatalf("%d of 54 phases have no frame of the one-paint reopen within a paint and %d ms, under the screencast gaps of take 20260930T150459-r3-1", missed, konst.MotionFrameLagMillis)
	}
}

func TestAOnePaintStateBetweenTwoScreencastFramesGetsAFrame(t *testing.T) {
	for _, p := range onePaintPhases(t, "null") {
		t.Logf("phase %d, close %.1f ms, symbol turning %.1f ms: page alone %+v; sampled %+v", p.Phase, p.CloseMs, p.SymbolMs, p.Page, p.Sampled)
		framed := p.framed(konst.MotionFrameLagMillis)
		if !p.Page.LateShowsOlder || p.Page.LateMs > 10 {
			t.Errorf("phase %d: the fake's first frame after the reopen row is +%.1f ms and shows the old paint %v; the real takes have one 2 to 10 ms after that does", p.Phase, p.Page.LateMs, p.Page.LateShowsOlder)
		}
		if !p.Sampled.Sampled || !framed || p.Sampled.Row != p.Page.Row || p.Sampled.Frames < p.Page.Frames {
			t.Errorf("phase %d: sampler read the reopen %v, framed at %v ms after the row (want one within %d), row +%.1f against +%.1f without the sampler, %d frames against %d",
				p.Phase, p.Sampled.Sampled, p.Sampled.FramedMs, konst.MotionFrameLagMillis, p.Sampled.Row, p.Page.Row, p.Sampled.Frames, p.Page.Frames)
		}
	}
}

const (
	fakeDOM = `
const vm = require('node:vm');
let now = 0, clock = 0, rafs = [], listeners = [];
const all = [], body = {tagName: 'BODY', children: [], childNodes: [], querySelectorAll: () => all};
const add = (parent, tag, attrs, text, box, look = () => ({})) => {
  const el = {tagName: tag, id: attrs.id || '', parentElement: parent, children: [], childNodes: text ? [{nodeType: 3, data: text}] : [], nodeType: 1, hidden: false, look,
    textContent: text, getAttribute: name => attrs[name] ?? null,
    getBoundingClientRect: () => { const r = box(now); return {...r, left: r.x, top: r.y, right: r.x + r.width, bottom: r.y + r.height}; }};
  parent.children.push(el);
  parent.childNodes.push(el);
  all.push(el);
  for (let up = parent; up !== body; up = up.parentElement) up.textContent += text;
  return el;
};
const styleOf = el => {
  let visibility = 'visible';
  for (let up = el; up !== body; up = up.parentElement) if (up.look(now).visibility) { visibility = up.look(now).visibility; break; }
  return {opacity: '1', display: 'block', visibility, getPropertyValue: () => ''};
};
const page = {
  window: {}, innerWidth: 960, innerHeight: 720, navigator: {userAgent: 'node'}, CSS: {escape: s => s},
  performance: {now: () => (clock += 0.01), timeOrigin: 0}, requestAnimationFrame: tick => rafs.push(tick), getComputedStyle: styleOf,
  MutationObserver: class { observe() {} disconnect() {} },
  document: {body, addEventListener: (type, fn) => listeners.push(fn), documentElement: {append() {}}, querySelector: () => null,
    createElement: () => ({style: {}, remove() {}, animate() {}})},
};
`
	fakeRun = `
const context = vm.createContext(page);
vm.runInContext(process.env.TOFU_INSTALL, context);
for (let k = 0, fired = false; k <= 42; k++) {
  now = clock = 900 + k * 50 / 3;
  if (now >= 1000 && !fired) {
    for (const fn of listeners) fn({type: 'pointerdown', timeStamp: 1000});
    fired = true;
  }
  const due = rafs;
  rafs = [];
  for (const tick of due) tick(now);
}
console.log(JSON.stringify(vm.runInContext(process.env.TOFU_READ, context)));
`
	flashPage = `
const main = add(body, 'MAIN', {}, '', () => ({x: 0, y: 0, width: 960, height: 720}));
const save = add(main, 'BUTTON', {id: 'save'}, '', () => ({x: 20, y: 20, width: 120, height: 40}), t => ({visibility: t > 1095 && t < 1110 ? 'hidden' : 'visible'}));
add(save, 'SPAN', {}, 'Save', () => ({x: 30, y: 30, width: 100, height: 20}));
add(main, 'DIV', {}, '', t => { const side = 40 * (Math.abs(Math.cos(t / 159.15)) + Math.abs(Math.sin(t / 159.15))); return {x: 220 - side / 2, y: 40 - side / 2, width: side, height: side}; });
add(main, 'P', {}, 'Nothing changes here', () => ({x: 20, y: 100, width: 400, height: 20}));
add(main, 'DIV', {}, '', () => ({x: 20, y: 140, width: 300, height: 200}));
`
	crowdPage = `
for (let i = 0; i < 5000; i++) add(body, 'DIV', {}, '', () => ({x: i % 96 * 10, y: Math.floor(i / 96) * 10, width: 10, height: 10}));
`
)

func discovered(t *testing.T, script string) motion.Take {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("no node on the PATH to run sampler.js")
	}
	page := &motionPage{script: fakeDOM + script + fakeRun}
	for ms := -100.0; ms <= 600; ms += 50.0 / 3 {
		page.frames = append(page.frames, ScreencastFrame{Data: []byte("frame"), ChromeSeconds: (1000 + ms + 3) / 1000})
	}
	sc := fieldNotes(t)
	sc.Watch = nil
	root := t.TempDir()
	takes, err := Capture(page.relay(t), sc, 1, "discover", root)
	if err != nil {
		t.Fatal(err, page.failed)
	}
	saved, err := motion.Load(root, takes[0].Manifest.TakeID)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestADiscoveredTakeNamesTheOnePaintFlashAndNotTheSpinner(t *testing.T) {
	saved := discovered(t, flashPage)
	frame := slices.IndexFunc(saved.Frames, func(f motion.Frame) bool { return *f.MsFromTrigger >= 100 }) + 1
	var numbered []string
	for _, line := range saved.Manifest.Report {
		if line != "" && line[0] >= '1' && line[0] <= '9' {
			numbered = append(numbered, line)
		}
	}
	want := fmt.Sprintf(`1. button "Save" at #save: +100.0 ms for 16.7 ms (1 paint), frames #%d #%d #%d; `, frame, frame+1, frame+2)
	if len(numbered) != 1 || len(saved.Manifest.Report) == 0 || !strings.HasPrefix(numbered[0], want) || !strings.Contains(numbered[0], "visibility visible -> hidden -> visible") ||
		saved.Manifest.Report[0] != "watched 6 of 6 elements seen" {
		t.Fatalf("report %q; want one transient, the button, as %q", saved.Manifest.Report, want)
	}
	t.Log(strings.Join(saved.Manifest.Report, "\n"))
}

func TestADiscoveredTakeOfFiveThousandElementsStaysWithinTheCap(t *testing.T) {
	saved := discovered(t, crowdPage)
	d := saved.Manifest.Discovery
	ceiling := konst.MotionDiscoverCeiling
	if d == nil || len(saved.Manifest.Report) == 0 || len(d.Elements) != ceiling || d.Seen != 5000 || len(saved.Trace[0].Elements) != ceiling ||
		!strings.HasPrefix(saved.Manifest.Report[0], fmt.Sprintf("watched %d of 5000 elements seen, capped at %d", ceiling, ceiling)) {
		t.Fatalf("discovery %+v, first sample %d elements, report %q; want %d watched of 5000 and the report saying so", d, len(saved.Trace[0].Elements), saved.Manifest.Report, ceiling)
	}
	t.Log(saved.Manifest.Report[0])
}
