package browser

import (
	"encoding/json"
	"net"
	"strconv"
	"strings"
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
	onParent       int
	navigated      time.Time
	titleAfter     time.Duration
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
	if !p.navigated.IsZero() && time.Since(p.navigated) >= p.titleAfter {
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
		if params["type"] == "mouseReleased" {
			p.pressed = [2]float64{x, y}
			if p.inQuad(x, y) {
				p.navigated = time.Now()
			} else {
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
	case strings.HasPrefix(expression, "document.title"):
		return byValue(p.heading())
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

func clickOn(t *testing.T, page *togglePage) Moved {
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
			if req.Op == opCDP {
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
	driver := &Driver{Client: &Client{conn: ours, out: json.NewEncoder(ours), in: json.NewDecoder(ours)}, Tab: 7,
		refs: refMap{entries: map[string]refEntry{"e25": {backend: 25, role: role}}}}
	moved, err := driver.Do(Move{Ref: "e25", Kind: MoveClick})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("1. click %s: %s", role, moved)
	return moved
}

func TestACheckboxDeafToTheMouseEndsCheckedThroughClickInOneAct(t *testing.T) {
	page := &togglePage{checkbox: true, mouseRerenders: true}
	moved := clickOn(t, page)
	if !page.checked || moved.Via != "click()" {
		t.Fatalf("after one click the box is checked=%v through %q; want checked through click()", page.checked, moved.Via)
	}
}

func TestACheckboxTheMouseChecksIsNotClickedAgain(t *testing.T) {
	page := &togglePage{checkbox: true, mouseChecks: true}
	moved := clickOn(t, page)
	if !page.checked || moved.Via != "" {
		t.Fatalf("after one click the box is checked=%v through %q; want checked by the mouse alone", page.checked, moved.Via)
	}
}

func TestAClickThatChangesNothingSaysThePageDidNotChange(t *testing.T) {
	if said := clickOn(t, &togglePage{}).String(); !strings.HasPrefix(said, Unchanged) {
		t.Fatalf("a click that changed nothing said %q; want it to start %q", said, Unchanged)
	}
}

func TestALinkWhoseBoxCentreHitsAParentIsPressedInItsLargestQuad(t *testing.T) {
	page := &togglePage{link: true, quads: googleResult()}
	moved := clickOn(t, page)
	if page.pressed != [2]float64{60, 10} || page.onParent != 0 || !moved.URLChanged || moved.Via != "" {
		t.Fatalf("the press landed at %v, %d on the parent, url changed %v through %q; want (60, 10) on the link by the mouse", page.pressed, page.onParent, moved.URLChanged, moved.Via)
	}
}

func TestAPressLandingOnAParentFallsBackToClickInOneAct(t *testing.T) {
	page := &togglePage{link: true}
	moved := clickOn(t, page)
	if page.onParent != 0 || !moved.URLChanged || moved.Via != "click()" {
		t.Fatalf("%d presses on the parent, url changed %v through %q; want none and the link opened through click()", page.onParent, moved.URLChanged, moved.Via)
	}
}

func TestAURLChangeWaitsForTheNewTitleAndAStuckTitleReturnsAtTheCap(t *testing.T) {
	moved := clickOn(t, &togglePage{link: true, quads: googleResult(), titleAfter: 800 * time.Millisecond})
	if said := moved.String(); !strings.Contains(said, `"Listing"`) || moved.SettledMS < 800 {
		t.Fatalf("a title that changes 800 ms after the url said %q; want the new title after at least 800 ms", said)
	}
	moved = clickOn(t, &togglePage{link: true, quads: googleResult(), titleAfter: time.Hour})
	if moved.SettledMS < konst.BrowserRenderWaitMaxMillis || moved.SettledMS > konst.BrowserRenderWaitMaxMillis+500 {
		t.Fatalf("a title that never changes settled in %d ms; want the %d ms cap", moved.SettledMS, konst.BrowserRenderWaitMaxMillis)
	}
}
