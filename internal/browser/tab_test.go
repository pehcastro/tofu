package browser_test

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"tofu/internal/browser"
	"tofu/internal/browser/jevloop"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/question"
	"tofu/library/questions"
)

const fakeOrigin = "chrome-extension://tofutabtest/"

type extensionCall struct {
	T     string          `json:"t"`
	ID    int64           `json:"id"`
	TabID int             `json:"tabId"`
	Op    string          `json:"op"`
	Args  json.RawMessage `json:"args"`
}

type fakeExtension struct {
	mu     sync.Mutex
	calls  []string
	answer func(extensionCall) string
}

func (f *fakeExtension) serve(fromHost io.Reader, toHost io.Writer) {
	for {
		raw, err := browser.ReadMessage(fromHost)
		if err != nil {
			return
		}
		var call extensionCall
		if json.Unmarshal(raw, &call) != nil || call.T != "call" {
			continue
		}
		f.mu.Lock()
		f.calls = append(f.calls, fmt.Sprintf("tab %d %s %s", call.TabID, call.Op, string(call.Args)))
		f.mu.Unlock()
		if browser.WriteMessage(toHost, fmt.Appendf(nil, `{"t":"result","id":%d,%s}`, call.ID, f.answer(call))) != nil {
			return
		}
	}
}

func (f *fakeExtension) sawOnly(t *testing.T, want ...string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Equal(f.calls, want) {
		t.Fatalf("the extension saw\n%s\nwant\n%s", strings.Join(f.calls, "\n"), strings.Join(want, "\n"))
	}
}

