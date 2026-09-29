package browser

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
)

type MoveKind string

const (
	MoveClick    MoveKind = "click"
	MoveFill     MoveKind = "fill"
	MoveSelect   MoveKind = "select"
	MovePress    MoveKind = "press"
	MoveScroll   MoveKind = "scroll"
	MoveNavigate MoveKind = "navigate"
	MoveBack     MoveKind = "back"
	MoveWait     MoveKind = "wait"
)

type Move struct {
	Ref   string
	Kind  MoveKind
	Value string
}

type Moved struct {
	Covered     string `json:"covered,omitempty"`
	Close       string `json:"close,omitempty"`
	Folded      int    `json:"folded,omitempty"`
	Value       string `json:"value,omitempty"`
	Opened      int    `json:"opened,omitempty"`
	URLChanged  bool   `json:"url_changed"`
	PageChanged bool   `json:"page_changed"`
}

const blockerAt = `function(x, y) {
  let doc = this.ownerDocument || document;
  while (doc.defaultView && doc.defaultView.frameElement) doc = doc.defaultView.frameElement.ownerDocument;
  let hit = doc.elementFromPoint(x, y);
  while (hit && (hit.tagName === 'IFRAME' || hit.tagName === 'FRAME') && hit.contentDocument && hit !== this) {
    const r = hit.getBoundingClientRect();
    x -= r.x + hit.clientLeft;
    y -= r.y + hit.clientTop;
    hit = hit.contentDocument.elementFromPoint(x, y);
  }
  if (!hit || hit === this) return null;
  const up = n => n.parentNode || n.host || null;
  for (let n = hit; n; n = up(n)) if (n === this) return null;
  for (let n = this; n; n = up(n)) if (n === hit) return null;
  const hitLabel = hit.closest ? hit.closest('label') : null;
  if (hitLabel && (hitLabel.control === this || hitLabel.contains(this))) return null;
  const ownLabel = this.closest ? this.closest('label') : null;
  if (ownLabel && ownLabel.contains(hit)) return null;
  return hit;
}`

const clearValue = `function() {
  this.select && this.select();
  this.value = '';
  this.dispatchEvent(new Event('input', {bubbles: true}));
}`

const selectOption = `function(values) {
  const normalize = value => String(value ?? '').replace(new RegExp('[' + String.fromCharCode(0x200B, 0x200C, 0x200D, 0x2060, 0xFEFF) + ']', 'g'), '').replace(/\s+/g, ' ').trim();
  const options = Array.from(this.options || []);
  const available = () => 'no option matched ' + JSON.stringify(values) + ', the options are: ' + options.map(o => o.value + ' ("' + normalize(o.label) + '")').join(', ');
  const wanted = new Set();
  for (const value of values) {
    let matches = options.filter(o => value === o.value || value === o.label.trim() || value === o.textContent.trim());
    if (matches.length === 0) matches = options.filter(o => normalize(o.label) === normalize(value));
    if (matches.length > 1 && !matches.some(o => value === o.value)) return {error: 'more than one option matched ' + JSON.stringify(value)};
    if (matches.length === 0) return {error: available()};
    matches.forEach(o => wanted.add(o));
  }
  for (const o of options) o.selected = wanted.has(o);
  this.dispatchEvent(new Event('change', {bubbles: true}));
  return {matched: wanted.size};
}`

const pendingRequests = `(() => {
  const now = performance.now();
  const noise = ['doubleclick.net', 'googlesyndication.com', 'googletagmanager.com', 'facebook.net', 'analytics', 'ads', 'tracking', 'pixel', 'hotjar.com', 'clarity.ms', 'mixpanel.com', 'segment.com', 'demdex.net', 'omtrdc.net', 'adobedtm.com', 'ensighten.com', 'newrelic.com', 'nr-data.net', 'google-analytics.com', 'connect.facebook.net', 'platform.twitter.com', 'platform.linkedin.com', '.cloudfront.net/image/', '.akamaized.net/image/', '/tracker/', '/collector/', '/beacon/', '/telemetry/', '/log/', '/events/', '/eventBatch', '/track.', '/metrics/'];
  const pending = performance.getEntriesByType('resource').filter(entry => {
    const url = entry.name, age = now - entry.startTime;
    const minor = ['img', 'image', 'icon', 'font'].includes(entry.initiatorType) || /\.(jpg|jpeg|png|gif|webp|svg|ico)(\?|$)/i.test(url);
    return entry.responseEnd === 0 && !noise.some(part => url.includes(part)) && !url.startsWith('data:') && url.length <= 500 && age <= 10000 && !(minor && age > 3000);
  });
  return {pending: pending.length, loading: document.readyState !== 'complete'};
})()`

