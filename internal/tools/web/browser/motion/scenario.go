package motion

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"tofu/internal/konst"
)

type Viewport struct {
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	DeviceScaleFactor float64 `json:"deviceScaleFactor"`
}

type Ready struct {
	Selector string `json:"selector,omitempty"`
	SettleMs int    `json:"settleMs"`
}

type Action struct {
	Action   string `json:"action"`
	Role     string `json:"role,omitempty"`
	Name     string `json:"name,omitempty"`
	Exact    bool   `json:"exact,omitempty"`
	Selector string `json:"selector,omitempty"`
	Key      string `json:"key,omitempty"`
}

type Watch struct {
	Name       string   `json:"name"`
	Selector   string   `json:"selector"`
	Attributes []string `json:"attributes,omitempty"`
	Styles     []string `json:"styles,omitempty"`
}

type Scenario struct {
	Name           string   `json:"name,omitempty"`
	URL            string   `json:"url"`
	Viewport       Viewport `json:"viewport"`
	ReducedMotion  string   `json:"reducedMotion"`
	Reload         bool     `json:"reload"`
	Ready          Ready    `json:"ready"`
	Setup          []Action `json:"setup"`
	Trigger        Action   `json:"trigger"`
	Watch          []Watch  `json:"watch"`
	RecordBeforeMs int      `json:"recordBeforeMs"`
	RecordAfterMs  int      `json:"recordAfterMs"`
}

func checkAction(a Action, field string) error {
	byRole := a.Role != "" || a.Name != ""
	switch {
	case byRole && a.Selector != "":
		return fmt.Errorf("%s: use role and name or a selector, not both", field)
	case byRole && (a.Role == "" || a.Name == ""):
		return fmt.Errorf("%s: role and name go together", field)
	case a.Exact && !byRole:
		return fmt.Errorf("%s: exact applies to a role and name", field)
	}
	switch a.Action {
	case "click", "hover":
		if !byRole && a.Selector == "" {
			return fmt.Errorf("%s: %s needs a role and name or a selector", field, a.Action)
		}
	case "press":
		if a.Key == "" {
			return fmt.Errorf("%s: press needs a key, for example Escape", field)
		}
	default:
		return fmt.Errorf("%s.action must be click, hover or press, not %q", field, a.Action)
	}
	return nil
}

func (s Scenario) Checked() (Scenario, error) {
	if !strings.HasPrefix(s.URL, "http://") && !strings.HasPrefix(s.URL, "https://") {
		return s, fmt.Errorf("url must start with http:// or https://, not %q", s.URL)
	}
	if err := checkAction(s.Trigger, "trigger"); err != nil {
		return s, err
	}
	for i, a := range s.Setup {
		if err := checkAction(a, fmt.Sprintf("setup[%d]", i)); err != nil {
			return s, err
		}
	}
	if len(s.Watch) > konst.MotionWatchCeiling {
		return s, fmt.Errorf("watch holds %d elements, at most %d", len(s.Watch), konst.MotionWatchCeiling)
	}
	names := map[string]bool{}
	for i, w := range s.Watch {
		if w.Name == "" || w.Selector == "" {
			return s, fmt.Errorf("watch[%d] needs a name and a selector", i)
		}
		if names[w.Name] {
			return s, fmt.Errorf("watch name %q is used twice", w.Name)
		}
		names[w.Name] = true
		for j, a := range w.Attributes {
			if a == "" || slices.Contains(w.Attributes[:j], a) {
				return s, fmt.Errorf("watch[%d].attributes[%d] is empty or named twice: %q", i, j, a)
			}
		}
	}
	if s.Viewport.Width == 0 && s.Viewport.Height == 0 {
		s.Viewport.Width, s.Viewport.Height = konst.MotionViewportWidthDefault, konst.MotionViewportHeightDefault
	}
	if s.Viewport.DeviceScaleFactor == 0 {
		s.Viewport.DeviceScaleFactor = 1
	}
	if s.Viewport.Width <= 0 || s.Viewport.Height <= 0 || s.Viewport.DeviceScaleFactor < 0 {
		return s, errors.New("viewport needs a positive width, height and deviceScaleFactor")
	}
	if s.ReducedMotion == "" {
		s.ReducedMotion = "no-preference"
	}
	if s.ReducedMotion != "no-preference" && s.ReducedMotion != "reduce" {
		return s, fmt.Errorf("reducedMotion must be no-preference or reduce, not %q", s.ReducedMotion)
	}
	if s.Ready.SettleMs < 0 {
		return s, fmt.Errorf("ready.settleMs must be at least 0, not %d", s.Ready.SettleMs)
	}
	if s.RecordBeforeMs == 0 {
		s.RecordBeforeMs = konst.MotionBeforeMillisDefault
	}
	if s.RecordAfterMs == 0 {
		s.RecordAfterMs = konst.MotionAfterMillisDefault
	}
	if s.RecordBeforeMs < 0 || s.RecordAfterMs < 0 || s.RecordBeforeMs+s.RecordAfterMs > konst.MotionRecordCeilingMillis {
		return s, fmt.Errorf("recordBeforeMs and recordAfterMs must be at least 0 and total at most %d", konst.MotionRecordCeilingMillis)
	}
	return s, nil
}
