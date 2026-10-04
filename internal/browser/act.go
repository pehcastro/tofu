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
	MoveHover    MoveKind = "hover"
	MoveFill     MoveKind = "fill"
	MoveSelect   MoveKind = "select"
	MovePress    MoveKind = "press"
	MoveScroll   MoveKind = "scroll"
	MoveNavigate MoveKind = "navigate"
	MoveBack     MoveKind = "back"
	MoveWait     MoveKind = "wait"
	MoveMotion   MoveKind = "reduced_motion"
)

type Move struct {
	Ref   string
	Kind  MoveKind
	Value string
}

type Moved struct {
	Covered      string `json:"covered,omitempty"`
	Close        string `json:"close,omitempty"`
	Folded       int    `json:"folded,omitempty"`
	Via          string `json:"via,omitempty"`
	SettledMS    int    `json:"settled_ms"`
	Held         string `json:"held,omitempty"`
	Requests     int    `json:"requests,omitempty"`
	Timers       int    `json:"timers,omitempty"`
	Visibility   string `json:"visibility,omitempty"`
	Waited       string `json:"waited,omitempty"`
	Value        string `json:"value,omitempty"`
	Field        string `json:"field,omitempty"`
	ErrorPage    string `json:"error_page,omitempty"`
	Title        string `json:"title,omitempty"`
	URL          string `json:"url,omitempty"`
	Control      string `json:"control,omitempty"`
	Reads        string `json:"reads,omitempty"`
	HiddenLength int    `json:"hidden_length,omitempty"`
	Opened       int    `json:"opened,omitempty"`
	URLChanged   bool   `json:"url_changed"`
	PageChanged  bool   `json:"page_changed"`
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

const watchScript = `(() => {
  const cursor = '[data-tofu-cursor]', nodes = new Set();
  const elementOf = node => node && (node.nodeType === 1 ? node : node.parentElement);
  const ours = node => {
    const element = elementOf(node), host = element && element.getRootNode && element.getRootNode().host;
    return !!(element && element.closest && element.closest(cursor) || host && host.closest(cursor));
  };
  const unshown = element => !element || /^(STYLE|SCRIPT|NOSCRIPT|TEMPLATE)$/.test(element.tagName) || !!element.checkVisibility && !element.checkVisibility();
  const watch = {changed: performance.now(),
    moving: () => !!document.getAnimations && document.getAnimations().some(a => a.playState === 'running' && a.effect && !ours(a.effect.target) && a.effect.getComputedTiming().endTime !== Infinity),
    take(cap) {
      const said = [];
      let size = 0;
      for (const node of nodes) {
        let covered = !node.isConnected || ours(node) || unshown(elementOf(node));
        for (let up = node.parentNode; up && !covered; up = up.parentNode) covered = nodes.has(up);
        const text = covered ? '' : String((node.nodeType === 3 ? node.textContent : node.innerText) || '').replace(/\s+/g, ' ').trim();
        if (!text || said.includes(text)) continue;
        said.push(text);
        if ((size += text.length + 1) >= cap) break;
      }
      nodes.clear();
      return said.join('\n').slice(0, cap);
    }};
  new MutationObserver(records => {
    if (records.length > 0 && records.every(record => ours(record.target))) return;
    watch.changed = performance.now();
    for (const record of records) for (const node of record.type === 'characterData' ? [record.target.parentNode] : record.addedNodes || []) if (node && nodes.size < %[1]d) nodes.add(node);
  }).observe(document, {subtree: true, childList: true, characterData: true, attributes: true});
  return watch;
})()`

const pageHooksScript = `(() => {
  const key = Symbol.for('tofu');
  if (globalThis[key]) return false;
  const noise = ['doubleclick.net', 'googlesyndication.com', 'googletagmanager.com', 'facebook.net', 'analytics', 'ads', 'tracking', 'pixel', 'hotjar.com', 'clarity.ms', 'mixpanel.com', 'segment.com', 'demdex.net', 'omtrdc.net', 'adobedtm.com', 'ensighten.com', 'newrelic.com', 'nr-data.net', 'google-analytics.com', 'connect.facebook.net', 'platform.twitter.com', 'platform.linkedin.com', '.cloudfront.net/image/', '.akamaized.net/image/', '/tracker/', '/collector/', '/beacon/', '/telemetry/', '/log/', '/events/', '/eventBatch', '/track.', '/metrics/'];
  const open = new Map(), ended = new Map();
  let next = 0;
  const begin = target => {
    const url = String((target && target.url) || target), id = next++;
    if (url.startsWith('data:') || noise.some(part => url.includes(part))) return () => {};
    const started = Date.now();
    open.set(id, started);
    return () => { if (open.delete(id)) ended.set(id, [started, Date.now()]); };
  };
  const timers = new Map();
  let inTimer = false;
  Object.defineProperty(globalThis, key, {value: {watch: ` + watchScript + `, requests: (since, longest) => {
    let inFlight = 0, last = -Infinity, waiting = 0;
    for (const started of open.values()) if (started >= since) inFlight++;
    for (const [id, [started, end]] of ended) if (started < since) ended.delete(id); else last = Math.max(last, end);
    for (const [started, delay] of timers.values()) if (started >= since && delay <= longest) waiting++;
    return [inFlight, Date.now() - last, waiting];
  }}});
  const setTimer = globalThis.setTimeout, clearTimer = globalThis.clearTimeout;
  if (setTimer) globalThis.setTimeout = {setTimeout(handler, delay) {
    if (typeof handler !== 'function') return setTimer.apply(this, arguments);
    const chained = inTimer, args = Array.from(arguments);
    let id;
    args[0] = function() {
      timers.delete(id);
      inTimer = true;
      try { return handler.apply(this, arguments); } finally { inTimer = false; }
    };
    id = setTimer.apply(this, args);
    if (!chained) timers.set(id, [Date.now(), Number(delay) || 0]);
    return id;
  }}.setTimeout;
  if (clearTimer) globalThis.clearTimeout = {clearTimeout(id) {
    timers.delete(id);
    return clearTimer.apply(this, arguments);
  }}.clearTimeout;
  const fetch = globalThis.fetch;
  if (fetch) globalThis.fetch = {fetch(target) {
    const answer = fetch.apply(this, arguments), end = begin(target);
    answer.then(end, end);
    return answer;
  }}.fetch;
  const request = globalThis.XMLHttpRequest && XMLHttpRequest.prototype;
  if (!request) return true;
  const urls = new WeakMap(), opened = request.open, send = request.send;
  request.open = {open(method, url) {
    urls.set(this, url);
    return opened.apply(this, arguments);
  }}.open;
  request.send = {send() {
    const end = begin(urls.get(this));
    this.addEventListener('loadend', end);
    try {
      return send.apply(this, arguments);
    } catch (error) {
      end();
      throw error;
    }
  }}.send;
  return true;
})()`

func pageHooks() string {
	return fmt.Sprintf(pageHooksScript, konst.BrowserChangeNodesMax)
}

const pageStateScript = `({url: location.href, count: document.querySelectorAll('*:not([data-tofu-cursor])').length, text: document.body ? document.body.innerText : '',
  status: (performance.getEntries().find(entry => entry.entryType === 'navigation') || {}).responseStatus || 0,
  heading: document.title + '\n' + ((document.querySelector('h1') || {}).innerText || ''),
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
	var answers []cdpAnswer
	if err == nil {
		answers, err = d.cdp(deadline, false, evaluate(pageStateScript), evaluate(pageHooks()))
	}
	if err != nil {
		return page, tabs, err
	}
	if hooked, err := answers[1].object(); err == nil && string(hooked.Value) == "true" {
		_, _ = d.one(deadline, false, cdpCall{Method: "Page.addScriptToEvaluateOnNewDocument", Params: map[string]any{"source": pageHooks()}})
	}
	state, err := answers[0].object()
	if err == nil {
		err = json.Unmarshal(state.Value, &page)
	}
	return page, tabs, err
}

const Unchanged = "the page did not change"

func (m Moved) String() string {
	said := m.said()
	if m.Waited != "" {
		said += ", " + m.Waited
	}
	if m.Via != "" {
		said += ", through " + m.Via + " because the mouse and keys missed"
	}
	switch {
	case m.HiddenLength > 0:
		said += fmt.Sprintf(", %s now reads a value of %d characters", m.Control, m.HiddenLength)
	case m.Control != "":
		said += ", " + m.Control + " now reads " + strconv.Quote(m.Reads)
	}
	if m.Covered != "" {
		return said
	}
	said += fmt.Sprintf(" (settled in %d ms", m.SettledMS)
	if m.Held != "" {
		said += ", cut at the cap, held by " + m.Held
	}
	if m.Visibility != "" {
		said += ", visibilityState " + m.Visibility
	}
	said += ")"
	if m.Requests+m.Timers > 0 {
		said += fmt.Sprintf("; still loading, %d requests and %d timers pending: act on what is here, the next result carries the rest", m.Requests, m.Timers)
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
		return "the url changed to " + m.URL + ", a page titled " + strconv.Quote(m.Title)
	case m.URLChanged:
		return "the url changed to " + m.URL
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

func (d *Driver) TakeChanges() (string, error) {
	var text string
	take := fmt.Sprintf("(%s, globalThis[Symbol.for('tofu')].watch.take(%d))", pageHooks(), konst.BrowserChangesMaxBytes)
	err := d.value(time.Now().Add(konst.BrowserActTimeoutMillis*time.Millisecond), false, evaluate(take), &text)
	return strings.ToValidUTF8(text[:min(len(text), konst.BrowserChangesMaxBytes)], ""), err
}

func (d *Driver) Do(move Move) (Moved, error) {
	budget := konst.BrowserActTimeoutMillis * time.Millisecond
	if move.Kind == MoveWait {
		budget += konst.BrowserWaitMaxMillis * time.Millisecond
	}
	moved, err := d.do(time.Now().Add(budget), move)
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return Moved{}, fmt.Errorf("%s on tab %d did not finish within %d ms, and tofu did not retry it: %w", move.Kind, d.Tab, budget.Milliseconds(), err)
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
	case MoveHover:
		var x, y float64
		if _, x, y, err = d.center(deadline, move.Ref); err == nil {
			err = d.act(deadline, mouse("mouseMoved", "none", x, y))
		}
	case MoveFill:
		var read fieldRead
		var path string
		read, path, err = d.fill(deadline, move.Ref, move.Value)
		moved.Value = read.Value
		switch moved.Field = "filled through " + path; {
		case read.Length == 0:
			moved.Field = "field still empty after insertText, typed keys and the native value setter"
		case read.Same:
		case read.Hidden:
			moved.Field = fmt.Sprintf("the field reads a value of %d characters, not the %d typed, after insertText, typed keys and the native value setter", read.Length, read.Typed)
		default:
			moved.Field = "the field reads " + strconv.Quote(read.Value) + " after insertText, typed keys and the native value setter"
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
	case MoveMotion:
		moved.Field, err = d.reduceMotion(deadline, move.Value, tabsBefore)
	default:
		return Moved{}, fmt.Errorf("there is no browser move %q", move.Kind)
	}
	if err != nil || moved.Covered != "" {
		return moved, err
	}
	if d.navigates(deadline, move) {
		d.awaitNavigation(deadline, before, tabsBefore, started)
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
	if err == nil && !moved.URLChanged && moved.Opened == 0 {
		moved.Control, moved.Reads, moved.HiddenLength = d.reads(deadline, move)
	}
	return moved, err
}

const hiddenField = `(this.type === 'password' || /\b(current|new)-password\b/.test(this.autocomplete ?? ''))`

const valueState = `function() {
  if ` + hiddenField + ` return this.value.length;
  return this.tagName === 'SELECT' ? Array.from(this.selectedOptions, option => option.label.trim()).join(', ') : this.value ?? this.textContent;
}`

func (d *Driver) reads(deadline time.Time, move Move) (control, reads string, hiddenLength int) {
	script := valueState
	switch move.Kind {
	case MoveClick:
		script = checkedState
	case MoveFill, MoveSelect:
	default:
		return "", "", 0
	}
	object, err := d.resolve(deadline, move.Ref)
	var read any
	if err != nil || d.value(deadline, false, callOn(object, script, true), &read) != nil {
		return "", "", 0
	}
	switch read := read.(type) {
	case string:
		return move.Ref, read, 0
	case float64:
		return move.Ref, "", int(read)
	}
	return "", "", 0
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

func (d *Driver) awaitNavigation(deadline time.Time, before pageState, tabsBefore []Tab, started time.Time) {
	for until := time.Now().Add(konst.BrowserDOMQuietMaxMillis * time.Millisecond); time.Now().Before(until); time.Sleep(konst.BrowserSettleTickMillis * time.Millisecond) {
		after, tabs, err := d.state(deadline)
		switch {
		case err != nil:
		case after.URL != before.URL || len(tabs) > len(tabsBefore):
			return
		case after != before:
			_, _ = d.settle(deadline, konst.BrowserDOMQuietMillis*time.Millisecond, konst.BrowserDOMQuietMaxMillis*time.Millisecond, before.URL, started)
			return
		}
	}
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
	loads := move.Kind == MoveNavigate || move.Kind == MoveBack
	quiet, limit := time.Duration(0), konst.BrowserDOMQuietMaxMillis*time.Millisecond
	if loads {
		quiet = konst.BrowserDOMQuietMillis * time.Millisecond
	}
	millis, notMillis := strconv.Atoi(move.Value)
	timed := move.Kind == MoveWait && notMillis == nil
	if timed {
		quiet, limit = konst.BrowserDOMQuietMillis*time.Millisecond, time.Duration(min(max(millis, 0), konst.BrowserWaitMaxMillis))*time.Millisecond
	}
	settled, settleErr := d.settle(deadline, quiet, limit, before.URL, started)
	switch {
	case timed && settleErr != nil:
		time.Sleep(time.Until(started.Add(limit)))
	case timed && settled.AtOnce:
		moved.Waited = "the wait returned at once, the page was already quiet"
	case timed && settled.Held == "":
		moved.Waited = "the wait ended early, the page went quiet"
	}
	after, tabsAfter, err := d.state(deadline)
	arrived := err == nil && (after.URL != before.URL || loads)
	if arrived && !timed && (settleErr != nil || settled.Took < konst.BrowserDOMQuietMillis*time.Millisecond) {
		if again, err := d.settle(deadline, konst.BrowserDOMQuietMillis*time.Millisecond, limit, before.URL, started); err == nil {
			settled = again
		}
		after, tabsAfter, err = d.state(deadline)
	}
	if err != nil {
		return moved, err
	}
	moved.SettledMS = int(time.Since(started).Milliseconds())
	moved.URL, moved.URLChanged, moved.PageChanged = after.URL, after.URL != before.URL, after != before
	moved.Held, moved.Visibility = settled.Held, settled.Visibility
	if arrived {
		moved.Requests, moved.Timers = settled.Requests, settled.Timers
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
  const state = box.type === 'checkbox' || box.type === 'radio' ? String(box.checked) : box.getAttribute ? box.getAttribute('aria-checked') : null;
  return state === 'true' ? 'checked' : state === 'false' ? 'unchecked' : state;
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

type fieldRead struct {
	Value  string `json:"value"`
	Hidden bool   `json:"hidden"`
	Length int    `json:"length"`
	Typed  int    `json:"typed"`
	Same   bool   `json:"same"`
}

const readBack = `function(text) {
  const value = this.value ?? this.textContent ?? '', hidden = ` + hiddenField + `;
  return {value: hidden ? '' : value, hidden, length: value.length, typed: text.length, same: value === text};
}`

func (d *Driver) fill(deadline time.Time, ref, text string) (read fieldRead, path string, err error) {
	object, err := d.resolve(deadline, ref)
	if err != nil {
		return read, "", err
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
	for _, try := range []struct {
		path  string
		calls []cdpCall
	}{
		{"insertText", append(slices.Clone(emptied), cdpCall{Method: "Input.insertText", Params: map[string]any{"text": text}})},
		{"typed keys after a click", typed},
		{"the native value setter", []cdpCall{callOn(object, nativeValue, true, text)}},
	} {
		if err = d.act(deadline, try.calls...); err == nil {
			err = d.value(deadline, false, callOn(object, readBack, true, text), &read)
		}
		if err != nil || read.Same {
			return read, try.path, err
		}
	}
	return read, "", nil
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
	if d.opened(tabs) {
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

func (d *Driver) opened(tabs []Tab) bool {
	return slices.ContainsFunc(tabs, func(tab Tab) bool { return tab.ID == d.Tab && tab.Opened })
}

func (d *Driver) reduceMotion(deadline time.Time, value string, tabs []Tab) (string, error) {
	preference, known := map[string]string{"on": "reduce", "off": ""}[value]
	switch {
	case !known:
		return "", fmt.Errorf("reduced_motion takes on or off, not %q", value)
	case !d.opened(tabs):
		return "", fmt.Errorf("tab %d is the person's, and tofu sets reduced motion only in a tab it opened", d.Tab)
	}
	features := []map[string]string{{"name": "prefers-reduced-motion", "value": preference}}
	return "the tab now prefers reduced motion " + value, d.act(deadline, cdpCall{Method: "Emulation.setEmulatedMedia", Params: map[string]any{"features": features}})
}

func (d *Driver) wait(deadline time.Time, value string) error {
	if _, err := strconv.Atoi(value); err == nil {
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

const probeScript = `(%s, (() => {
  const hooks = globalThis[Symbol.for('tofu')], [inFlight, sinceEnd, timers] = hooks.requests(%d, %d), main = %t && (document.querySelector('main, [role=main]') || document.body);
  return {href: location.href, ready: document.readyState, count: document.getElementsByTagName('*').length, text: !!main && /\S/.test(main.innerText || ''), inFlight,
    sinceEnd: sinceEnd === Infinity ? null : Math.round(sinceEnd), timers, quietFor: Math.round(performance.now() - hooks.watch.changed), moving: %t && hooks.watch.moving(), visibility: document.visibilityState};
})())`

type probe struct {
	Href       string `json:"href"`
	Ready      string `json:"ready"`
	Count      int    `json:"count"`
	Text       bool   `json:"text"`
	InFlight   int    `json:"inFlight"`
	SinceEnd   *int   `json:"sinceEnd"`
	Timers     int    `json:"timers"`
	QuietFor   int    `json:"quietFor"`
	Moving     bool   `json:"moving"`
	Visibility string `json:"visibility"`
}

type settled struct {
	Held             string
	Requests, Timers int
	Visibility       string
	AtOnce           bool
	Took             time.Duration
}

func (d *Driver) settle(deadline time.Time, quiet, limit time.Duration, from string, since time.Time) (settled, error) {
	begun, hooks := time.Now(), pageHooks()
	var committed, grown, moving time.Time
	count, texted := 0, false
	for first := true; ; first = false {
		var page probe
		err := d.value(deadline, false, evaluate(fmt.Sprintf(probeScript, hooks, since.UnixMilli(), konst.BrowserTimerCountMaxMillis, !texted, quiet > 0)), &page)
		if err == nil && page.Ready == "" {
			err = errors.New("the page answered the settle probe without a readyState")
		}
		if err != nil {
			return settled{}, err
		}
		now, away := time.Now(), page.Href != from
		hush := quiet
		if away || page.SinceEnd != nil {
			hush = max(quiet, konst.BrowserDOMQuietMillis*time.Millisecond)
		}
		if page.Moving {
			moving = now
		}
		over := now.Sub(begun) >= limit
		if away {
			if committed.IsZero() {
				committed, grown, count = now, now, page.Count
			}
			if page.Count > count {
				grown = now
			}
			count, texted, over = page.Count, texted || page.Text, now.Sub(committed) >= konst.BrowserCommitReturnMillis*time.Millisecond
		}
		held := ""
		switch {
		case away && page.Ready == "loading":
			held = "readyState"
		case away && !texted:
			held = "text"
		case away && now.Sub(grown) < konst.BrowserDOMQuietMillis*time.Millisecond:
			held = "growth"
		case page.InFlight > 0 || page.SinceEnd != nil && time.Duration(*page.SinceEnd)*time.Millisecond < hush:
			held = "requests"
		case away:
		case page.Ready != "complete":
			held = "readyState"
		case time.Duration(page.QuietFor)*time.Millisecond < hush:
			held = "changes"
		case now.Sub(moving) < hush:
			held = "animation"
		}
		if held == "" || over {
			return settled{Held: held, Requests: page.InFlight, Timers: page.Timers, Visibility: page.Visibility, AtOnce: first && held == "", Took: now.Sub(begun)}, nil
		}
		time.Sleep(konst.BrowserSettleTickMillis * time.Millisecond)
	}
}
