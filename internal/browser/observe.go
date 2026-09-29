package browser

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
)

const cursorTag = "data-tofu-ci"

const cursorScan = `(() => {
  const results = [];
  if (!document.body) return results;
  const interactiveRoles = ['button', 'link', 'textbox', 'checkbox', 'radio', 'combobox', 'listbox', 'menuitem', 'menuitemcheckbox', 'menuitemradio', 'option', 'searchbox', 'slider', 'spinbutton', 'switch', 'tab', 'treeitem'];
  const interactiveTags = ['a', 'button', 'input', 'select', 'textarea', 'details', 'summary'];
  const scrolls = style => [style.overflow, style.overflowX, style.overflowY].some(value => ['auto', 'scroll', 'overlay'].includes(value));
  for (const el of document.body.querySelectorAll('*')) {
    if (el.closest('[hidden], [aria-hidden="true"]')) continue;
    const style = getComputedStyle(el);
    const plain = !interactiveTags.includes(el.tagName.toLowerCase()) && !interactiveRoles.includes((el.getAttribute('role') || '').toLowerCase());
    const isScrollable = (el.scrollHeight > el.clientHeight + 1 || el.scrollWidth > el.clientWidth + 1) && scrolls(style);
    const hasCursorPointer = plain && style.cursor === 'pointer';
    const hasOnClick = plain && (el.hasAttribute('onclick') || el.onclick !== null);
    const tabIndex = el.getAttribute('tabindex');
    const hasTabIndex = plain && tabIndex !== null && tabIndex !== '-1';
    const editable = el.getAttribute('contenteditable');
    const isEditable = plain && (editable === '' || editable === 'true');
    if (!hasCursorPointer && !hasOnClick && !hasTabIndex && !isEditable && !isScrollable) continue;
    const inherited = hasCursorPointer && !hasOnClick && !hasTabIndex && !isEditable && el.parentElement && getComputedStyle(el.parentElement).cursor === 'pointer';
    if (inherited && !isScrollable) continue;
    const rect = el.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) continue;
    let hiddenInputType = null, hiddenInputChecked = null;
    const hiddenInput = el.querySelector('input[type="radio"], input[type="checkbox"]');
    if (hiddenInput) {
      const inputStyle = getComputedStyle(hiddenInput);
      if (inputStyle.display === 'none' || inputStyle.visibility === 'hidden' || hiddenInput.hidden) {
        hiddenInputType = hiddenInput.type;
        hiddenInputChecked = hiddenInput.indeterminate ? 'mixed' : String(hiddenInput.checked);
      }
    }
    el.setAttribute('%[1]s', String(results.length));
    results.push({text: (el.textContent || '').trim().slice(0, %[2]d), hasOnClick, hasCursorPointer, hasTabIndex, isEditable, isScrollable, hiddenInputType, hiddenInputChecked});
  }
  return results;
})()`

const cursorCleanup = `document.querySelectorAll('[%[1]s]').forEach(el => el.removeAttribute('%[1]s'))`

type frameTree struct {
	Frame struct {
		ID       string `json:"id"`
		LoaderID string `json:"loaderId"`
		URL      string `json:"url"`
	} `json:"frame"`
	ChildFrames []frameTree `json:"childFrames"`
}

func (f frameTree) loaders(into map[string]string) {
	into[f.Frame.ID] = f.Frame.LoaderID
	for _, child := range f.ChildFrames {
		child.loaders(into)
	}
}

func (d *Driver) Observe(interactive bool) (string, error) {
	snapshot, err := d.observe(time.Now().Add(konst.BrowserObserveTimeoutMillis*time.Millisecond), interactive)
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return "", fmt.Errorf("observe on tab %d did not finish within %d ms", d.Tab, konst.BrowserObserveTimeoutMillis)
	}
	if err == nil && interactive {
		d.refs.shown = snapshot
	}
	return snapshot, err
}

func (d *Driver) ObserveChanges() (string, error) {
	before := d.refs.shown
	snapshot, err := d.Observe(true)
	if err != nil || before == "" {
		return snapshot, err
	}
	return changes(before, snapshot), nil
}

