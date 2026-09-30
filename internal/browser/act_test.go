package browser

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"tofu/internal/konst"
)

type togglePage struct {
	checkbox       bool
	checked        bool
	mouseChecks    bool
	mouseRerenders bool
	rerenders      int
	cursorPainted  bool
	link           bool
	quads          [][]float64
	pressed        [2]float64
	hovered        [2]float64
	buttonDowns    int
	onParent       int
	navigated      time.Time
	js             *jsPage
	registered     []string
	read           string
}

func googleResult() [][]float64 {
	return [][]float64{{0, 40, 60, 40, 60, 55, 0, 55}, {0, 0, 120, 0, 120, 20, 0, 20}}
}

func (p *togglePage) inQuad(x, y float64) bool {
	for _, q := range p.quads {
		if x >= q[0] && x <= q[2] && y >= q[1] && y <= q[5] {
			return true
		}
	}
	return false
}

func (p *togglePage) heading() string {
	if !p.navigated.IsZero() {
		return "Listing\n"
	}
	return "Search results\n"
}

func (p *togglePage) answer(method string, params map[string]any) any {
	script, _ := params["functionDeclaration"].(string)
	expression, _ := params["expression"].(string)
	x, _ := params["x"].(float64)
	y, _ := params["y"].(float64)
	if args, _ := params["arguments"].([]any); len(args) == 2 {
		x, _ = args[0].(map[string]any)["value"].(float64)
		y, _ = args[1].(map[string]any)["value"].(float64)
	}
	switch {
	case method == "Page.addScriptToEvaluateOnNewDocument":
		p.registered = append(p.registered, params["source"].(string))
		return map[string]any{"identifier": "1"}
	case p.js != nil && method == "Runtime.evaluate":
		value, ok := p.js.eval(expression)
		if state, isState := value.(map[string]any); isState && expression == pageStateScript {
			p.read, _ = state["text"].(string)
		}
		if !ok {
			return nil
		}
		return byValue(value)
	case method == "DOM.scrollIntoViewIfNeeded":
		return map[string]any{}
	case method == "DOM.getBoxModel" && p.link:
		return map[string]any{"model": map[string]any{"content": []float64{0, 0, 200, 0, 200, 60, 0, 60}}}
	case method == "DOM.getBoxModel":
		return map[string]any{"model": map[string]any{"content": []float64{20, 20, 33, 20, 33, 33, 20, 33}}}
	case method == "DOM.getContentQuads" && p.quads != nil:
		return map[string]any{"quads": p.quads}
	case method == "DOM.resolveNode":
		return map[string]any{"object": map[string]any{"objectId": "target"}}
	case method == "Input.dispatchMouseEvent" && p.link:
		switch params["type"] {
		case "mouseMoved":
			p.hovered = [2]float64{x, y}
		case "mousePressed":
			p.buttonDowns++
		case "mouseReleased":
			p.pressed = [2]float64{x, y}
			switch {
			case p.inQuad(x, y) && p.js != nil:
				p.navigated = time.Now()
				p.js.eval("open()")
			case p.inQuad(x, y):
				p.navigated = time.Now()
			default:
				p.onParent++
			}
		}
		return map[string]any{}
	case method == "Input.dispatchMouseEvent":
		p.cursorPainted = true
		if params["type"] == "mouseReleased" && p.mouseRerenders {
			p.rerenders++
		}
		if params["type"] == "mouseReleased" && p.mouseChecks {
			p.checked = !p.checked
		}
		return map[string]any{}
	case script == blockerAt && p.link && !p.inQuad(x, y):
		return byValue("parent")
	case script == blockerAt:
		return map[string]any{"result": map[string]any{"type": "object", "subtype": "null", "value": nil}}
	case script == submitsOrLinks:
		return byValue(false)
	case strings.Contains(script, "this.click()") && p.link:
		p.navigated = time.Now()
		return byValue(nil)
	case strings.Contains(script, "this.click()"):
		p.checked = p.checked != p.checkbox
		return byValue(nil)
	case strings.Contains(script, "aria-checked") && p.checkbox:
		return byValue(strconv.FormatBool(p.checked))
	case strings.Contains(script, "aria-checked"):
		return byValue(nil)
	case strings.Contains(expression, "MutationObserver"):
		return byValue(0)
	case expression == pageStateScript && p.link:
		url := "https://www.airbnb.test/s/homes"
		if !p.navigated.IsZero() {
			url = "https://www.airbnb.test/rooms/1"
		}
		return byValue(map[string]any{"url": url, "count": 10, "text": strconv.Itoa(p.onParent), "heading": p.heading()})
	case expression == pageStateScript:
		count := 10
		if p.cursorPainted && !strings.Contains(expression, "data-tofu-cursor") {
			count++
		}
		return byValue(map[string]any{"url": "https://the-internet.test/checkboxes", "count": count, "text": strconv.Itoa(p.rerenders), "controls": strconv.FormatBool(p.checked)})
	}
	return nil
}

