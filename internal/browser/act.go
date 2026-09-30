package browser

import (
	"cmp"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
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
	Via         string `json:"via,omitempty"`
	SettledMS   int    `json:"settled_ms"`
	Value       string `json:"value,omitempty"`
	Field       string `json:"field,omitempty"`
	ErrorPage   string `json:"error_page,omitempty"`
	Title       string `json:"title,omitempty"`
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
  const hitLabel = hit.closest ? hit.closest('label') : null;
  if (hitLabel && (hitLabel.control === this || hitLabel.contains(this))) return null;
  const ownLabel = this.closest ? this.closest('label') : null;
  if (ownLabel && ownLabel.contains(hit)) return null;
  for (let n = this; n; n = up(n)) if (n === hit) return 'parent';
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

const headingScript = `document.title + '\n' + ((document.querySelector('h1') || {}).innerText || '')`

const pageStateScript = `({url: location.href, count: document.querySelectorAll('*:not([data-tofu-cursor])').length, text: document.body ? document.body.innerText : '',
  status: (performance.getEntries().find(entry => entry.entryType === 'navigation') || {}).responseStatus || 0,
  heading: ` + headingScript + `,
  controls: Array.from(document.querySelectorAll('input, select, textarea, [aria-checked], [aria-expanded], [aria-pressed], [aria-selected]'),
    e => [e.checked, e.value, e.getAttribute('aria-checked'), e.getAttribute('aria-expanded'), e.getAttribute('aria-pressed'), e.getAttribute('aria-selected')].join(',')).join('|')})`

func mouse(kind, button string, x, y float64) cdpCall {
	return cdpCall{Method: "Input.dispatchMouseEvent", Params: map[string]any{"type": kind, "x": x, "y": y, "button": button, "clickCount": 1}}
}

type pageState struct {
	URL      string `json:"url"`
	Count    int    `json:"count"`
	Text     string `json:"text"`
	Controls string `json:"controls"`
	Status   int    `json:"status"`
	Heading  string `json:"heading"`
}

func (p pageState) errorPage() string {
	if p.Status >= 400 {
		return fmt.Sprintf("HTTP %d", p.Status)
	}
	if regexp.MustCompile(`(?im)^\s*[45]\d\d\b|\b(error|not found|forbidden|unavailable|bad gateway)\b`).MatchString(p.Heading) {
		return "titled " + strconv.Quote(strings.TrimSpace(strings.ReplaceAll(p.Heading, "\n", " ")))
	}
	return ""
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
	said := m.said()
	if m.Via != "" {
		said += ", through " + m.Via + " because the mouse and keys missed"
	}
	if m.Covered == "" {
		said += fmt.Sprintf(" (settled in %d ms)", m.SettledMS)
	}
	return said
}

func (m Moved) said() string {
	switch {
	case m.Covered != "" && m.Close != "":
		return "did not run, covered by " + m.Covered + ": close it first with " + m.Close
	case strings.HasPrefix(m.Covered, "dialog") || strings.HasPrefix(m.Covered, "alertdialog"):
		return "did not run, covered by " + m.Covered + ": close it first"
	case m.Covered != "":
		return "did not run, covered by " + m.Covered
	case m.ErrorPage != "":
		return "landed on an error page, " + m.ErrorPage
	case m.Folded != 0:
		return fmt.Sprintf("the page opened a popup, so tofu loaded its url in this tab and closed popup tab %d", m.Folded)
	case m.Opened != 0:
		return fmt.Sprintf("opened tab %d, which the next actions use", m.Opened)
	case m.Field != "":
		return m.Field
	case m.URLChanged && m.Title != "":
		return "the url changed to a page titled " + strconv.Quote(m.Title)
	case m.URLChanged:
		return "the url changed"
	case m.PageChanged:
		return "the page changed"
	}
	return Unchanged
}

func (d *Driver) Target(ref string) string {
	entry, known := d.refs.entries[ref]
	if !known {
		return ref
	}
	return entry.role + " " + strconv.Quote(entry.name)
}

func (d *Driver) Use(tab int) {
	if tab != d.Tab {
		d.Tab, d.refs = tab, refMap{next: d.refs.next}
	}
}

func (d *Driver) Page() (url, text string, err error) {
	var page pageState
	err = d.value(time.Now().Add(konst.BrowserActTimeoutMillis*time.Millisecond), false, evaluate(pageStateScript), &page)
	return page.URL, page.Text, err
}

func (d *Driver) Fingerprint() (string, error) {
	var page pageState
	err := d.value(time.Now().Add(konst.BrowserActTimeoutMillis*time.Millisecond), false, evaluate(pageStateScript), &page)
	text := sha256.Sum256([]byte(page.Text + "\x00" + page.Controls))
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
	started := time.Now()
	var moved Moved
	var toggleStuck bool
	switch move.Kind {
	case MoveClick:
		moved, toggleStuck, err = d.click(deadline, move.Ref)
	case MoveFill:
		var path string
		moved.Value, path, err = d.fill(deadline, move.Ref, move.Value)
		switch moved.Field = "filled through " + path; {
		case moved.Value == "":
			moved.Field = "field still empty after insertText, typed keys and the native value setter"
		case moved.Value != move.Value:
			moved.Field = "the field reads " + strconv.Quote(moved.Value) + " after insertText, typed keys and the native value setter"
		}
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
	if d.navigates(deadline, move) {
		d.awaitNavigation(deadline, before, tabsBefore)
	}
	moved, err = d.measure(deadline, move, before, tabsBefore, moved, started)
	if err == nil && moved.Via == "" && (!moved.PageChanged || toggleStuck) && moved.Opened == 0 {
		if via := d.fallback(deadline, move); via != "" {
			moved, err = d.measure(deadline, move, before, tabsBefore, moved, started)
			if moved.PageChanged || moved.Opened != 0 {
				moved.Via = via
			}
		}
	}
	return moved, err
}

const submitsOrLinks = `function() { return !!(this.closest('a') || (this.form && (this.type === 'submit' || this.type === 'image'))); }`

func (d *Driver) navigates(deadline time.Time, move Move) bool {
	switch move.Kind {
	case MovePress:
		name, _, _ := keyInfo(move.Value)
		return name == "Enter"
	case MoveClick:
		if d.refs.entries[move.Ref].role == "link" {
			return true
		}
		object, err := d.resolve(deadline, move.Ref)
		var navigates bool
		return err == nil && d.value(deadline, false, callOn(object, submitsOrLinks, true), &navigates) == nil && navigates
	}
	return false
}

func (d *Driver) awaitNavigation(deadline time.Time, before pageState, tabsBefore []Tab) {
	for until := time.Now().Add(konst.BrowserDOMQuietMaxMillis * time.Millisecond); time.Now().Before(until); time.Sleep(konst.BrowserSettleTickMillis * time.Millisecond) {
		after, tabs, err := d.state(deadline)
		switch {
		case err != nil:
		case after.URL != before.URL || len(tabs) > len(tabsBefore):
			return
		case after != before:
			d.settle(deadline, konst.BrowserDOMQuietMillis)
			return
		}
	}
}

func withoutQuery(address string) string {
	address, _, _ = strings.Cut(address, "#")
	address, _, _ = strings.Cut(address, "?")
	return address
}

func (d *Driver) awaitRender(deadline time.Time, before, after pageState, started time.Time) bool {
	heading := after.Heading
	for withoutQuery(after.URL) != withoutQuery(before.URL) && heading == before.Heading {
		if time.Since(started) >= konst.BrowserRenderWaitMaxMillis*time.Millisecond {
			return false
		}
		time.Sleep(konst.BrowserSettleTickMillis * time.Millisecond)
		if d.value(deadline, false, evaluate(headingScript), &heading) != nil {
			return false
		}
	}
	return true
}

const clickElement = "function() { this.click(); }"

const submitFocused = `(() => {
  const form = document.activeElement && document.activeElement.form;
  if (!form) return false;
  if (form.requestSubmit) form.requestSubmit(); else form.submit();
  return true;
})()`

func (d *Driver) fallback(deadline time.Time, move Move) string {
	switch {
	case move.Kind == MoveClick:
		object, err := d.resolve(deadline, move.Ref)
		if err == nil && d.act(deadline, callOn(object, clickElement, true)) == nil {
			return "click()"
		}
	case move.Kind == MovePress && strings.EqualFold(move.Value, "enter"):
		var submitted bool
		if d.value(deadline, true, evaluate(submitFocused), &submitted) == nil && submitted {
			return "the field's form submit"
		}
	}
	return ""
}

func (d *Driver) measure(deadline time.Time, move Move, before pageState, tabsBefore []Tab, moved Moved, started time.Time) (Moved, error) {
	d.settle(deadline, 0)
	after, tabsAfter, err := d.state(deadline)
	arrived := err == nil && (after.URL != before.URL || move.Kind == MoveNavigate || move.Kind == MoveBack)
	if arrived {
		if d.awaitRender(deadline, before, after, started) {
			d.settle(deadline, konst.BrowserDOMQuietMillis)
		}
		after, tabsAfter, err = d.state(deadline)
	}
	if err != nil {
		return moved, err
	}
	moved.SettledMS = int(time.Since(started).Milliseconds())
	moved.URLChanged, moved.PageChanged = after.URL != before.URL, after != before
	if arrived {
		moved.ErrorPage = after.errorPage()
		moved.Title, _, _ = strings.Cut(after.Heading, "\n")
	}
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
	if _, err := d.Client.callBy(deadline, popup.ID, opClose, nil); err != nil {
		return 0
	}
	args, _ := json.Marshal(openArgs{URL: popup.URL})
	_, _ = d.Client.callBy(deadline, d.Tab, opNavigate, args)
	return popup.ID
}

func (d *Driver) center(deadline time.Time, ref string) (int, float64, float64, error) {
	var box struct {
		Model struct {
			Content []float64 `json:"content"`
		} `json:"model"`
	}
	var content struct {
		Quads [][]float64 `json:"quads"`
	}
	var metrics struct {
		Viewport struct {
			Width  float64 `json:"clientWidth"`
			Height float64 `json:"clientHeight"`
		} `json:"cssVisualViewport"`
	}
	backend, err := d.onNode(deadline, ref, func(backend int) error {
		answers, err := d.cdp(deadline, false, byBackend("DOM.scrollIntoViewIfNeeded", backend), byBackend("DOM.getBoxModel", backend),
			byBackend("DOM.getContentQuads", backend), cdpCall{Method: "Page.getLayoutMetrics"})
		if err != nil {
			return err
		}
		_ = answers[2].into(&content)
		_ = answers[3].into(&metrics)
		return answers[1].into(&box)
	})
	quad := box.Model.Content
	if err == nil && len(quad) < 8 {
		err = fmt.Errorf("the ref %s has no box on tab %d", ref, d.Tab)
	}
	if err != nil {
		return 0, 0, 0, err
	}
	x, y := (quad[0]+quad[2]+quad[4]+quad[6])/4, (quad[1]+quad[3]+quad[5]+quad[7])/4
	width, height := cmp.Or(metrics.Viewport.Width, math.Inf(1)), cmp.Or(metrics.Viewport.Height, math.Inf(1))
	largest := 0.0
	for _, q := range content.Quads {
		if len(q) < 8 {
			continue
		}
		left, top := max(min(q[0], q[2], q[4], q[6]), 0), max(min(q[1], q[3], q[5], q[7]), 0)
		right, bottom := min(max(q[0], q[2], q[4], q[6]), width), min(max(q[1], q[3], q[5], q[7]), height)
		if area := (right - left) * (bottom - top); right > left && bottom > top && area > largest {
			largest, x, y = area, (left+right)/2, (top+bottom)/2
		}
	}
	return backend, x, y, nil
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

const checkedState = `function() {
  const box = this.control || this;
  if (box.type === 'checkbox' || box.type === 'radio') return String(box.checked);
  return box.getAttribute ? box.getAttribute('aria-checked') : null;
}`

func (d *Driver) click(deadline time.Time, ref string) (moved Moved, toggleStuck bool, err error) {
	backend, x, y, err := d.center(deadline, ref)
	var resolved struct {
		Object remoteObject `json:"object"`
	}
	var answer cdpAnswer
	if err == nil {
		answer, err = d.one(deadline, false, byBackend("DOM.resolveNode", backend))
	}
	if err == nil {
		err = answer.into(&resolved)
	}
	object := resolved.Object.ObjectID
	if err == nil {
		moved, err = d.blocker(deadline, object, x, y)
	}
	if err != nil || moved.Covered != "" {
		return moved, false, err
	}
	if moved.Via != "" {
		return moved, false, d.act(deadline, callOn(object, clickElement, true))
	}
	var before, after *string
	_ = d.value(deadline, false, callOn(object, checkedState, true), &before)
	if err = d.act(deadline, mouse("mouseMoved", "none", x, y), mouse("mousePressed", "left", x, y), mouse("mouseReleased", "left", x, y)); err != nil || before == nil {
		return Moved{}, false, err
	}
	_ = d.value(deadline, false, callOn(object, checkedState, true), &after)
	return Moved{}, after != nil && *after == *before, nil
}

const linkOf = "function() { const link = this.closest && this.closest('a[href]'); return link ? link.href : ''; }"

type axTreeNode struct {
	axNode
	ParentID string `json:"parentId"`
}

func (d *Driver) blocker(deadline time.Time, object string, x, y float64) (Moved, error) {
	answer, err := d.one(deadline, false, callOn(object, blockerAt, false, x, y))
	if err != nil {
		return Moved{}, err
	}
	hit, err := answer.object()
	if err == nil && string(hit.Value) == `"parent"` {
		return Moved{Via: "click()"}, nil
	}
	if err != nil || hit.ObjectID == "" {
		return Moved{}, nil
	}
	answers, err := d.cdp(deadline, false, callOn(object, linkOf, true), callOn(hit.ObjectID, linkOf, true),
		cdpCall{Method: "DOM.describeNode", Params: map[string]any{"objectId": hit.ObjectID}}, cdpCall{Method: "Accessibility.getFullAXTree"})
	if err != nil {
		return Moved{}, err
	}
	var links [2]string
	for i := range links {
		if link, err := answers[i].object(); err == nil {
			_ = json.Unmarshal(link.Value, &links[i])
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
		closes := name == "x" || name == "\u00d7" || name == "\u2715" || slices.ContainsFunc([]string{"close", "fechar", "cerrar", "fermer", "schlie\u00dfen", "chiudi", "sluiten"}, func(word string) bool { return strings.Contains(name, word) })
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

const nativeValue = `function(text) {
  const proto = this instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : this instanceof HTMLInputElement ? HTMLInputElement.prototype : null;
  const setter = proto && Object.getOwnPropertyDescriptor(proto, 'value').set;
  if (setter) setter.call(this, text); else if (this.isContentEditable) this.textContent = text; else this.value = text;
  for (const type of ['input', 'change']) this.dispatchEvent(new Event(type, {bubbles: true}));
  this.dispatchEvent(new FocusEvent('blur'));
}`

func (d *Driver) show(deadline time.Time, ref, label string) (float64, float64, error) {
	_, x, y, err := d.center(deadline, ref)
	if err == nil {
		args, _ := json.Marshal(cdpArgs{Calls: []cdpCall{}, Act: true, Point: &cursorPoint{X: x, Y: y, Label: label}})
		_, err = d.Client.callBy(deadline, d.Tab, opCDP, args)
	}
	return x, y, err
}

func (d *Driver) Thinking() {
	args, _ := json.Marshal(cdpArgs{Calls: []cdpCall{}, Thinking: true})
	_, _ = d.Client.callBy(time.Now().Add(konst.BrowserActTimeoutMillis*time.Millisecond), d.Tab, opCDP, args)
}

func (d *Driver) fill(deadline time.Time, ref, text string) (value, path string, err error) {
	object, err := d.resolve(deadline, ref)
	if err != nil {
		return "", "", err
	}
	emptied := []cdpCall{callOn(object, "function() { this.focus(); }", true), callOn(object, clearValue, true)}
	var typed []cdpCall
	if x, y, err := d.show(deadline, ref, "tofu typing"); err == nil {
		typed = append(typed, mouse("mousePressed", "left", x, y), mouse("mouseReleased", "left", x, y))
	}
	typed = append(typed, emptied...)
	for _, char := range text {
		key := string(char)
		typed = append(typed,
			cdpCall{Method: "Input.dispatchKeyEvent", Params: map[string]any{"type": "keyDown", "key": key, "text": key, "unmodifiedText": key}},
			cdpCall{Method: "Input.dispatchKeyEvent", Params: map[string]any{"type": "keyUp", "key": key}})
	}
	readBack := callOn(object, "function() { return this.value ?? this.textContent; }", true)
	for _, try := range []struct {
		path  string
		calls []cdpCall
	}{
		{"insertText", append(slices.Clone(emptied), cdpCall{Method: "Input.insertText", Params: map[string]any{"text": text}})},
		{"typed keys after a click", typed},
		{"the native value setter", []cdpCall{callOn(object, nativeValue, true, text)}},
	} {
		if err = d.act(deadline, try.calls...); err == nil {
			err = d.value(deadline, false, readBack, &value)
		}
		if err != nil || value == text {
			return value, try.path, err
		}
	}
	return value, "", nil
}

func (d *Driver) pick(deadline time.Time, ref, option string) error {
	_, _, _ = d.show(deadline, ref, "tofu")
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
	_, _, _ = d.show(deadline, ref, "tofu")
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

const settleScript = `new Promise(resolve => {
  const started = performance.now();
  let changed = started;
  const observer = new MutationObserver(() => { changed = performance.now(); });
  observer.observe(document, {subtree: true, childList: true, characterData: true, attributes: true});
  const pending = () => {
    const entries = %[4]s;
    return entries.pending > 0 || entries.loading;
  };
  const tick = () => {
    const now = performance.now();
    if ((now - changed >= %[1]d && !pending()) || now - started >= %[2]d) {
      observer.disconnect();
      resolve(Math.round(now - started));
      return;
    }
    setTimeout(tick, %[3]d);
  };
  tick();
})`

func settleExpression(quietMS int) string {
	return fmt.Sprintf(settleScript, quietMS, konst.BrowserDOMQuietMaxMillis, konst.BrowserSettleTickMillis, pendingRequests)
}

func (d *Driver) settle(deadline time.Time, quietMS int) {
	_ = d.value(deadline, false, evaluate(settleExpression(quietMS)), new(int))
}