func changes(before, after string) string {
	refLine := regexp.MustCompile(`ref=(e\d+)[,\]]`)
	lines := func(snapshot string) ([]string, map[string]string) {
		var order []string
		byRef := map[string]string{}
		for line := range strings.Lines(snapshot) {
			if found := refLine.FindStringSubmatch(line); found != nil {
				order = append(order, found[1])
				byRef[found[1]] = strings.TrimSpace(line)
			}
		}
		return order, byRef
	}
	oldOrder, old := lines(before)
	newOrder, now := lines(after)
	var shown []string
	for _, ref := range newOrder {
		switch was, known := old[ref]; {
		case !known:
			shown = append(shown, "+ "+now[ref])
		case was != now[ref]:
			shown = append(shown, "~ "+now[ref])
		}
	}
	for _, ref := range oldOrder {
		if _, kept := now[ref]; !kept {
			shown = append(shown, "x gone: "+old[ref])
		}
	}
	if len(shown)*100 >= konst.BrowserDeltaWholePercent*max(len(newOrder), 1) {
		return after
	}
	header, _, _ := strings.Cut(after, "\n")
	if len(shown) == 0 {
		return header + "\nnothing changed since the last snapshot; every ref in it still stands\n"
	}
	return header + fmt.Sprintf("\nchanged since the last snapshot, %d of %d refs; every other ref still stands (+ new, ~ changed, x gone):\n", len(shown), len(newOrder)) + strings.Join(shown, "\n") + "\n"
}

func (d *Driver) observe(deadline time.Time, interactive bool) (string, error) {
	answers, err := d.cdp(deadline, false, cdpCall{Method: "Page.getFrameTree"}, cdpCall{Method: "Accessibility.getFullAXTree"},
		evaluate(fmt.Sprintf(cursorScan, cursorTag, konst.BrowserCursorTextRunes)))
	if err != nil {
		return "", err
	}
	var frames struct {
		FrameTree frameTree `json:"frameTree"`
	}
	var tree struct {
		Nodes []axNode `json:"nodes"`
	}
	var found []cursorInfo
	scanned, err := answers[2].object()
	if err == nil {
		err = errors.Join(answers[0].into(&frames), answers[1].into(&tree), json.Unmarshal(scanned.Value, &found))
	}
	var cursors map[int]cursorInfo
	if err == nil {
		cursors, err = d.cursors(deadline, found)
	}
	if err != nil {
		return "", err
	}
	loaders := map[string]string{}
	frames.FrameTree.loaders(loaders)
	d.refs.entries = map[string]refEntry{}
	top, roots := d.refs.snapshot("", frames.FrameTree.Frame.LoaderID, tree.Nodes, cursors)
	children, err := d.childFrames(deadline, top, loaders, cursors, interactive)
	if err != nil {
		return "", err
	}
	if err := d.linkURLs(deadline, top, frames.FrameTree.Frame.URL); err != nil {
		return "", err
	}
	title := ""
	if len(roots) > 0 {
		title = top[roots[0]].name
	}
	var out strings.Builder
	fmt.Fprintf(&out, "tab %d %s %s\n", d.Tab, frames.FrameTree.Frame.URL, strconv.Quote(title))
	all := view{interactive: interactive, frames: children, skip: -1}
	modal := slices.IndexFunc(tree.Nodes, axNode.isModal)
	if modal < 0 || top[modal].role == "" {
		for _, root := range roots {
			render(&out, top, root, 0, all)
		}
		return out.String(), nil
	}
	render(&out, top, modal, 0, all)
	fmt.Fprintf(&out, "behind dialog %s, no ref here acts until it closes:\n", strconv.Quote(top[modal].name))
	behind := all
	behind.behind, behind.skip = true, modal
	for _, root := range roots {
		render(&out, top, root, 1, behind)
	}
	inside := map[int]bool{}
	for queue := []int{modal}; len(queue) > 0; queue = append(queue[1:], top[queue[0]].children...) {
		inside[queue[0]] = true
	}
	for i, node := range top {
		if node.ref != "" && !inside[i] {
			delete(d.refs.entries, node.ref)
		}
	}
	return out.String(), nil
}

func (n axNode) isModal() bool {
	role := n.Role.text()
	if role != "dialog" && role != "alertdialog" {
		return false
	}
	for _, property := range n.Properties {
		if property.Name == "modal" && property.Value.text() == "true" {
			return true
		}
	}
	return false
}

func (d *Driver) linkURLs(deadline time.Time, top []treeNode, page string) error {
	var links []int
	var calls []cdpCall
	for i, node := range top {
		if node.role == "link" && node.ref != "" && node.backend != 0 {
			links = append(links, i)
			calls = append(calls, byBackend("DOM.resolveNode", node.backend))
		}
	}
	if len(calls) == 0 {
		return nil
	}
	answers, err := d.cdp(deadline, false, calls...)
	if err != nil {
		return err
	}
	var owners []int
	calls = nil
	for n, answer := range answers {
		var resolved struct {
			Object remoteObject `json:"object"`
		}
		if answer.into(&resolved) == nil && resolved.Object.ObjectID != "" {
			owners = append(owners, links[n])
			calls = append(calls, callOn(resolved.Object.ObjectID, "function() { return this.href || ''; }", true))
		}
	}
	if len(calls) == 0 {
		return nil
	}
	if answers, err = d.cdp(deadline, false, calls...); err != nil {
		return err
	}
	base, _ := url.Parse(page)
	for n, answer := range answers {
		var href string
		object, err := answer.object()
		if err == nil && json.Unmarshal(object.Value, &href) == nil && href != "" {
			top[owners[n]].url = shortURL(href, base)
		}
	}
	return nil
}