func byValue(v any) map[string]any {
	return map[string]any{"result": map[string]any{"type": "object", "value": v}}
}

func moveOn(t *testing.T, kind MoveKind, page *togglePage) Moved {
	t.Helper()
	return moveWith(t, relayTo(t, page), kind, page)
}

func relayTo(t *testing.T, page *togglePage) *Driver {
	t.Helper()
	ours, relay := net.Pipe()
	t.Cleanup(func() { _ = ours.Close() })
	go func() {
		in, out := json.NewDecoder(relay), json.NewEncoder(relay)
		for {
			var req request
			if in.Decode(&req) != nil {
				return
			}
			answer := result{ID: req.ID, OK: true, Value: json.RawMessage(`[{"id":7,"url":"https://the-internet.test/checkboxes","opened":true}]`)}
			if _, refused := cdpStatus(req.Args); req.Op == opCDP && refused != nil {
				answer = result{ID: req.ID, Error: refused.Error()}
			} else if req.Op == opCDP {
				var args cdpArgs
				_ = json.Unmarshal(req.Args, &args)
				answers := []cdpAnswer{}
				for _, call := range args.Calls {
					params, _ := call.Params.(map[string]any)
					answered := page.answer(call.Method, params)
					if answered == nil {
						answers = append(answers, cdpAnswer{Error: "the fake page has no " + call.Method})
						continue
					}
					raw, _ := json.Marshal(answered)
					answers = append(answers, cdpAnswer{Result: raw})
				}
				answer.Value, _ = json.Marshal(answers)
			}
			if out.Encode(answer) != nil {
				return
			}
		}
	}()
	role := "checkbox"
	if page.link {
		role = "link"
	}
	return &Driver{Client: &Client{conn: ours, out: json.NewEncoder(ours), in: json.NewDecoder(ours)}, Tab: 7,
		refs: refMap{entries: map[string]refEntry{"e25": {backend: 25, role: role}}}}
}

func moveWith(t *testing.T, driver *Driver, kind MoveKind, page *togglePage) Moved {
	t.Helper()
	moved, err := driver.Do(Move{Ref: "e25", Kind: kind})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %s, reading %q", kind, moved, page.read)
	return moved
}

func TestACheckboxDeafToTheMouseEndsCheckedThroughClickInOneAct(t *testing.T) {
	page := &togglePage{checkbox: true, mouseRerenders: true}
	moved := moveOn(t, MoveClick, page)
	if !page.checked || moved.Via != "click()" {
		t.Fatalf("after one click the box is checked=%v through %q; want checked through click()", page.checked, moved.Via)
	}
}

func TestACheckboxTheMouseChecksIsNotClickedAgain(t *testing.T) {
	page := &togglePage{checkbox: true, mouseChecks: true}
	moved := moveOn(t, MoveClick, page)
	if !page.checked || moved.Via != "" {
		t.Fatalf("after one click the box is checked=%v through %q; want checked by the mouse alone", page.checked, moved.Via)
	}
}