const pageStateScript = `({url: location.href, count: document.getElementsByTagName('*').length, text: document.body ? document.body.innerText : ''})`

func mouse(kind, button string, x, y float64) cdpCall {
	return cdpCall{Method: "Input.dispatchMouseEvent", Params: map[string]any{"type": kind, "x": x, "y": y, "button": button, "clickCount": 1}}
}

type pageState struct {
	URL   string `json:"url"`
	Count int    `json:"count"`
	Text  string `json:"text"`
}

func (d *Driver) state(deadline time.Time) (pageState, []Tab, error) {
	raw, err := d.Client.callBy(deadline, 0, opTabs, nil)
	var tabs []Tab
	if err == nil {
		err = json.Unmarshal(raw, &tabs)
	}
	var page pageState
	if err == nil {
		err = d.value(deadline, false, evaluate(pageStateScript), &page)
	}
	return page, tabs, err
}

const Unchanged = "the page did not change"

func (m Moved) String() string {
	switch {
	case m.Covered != "" && m.Close != "":
		return "did not run, covered by " + m.Covered + ": close it first with " + m.Close
	case strings.HasPrefix(m.Covered, "dialog") || strings.HasPrefix(m.Covered, "alertdialog"):
		return "did not run, covered by " + m.Covered + ": close it first"
	case m.Covered != "":
		return "did not run, covered by " + m.Covered
	case m.Folded != 0:
		return fmt.Sprintf("the page opened a popup, so tofu loaded its url in this tab and closed popup tab %d", m.Folded)
	case m.Opened != 0:
		return fmt.Sprintf("opened tab %d, which the next actions use", m.Opened)
	case m.URLChanged:
		return "the url changed"
	case m.PageChanged && m.Value != "":
		return "the page changed, the field reads " + strconv.Quote(m.Value)
	case m.PageChanged:
		return "the page changed"
	}
	return Unchanged
}

func (d *Driver) Use(tab int) {
	if tab != d.Tab {
		d.Tab, d.refs = tab, refMap{next: d.refs.next}
	}
}

func (d *Driver) Fingerprint() (string, error) {
	var page pageState
	err := d.value(time.Now().Add(konst.BrowserActTimeoutMillis*time.Millisecond), false, evaluate(pageStateScript), &page)
	text := sha256.Sum256([]byte(page.Text))
	return fmt.Sprintf("%s %d %x", page.URL, page.Count, text[:8]), err
}

func (d *Driver) Do(move Move) (Moved, error) {
	budget := konst.BrowserActTimeoutMillis * time.Millisecond
	if move.Kind == MoveWait {
		budget += konst.BrowserWaitMaxMillis * time.Millisecond
	}
	moved, err := d.do(time.Now().Add(budget), move)
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return Moved{}, fmt.Errorf("%s on tab %d did not finish within %d ms, and tofu did not retry it", move.Kind, d.Tab, budget.Milliseconds())
	}
	return moved, err
}

func (d *Driver) do(deadline time.Time, move Move) (Moved, error) {
	before, tabsBefore, err := d.state(deadline)
	if err != nil {
		return Moved{}, err
	}
	var moved Moved
	switch move.Kind {
	case MoveClick:
		moved, err = d.click(deadline, move.Ref)
	case MoveFill:
		moved.Value, err = d.fill(deadline, move.Ref, move.Value)
	case MoveSelect:
		err = d.pick(deadline, move.Ref, move.Value)
	case MovePress:
		err = d.press(deadline, move.Value)
	case MoveScroll:
		err = d.scroll(deadline, move.Ref, move.Value)
	case MoveNavigate:
		err = d.navigate(deadline, move.Value, tabsBefore)
	case MoveBack:
		_, err = d.Client.callBy(deadline, d.Tab, opBack, nil)
	case MoveWait:
		err = d.wait(deadline, move.Value)
	default:
		return Moved{}, fmt.Errorf("there is no browser move %q", move.Kind)
	}
	if err != nil || moved.Covered != "" {
		return moved, err
	}
	d.settle(deadline)
	after, tabsAfter, err := d.state(deadline)
	if err != nil {
		return moved, err
	}
	moved.URLChanged, moved.PageChanged = after.URL != before.URL, after != before
	if move.Kind != MoveNavigate {
		for _, tab := range tabsAfter {
			if !slices.ContainsFunc(tabsBefore, func(held Tab) bool { return held.ID == tab.ID }) {
				moved.Opened = tab.ID
				moved.Folded = d.fold(deadline, tab)
			}
		}
	}
	switch {
	case moved.Folded != 0:
		moved.Opened, moved.URLChanged, moved.PageChanged = 0, true, true
		d.refs = refMap{next: d.refs.next}
	case moved.Opened != 0:
		d.Use(moved.Opened)
	case moved.URLChanged:
		d.refs = refMap{next: d.refs.next}
	}
	return moved, nil
}