func shortURL(href string, page *url.URL) string {
	link, err := url.Parse(href)
	if err != nil {
		return href
	}
	short := link.EscapedPath()
	if page == nil || link.Host != page.Host {
		short = link.Scheme + "://" + link.Host + short
	}
	if query := []rune(link.RawQuery); len(query) > 0 {
		short += "?" + string(query[:min(len(query), konst.BrowserURLQueryRunes)])
	}
	return short
}

func (d *Driver) cursors(deadline time.Time, found []cursorInfo) (map[int]cursorInfo, error) {
	cursors := map[int]cursorInfo{}
	if len(found) == 0 {
		return cursors, nil
	}
	var root struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	var tagged struct {
		NodeIDs []int `json:"nodeIds"`
	}
	answer, err := d.one(deadline, false, cdpCall{Method: "DOM.getDocument", Params: map[string]any{"depth": 0}})
	if err == nil {
		err = answer.into(&root)
	}
	if err == nil {
		answer, err = d.one(deadline, false, cdpCall{Method: "DOM.querySelectorAll", Params: map[string]any{"nodeId": root.Root.NodeID, "selector": "[" + cursorTag + "]"}})
	}
	if err == nil {
		err = answer.into(&tagged)
	}
	if err != nil {
		return nil, err
	}
	var calls []cdpCall
	for _, id := range tagged.NodeIDs {
		calls = append(calls, cdpCall{Method: "DOM.describeNode", Params: map[string]any{"nodeId": id}})
	}
	answers, err := d.cdp(deadline, false, append(calls, evaluate(fmt.Sprintf(cursorCleanup, cursorTag)))...)
	if err != nil {
		return nil, err
	}
	for _, answer := range answers[:len(calls)] {
		var described struct {
			Node struct {
				BackendNodeID int      `json:"backendNodeId"`
				Attributes    []string `json:"attributes"`
			} `json:"node"`
		}
		if answer.into(&described) != nil {
			continue
		}
		attributes := described.Node.Attributes
		if at := slices.Index(attributes, cursorTag); at >= 0 && at+1 < len(attributes) {
			if index, err := strconv.Atoi(attributes[at+1]); err == nil && index < len(found) {
				cursors[described.Node.BackendNodeID] = found[index]
			}
		}
	}
	return cursors, nil
}

func (d *Driver) childFrames(deadline time.Time, top []treeNode, loaders map[string]string, cursors map[int]cursorInfo, interactive bool) (map[int]string, error) {
	var owners []int
	var calls []cdpCall
	for i, node := range top {
		if node.role == "Iframe" && node.ref != "" && node.backend != 0 {
			owners = append(owners, i)
			calls = append(calls, cdpCall{Method: "DOM.describeNode", Params: map[string]any{"backendNodeId": node.backend, "depth": 1}})
		}
	}
	rendered := map[int]string{}
	if len(calls) == 0 {
		return rendered, nil
	}
	answers, err := d.cdp(deadline, false, calls...)
	if err != nil {
		return nil, err
	}
	var frameOwners []int
	var frameIDs []string
	calls = nil
	for n, answer := range answers {
		var described struct {
			Node struct {
				FrameID         string `json:"frameId"`
				ContentDocument struct {
					FrameID string `json:"frameId"`
				} `json:"contentDocument"`
			} `json:"node"`
		}
		if answer.into(&described) != nil {
			continue
		}
		if frame := cmp.Or(described.Node.ContentDocument.FrameID, described.Node.FrameID); frame != "" {
			frameOwners, frameIDs = append(frameOwners, owners[n]), append(frameIDs, frame)
			calls = append(calls, cdpCall{Method: "Accessibility.getFullAXTree", Params: map[string]any{"frameId": frame}})
		}
	}
	if len(calls) == 0 {
		return rendered, nil
	}
	if answers, err = d.cdp(deadline, false, calls...); err != nil {
		return nil, err
	}
	for n, answer := range answers {
		var tree struct {
			Nodes []axNode `json:"nodes"`
		}
		if answer.into(&tree) != nil {
			continue
		}
		nodes, roots := d.refs.snapshot(frameIDs[n], loaders[frameIDs[n]], tree.Nodes, cursors)
		var out strings.Builder
		for _, root := range roots {
			render(&out, nodes, root, 0, view{interactive: interactive, skip: -1})
		}
		rendered[frameOwners[n]] = out.String()
	}
	return rendered, nil
}