func TestAClickThatChangesNothingSaysThePageDidNotChange(t *testing.T) {
	if said := moveOn(t, MoveClick, &togglePage{}).String(); !strings.HasPrefix(said, Unchanged) {
		t.Fatalf("a click that changed nothing said %q; want it to start %q", said, Unchanged)
	}
}

func TestALinkWhoseBoxCentreHitsAParentIsPressedInItsLargestQuad(t *testing.T) {
	page := &togglePage{link: true, quads: googleResult()}
	moved := moveOn(t, MoveClick, page)
	if page.pressed != [2]float64{60, 10} || page.onParent != 0 || !moved.URLChanged || moved.Via != "" {
		t.Fatalf("the press landed at %v, %d on the parent, url changed %v through %q; want (60, 10) on the link by the mouse", page.pressed, page.onParent, moved.URLChanged, moved.Via)
	}
}

func TestAHoverMovesToTheLargestQuadAndNeverPressesOrClicks(t *testing.T) {
	page := &togglePage{link: true, quads: googleResult()}
	moved := moveOn(t, MoveHover, page)
	if page.hovered != [2]float64{60, 10} || page.buttonDowns != 0 || !page.navigated.IsZero() || moved.Via != "" {
		t.Fatalf("the hover moved to %v with %d presses, opened the link %v, through %q; want (60, 10), no press, nothing opened", page.hovered, page.buttonDowns, !page.navigated.IsZero(), moved.Via)
	}
}

func TestAPressLandingOnAParentFallsBackToClickInOneAct(t *testing.T) {
	page := &togglePage{link: true}
	moved := moveOn(t, MoveClick, page)
	if page.onParent != 0 || !moved.URLChanged || moved.Via != "click()" {
		t.Fatalf("%d presses on the parent, url changed %v through %q; want none and the link opened through click()", page.onParent, moved.URLChanged, moved.Via)
	}
}

const singlePageApp = `const vm = require('node:vm');
const readline = require('node:readline');
const config = JSON.parse(process.argv[2]);
const failure = new Error('the page refused');
const issued = [];
const later = (ms, value) => new Promise(resolve => setTimeout(() => resolve(value), ms));
const listing = 'Listing: a cabin by the lake';
const observers = [];
class MutationObserver {
  constructor(callback) { this.callback = callback; observers.push(this); }
  observe() {}
  disconnect() { this.off = true; }
}
const page = {url: 'https://www.airbnb.test/s/homes', h1: 'Search results', text: 'results', elements: 10};
const render = changes => {
  Object.assign(page, changes);
  for (const observer of observers) if (!observer.off) observer.callback([]);
};
function fetch(url) {
  const answer = url === '/fail' ? Promise.reject(failure) : url === '/poll' ? new Promise(() => {}) : later(config.contentAfter, {json: () => later(0, {text: listing})});
  issued.push(answer);
  return answer;
}
class XMLHttpRequest {
  open(method, url) { this.url = url; this.listeners = []; }
  addEventListener(type, listener) { this.listeners.push([type, listener]); }
  send() {
    if (this.url === '/throw') throw failure;
    setTimeout(() => {
      this.responseText = JSON.stringify({text: listing});
      for (const type of ['load', 'loadend']) for (const [on, listener] of this.listeners) if (on === type) listener();
    }, config.contentAfter);
  }
}
const document = {readyState: 'complete', title: 'Airbnb', get body() { return {innerText: page.text}; },
  querySelector: selector => selector === 'h1' ? {innerText: page.h1} : null, querySelectorAll: () => [], getElementsByTagName: () => ({length: page.elements})};
const location = {get href() { return page.url; }};
const context = vm.createContext({MutationObserver, performance, setTimeout, fetch, XMLHttpRequest, document, location});
const shape = () => vm.runInContext('({keys: Object.keys(globalThis).sort().join(), names: Object.getOwnPropertyNames(globalThis).sort().join(), symbols: Object.getOwnPropertySymbols(globalThis).map(key => Object.getOwnPropertyDescriptor(globalThis, key).enumerable)})', context);
context.open = () => {
  render({url: 'https://www.airbnb.test/rooms/1', h1: config.h1 || page.h1, text: 'skeleton'});
  if (config.sections) return void config.sections.forEach((at, i) => context.setTimeout(() => render({elements: page.elements + 5, text: page.text + ' section' + i}), at));
  const show = text => setTimeout(() => render({text}), 30);
  if (config.via !== 'xhr') return void context.fetch('/api/listing').then(response => response.json()).then(body => show(body.text));
  const request = new context.XMLHttpRequest();
  request.open('GET', '/api/listing');
  request.addEventListener('load', () => show(JSON.parse(request.responseText).text));
  request.send();
};
context.poll = () => {
  const clock = vm.runInContext('Date', context), now = clock.now;
  clock.now = () => now() - 6000;
  context.fetch('/poll');
  clock.now = now;
};
context.busy = () => void setInterval(() => { context.fetch('/api/listing'); render({text: 'ticker ' + Date.now()}); }, 100);
let bare;
context.audit = async () => {
  const now = shape(), answer = context.fetch('/api/listing');
  let rejected, thrown;
  await context.fetch('/fail').catch(error => { rejected = error === failure; });
  const request = new context.XMLHttpRequest();
  request.open('GET', '/throw');
  try { request.send(); } catch (error) { thrown = error === failure; }
  return {globals: now.keys === bare.keys && now.names === bare.names && now.symbols.length === bare.symbols.length + 1 && !now.symbols.includes(true),
    same: answer === issued.at(-2), rejected, thrown};
};
bare = shape();
readline.createInterface({input: process.stdin}).on('line', async line => {
  let reply;
  try { reply = {value: await vm.runInContext(JSON.parse(line), context)}; } catch (error) { reply = {error: String(error)}; }
  process.stdout.write(JSON.stringify(reply) + '\n');
});
`

