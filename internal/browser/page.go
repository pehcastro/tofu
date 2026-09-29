package browser

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"tofu/internal/konst"
)

type Role string

const (
	RoleButton        Role = "button"
	RoleLink          Role = "link"
	RoleCheckbox      Role = "checkbox"
	RoleRadio         Role = "radio"
	RoleSwitch        Role = "switch"
	RoleTab           Role = "tab"
	RoleMenuItem      Role = "menuitem"
	RoleMenuItemRadio Role = "menuitemradio"
	RoleOption        Role = "option"
	RoleGridCell      Role = "gridcell"
	RoleCombobox      Role = "combobox"
	RoleTextbox       Role = "textbox"
	RoleSearchbox     Role = "searchbox"
	RoleSpinbutton    Role = "spinbutton"
	RoleSelect        Role = "select"
)

type Op int

const (
	OpClick Op = iota
	OpTypeText
	OpSelect
	OpScrollUp
	OpScrollDown
	OpWait
	OpDone
	OpBlocked
)

func (o Op) String() string {
	switch o {
	case OpClick:
		return "CLICK"
	case OpTypeText:
		return "TYPE_TEXT"
	case OpSelect:
		return "SELECT"
	case OpScrollUp:
		return "SCROLL_UP"
	case OpScrollDown:
		return "SCROLL_DOWN"
	case OpWait:
		return "WAIT"
	case OpDone:
		return "DONE"
	case OpBlocked:
		return "BLOCKED"
	}
	panic(fmt.Sprintf("browser: unknown op %d", int(o)))
}

func ParseOp(name string) (Op, error) {
	for op := OpClick; op <= OpBlocked; op++ {
		if op.String() == name {
			return op, nil
		}
	}
	return 0, fmt.Errorf("unknown browser op %q", name)
}

func (o Op) Accepts(role Role) bool {
	switch o {
	case OpClick:
		switch role {
		case RoleButton, RoleLink, RoleCheckbox, RoleRadio, RoleSwitch, RoleTab, RoleMenuItem, RoleMenuItemRadio,
			RoleOption, RoleGridCell, RoleCombobox, RoleTextbox, RoleSearchbox, RoleSpinbutton:
			return true
		}
		return false
	case OpTypeText:
		return role == RoleTextbox || role == RoleSearchbox || role == RoleSpinbutton || role == RoleCombobox
	case OpSelect:
		return role == RoleSelect
	case OpScrollUp, OpScrollDown, OpWait, OpDone, OpBlocked:
		return false
	}
	panic(fmt.Sprintf("browser: unknown op %d", int(o)))
}

type SelectOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type Element struct {
	Index    int            `json:"index"`
	Role     Role           `json:"role"`
	Label    string         `json:"label"`
	Value    string         `json:"value"`
	Input    string         `json:"input,omitempty"`
	Checked  string         `json:"checked,omitempty"`
	Selected string         `json:"selected,omitempty"`
	Expanded string         `json:"expanded,omitempty"`
	ReadOnly bool           `json:"readonly,omitempty"`
	Options  []SelectOption `json:"options,omitempty"`
}

type Scroll struct {
	Up   bool `json:"up"`
	Down bool `json:"down"`
}

type Page struct {
	URL         string           `json:"url"`
	Title       string           `json:"title"`
	Text        string           `json:"text"`
	Fingerprint string           `json:"fingerprint"`
	Scroll      Scroll           `json:"scroll"`
	Elements    []Element        `json:"elements"`
	Guards      map[int]string   `json:"guards"`
	Names       map[int][]string `json:"names"`
	Links       map[int]string   `json:"links"`
}

func (p Page) Element(index int) (Element, bool) {
	for _, element := range p.Elements {
		if element.Index == index {
			return element, true
		}
	}
	return Element{}, false
}

type Action struct {
	Op      Op
	Element int
	Value   string
}

func ParsePage(raw []byte) (Page, error) {
	var page Page
	if err := json.Unmarshal(raw, &page); err != nil {
		return Page{}, fmt.Errorf("the snapshot is not a page: %w", err)
	}
	if page.Fingerprint == "" {
		return Page{}, fmt.Errorf("the snapshot of %s carries no fingerprint", page.URL)
	}
	if text := []rune(page.Text); len(text) > konst.BrowserPageTextRunes {
		page.Text = string(text[:konst.BrowserPageTextRunes])
	}
	offered := make([]Element, 0, len(page.Elements))
	for _, element := range page.Elements {
		if !OpClick.Accepts(element.Role) && !OpSelect.Accepts(element.Role) {
			return Page{}, fmt.Errorf("element %d has the unknown role %q", element.Index, element.Role)
		}
		switch element.Input {
		case "password", "file", "hidden":
			continue
		}
		offered = append(offered, element)
	}
	page.Elements = offered[:min(len(offered), konst.BrowserElementCeiling)]
	return page, nil
}

