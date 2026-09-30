package browser

import (
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"testing"
)

type togglePage struct {
	checkbox       bool
	checked        bool
	mouseChecks    bool
	mouseRerenders bool
	rerenders      int
	cursorPainted  bool
}

func (p *togglePage) answer(method string, params map[string]any) any {
	script, _ := params["functionDeclaration"].(string)
	expression, _ := params["expression"].(string)
	switch {
	case method == "DOM.scrollIntoViewIfNeeded":
		return map[string]any{}
	case method == "DOM.getBoxModel":
		return map[string]any{"model": map[string]any{"content": []float64{20, 20, 33, 20, 33, 33, 20, 33}}}
	case method == "DOM.resolveNode":
		return map[string]any{"object": map[string]any{"objectId": "target"}}
	case method == "Input.dispatchMouseEvent":
		p.cursorPainted = true
		if params["type"] == "mouseReleased" && p.mouseRerenders {
			p.rerenders++
		}
		if params["type"] == "mouseReleased" && p.mouseChecks {
			p.checked = !p.checked
		}
		return map[string]any{}
	case script == blockerAt:
		return map[string]any{"result": map[string]any{"type": "object", "subtype": "null", "value": nil}}
	case script == submitsOrLinks:
		return byValue(false)
	case strings.Contains(script, "this.click()"):
		p.checked = p.checked != p.checkbox
		return byValue(nil)
	case strings.Contains(script, "aria-checked") && p.checkbox:
		return byValue(strconv.FormatBool(p.checked))
	case strings.Contains(script, "aria-checked"):
		return byValue(nil)
	case strings.Contains(expression, "MutationObserver"):
		return byValue(0)
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
	driver := &Driver{Client: &Client{conn: ours, out: json.NewEncoder(ours), in: json.NewDecoder(ours)}, Tab: 7,
		refs: refMap{entries: map[string]refEntry{"e25": {backend: 25, role: "checkbox"}}}}
	moved, err := driver.Do(Move{Ref: "e25", Kind: MoveClick})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("1. click checkbox: %s", moved)
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