type jsPage struct {
	mu  sync.Mutex
	in  io.Writer
	out *bufio.Scanner
}

func startPage(t *testing.T, config string) *jsPage {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on the PATH to run the page")
	}
	script := filepath.Join(t.TempDir(), "page.js")
	if err := os.WriteFile(script, []byte(singlePageApp), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(node, script, config)
	command.Stderr = os.Stderr
	in, _ := command.StdinPipe()
	out, _ := command.StdoutPipe()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	scanner := bufio.NewScanner(out)
	scanner.Buffer(nil, 1<<20)
	return &jsPage{in: in, out: scanner}
}

func (j *jsPage) eval(expression string) (any, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	line, _ := json.Marshal(expression)
	var reply struct {
		Value any    `json:"value"`
		Error string `json:"error"`
	}
	if _, err := j.in.Write(append(line, '\n')); err != nil || !j.out.Scan() || json.Unmarshal(j.out.Bytes(), &reply) != nil {
		return nil, false
	}
	return reply.Value, reply.Error == ""
}

func listingPage(t *testing.T, config string) *togglePage {
	return &togglePage{link: true, quads: googleResult(), js: startPage(t, config)}
}

func TestAURLChangeReadsWhatItsRequestBringsNotTheSkeleton(t *testing.T) {
	for _, via := range []string{"fetch", "xhr"} {
		page := listingPage(t, `{"contentAfter": 900, "h1": "Listing", "via": "`+via+`"}`)
		moved := moveOn(t, MoveClick, page)
		if !strings.Contains(page.read, "cabin") || moved.SettledMS < 900 || !moved.URLChanged {
			t.Fatalf("a url change whose content arrives by %s after 900 ms read %q in %d ms; want the listing after at least 900 ms", via, page.read, moved.SettledMS)
		}
	}
}