type axValue struct {
	Value any `json:"value"`
}

func (v *axValue) text() string {
	if v == nil {
		return ""
	}
	switch value := v.Value.(type) {
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(value)
	}
	return ""
}

type axNode struct {
	NodeID     string   `json:"nodeId"`
	Ignored    bool     `json:"ignored"`
	Role       *axValue `json:"role"`
	Name       *axValue `json:"name"`
	Value      *axValue `json:"value"`
	Properties []struct {
		Name  string  `json:"name"`
		Value axValue `json:"value"`
	} `json:"properties"`
	ChildIDs []string `json:"childIds"`
	Backend  int      `json:"backendDOMNodeId"`
}

func (n axNode) attrs() []string {
	var attrs []string
	for _, name := range []string{"level", "checked", "expanded", "selected", "disabled", "required"} {
		for _, property := range n.Properties {
			value := property.Value.text()
			switch {
			case property.Name != name:
			case name == "level" || name == "checked" || name == "expanded":
				attrs = append(attrs, name+"="+value)
			case value == "true":
				attrs = append(attrs, name)
			}
		}
	}
	return attrs
}

type cursorInfo struct {
	Text               string `json:"text"`
	HasOnClick         bool   `json:"hasOnClick"`
	HasCursorPointer   bool   `json:"hasCursorPointer"`
	HasTabIndex        bool   `json:"hasTabIndex"`
	IsEditable         bool   `json:"isEditable"`
	IsScrollable       bool   `json:"isScrollable"`
	HiddenInputType    string `json:"hiddenInputType"`
	HiddenInputChecked string `json:"hiddenInputChecked"`
}

func (c cursorInfo) describe() string {
	var hints []string
	for _, hint := range []struct {
		on   bool
		name string
	}{{c.HasCursorPointer, "cursor:pointer"}, {c.HasOnClick, "onclick"}, {c.HasTabIndex, "tabindex"}, {c.IsEditable, "contenteditable"}} {
		if hint.on {
			hints = append(hints, hint.name)
		}
	}
	described := ""
	switch {
	case c.HasCursorPointer || c.HasOnClick:
		described = " clickable"
	case c.IsEditable:
		described = " editable"
	case c.HasTabIndex:
		described = " focusable"
	}
	if len(hints) > 0 {
		described += " [" + strings.Join(hints, ", ") + "]"
	}
	if c.IsScrollable {
		described += " scrollable"
	}
	return described
}

type treeNode struct {
	role, name, value string
	attrs             []string
	backend           int
	children          []int
	ref, url          string
	fresh             bool
	cursor            *cursorInfo
}

type view struct {
	interactive, behind bool
	frames              map[int]string
	skip                int
}

func buildTree(nodes []axNode) ([]treeNode, []int) {
	tree := make([]treeNode, len(nodes))
	index := map[string]int{}
	for i, node := range nodes {
		index[node.NodeID] = i
		role := node.Role.text()
		if (node.Ignored && role != "RootWebArea") || role == "InlineTextBox" {
			continue
		}
		tree[i] = treeNode{role: role, name: node.Name.text(), value: node.Value.text(), attrs: node.attrs(), backend: node.Backend}
	}
	isChild := make([]bool, len(nodes))
	for i, node := range nodes {
		for _, id := range node.ChildIDs {
			if child, found := index[id]; found {
				tree[i].children = append(tree[i].children, child)
				isChild[child] = true
			}
		}
	}
	for i := range tree {
		kids := tree[i].children
		if tree[i].role == "" {
			continue
		}
		for start := 0; start < len(kids); {
			end := start + 1
			for tree[kids[start]].role == "StaticText" && end < len(kids) && tree[kids[end]].role == "StaticText" {
				tree[kids[start]].name += tree[kids[end]].name
				tree[kids[end]] = treeNode{}
				end++
			}
			start = end
		}
		if len(kids) == 1 && tree[kids[0]].role == "StaticText" && tree[i].name == tree[kids[0]].name {
			tree[kids[0]] = treeNode{}
		}
	}
	var roots []int
	for i, child := range isChild {
		if !child {
			roots = append(roots, i)
		}
	}
	return tree, roots
}

func refRole(role string, named bool) bool {
	switch role {
	case "button", "link", "textbox", "checkbox", "radio", "combobox", "listbox", "menuitem", "menuitemcheckbox", "menuitemradio",
		"option", "searchbox", "slider", "spinbutton", "switch", "tab", "treeitem", "Iframe":
		return true
	case "heading", "cell", "gridcell", "columnheader", "rowheader", "listitem", "article", "region", "main", "navigation":
		return named
	}
	return false
}

