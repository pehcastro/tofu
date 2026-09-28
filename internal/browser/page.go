package browser

import (
	"encoding/json"
	"fmt"

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
	Options  []SelectOption `json:"options,omitempty"`
}

type Scroll struct {
	Up   bool `json:"up"`
	Down bool `json:"down"`
}

type Page struct {
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	Text        string    `json:"text"`
	Fingerprint string    `json:"fingerprint"`
	Scroll      Scroll    `json:"scroll"`
	Elements    []Element `json:"elements"`
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