func (d *Driver) fold(deadline time.Time, popup Tab) int {
	if !popup.Opened || popup.URL == "" {
		return 0
	}
	args, _ := json.Marshal(openArgs{URL: popup.URL})
	if _, err := d.Client.callBy(deadline, d.Tab, opNavigate, args); err != nil {
		return 0
	}
	_, _ = d.Client.callBy(deadline, popup.ID, opClose, nil)
	return popup.ID
}

func (d *Driver) center(deadline time.Time, ref string) (int, float64, float64, error) {
	var box struct {
		Model struct {
			Content []float64 `json:"content"`
		} `json:"model"`
	}
	backend, err := d.onNode(deadline, ref, func(backend int) error {
		answers, err := d.cdp(deadline, false, byBackend("DOM.scrollIntoViewIfNeeded", backend), byBackend("DOM.getBoxModel", backend))
		if err != nil {
			return err
		}
		return answers[1].into(&box)
	})
	quad := box.Model.Content
	if err == nil && len(quad) < 8 {
		err = fmt.Errorf("the ref %s has no box on tab %d", ref, d.Tab)
	}
	if err != nil {
		return 0, 0, 0, err
	}
	return backend, (quad[0] + quad[2] + quad[4] + quad[6]) / 4, (quad[1] + quad[3] + quad[5] + quad[7]) / 4, nil
}

func (d *Driver) resolve(deadline time.Time, ref string) (string, error) {
	var resolved struct {
		Object remoteObject `json:"object"`
	}
	_, err := d.onNode(deadline, ref, func(backend int) error {
		answer, err := d.one(deadline, false, byBackend("DOM.resolveNode", backend))
		if err != nil {
			return err
		}
		return answer.into(&resolved)
	})
	return resolved.Object.ObjectID, err
}

func (d *Driver) click(deadline time.Time, ref string) (Moved, error) {
	backend, x, y, err := d.center(deadline, ref)
	if err != nil {
		return Moved{}, err
	}
	covered, err := d.blocker(deadline, backend, x, y)
	if err != nil || covered.Covered != "" {
		return covered, err
	}
	return Moved{}, d.act(deadline, mouse("mouseMoved", "none", x, y), mouse("mousePressed", "left", x, y), mouse("mouseReleased", "left", x, y))
}

const linkOf = "function() { const link = this.closest && this.closest('a[href]'); return link ? link.href : ''; }"

type axTreeNode struct {
	axNode
	ParentID string `json:"parentId"`
}

func (d *Driver) blocker(deadline time.Time, backend int, x, y float64) (Moved, error) {
	answer, err := d.one(deadline, false, byBackend("DOM.resolveNode", backend))
	var resolved struct {
		Object remoteObject `json:"object"`
	}
	if err != nil || answer.into(&resolved) != nil {
		return Moved{}, err
	}
	if answer, err = d.one(deadline, false, callOn(resolved.Object.ObjectID, blockerAt, false, x, y)); err != nil {
		return Moved{}, err
	}
	hit, err := answer.object()
	if err != nil || hit.ObjectID == "" {
		return Moved{}, nil
	}
	answers, err := d.cdp(deadline, false, callOn(resolved.Object.ObjectID, linkOf, true), callOn(hit.ObjectID, linkOf, true),
		cdpCall{Method: "DOM.describeNode", Params: map[string]any{"objectId": hit.ObjectID}}, cdpCall{Method: "Accessibility.getFullAXTree"})
	if err != nil {
		return Moved{}, err
	}
	var links [2]string
	for i := range links {
		if object, err := answers[i].object(); err == nil {
			_ = json.Unmarshal(object.Value, &links[i])
		}
	}
	if links[0] != "" && links[0] == links[1] {
		return Moved{}, nil
	}
	var described struct {
		Node struct {
			BackendNodeID int `json:"backendNodeId"`
		} `json:"node"`
	}
	var tree struct {
		Nodes []axTreeNode `json:"nodes"`
	}
	if answers[2].into(&described) != nil || answers[3].into(&tree) != nil {
		return Moved{Covered: "an element tofu cannot name"}, nil
	}
	return d.cover(tree.Nodes, described.Node.BackendNodeID), nil
}