type refEntry struct {
	backend    int
	role, name string
	nth        int
	frame      string
}

type document struct {
	loader string
	refs   map[int]string
}

type refMap struct {
	entries   map[string]refEntry
	documents map[string]*document
	next      int
}

func (r *refMap) document(frame, loader string) (*document, bool) {
	if known := r.documents[frame]; known != nil && loader != "" && known.loader == loader {
		return known, false
	}
	if frame == "" || r.documents == nil {
		r.documents = map[string]*document{}
	}
	delete(r.documents, frame)
	if loader == "" {
		return nil, false
	}
	r.documents[frame] = &document{loader: loader, refs: map[int]string{}}
	return r.documents[frame], true
}

func (r *refMap) snapshot(frame, loader string, nodes []axNode, cursors map[int]cursorInfo) ([]treeNode, []int) {
	tree, roots := buildTree(nodes)
	doc, fresh := r.document(frame, loader)
	seen := map[string]int{}
	for i := range tree {
		node := &tree[i]
		cursor, scanned := cursors[node.backend]
		if scanned {
			node.cursor = &cursor
			if (node.role == "LabelText" || node.role == "generic") && (cursor.HiddenInputType == "radio" || cursor.HiddenInputType == "checkbox") {
				node.role = cursor.HiddenInputType
				node.name = cmp.Or(node.name, cursor.Text)
				if cursor.HiddenInputChecked != "" {
					node.attrs = append(node.attrs, "checked="+cursor.HiddenInputChecked)
				}
			}
		}
		if node.role == "" || !refRole(node.role, node.name != "") && !scanned {
			node.cursor = nil
			continue
		}
		key := node.role + ":" + node.name
		r.entries[r.allocate(doc, fresh, node)] = refEntry{backend: node.backend, role: node.role, name: node.name, nth: seen[key], frame: frame}
		seen[key]++
	}
	return tree, roots
}

func (r *refMap) allocate(doc *document, fresh bool, node *treeNode) string {
	if doc != nil && node.backend != 0 {
		if ref, known := doc.refs[node.backend]; known {
			node.ref = ref
			return ref
		}
	}
	r.next++
	node.ref = "e" + strconv.Itoa(r.next)
	if doc != nil && node.backend != 0 {
		doc.refs[node.backend] = node.ref
		node.fresh = !fresh
	}
	return node.ref
}

func snapshotName(name string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case 0xa0:
			return ' '
		case 0xfeff, 0x200b, 0x200c, 0x200d, 0x2060:
			return -1
		}
		return r
	}, name)
}

func render(out *strings.Builder, tree []treeNode, i, indent int, v view) {
	node := tree[i]
	if i == v.skip {
		return
	}
	passThrough := node.role == "" || node.role == "RootWebArea" || node.role == "WebArea" ||
		node.role == "generic" && node.ref == "" && len(node.children) <= 1 ||
		node.role == "StaticText" && strings.TrimSpace(snapshotName(node.name)) == "" ||
		v.interactive && node.ref == ""
	if passThrough {
		for _, child := range node.children {
			render(out, tree, child, indent, v)
		}
		return
	}
	bullet := "- "
	if node.fresh && !v.behind {
		bullet = "* "
	}
	out.WriteString(strings.Repeat("  ", indent) + bullet + node.role)
	name := node.name
	if name == "" && v.interactive && node.cursor != nil {
		name = node.cursor.Text
	}
	if name != "" {
		out.WriteString(" " + strconv.Quote(snapshotName(name)))
	}
	attrs := slices.Clone(node.attrs)
	if node.ref != "" && !v.behind {
		attrs = append(attrs, "ref="+node.ref)
	}
	if node.url != "" {
		attrs = append(attrs, "url="+node.url)
	}
	if len(attrs) > 0 {
		out.WriteString(" [" + strings.Join(attrs, ", ") + "]")
	}
	if node.cursor != nil {
		out.WriteString(node.cursor.describe())
	}
	if node.value != "" && node.value != node.name {
		out.WriteString(": " + node.value)
	}
	out.WriteString("\n")
	for _, line := range strings.SplitAfter(v.frames[i], "\n") {
		if line != "" {
			out.WriteString(strings.Repeat("  ", indent+1) + line)
		}
	}
	for _, child := range node.children {
		render(out, tree, child, indent+1, v)
	}
}

func capSnapshot(text string) string {
	runes := []rune(text)
	if len(runes) <= konst.BrowserSnapshotMaxChars {
		return text
	}
	head := string(runes[:konst.BrowserSnapshotMaxChars])
	head = head[:strings.LastIndexByte(head, '\n')+1]
	left := text[len(head):]
	return head + fmt.Sprintf("[cut: %d more lines, %d characters, left out]\n", strings.Count(left, "\n"), utf8.RuneCountInString(left))
}