func sharedTab(t *testing.T, answer func(extensionCall) string) (browser.SharedTab, *fakeExtension) {
	t.Helper()
	home, err := os.MkdirTemp("", "tb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	manifest := filepath.Join(filepath.Dir(browser.ExtensionDir(home)), browser.HostName+".json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(`{"allowed_origins":["`+fakeOrigin+`"]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	toHostR, toHostW := io.Pipe()
	fromHostR, fromHostW := io.Pipe()
	hostDone := make(chan error, 1)
	go func() {
		hostDone <- browser.Host(fakeOrigin, toHostR, fromHostW, home)
		_ = fromHostW.Close()
	}()
	t.Cleanup(func() {
		_ = toHostW.Close()
		_ = fromHostR.Close()
		select {
		case <-hostDone:
		case <-time.After(5 * time.Second):
			t.Error("the host did not stop")
		}
	})
	hello := `{"t":"hello","version":2,"tabs":[{"id":7,"url":"http://127.0.0.1:8000/fixture.html","title":"Forma"}]}`
	if err := browser.WriteMessage(toHostW, []byte(hello)); err != nil {
		t.Fatalf("the host did not read hello: %v, %v", err, <-hostDone)
	}
	ext := &fakeExtension{answer: answer}
	go ext.serve(fromHostR, toHostW)

	client, err := browser.Dial(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		tabs, err := client.Tabs()
		if err == nil && len(tabs) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the host never listed tab 7: %v, %v", tabs, err)
		}
	}
	return browser.SharedTab{Client: client, ID: 7}, ext
}

func recordedSnapshot(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("jevloop/testdata/hotel.json")
	if err != nil {
		t.Fatal(err)
	}
	return `"ok":true,"value":` + string(raw)
}

func TestSnapshotParsesTheFixtureWithoutPasswordOrHiddenInputs(t *testing.T) {
	snapshot := recordedSnapshot(t)
	tab, ext := sharedTab(t, func(extensionCall) string { return snapshot })

	page, err := tab.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ext.sawOnly(t, "tab 7 snapshot ")
	var selects, typeable []string
	for _, element := range page.Elements {
		if element.Input == "password" || element.Input == "hidden" || element.Label == "Account password" || element.Label == "csrf" {
			t.Fatalf("the page offers %+v", element)
		}
		if element.Role == browser.RoleSelect && len(element.Options) == 3 {
			selects = append(selects, element.Label)
		}
		if browser.OpTypeText.Accepts(element.Role) {
			typeable = append(typeable, element.Label)
		}
	}
	if !slices.Equal(selects, []string{"Stay category"}) || !slices.Equal(typeable, []string{"Destination"}) || page.Fingerprint != "hotel-0" {
		t.Fatalf("selects %q, typeable %q, fingerprint %q; want the category select, the destination and hotel-0", selects, typeable, page.Fingerprint)
	}
}

func TestSnapshotRefusesAnAnswerThatIsNotAPage(t *testing.T) {
	tab, _ := sharedTab(t, func(extensionCall) string { return `"ok":true,"value":{"url":"https://a.test/"}` })
	if page, err := tab.Snapshot(context.Background()); err == nil {
		t.Fatalf("a snapshot without a fingerprint parsed into %+v", page)
	}
}

func TestActSendsTheTargetGuardAndReadsTheStaleKind(t *testing.T) {
	answers := map[string]string{
		"select": `"ok":true,"value":{"stale":"covered"}`,
		"fill":   `"ok":false,"error":"element 4 is read-only"`,
		"scroll": `"ok":true,"value":{}`,
		"click":  `"ok":true,"value":{}`,
		"wait":   `"ok":true,"value":{"stale":"moved"}`,
	}
	tab, ext := sharedTab(t, func(call extensionCall) string { return answers[call.Op] })
	page := browser.Page{Fingerprint: "hotel-0", Guards: map[int]string{5: "g5", 6: "g6"}}
	ctx := context.Background()

	if stale, err := tab.Act(ctx, page, browser.Action{Op: browser.OpSelect, Element: 6, Value: "Design"}); stale != browser.StaleCovered || err != nil {
		t.Fatalf("Act on a covered target is %q, %v; want covered and no error", stale, err)
	}
	if _, err := tab.Act(ctx, page, browser.Action{Op: browser.OpTypeText, Element: 4, Value: "Lisbon"}); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("Act on a read-only field is %v; want the error", err)
	}
	for _, action := range []browser.Action{{Op: browser.OpScrollUp}, {Op: browser.OpScrollDown}, {Op: browser.OpClick, Element: 5}} {
		if stale, err := tab.Act(ctx, page, action); stale != browser.StaleNone || err != nil {
			t.Fatalf("Act %s is %q, %v", action.Op, stale, err)
		}
	}
	if _, err := tab.Act(ctx, page, browser.Action{Op: browser.OpWait}); err == nil || !strings.Contains(err.Error(), "moved") {
		t.Fatalf("an unknown stale kind answered %v, want an error naming it", err)
	}
	for _, op := range []browser.Op{browser.OpDone, browser.OpBlocked} {
		if _, err := tab.Act(ctx, page, browser.Action{Op: op}); err == nil {
			t.Fatalf("Act %s reached the tab", op)
		}
	}

	ext.sawOnly(t,
		`tab 7 select {"element":6,"guard":"g6","value":"Design"}`,
		`tab 7 fill {"element":4,"value":"Lisbon"}`,
		`tab 7 scroll {"direction":"up"}`,
		`tab 7 scroll {"direction":"down"}`,
		`tab 7 click {"element":5,"guard":"g5"}`,
		`tab 7 wait {}`,
	)
}

type recordedWire struct{ answer []byte }

func (w recordedWire) Caps() jev.WireCaps {
	return jev.WireCaps{Name: "recorded", CriteriaKinds: []jev.CriteriaKind{jev.CriteriaString, jev.CriteriaObject}}
}

func (w recordedWire) Model() string { return "~typesafe/jev-latest" }

func (w recordedWire) Post(context.Context, []byte) (jev.Raw, error) {
	return jev.Raw{Body: w.answer, Attempts: 1, Latency: time.Millisecond}, nil
}

func TestBrowserRunsOneJevStepOnTheSharedTab(t *testing.T) {
	answer, err := os.ReadFile("jevloop/testdata/hotel_answer.json")
	if err != nil {
		t.Fatal(err)
	}
	set, _, err := question.Resolve("browser_step@1", []question.Layer{{Name: "library", Origin: "library/questions", FS: questions.Files()}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := jev.NewClient(jev.Config{Wire: recordedWire{answer}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := recordedSnapshot(t)
	tab, ext := sharedTab(t, func(call extensionCall) string {
		if call.Op == "snapshot" {
			return snapshot
		}
		return `"ok":true,"value":{}`
	})

	loop := jevloop.Loop{
		Browser: jevloop.Browser{Snapshot: tab.Snapshot, Act: tab.Act},
		Choose:  jevloop.Jev{Client: client, Set: set}.Choose,
		Actions: 1,
	}
	result := loop.Run(context.Background(), "show only design stays")

	ext.sawOnly(t,
		"tab 7 snapshot ",
		`tab 7 select {"element":6,"value":"Design"}`,
		"tab 7 snapshot ",
	)
	if len(result.Steps) != 1 || result.Steps[0].Action != (browser.Action{Op: browser.OpSelect, Element: 6, Value: "Design"}) || result.Steps[0].Stale != browser.StaleNone {
		t.Fatalf("the loop recorded %+v", result.Steps)
	}
	if result.Status != jevloop.StatusBlocked || !strings.Contains(result.Reason, "action budget of 1") {
		t.Fatalf("the loop stopped %v with %q, want the action budget", result.Status, result.Reason)
	}
}

func scripted(actions ...browser.Action) jevloop.Chooser {
	return func(context.Context, string, browser.Page, []jevloop.Step) (jevloop.Choice, error) {
		action := actions[0]
		actions = actions[min(1, len(actions)-1):]
		return jevloop.Choice{Action: action}, nil
	}
}

func TestACoveredTargetStopsBlockedAfterThreeDecisions(t *testing.T) {
	snapshot := recordedSnapshot(t)
	tab, _ := sharedTab(t, func(call extensionCall) string {
		if call.Op == "snapshot" {
			return snapshot
		}
		return `"ok":true,"value":{"stale":"covered"}`
	})
	result := jevloop.Loop{
		Browser: jevloop.Browser{Snapshot: tab.Snapshot, Act: tab.Act},
		Choose:  scripted(browser.Action{Op: browser.OpClick, Element: 5}),
		Actions: 30,
	}.Run(context.Background(), "find stays")
	t.Logf("stopped %v after %d decisions: %s", result.Status, result.Decisions, result.Reason)
	if result.Status != jevloop.StatusBlocked || result.Decisions != 3 || !strings.Contains(result.Reason, "covered") {
		t.Fatalf("stopped %v after %d decisions with %q, want blocked after 3 naming covered", result.Status, result.Decisions, result.Reason)
	}
}

type fakeNode struct {
	ID         int       `json:"id"`
	Role       string    `json:"role"`
	Name       string    `json:"name"`
	Children   []int     `json:"children"`
	Box        []float64 `json:"box"`
	In         int       `json:"in"`
	Z          int       `json:"z"`
	Cursor     bool      `json:"cursor"`
	Text       string    `json:"text"`
	Scrollable float64   `json:"scrollable"`
	Fires      string    `json:"fires"`
	Opens      int       `json:"opens"`
	Href       string    `json:"href"`
	Modal      bool      `json:"modal"`
	Hidden     bool      `json:"hidden"`
	Toggles    int       `json:"toggles"`
	OpensURL   string    `json:"opensURL"`
	Ignored    bool      `json:"ignored"`
	Passive    bool      `json:"passive"`
	Value      string    `json:"value"`
	Stubborn   bool      `json:"ignoresInsert"`
	KeyDeaf    bool      `json:"ignoresKeys"`
}

type fakePage struct {
	mu       sync.Mutex
	URL      string      `json:"url"`
	Title    string      `json:"title"`
	Loader   string      `json:"loader"`
	Nodes    []*fakeNode `json:"nodes"`
	Ours     bool        `json:"ours"`
	Deaf     bool        `json:"deaf"`
	Status   int         `json:"status"`
	focused  int
	scrolled map[int]float64
	tagged   []int
	fired    []string
	methods  []string
	tabOps   []string
	points   []string
}

func loadPage(t *testing.T, name string) *fakePage {
	t.Helper()
	raw, err := os.ReadFile("extension/testdata/pages/" + name + ".json")
	page := &fakePage{scrolled: map[int]float64{}}
	if err == nil {
		err = json.Unmarshal(raw, page)
	}
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func (p *fakePage) node(id int) *fakeNode {
	for _, n := range p.Nodes {
		if n.ID == id {
			return n
		}
	}
	return nil
}

func (p *fakePage) parent(id int) *fakeNode {
	for _, n := range p.Nodes {
		if slices.Contains(n.Children, id) {
			return n
		}
	}
	return nil
}

func (p *fakePage) related(a, b *fakeNode) bool {
	for _, pair := range [][2]*fakeNode{{a, b}, {b, a}} {
		for n := pair[0]; n != nil; n = p.parent(n.ID) {
			if n == pair[1] {
				return true
			}
		}
	}
	return false
}

func (p *fakePage) rect(n *fakeNode) (x, top, w, bottom float64) {
	x, top, w, bottom = n.Box[0], n.Box[1], n.Box[2], n.Box[1]+n.Box[3]
	if container := p.node(n.In); container != nil {
		top, bottom = top-p.scrolled[n.In], bottom-p.scrolled[n.In]
		top, bottom = max(top, container.Box[1]), min(bottom, container.Box[1]+container.Box[3])
	}
	return x, top, w, bottom
}

func (p *fakePage) shown(n *fakeNode) bool {
	for ; n != nil; n = p.parent(n.ID) {
		if n.Hidden {
			return false
		}
	}
	return true
}

func (p *fakePage) href(n *fakeNode) string {
	for ; n != nil; n = p.parent(n.ID) {
		if n.Href != "" {
			return n.Href
		}
	}
	return ""
}

func (p *fakePage) hit(x, y float64) *fakeNode {
	var hit *fakeNode
	for _, n := range p.Nodes {
		if len(n.Box) != 4 || !p.shown(n) || n.Passive {
			continue
		}
		left, top, w, bottom := p.rect(n)
		if x >= left && x <= left+w && y >= top && y <= bottom && (hit == nil || n.Z >= hit.Z) {
			hit = n
		}
	}
	return hit
}

func (p *fakePage) rerender() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, n := range p.Nodes {
		n.ID += 100
		for i := range n.Children {
			n.Children[i] += 100
		}
	}
}

func (p *fakePage) axNode(n *fakeNode) map[string]any {
	ids := []string{}
	for _, child := range n.Children {
		ids = append(ids, strconv.Itoa(child))
	}
	node := map[string]any{"nodeId": strconv.Itoa(n.ID), "ignored": n.Ignored, "backendDOMNodeId": n.ID, "childIds": ids,
		"role": map[string]any{"type": "role", "value": n.Role}, "name": map[string]any{"type": "computedString", "value": n.Name},
		"properties": []any{map[string]any{"name": "modal", "value": map[string]any{"type": "boolean", "value": n.Modal}}}}
	if parent := p.parent(n.ID); parent != nil {
		node["parentId"] = strconv.Itoa(parent.ID)
	}
	return node
}

func (p *fakePage) byObject(params map[string]any) *fakeNode {
	id, _ := strconv.Atoi(strings.TrimPrefix(fmt.Sprint(params["objectId"]), "node-"))
	return p.node(id)
}

func (p *fakePage) byBackend(params map[string]any) (*fakeNode, error) {
	backend, _ := params["backendNodeId"].(float64)
	if n := p.node(int(backend)); n != nil {
		return n, nil
	}
	return nil, fmt.Errorf("No node with given id found")
}

func value(v any) map[string]any {
	return map[string]any{"result": map[string]any{"type": "object", "value": v}}
}

func (p *fakePage) cdp(method string, params map[string]any, opened func(int, string)) (any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.methods = append(p.methods, method)
	script := fmt.Sprint(params["expression"], params["functionDeclaration"])
	switch method {
	case "Page.getFrameTree":
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "main", "loaderId": p.Loader, "url": p.URL}}}, nil
	case "Accessibility.getFullAXTree":
		nodes := []any{}
		for _, n := range p.Nodes {
			if p.shown(n) {
				nodes = append(nodes, p.axNode(n))
			}
		}
		return map[string]any{"nodes": nodes}, nil
	case "Accessibility.getPartialAXTree":
		return map[string]any{"nodes": []any{p.axNode(p.byObject(params))}}, nil
	case "DOM.getDocument":
		return map[string]any{"root": map[string]any{"nodeId": 1000}}, nil
	case "DOM.querySelectorAll":
		ids := []int{}
		for _, id := range p.tagged {
			ids = append(ids, id+1000)
		}
		return map[string]any{"nodeIds": ids}, nil
	case "DOM.describeNode":
		if n := p.byObject(params); n != nil {
			return map[string]any{"node": map[string]any{"backendNodeId": n.ID}}, nil
		}
		id, _ := params["nodeId"].(float64)
		index := slices.Index(p.tagged, int(id)-1000)
		return map[string]any{"node": map[string]any{"backendNodeId": int(id) - 1000, "attributes": []string{"data-tofu-ci", strconv.Itoa(index)}}}, nil
	case "DOM.scrollIntoViewIfNeeded", "DOM.getBoxModel", "DOM.resolveNode":
		n, err := p.byBackend(params)
		if err != nil {
			return nil, err
		}
		if method == "DOM.resolveNode" {
			return map[string]any{"object": map[string]any{"objectId": "node-" + strconv.Itoa(n.ID)}}, nil
		}
		if _, top, _, bottom := p.rect(n); method == "DOM.scrollIntoViewIfNeeded" && n.In != 0 && bottom <= top {
			p.scrolled[n.In] = n.Box[1] - p.node(n.In).Box[1]
		}
		if method == "DOM.scrollIntoViewIfNeeded" {
			return map[string]any{}, nil
		}
		x, y := n.Box[0], n.Box[1]-p.scrolled[n.In]
		w, h := n.Box[2], n.Box[3]
		return map[string]any{"model": map[string]any{"content": []float64{x, y, x + w, y, x + w, y + h, x, y + h}}}, nil
	case "Runtime.evaluate":
		switch {
		case strings.Contains(script, "querySelectorAll('*')"):
			found := []any{}
			p.tagged = nil
			for _, n := range p.Nodes {
				if n.Cursor || n.Scrollable > 0 {
					p.tagged = append(p.tagged, n.ID)
					found = append(found, map[string]any{"text": n.Text, "hasCursorPointer": n.Cursor, "isScrollable": n.Scrollable > 0})
				}
			}
			return value(found), nil
		case strings.Contains(script, "removeAttribute"):
			return value(len(p.tagged)), nil
		case strings.Contains(script, "requestSubmit"):
			p.fired = append(p.fired, "submit")
			return value(true), nil
		case strings.Contains(script, "getEntriesByType"):
			return value(map[string]any{"pending": 0, "loading": false}), nil
		case strings.Contains(script, "innerText"):
			return value(map[string]any{"url": p.URL, "count": len(p.Nodes), "text": strings.Join(p.fired, ","), "status": p.Status, "heading": p.Title}), nil
		}
	case "Runtime.callFunctionOn":
		target := p.byObject(params)
		args, _ := params["arguments"].([]any)
		number := func(i int) float64 { return args[i].(map[string]any)["value"].(float64) }
		switch {
		case strings.Contains(script, "this.focus()"):
			p.focused = target.ID
			return value(nil), nil
		case strings.Contains(script, "this.value = ''"):
			target.Value = ""
			return value(nil), nil
		case strings.Contains(script, "getOwnPropertyDescriptor"):
			target.Value = fmt.Sprint(args[0].(map[string]any)["value"])
			return value(nil), nil
		case strings.Contains(script, "return this.value"):
			return value(target.Value), nil
		case strings.Contains(script, "this.click()"):
			for n := target; n != nil; n = p.parent(n.ID) {
				if n.Fires != "" {
					p.fired = append(p.fired, n.Fires)
					break
				}
			}
			return value(nil), nil
		case strings.Contains(script, "closest('a[href]')"):
			return value(p.href(target)), nil
		case strings.Contains(script, "this.href"):
			return value(target.Href), nil
		case strings.Contains(script, "elementFromPoint"):
			if hit := p.hit(number(0), number(1)); hit != nil && !p.related(hit, target) {
				return map[string]any{"result": map[string]any{"type": "object", "objectId": "node-" + strconv.Itoa(hit.ID)}}, nil
			}
			return map[string]any{"result": map[string]any{"type": "object", "subtype": "null", "value": nil}}, nil
		case strings.Contains(script, "scrollBy"):
			p.scrolled[target.ID] = min(max(p.scrolled[target.ID]+number(1), 0), target.Scrollable-target.Box[3])
			return value(nil), nil
		}
	case "Input.insertText":
		if n := p.node(p.focused); n != nil && !n.Stubborn {
			n.Value += fmt.Sprint(params["text"])
		}
		return map[string]any{}, nil
	case "Input.dispatchKeyEvent":
		if text, typed := params["text"].(string); typed && params["type"] == "keyDown" && text != "\r" && p.node(p.focused) != nil && !p.node(p.focused).KeyDeaf {
			p.node(p.focused).Value += text
		}
		return map[string]any{}, nil
	case "Input.dispatchMouseEvent":
		if params["type"] != "mouseReleased" || p.Deaf {
			return map[string]any{}, nil
		}
		for n := p.hit(params["x"].(float64), params["y"].(float64)); n != nil; n = p.parent(n.ID) {
			if n.Fires != "" {
				p.fired = append(p.fired, n.Fires)
				if n.Opens != 0 {
					opened(n.Opens, n.OpensURL)
				}
				if toggled := p.node(n.Toggles); toggled != nil {
					toggled.Hidden = !toggled.Hidden
				}
				break
			}
		}
		return map[string]any{}, nil
	}
	return nil, fmt.Errorf("the fake page has no %s for %.60s", method, script)
}

func (p *fakePage) sawFired(t *testing.T, want ...string) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if !slices.Equal(p.fired, want) {
		t.Fatalf("the page fired %q; want %q", p.fired, want)
	}
}

func drivenPage(t *testing.T, name string) (*browser.Driver, *fakePage) {
	t.Helper()
	page := loadPage(t, name)
	home, err := os.MkdirTemp("", "tb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	manifest := filepath.Join(filepath.Dir(browser.ExtensionDir(home)), browser.HostName+".json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(`{"allowed_origins":["`+fakeOrigin+`"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	toHostR, toHostW := io.Pipe()
	fromHostR, fromHostW := io.Pipe()
	go func() {
		_ = browser.Host(fakeOrigin, toHostR, fromHostW, home)
		_ = fromHostW.Close()
	}()
	t.Cleanup(func() {
		_ = toHostW.Close()
		_ = fromHostR.Close()
	})
	hello, _ := json.Marshal(map[string]any{"t": "hello", "version": 2, "tabs": []any{map[string]any{"id": 7, "url": cmp.Or(page.URL, "https://stays.test/"), "title": "Stays", "opened": page.Ours}}})
	if err := browser.WriteMessage(toHostW, hello); err != nil {
		t.Fatal(err)
	}
	go func() {
		opened := func(tab int, url string) {
			update, _ := json.Marshal(map[string]any{"t": "tabUpdated", "tab": map[string]any{"id": tab, "url": cmp.Or(url, "https://stays.test/listing"), "title": "Listing", "opened": page.Ours}})
			_ = browser.WriteMessage(toHostW, update)
		}
		for {
			raw, err := browser.ReadMessage(fromHostR)
			if err != nil {
				return
			}
			var call struct {
				T     string `json:"t"`
				ID    int64  `json:"id"`
				TabID int    `json:"tabId"`
				Op    string `json:"op"`
				Args  struct {
					URL   string `json:"url"`
					Calls []struct {
						Method string         `json:"method"`
						Params map[string]any `json:"params"`
					} `json:"calls"`
					Point *struct {
						X     float64 `json:"x"`
						Y     float64 `json:"y"`
						Label string  `json:"label"`
					} `json:"point"`
				} `json:"args"`
			}
			if json.Unmarshal(raw, &call) != nil || call.T != "call" {
				continue
			}
			if point := call.Args.Point; point != nil {
				page.mu.Lock()
				page.points = append(page.points, fmt.Sprintf("%g,%g %s", point.X, point.Y, point.Label))
				page.mu.Unlock()
			}
			if call.Op != "cdp" {
				page.mu.Lock()
				page.tabOps = append(page.tabOps, fmt.Sprintf("%s %d %s", call.Op, call.TabID, call.Args.URL))
				if call.Op == "navigate" {
					page.URL = call.Args.URL
				}
				page.mu.Unlock()
				if call.Op == "close" {
					_ = browser.WriteMessage(toHostW, fmt.Appendf(nil, `{"t":"tabRemoved","tabId":%d}`, call.TabID))
				}
				value := call.TabID
				if call.Op == "open" {
					value = 30
					update, _ := json.Marshal(map[string]any{"t": "tabUpdated", "tab": map[string]any{"id": value, "url": call.Args.URL, "title": "Opened", "opened": true}})
					_ = browser.WriteMessage(toHostW, update)
				}
				if browser.WriteMessage(toHostW, fmt.Appendf(nil, `{"t":"result","id":%d,"ok":true,"value":%d}`, call.ID, value)) != nil {
					return
				}
				continue
			}
			answers := []any{}
			for _, command := range call.Args.Calls {
				result, err := page.cdp(command.Method, command.Params, opened)
				if err != nil {
					answers = append(answers, map[string]any{"error": err.Error()})
					continue
				}
				answers = append(answers, map[string]any{"result": result})
			}
			answer, _ := json.Marshal(map[string]any{"t": "result", "id": call.ID, "ok": call.Op == "cdp", "value": answers, "error": "the fake page speaks only cdp"})
			if browser.WriteMessage(toHostW, answer) != nil {
				return
			}
		}
	}()
	client, err := browser.Dial(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		if tabs, err := client.Tabs(); err == nil && len(tabs) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the host never listed tab 7")
		}
	}
	return &browser.Driver{Client: client, Tab: 7}, page
}

func refOf(t *testing.T, snapshot, role, name string) string {
	t.Helper()
	match := regexp.MustCompile(`(?m)^\s*[-*] ` + regexp.QuoteMeta(role) + ` "` + regexp.QuoteMeta(name) + `" \[(?:[^\]]*, )?ref=(e\d+)`).FindStringSubmatch(snapshot)
	if match == nil {
		t.Fatalf("no %s %q with a ref in\n%s", role, name, snapshot)
	}
	return match[1]
}

func observe(t *testing.T, driver *browser.Driver, interactive bool) string {
	t.Helper()
	snapshot, err := driver.Observe(interactive)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("observed\n%s", snapshot)
	return snapshot
}

func TestADivWithOnlyAPointerCursorHasARefAndItsClickFires(t *testing.T) {
	driver, page := drivenPage(t, "date")
	snapshot := observe(t, driver, true)
	if !strings.Contains(snapshot, "tab 7") || !strings.Contains(snapshot, "https://stays.test/") || strings.Contains(snapshot, "StaticText") {
		t.Fatalf("the interactive snapshot has no header or carries plain text:\n%s", snapshot)
	}
	moved, err := driver.Do(browser.Move{Ref: refOf(t, snapshot, "generic", "12"), Kind: browser.MoveClick})
	if err != nil || moved.Covered != "" {
		t.Fatalf("the click on day 12 returned %+v, %v", moved, err)
	}
	page.sawFired(t, "day 12")
	if !moved.PageChanged || moved.URLChanged || moved.Opened != 0 {
		t.Fatalf("the click reported %+v; want the page changed and nothing else", moved)
	}
}

func TestAStickyFooterCoversThePlusUntilTheDialogScrolls(t *testing.T) {
	driver, page := drivenPage(t, "dialog")
	snapshot := observe(t, driver, false)
	plus, dialog := refOf(t, snapshot, "button", "+"), refOf(t, snapshot, "dialog", "Filtros")
	if !regexp.MustCompile(`dialog "Filtros" \[ref=` + dialog + `\].* scrollable`).MatchString(snapshot) {
		t.Fatalf("the dialog is not marked scrollable:\n%s", snapshot)
	}

	moved, err := driver.Do(browser.Move{Ref: plus, Kind: browser.MoveClick})
	if err != nil || moved.Covered != `button "Mostrar 1.000 lugares"` {
		t.Fatalf("the first click on + returned %+v, %v; want covered by the footer button", moved, err)
	}
	page.sawFired(t)
	if _, err := driver.Do(browser.Move{Ref: dialog, Kind: browser.MoveScroll, Value: "down"}); err != nil {
		t.Fatal(err)
	}
	moved, err = driver.Do(browser.Move{Ref: plus, Kind: browser.MoveClick})
	if err != nil || moved.Covered != "" {
		t.Fatalf("the click on + after the dialog scrolled returned %+v, %v", moved, err)
	}
	page.sawFired(t, "rooms +1")
}

func TestARefTakenBeforeAReRenderClicksTheSameNthElement(t *testing.T) {
	driver, page := drivenPage(t, "rerender")
	snapshot := observe(t, driver, true)
	refs := regexp.MustCompile(`button "Adicionar" \[[^\]]*ref=(e\d+)`).FindAllStringSubmatch(snapshot, -1)
	if len(refs) != 3 {
		t.Fatalf("want three Adicionar refs in\n%s", snapshot)
	}
	page.rerender()
	if moved, err := driver.Do(browser.Move{Ref: refs[1][1], Kind: browser.MoveClick}); err != nil || moved.Covered != "" {
		t.Fatalf("the click on the second Adicionar after a re-render returned %+v, %v", moved, err)
	}
	page.sawFired(t, "add 2")
	again := observe(t, driver, true)
	if again == snapshot || strings.Contains(again, "* button") == false {
		t.Fatalf("after a re-render in the same document the new nodes are not marked new:\n%s", again)
	}
}

func TestAClickThatOpensATabReturnsItAndDrivesItNext(t *testing.T) {
	driver, page := drivenPage(t, "newtab")
	snapshot := observe(t, driver, true)
	moved, err := driver.Do(browser.Move{Ref: refOf(t, snapshot, "link", "Casa em Lisboa"), Kind: browser.MoveClick})
	if err != nil || moved.Opened != 21 || driver.Tab != 21 {
		t.Fatalf("the click returned %+v, %v, and the driver is on tab %d; want tab 21 opened and driven", moved, err, driver.Tab)
	}
	page.sawFired(t, "listing")
}

func TestAModalDialogNamesItselfAsTheCoverHidesThePageBehindItAndClosesByItsRef(t *testing.T) {
	driver, page := drivenPage(t, "results")
	before := observe(t, driver, true)
	card, filters := refOf(t, before, "link", "Casa ⋅ Atibaia"), refOf(t, before, "button", "Filtros")
	if !strings.Contains(before, `link "Casa ⋅ Atibaia" [ref=`+card+`, url=/rooms/123?adults=2&check_in=2026-10-09]`) {
		t.Fatalf("the card link carries no url:\n%s", before)
	}
	if _, err := driver.Do(browser.Move{Ref: filters, Kind: browser.MoveClick}); err != nil {
		t.Fatal(err)
	}
	covered, err := driver.Do(browser.Move{Ref: card, Kind: browser.MoveClick})
	if err != nil || covered.Covered != `dialog "Filtros"` || covered.Close == "" {
		t.Fatalf("a click on the card under the open dialog returned %+v, %v; want covered by dialog \"Filtros\" and its close ref", covered, err)
	}
	t.Logf("the covered click says: %s", covered)

	under := observe(t, driver, true)
	behind := strings.Index(under, `behind dialog "Filtros"`)
	if behind < 0 || refOf(t, under, "button", "Fechar") != covered.Close || strings.Contains(under[behind:], "ref=") || !strings.Contains(under[behind:], `link "Casa ⋅ Atibaia"`) {
		t.Fatalf("the snapshot under the dialog does not list the cards behind it without refs, or the close ref differs from %s:\n%s", covered.Close, under)
	}
	if _, err := driver.Do(browser.Move{Ref: card, Kind: browser.MoveClick}); err == nil {
		t.Fatal("a ref behind the modal is still actionable")
	}
	if _, err := driver.Do(browser.Move{Ref: covered.Close, Kind: browser.MoveClick}); err != nil {
		t.Fatal(err)
	}
	after := observe(t, driver, true)
	if refOf(t, after, "link", "Casa ⋅ Atibaia") != card {
		t.Fatalf("after the dialog closed the card is not %s again:\n%s", card, after)
	}
	if moved, err := driver.Do(browser.Move{Ref: card, Kind: browser.MoveClick}); err != nil || moved.Covered != "" {
		t.Fatalf("the click on the card after closing returned %+v, %v", moved, err)
	}
	page.sawFired(t, "open filters", "close filters", "listing 123")
}

func TestACardsFullAreaAnchorOverItsTitleLinkLetsTheClickThrough(t *testing.T) {
	driver, page := drivenPage(t, "card")
	snapshot := observe(t, driver, true)
	title := refOf(t, snapshot, "link", "Casa ⋅ Atibaia")
	if moved, err := driver.Do(browser.Move{Ref: title, Kind: browser.MoveClick}); err != nil || moved.Covered != "" {
		t.Fatalf("the click on the title under the card's own anchor returned %+v, %v", moved, err)
	}
	page.sawFired(t, "listing 123")
}

func (p *fakePage) sawTabOps(t *testing.T, want ...string) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if !slices.Equal(p.tabOps, want) {
		t.Fatalf("the tabs saw %q; want %q", p.tabOps, want)
	}
}

func TestAFieldThatIgnoresInsertTextIsTypedAndSaysFilled(t *testing.T) {
	driver, page := drivenPage(t, "login")
	snapshot := observe(t, driver, true)
	moved, err := driver.Do(browser.Move{Ref: refOf(t, snapshot, "textbox", "Username"), Kind: browser.MoveFill, Value: "tomsmith"})
	t.Logf("the fill says: %s", moved)
	page.mu.Lock()
	typed := page.node(2).Value
	page.mu.Unlock()
	if err != nil || typed != "tomsmith" || !strings.HasPrefix(moved.String(), "filled") {
		t.Fatalf("a fill into a field that ignores insertText left %q and said %q, %v; want tomsmith typed and filled", typed, moved, err)
	}
}

func TestALinkThatSwapsContentInPlaceReturnsWellUnderASecond(t *testing.T) {
	driver, page := drivenPage(t, "chips")
	chip := refOf(t, observe(t, driver, true), "link", "Piscina")
	started := time.Now()
	moved, err := driver.Do(browser.Move{Ref: chip, Kind: browser.MoveClick})
	took := time.Since(started)
	t.Logf("the chip click took %v and says: %s", took, moved)
	if err != nil || !moved.PageChanged || moved.URLChanged || took >= 700*time.Millisecond {
		t.Fatalf("a link that changes the page in place took %v and returned %+v, %v; want the page changed, no url change, well under a second", took, moved, err)
	}
	page.sawFired(t, "pool")
}

func TestAFillMovesTheCursorToItsFieldReadingTyping(t *testing.T) {
	driver, page := drivenPage(t, "login")
	if _, err := driver.Do(browser.Move{Ref: refOf(t, observe(t, driver, true), "textbox", "Password"), Kind: browser.MoveFill, Value: "secret"}); err != nil {
		t.Fatal(err)
	}
	page.mu.Lock()
	points := slices.Clone(page.points)
	page.mu.Unlock()
	if !slices.Equal(points, []string{"110,50 tofu typing"}) {
		t.Fatalf("a fill of the Password box sent the cursor points %q; want its centre 110,50 reading tofu typing", points)
	}
}

func TestAComboboxDeafToInsertAndKeysTakesTheNativeSetterAndSaysSo(t *testing.T) {
	driver, page := drivenPage(t, "combobox")
	moved, err := driver.Do(browser.Move{Ref: refOf(t, observe(t, driver, true), "combobox", "Onde"), Kind: browser.MoveFill, Value: "Atibaia"})
	t.Logf("the fill says: %s", moved)
	page.mu.Lock()
	filled := page.node(2).Value
	page.mu.Unlock()
	if err != nil || filled != "Atibaia" || !strings.HasPrefix(moved.String(), "filled through the native value setter") {
		t.Fatalf("a combobox deaf to insertText and keys holds %q after a fill that said %q, %v", filled, moved, err)
	}
}

func TestANavigateToA500PageSaysItIsAnErrorPage(t *testing.T) {
	driver, _ := drivenPage(t, "error")
	moved, err := driver.Do(browser.Move{Kind: browser.MoveNavigate, Value: "https://stays.test/rooms/9"})
	t.Logf("the navigate says: %s", moved)
	if err != nil || !strings.Contains(moved.String(), "error page") || !strings.Contains(moved.String(), "500") {
		t.Fatalf("a navigate to a 500 page said %q, %v; want it named an error page with its status", moved, err)
	}
}

func TestATaskWorksInOneTabAPopupOnAnySiteLoadsThereAndIsClosed(t *testing.T) {
	driver, page := drivenPage(t, "popup")
	for _, link := range []struct {
		name  string
		popup int
		url   string
	}{{"Casa em Lisboa", 21, "https://stays.test/rooms/123"}, {"Mapa", 22, "https://maps.test/atibaia"}} {
		if _, err := driver.Do(browser.Move{Kind: browser.MoveNavigate, Value: "https://stays.test/s/atibaia"}); err != nil {
			t.Fatal(err)
		}
		moved, err := driver.Do(browser.Move{Ref: refOf(t, observe(t, driver, true), "link", link.name), Kind: browser.MoveClick})
		if err != nil || moved.Opened != 0 || moved.Folded != link.popup || driver.Tab != 7 || !moved.URLChanged {
			t.Fatalf("a popup to %s returned %+v, %v, on tab %d; want it loaded into tab 7 and closed", link.url, moved, err, driver.Tab)
		}
		t.Logf("the click on %s says: %s", link.name, moved)
	}
	page.sawTabOps(t,
		"navigate 7 https://stays.test/s/atibaia", "close 21 ", "navigate 7 https://stays.test/rooms/123",
		"navigate 7 https://stays.test/s/atibaia", "close 22 ", "navigate 7 https://maps.test/atibaia")
}

func TestATaskOnThePersonsTabCreatesOneTabAndStaysInIt(t *testing.T) {
	driver, page := drivenPage(t, "newtab")
	for _, url := range []string{"https://www.google.test/search?q=airbnb", "https://www.airbnb.test/", "https://www.airbnb.test/rooms/123"} {
		if moved, err := driver.Do(browser.Move{Kind: browser.MoveNavigate, Value: url}); err != nil {
			t.Fatalf("navigate to %s returned %+v, %v", url, moved, err)
		}
	}
	if driver.Tab != 30 {
		t.Fatalf("the task ends on tab %d; want 30, the one tab it created", driver.Tab)
	}
	page.sawTabOps(t, "open 0 https://www.google.test/search?q=airbnb", "navigate 30 https://www.airbnb.test/", "navigate 30 https://www.airbnb.test/rooms/123")
}

func TestAClickTheBackgroundTabIgnoresFallsBackToTheElementsOwnClick(t *testing.T) {
	driver, page := drivenPage(t, "deaf")
	snapshot := observe(t, driver, true)
	moved, err := driver.Do(browser.Move{Ref: refOf(t, snapshot, "link", "Learn more"), Kind: browser.MoveClick})
	t.Logf("the click says: %s", moved)
	if err != nil || !moved.PageChanged || moved.Via != "click()" || !strings.Contains(moved.String(), "click()") {
		t.Fatalf("a click the page ignored returned %+v, %v; want the page changed through the element's own click()", moved, err)
	}
	page.sawFired(t, "iana")
	moved, err = driver.Do(browser.Move{Kind: browser.MovePress, Value: "Enter"})
	t.Logf("Enter says: %s", moved)
	if err != nil || !moved.PageChanged || moved.Via == "" {
		t.Fatalf("an Enter the page ignored returned %+v, %v; want the form submitted", moved, err)
	}
	page.sawFired(t, "iana", "submit")
}

func TestTheCursorOverlayChangesNoSnapshotAndNoHitTest(t *testing.T) {
	var snapshots []string
	for _, overlay := range []bool{true, false} {
		driver, page := drivenPage(t, "cursor")
		if !overlay {
			page.Nodes = page.Nodes[:len(page.Nodes)-1]
			page.Nodes[0].Children = page.Nodes[0].Children[:2]
		}
		for _, interactive := range []bool{true, false} {
			snapshots = append(snapshots, observe(t, driver, interactive))
		}
		moved, err := driver.Do(browser.Move{Ref: refOf(t, snapshots[len(snapshots)-2], "button", "Buscar"), Kind: browser.MoveClick})
		if err != nil || moved.Covered != "" || moved.Via != "" {
			t.Fatalf("with the overlay %v the click returned %+v, %v; want it to land by the mouse", overlay, moved, err)
		}
		page.sawFired(t, "search")
	}
	if snapshots[0] != snapshots[2] || snapshots[1] != snapshots[3] || strings.Contains(snapshots[1], "tofu") {
		t.Fatalf("the overlay changed the snapshot:\nwith\n%s\nwithout\n%s", snapshots[1], snapshots[3])
	}
}

func TestAPageWhoseFingerprintAlwaysMovesStillClicksOnceAndIsDone(t *testing.T) {
	var page map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(recordedSnapshot(t), `"ok":true,"value":`)), &page); err != nil {
		t.Fatal(err)
	}
	snapshots := 0
	tab, ext := sharedTab(t, func(call extensionCall) string {
		if call.Op == "snapshot" {
			snapshots++
			page["fingerprint"] = fmt.Sprint("tick-", snapshots)
			raw, _ := json.Marshal(page)
			return `"ok":true,"value":` + string(raw)
		}
		return `"ok":true,"value":{}`
	})
	result := jevloop.Loop{
		Browser: jevloop.Browser{Snapshot: tab.Snapshot, Act: tab.Act},
		Choose:  scripted(browser.Action{Op: browser.OpClick, Element: 5}, browser.Action{Op: browser.OpDone}),
		Actions: 30,
	}.Run(context.Background(), "find stays")
	ext.mu.Lock()
	clicks := slices.DeleteFunc(slices.Clone(ext.calls), func(call string) bool { return !strings.Contains(call, " click ") })
	ext.mu.Unlock()
	t.Logf("stopped %v after %d decisions and %d clicks: %s", result.Status, result.Decisions, len(clicks), result.Reason)
	if result.Status != jevloop.StatusDone || len(clicks) != 1 || result.Decisions != 2 {
		t.Fatalf("stopped %v after %d decisions and %d clicks, want done after 2 and 1", result.Status, result.Decisions, len(clicks))
	}
}