func TestAURLChangeWhoseTitleNeverChangesReturnsOnceItsFetchEnds(t *testing.T) {
	page := listingPage(t, `{"contentAfter": 400}`)
	moved := moveOn(t, MoveClick, page)
	if !strings.Contains(page.read, "cabin") || moved.SettledMS >= 1000 {
		t.Fatalf("a url change under a title that never changes, fetched in 400 ms, read %q in %d ms; want the listing under 1000", page.read, moved.SettledMS)
	}
}

func TestAURLChangeWhoseFetchNeverEndsReturnsAtTheActCap(t *testing.T) {
	moved := moveOn(t, MoveClick, listingPage(t, `{"contentAfter": 60000}`))
	if moved.SettledMS < konst.BrowserRenderWaitMaxMillis || moved.SettledMS > konst.BrowserRenderWaitMaxMillis+300 {
		t.Fatalf("a url change whose fetch never ends settled in %d ms; want the %d ms cap from the act start", moved.SettledMS, konst.BrowserRenderWaitMaxMillis)
	}
}

func TestALongPollOpenOverFiveSecondsDoesNotHoldTheSettle(t *testing.T) {
	page := listingPage(t, `{"contentAfter": 400, "h1": "Listing"}`)
	driver := relayTo(t, page)
	moveWith(t, driver, MoveHover, page)
	page.js.eval("poll()")
	moved := moveWith(t, driver, MoveClick, page)
	if !strings.Contains(page.read, "cabin") || moved.SettledMS >= 1000 || len(page.registered) != 1 {
		t.Fatalf("beside a long-poll open 6 s the click read %q in %d ms, the counter registered %d times; want the listing under 1000 ms, registered once", page.read, moved.SettledMS, len(page.registered))
	}
}

func TestTheRequestCounterAddsNoEnumerableGlobalAndChangesNoAnswer(t *testing.T) {
	page := listingPage(t, `{"contentAfter": 100}`)
	moveOn(t, MoveHover, page)
	audit, _ := page.js.eval("audit()")
	if want := map[string]any{"globals": true, "same": true, "rejected": true, "thrown": true}; !reflect.DeepEqual(audit, want) {
		t.Fatalf("after the counter went in the page audit says %v; want %v", audit, want)
	}
}

func TestATimedWaitOnABusyPageReturnsWhenItsSleepEnds(t *testing.T) {
	page := listingPage(t, `{"contentAfter": 400}`)
	driver := relayTo(t, page)
	page.js.eval("busy()")
	begun := time.Now()
	if _, err := driver.Do(Move{Kind: MoveWait, Value: "1500"}); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(begun); took >= 1600*time.Millisecond {
		t.Fatalf("wait 1500 on a page always fetching took %d ms; want under 1600", took.Milliseconds())
	}
}

func TestAURLChangeWaitsForSectionsThatMountWithoutNetwork(t *testing.T) {
	page := listingPage(t, `{"sections": [800, 1400]}`)
	moved := moveOn(t, MoveClick, page)
	if !strings.Contains(page.read, "section0") || !strings.Contains(page.read, "section1") || moved.SettledMS < 1400 {
		t.Fatalf("sections mounting at 800 and 1400 ms read %q in %d ms; want both, after 1400", page.read, moved.SettledMS)
	}
}

func TestAURLChangeThatStopsGrowingAt300MsReturnsUnderASecond(t *testing.T) {
	page := listingPage(t, `{"sections": [100, 200, 300]}`)
	moved := moveOn(t, MoveClick, page)
	if !strings.Contains(page.read, "section2") || moved.SettledMS >= 1000 {
		t.Fatalf("a page that stops growing at 300 ms read %q in %d ms; want every section under 1000", page.read, moved.SettledMS)
	}
}

func TestAClickThatChangesNothingSettlesUnder50Ms(t *testing.T) {
	page := &togglePage{js: startPage(t, `{}`)}
	driver := relayTo(t, page)
	moveWith(t, driver, MoveHover, page)
	if moved := moveWith(t, driver, MoveClick, page); moved.SettledMS >= 50 {
		t.Fatalf("a click that changed nothing settled in %d ms; want under 50", moved.SettledMS)
	}
}