func (d *Driver) cover(nodes []axTreeNode, backend int) Moved {
	byID := map[string]int{}
	at := -1
	for i, node := range nodes {
		byID[node.NodeID] = i
		if node.Backend == backend {
			at = i
		}
	}
	if at < 0 {
		return Moved{Covered: "an element tofu cannot name"}
	}
	named := at
	for i, known := at, true; known; i, known = byID[nodes[i].ParentID] {
		node := nodes[i]
		anonymous := slices.Contains([]string{"", "generic", "none", "presentation", "StaticText", "InlineTextBox", "RootWebArea", "WebArea"}, node.Role.text())
		if !anonymous && !node.Ignored && node.Name.text() != "" {
			named = i
			break
		}
	}
	cover := nodes[named]
	moved := Moved{Covered: cover.Role.text()}
	if name := cover.Name.text(); name != "" {
		moved.Covered += " " + strconv.Quote(name)
	}
	if role := cover.Role.text(); role == "dialog" || role == "alertdialog" {
		moved.Close = d.closeRef(nodes, byID, named)
	}
	return moved
}

func (d *Driver) closeRef(nodes []axTreeNode, byID map[string]int, dialog int) string {
	queue := slices.Clone(nodes[dialog].ChildIDs)
	for len(queue) > 0 {
		at, known := byID[queue[0]]
		queue = queue[1:]
		if !known {
			continue
		}
		node := nodes[at]
		queue = append(queue, node.ChildIDs...)
		name := strings.ToLower(strings.TrimSpace(node.Name.text()))
		closes := name == "x" || name == "×" || name == "✕" || slices.ContainsFunc([]string{"close", "fechar", "cerrar", "fermer", "schließen", "chiudi", "sluiten"}, func(word string) bool { return strings.Contains(name, word) })
		if node.Ignored || node.Role.text() != "button" || !closes || node.Backend == 0 {
			continue
		}
		nth := 0
		for _, earlier := range nodes[:at] {
			if !earlier.Ignored && earlier.Role.text() == "button" && earlier.Name.text() == node.Name.text() {
				nth++
			}
		}
		if d.refs.entries == nil {
			d.refs.entries = map[string]refEntry{}
		}
		ref := d.refs.allocate(d.refs.documents[""], false, &treeNode{backend: node.Backend})
		d.refs.entries[ref] = refEntry{backend: node.Backend, role: "button", name: node.Name.text(), nth: nth}
		return ref
	}
	return ""
}

func (d *Driver) fill(deadline time.Time, ref, text string) (string, error) {
	object, err := d.resolve(deadline, ref)
	if err == nil {
		err = d.act(deadline, callOn(object, "function() { this.focus(); }", true), callOn(object, clearValue, true), cdpCall{Method: "Input.insertText", Params: map[string]any{"text": text}})
	}
	var value string
	if err == nil {
		err = d.value(deadline, false, callOn(object, "function() { return this.value ?? this.textContent; }", true), &value)
	}
	return value, err
}

func (d *Driver) pick(deadline time.Time, ref, option string) error {
	object, err := d.resolve(deadline, ref)
	var picked struct {
		Error string `json:"error"`
	}
	if err == nil {
		err = d.value(deadline, true, callOn(object, selectOption, true, []string{option}), &picked)
	}
	if err == nil && picked.Error != "" {
		err = errors.New(picked.Error)
	}
	return err
}

