package motion

import (
	"errors"
	"fmt"
	"strings"

	"tofu/internal/konst"
)

type Viewport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type Action struct {
	Action string `json:"action"`
	Ref    string `json:"ref,omitempty"`
	Key    string `json:"key,omitempty"`
}

type Watch struct {
	Name   string   `json:"name"`
	Ref    string   `json:"ref"`
	Styles []string `json:"styles,omitempty"`
}

type Scenario struct {
	URL           string   `json:"url"`
	Viewport      Viewport `json:"viewport"`
	ReducedMotion bool     `json:"reducedMotion"`
	Reload        bool     `json:"reload"`
	Setup         []Action `json:"setup"`
	Trigger       Action   `json:"trigger"`
	Watch         []Watch  `json:"watch"`
	BeforeMs      int      `json:"beforeMs"`
	AfterMs       int      `json:"afterMs"`
}

func checkAction(a Action, field string) error {
	switch a.Action {
	case "click", "hover":
		if a.Ref == "" {
			return fmt.Errorf("%s: %s needs a ref", field, a.Action)
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
		if w.Name == "" || w.Ref == "" {
			return s, fmt.Errorf("watch[%d] needs a name and a ref", i)
		}
		if names[w.Name] {
			return s, fmt.Errorf("watch name %q is used twice", w.Name)
		}
		names[w.Name] = true
	}
	if s.Viewport == (Viewport{}) {
		s.Viewport = Viewport{Width: konst.MotionViewportWidthDefault, Height: konst.MotionViewportHeightDefault}
	}
	if s.Viewport.Width <= 0 || s.Viewport.Height <= 0 {
		return s, errors.New("viewport needs a positive width and height")
	}
	if s.BeforeMs == 0 {
		s.BeforeMs = konst.MotionBeforeMillisDefault
	}
	if s.AfterMs == 0 {
		s.AfterMs = konst.MotionAfterMillisDefault
	}
	if s.BeforeMs < 0 || s.AfterMs < 0 || s.BeforeMs+s.AfterMs > konst.MotionRecordCeilingMillis {
		return s, fmt.Errorf("before_ms and after_ms must be at least 0 and total at most %d", konst.MotionRecordCeilingMillis)
	}
	return s, nil
}