func keyInfo(key string) (name, code string, keyCode int) {
	switch strings.ToLower(key) {
	case "enter", "return":
		return "Enter", "Enter", 13
	case "tab":
		return "Tab", "Tab", 9
	case "escape", "esc":
		return "Escape", "Escape", 27
	case "backspace":
		return "Backspace", "Backspace", 8
	case "delete":
		return "Delete", "Delete", 46
	case "arrowup", "up":
		return "ArrowUp", "ArrowUp", 38
	case "arrowdown", "down":
		return "ArrowDown", "ArrowDown", 40
	case "arrowleft", "left":
		return "ArrowLeft", "ArrowLeft", 37
	case "arrowright", "right":
		return "ArrowRight", "ArrowRight", 39
	case "home":
		return "Home", "Home", 36
	case "end":
		return "End", "End", 35
	case "pageup":
		return "PageUp", "PageUp", 33
	case "pagedown":
		return "PageDown", "PageDown", 34
	case "space", " ":
		return " ", "Space", 32
	}
	if len(key) != 1 {
		return key, key, 0
	}
	upper := strings.ToUpper(key)
	switch char := key[0]; {
	case char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z':
		return key, "Key" + upper, int(upper[0])
	case char >= '0' && char <= '9':
		return key, "Digit" + key, int(char)
	}
	for _, punctuation := range []struct {
		keys, code string
		keyCode    int
	}{{";:", "Semicolon", 186}, {"=+", "Equal", 187}, {",<", "Comma", 188}, {"-_", "Minus", 189}, {".>", "Period", 190}, {"/?", "Slash", 191},
		{"`~", "Backquote", 192}, {"[{", "BracketLeft", 219}, {"\\|", "Backslash", 220}, {"]}", "BracketRight", 221}, {"'\"", "Quote", 222}} {
		if strings.Contains(punctuation.keys, key) {
			return key, punctuation.code, punctuation.keyCode
		}
	}
	return key, "", 0
}

func (d *Driver) press(deadline time.Time, key string) error {
	name, code, keyCode := keyInfo(key)
	down := map[string]any{"type": "keyDown", "key": name, "code": code, "windowsVirtualKeyCode": keyCode, "nativeVirtualKeyCode": keyCode}
	text := map[string]string{"Enter": "\r", "Tab": "\t"}[name]
	if len(name) == 1 {
		text = name
	}
	if text != "" {
		down["text"], down["unmodifiedText"] = text, text
	}
	up := map[string]any{"type": "keyUp", "key": name, "code": code, "windowsVirtualKeyCode": keyCode, "nativeVirtualKeyCode": keyCode}
	return d.act(deadline, cdpCall{Method: "Input.dispatchKeyEvent", Params: down}, cdpCall{Method: "Input.dispatchKeyEvent", Params: up})
}

func (d *Driver) scroll(deadline time.Time, ref, direction string) error {
	pixels := konst.BrowserScrollPixels
	switch direction {
	case "up":
		pixels = -pixels
	case "", "down":
	default:
		return fmt.Errorf("scroll goes up or down, not %q", direction)
	}
	if ref == "" {
		return d.act(deadline, evaluate(fmt.Sprintf("window.scrollBy(0, %d)", pixels)))
	}
	object, err := d.resolve(deadline, ref)
	if err != nil {
		return err
	}
	return d.act(deadline, callOn(object, "function(dx, dy) { this.scrollBy(dx, dy); }", true, 0, pixels))
}

func (d *Driver) navigate(deadline time.Time, url string, tabs []Tab) error {
	args, _ := json.Marshal(openArgs{URL: url})
	if slices.ContainsFunc(tabs, func(tab Tab) bool { return tab.ID == d.Tab && tab.Opened }) {
		_, err := d.Client.callBy(deadline, d.Tab, opNavigate, args)
		return err
	}
	raw, err := d.Client.callBy(deadline, 0, opOpen, args)
	var tab int
	if err == nil {
		err = json.Unmarshal(raw, &tab)
	}
	if err == nil {
		d.Use(tab)
	}
	return err
}

func (d *Driver) wait(deadline time.Time, value string) error {
	if millis, err := strconv.Atoi(value); err == nil {
		time.Sleep(time.Duration(min(millis, konst.BrowserWaitMaxMillis)) * time.Millisecond)
		return nil
	}
	quoted, _ := json.Marshal(value)
	for until := time.Now().Add(konst.BrowserWaitMaxMillis * time.Millisecond); ; time.Sleep(konst.BrowserSettlePollMillis * time.Millisecond) {
		var shown bool
		if err := d.value(deadline, false, evaluate("document.body !== null && document.body.innerText.includes("+string(quoted)+")"), &shown); err != nil {
			return err
		}
		if shown {
			return nil
		}
		if time.Now().After(until) {
			return fmt.Errorf("%q did not appear on tab %d within %d ms", value, d.Tab, konst.BrowserWaitMaxMillis)
		}
	}
}

func (d *Driver) settle(deadline time.Time) {
	for until := time.Now().Add(konst.BrowserSettleMaxMillis * time.Millisecond); time.Now().Before(until); time.Sleep(konst.BrowserSettlePollMillis * time.Millisecond) {
		var busy struct {
			Pending int  `json:"pending"`
			Loading bool `json:"loading"`
		}
		if d.value(deadline, false, evaluate(pendingRequests), &busy) == nil && busy.Pending == 0 && !busy.Loading {
			return
		}
	}
}
