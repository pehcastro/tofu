package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/browser"
	"tofu/internal/browser/jevloop"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/settings"
	"tofu/internal/turn"
	"tofu/internal/web"
)

type browserSession struct {
	home    string
	mu      sync.Mutex
	client  *browser.Client
	driver  *browser.Driver
	recent  []string
	hosts   map[string]int
	ownOnly bool
}

const noTabYet = "tofu has no tab of its own for this task yet: act with navigate to a url, which opens one"

type BrowserSettings struct {
	Home   string
	Mode   string
	Driver string
	Steps  int
	Judge  func() (jevloop.Jev, error)
	Model  func() (turn.Model, string, error)
}

func NewBrowser(config BrowserSettings) ([]turn.Tool, error) {
	if config.Mode == settings.BrowserOff {
		return nil, nil
	}
	session := &browserSession{home: config.Home, hosts: map[string]int{}, ownOnly: config.Driver == settings.DriverSubagent}
	var reads, acts turn.Tool
	switch config.Driver {
	case settings.DriverSteps, settings.DriverSubagent:
		reads, acts = browserObserve{session}, browserAct{session}
	case settings.DriverGoal:
		reads, acts = browserRead{session}, browserDo{session, config.Steps, config.Judge, config.Model}
	default:
		return nil, fmt.Errorf("the %s setting is %q, and it takes %s, %s or %s", settings.BrowserDriver, config.Driver, settings.DriverSteps, settings.DriverGoal, settings.DriverSubagent)
	}
	switch config.Mode {
	case settings.BrowserRead:
		return []turn.Tool{browserTabs{session}, reads}, nil
	case settings.BrowserDrive:
		return []turn.Tool{browserTabs{session}, reads, acts}, nil
	}
	return nil, fmt.Errorf("the browser setting is %q, and it takes %s, %s or %s", config.Mode, settings.BrowserOff, settings.BrowserRead, settings.BrowserDrive)
}

func (s *browserSession) drive(tab int, use func(*browser.Driver) error) error {
	return s.with(func(client *browser.Client) error {
		if s.driver == nil || s.driver.Client != client {
			s.driver = &browser.Driver{Client: client}
		}
		if tab != 0 && s.ownOnly {
			found, err := driveTab(client, tab)
			if err != nil {
				return err
			}
			if !found.Opened {
				return fmt.Errorf("tab %d is the person's, and the browser sub-agent works only in a tab tofu opened: %s", tab, noTabYet)
			}
		}
		if tab != 0 {
			s.driver.Use(tab)
		}
		switch {
		case s.driver.Tab == 0 && s.ownOnly:
			return errors.New(noTabYet)
		case s.driver.Tab == 0:
			return errors.New("name the tab: browser_tabs lists the tabs tofu can reach")
		}
		return use(s.driver)
	})
}

func (s *browserSession) start(url string) (int, error) {
	opened := 0
	err := s.with(func(client *browser.Client) (err error) {
		if s.driver == nil || s.driver.Client != client {
			s.driver = &browser.Driver{Client: client}
		}
		if s.driver.Tab != 0 {
			return nil
		}
		if opened, err = client.Open(url); err == nil {
			s.driver.Use(opened)
		}
		return err
	})
	return opened, err
}

func (s *browserSession) with(use func(*browser.Client) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		client, err := browser.Dial(s.home)
		if err != nil {
			return err
		}
		s.client = client
	}
	err := use(s.client)
	if errors.Is(err, browser.ErrNotConnected) {
		_ = s.client.Close()
		s.client = nil
	}
	return err
}

const whatTofuReaches = "tofu reaches the person's ordinary Chrome tabs, never a chrome:// page, DevTools, an extension or the web store, and never runs script the model wrote in one"

type browserTabs struct{ session *browserSession }

func (browserTabs) Name() string { return "browser_tabs" }

func (browserTabs) Definition() llm.Tool {
	return llm.Tool{
		Name:        "browser_tabs",
		Description: "lists the Chrome tabs tofu can reach, one a line: id, title and address. " + whatTofuReaches,
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func (t browserTabs) Run(context.Context, json.RawMessage) (turn.Result, error) {
	var tabs []browser.Tab
	err := t.session.with(func(client *browser.Client) (err error) {
		tabs, err = client.Tabs()
		return err
	})
	if err != nil {
		return turn.Result{}, fmt.Errorf("browser_tabs: %w", err)
	}
	if t.session.ownOnly {
		tabs = slices.DeleteFunc(tabs, func(tab browser.Tab) bool { return !tab.Opened })
		if len(tabs) == 0 {
			return turn.Result{Content: noTabYet + "; the person's own tabs are not listed, and the browser sub-agent never uses them", Command: "open tabs"}, nil
		}
	}
	if len(tabs) == 0 {
		return turn.Result{Content: "Chrome has no tab tofu can reach: every open tab is a chrome:// page, DevTools, an extension or the web store", Command: "open tabs"}, nil
	}
	lines := make([]string, len(tabs))
	for i, tab := range tabs {
		lines[i] = fmt.Sprintf("%d %s %s", tab.ID, tab.Title, tab.URL)
	}
	return turn.Result{
		Content: fmt.Sprintf("%d open tabs\n%s", len(tabs), web.Untrusted("the titles of the open Chrome tabs", strings.Join(lines, "\n"))),
		Command: "open tabs",
	}, nil
}

type browserRead struct{ session *browserSession }

func (browserRead) Name() string { return "browser_read" }

func (browserRead) Definition() llm.Tool {
	return llm.Tool{
		Name: "browser_read",
		Description: "reads one Chrome tab: its visible text, then a numbered table of the controls on screen, each with its role, label, value and state. " +
			"password, file and hidden fields are never listed. a step names a control by its number in the latest read of that tab. " +
			everythingFetchedIsUntrusted,
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"tab": map[string]any{"type": "integer"}},
			"required":   []string{"tab"},
		},
	}
}

func (t browserRead) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		Tab int `json:"tab"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("browser_read: arguments are not the expected shape: %w", err)
	}
	var page browser.Page
	err := t.session.with(func(client *browser.Client) (err error) {
		page, err = browser.SharedTab{Client: client, ID: args.Tab}.Snapshot(ctx)
		return err
	})
	if err != nil {
		return turn.Result{}, fmt.Errorf("browser_read: %w", err)
	}
	return turn.Result{Content: readOut(args.Tab, page), Command: fmt.Sprintf("tab %d %s", args.Tab, page.URL)}, nil
}

func readOut(tab int, page browser.Page) string {
	var body strings.Builder
	fmt.Fprintf(&body, "%s\n%s\n\nscrolls up %v, down %v\n", page.Title, page.Text, page.Scroll.Up, page.Scroll.Down)
	for _, element := range page.Elements {
		fmt.Fprintf(&body, "\n[%d] %s %q", element.Index, element.Role, element.Label)
		for _, state := range [][2]string{{"value", element.Value}, {"checked", element.Checked}, {"selected", element.Selected}, {"expanded", element.Expanded}} {
			if state[1] != "" {
				fmt.Fprintf(&body, " %s %q", state[0], state[1])
			}
		}
		if element.ReadOnly {
			body.WriteString(" readonly")
		}
		if href, linked := page.Links[element.Index]; linked {
			fmt.Fprintf(&body, " href %q", href)
		}
		for _, option := range element.Options {
			fmt.Fprintf(&body, " option %q", option.Label)
		}
	}
	return fmt.Sprintf("tab %d holds %d controls\n%s", tab, len(page.Elements), web.Untrusted("the Chrome tab "+page.URL, body.String()))
}

func driveTab(client *browser.Client, id int) (browser.Tab, error) {
	tabs, err := client.Tabs()
	if err != nil {
		return browser.Tab{}, err
	}
	for _, tab := range tabs {
		if tab.ID == id {
			return tab, nil
		}
	}
	return browser.Tab{}, fmt.Errorf("tofu cannot reach tab %d: browser_tabs lists the tabs it can", id)
}

const howToBrowse = "observe the tab first, act on the refs it shows, and observe again after anything changes. " +
	"close a popup, a cookie banner or a dialog in the way before anything else. apply the page's filters before reading its results. " +
	"before opening a result, check the results match the task, the place and whether a price cap is per night or for the whole stay, and report a mismatch rather than open a wrong result. " +
	"after two ways to reach a state fail, build the url from the site's own parameters, navigate to it and say so; call a task blocked only after that fails too. " +
	"a task works in one tab from start to end: open listings one after another in it with a link's url= or a click, and go back between them. " +
	"when an act is covered by a dialog, close that dialog with the ref it names. a sponsored or ad result is not the organic one. "

type browserObserve struct{ session *browserSession }

func (browserObserve) Name() string { return "browser_observe" }

func (browserObserve) Definition() llm.Tool {
	return llm.Tool{
		Name: "browser_observe",
		Description: "shows one Chrome tab as an accessibility snapshot: a line a node, indented, as role \"name\" [state, ref=e5], a value after a colon. " +
			"a ref names one element for browser_act. interactive, the default, shows only nodes with a ref; false shows the whole tree. " +
			"scrollable marks a container browser_act can scroll by its ref. * marks a ref new since the last observe of this page. " +
			"a long tree is cut, and the cut names the from line that shows the rest. " +
			"tab defaults to the tab tofu last worked in. " + howToBrowse + everythingFetchedIsUntrusted,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tab":         map[string]any{"type": "integer"},
				"interactive": map[string]any{"type": "boolean"},
				"from":        map[string]any{"type": "integer"},
			},
		},
	}
}

func (t browserObserve) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		Tab         int   `json:"tab"`
		Interactive *bool `json:"interactive"`
		From        int   `json:"from"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("browser_observe: arguments are not the expected shape: %w", err)
	}
	var snapshot string
	err := t.session.drive(args.Tab, func(driver *browser.Driver) (err error) {
		args.Tab = driver.Tab
		snapshot, err = driver.Observe(args.Interactive == nil || *args.Interactive)
		return err
	})
	if err != nil {
		return turn.Result{}, fmt.Errorf("browser_observe: %w", err)
	}
	return turn.Result{Content: web.Untrusted(fmt.Sprintf("Chrome tab %d", args.Tab), window(snapshot, args.From)), Command: fmt.Sprintf("tab %d observe", args.Tab)}, nil
}

func window(snapshot string, from int) string {
	lines := slices.Collect(strings.Lines(snapshot))
	from = min(max(from, 1), len(lines))
	var shown strings.Builder
	if from > 1 {
		shown.WriteString(lines[0])
	}
	end := from - 1
	for ; end < len(lines) && (end < from || shown.Len()+len(lines[end]) <= konst.BrowserSnapshotMaxBytes); end++ {
		shown.WriteString(lines[end])
	}
	if end < len(lines) {
		fmt.Fprintf(&shown, "[cut: lines %d to %d, %d bytes, left out: browser_observe with {\"from\":%d} shows them]\n", end+1, len(lines), len(snapshot)-len(strings.Join(lines[:end], "")), end+1)
	}
	return shown.String()
}

type browserAct struct{ session *browserSession }

func (browserAct) Name() string { return "browser_act" }

func (browserAct) Definition() llm.Tool {
	return llm.Tool{
		Name: "browser_act",
		Description: fmt.Sprintf("runs up to %d actions in one Chrome tab, in order, each on a ref from the latest browser_observe or on a target by role and name. ", konst.BrowserBatchMax) +
			"plan a predictable stretch as one guarded batch, the way a person does: open the date picker, click day 9, click day 15, apply. " +
			"target {role, name, nth} finds the element on the page as it is when that action runs, so it survives refs that renumber; nth picks among equal names, from 1. " +
			"expect is checked before the action and expect_after after it settles: url_has, text_has, and gone {role, name} for a dialog or a button that should close. " +
			fmt.Sprintf("a target or a guard waits up to %d ms for the page. ", konst.BrowserGuardWaitMillis) +
			"the batch stops at the first guard that fails, names it, and ends with what changed since the batch began. " +
			"click takes a ref. fill takes a ref and the text as value, and answers the value it reads back. select takes a ref and the option as value. " +
			"press takes a key as value, Enter or Escape or a letter. scroll takes up or down as value, and a ref to scroll that container instead of the page. " +
			"navigate loads the url in value in the task's one tab: on the person's own tab it opens that one tab of tofu's first, and every later navigate loads there, whatever the site. back goes back in it. a popup the page opens is loaded into that tab and closed. " +
			"after a navigate, plan the next stretch as one batch rather than one action a call: a login or a search form is every fill and the submit together. an empty actions list is refused. " +
			"wait takes a number of milliseconds, or text to wait for, as value. " +
			"an action with no target and no expect does not run after one that changed the url, and the result says which it skipped. " +
			"a click that another element covers does not run, and says what covers it. " +
			"after a navigate, back or wait the result ends with the page's full tree, with its text. after any other act it lists only the refs that changed since the last snapshot, + new, ~ changed, x gone, and every other ref still stands; when most of the page changed it ends with the whole interactive snapshot. " +
			"the same action on the same role and name, on a page that did not change, is flagged, then refused, whatever its ref. " + howToBrowse + whatTofuReaches,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tab": map[string]any{"type": "integer"},
				"actions": map[string]any{
					"type":     "array",
					"maxItems": konst.BrowserBatchMax,
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"ref":          map[string]any{"type": "string"},
							"target":       roleAndName(),
							"action":       map[string]any{"type": "string", "enum": []string{"click", "fill", "select", "press", "scroll", "navigate", "back", "wait"}},
							"value":        map[string]any{"type": "string"},
							"expect":       guard(),
							"expect_after": guard(),
						},
						"required": []string{"action"},
					},
				},
			},
			"required": []string{"actions"},
		},
	}
}

func roleAndName() map[string]any {
	return map[string]any{"type": "object", "required": []string{"role", "name"}, "properties": map[string]any{
		"role": map[string]any{"type": "string"}, "name": map[string]any{"type": "string"}, "nth": map[string]any{"type": "integer"},
	}}
}

func guard() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"url_has": map[string]any{"type": "string"}, "text_has": map[string]any{"type": "string"}, "gone": roleAndName(),
	}}
}

type browserStep struct {
	Ref         string         `json:"ref"`
	Action      string         `json:"action"`
	Value       string         `json:"value"`
	Target      *browserTarget `json:"target"`
	Expect      *browserGuard  `json:"expect"`
	ExpectAfter *browserGuard  `json:"expect_after"`
}

type browserTarget struct {
	Role string `json:"role"`
	Name string `json:"name"`
	Nth  int    `json:"nth"`
}

func (t browserTarget) String() string {
	if t.Nth > 1 {
		return fmt.Sprintf("%s %q number %d", t.Role, t.Name, t.Nth)
	}
	return fmt.Sprintf("%s %q", t.Role, t.Name)
}

type browserGuard struct {
	URLHas  string         `json:"url_has"`
	TextHas string         `json:"text_has"`
	Gone    *browserTarget `json:"gone"`
}

func waitFor(check func() (string, error)) (string, error) {
	until := time.Now().Add(konst.BrowserGuardWaitMillis * time.Millisecond)
	for {
		failed, err := check()
		if err != nil || failed == "" || time.Now().After(until) {
			return failed, err
		}
		time.Sleep(konst.BrowserGuardPollMillis * time.Millisecond)
	}
}

func (g *browserGuard) failed(driver *browser.Driver, label string) (string, error) {
	if g == nil {
		return "", nil
	}
	return waitFor(func() (string, error) {
		url, text, err := driver.Page()
		switch {
		case err != nil:
			return "", err
		case g.URLHas != "" && !strings.Contains(url, g.URLHas):
			return fmt.Sprintf("%s url_has %q failed, the url is %s", label, g.URLHas, url), nil
		case g.TextHas != "" && !strings.Contains(text, g.TextHas):
			return fmt.Sprintf("%s text_has %q failed, the page does not show it", label, g.TextHas), nil
		case g.Gone == nil:
			return "", nil
		}
		ref, err := driver.Find(g.Gone.Role, g.Gone.Name, g.Gone.Nth)
		if ref != "" {
			return fmt.Sprintf("%s gone %s failed, it is still on the page as %s", label, g.Gone, ref), err
		}
		return "", err
	})
}

func (s *browserStep) resolve(driver *browser.Driver) (string, error) {
	if s.Target == nil {
		return "", nil
	}
	return waitFor(func() (ref string, err error) {
		if s.Ref, err = driver.Find(s.Target.Role, s.Target.Name, s.Target.Nth); s.Ref == "" {
			return fmt.Sprintf("target %s is not on the page", s.Target), err
		}
		return "", err
	})
}

func (s browserStep) move() browser.Move {
	return browser.Move{Ref: s.Ref, Kind: browser.MoveKind(s.Action), Value: s.Value}
}

func (s browserStep) normalised(target string) string {
	switch s.Action {
	case "click":
		return "click " + target
	case "fill":
		return "fill " + target + " " + strings.ToLower(strings.TrimSpace(s.Value))
	case "navigate":
		return "navigate " + s.Value
	case "scroll":
		return "scroll " + cmp.Or(s.Value, "down") + " " + target
	}
	return s.Action + " " + target + " " + s.Value
}

func (s *browserSession) repeats(key string) int {
	count := 0
	for _, recent := range s.recent {
		if recent == key {
			count++
		}
	}
	return count
}

func (s browserStep) String() string {
	target := s.Ref
	if s.Target != nil {
		target = s.Target.String()
	}
	said := strings.TrimSpace(s.Action + " " + target)
	if s.Value != "" {
		said += " " + strconv.Quote(s.Value)
	}
	return said
}

func (t browserAct) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		Tab     int           `json:"tab"`
		Actions []browserStep `json:"actions"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("browser_act: arguments are not the expected shape: %w", err)
	}
	if len(args.Actions) == 0 {
		var snapshot string
		err := t.session.drive(args.Tab, func(driver *browser.Driver) (err error) {
			args.Tab = driver.Tab
			snapshot, err = driver.Observe(true)
			return err
		})
		if err != nil {
			return turn.Result{}, fmt.Errorf("browser_act: refused, it carried no action, and %w", err)
		}
		content := fmt.Sprintf("refused: browser_act carried no action, and nothing ran. give 1 to %d actions on the page below\n\n", konst.BrowserBatchMax)
		return turn.Result{Content: content + web.Untrusted(fmt.Sprintf("Chrome tab %d", args.Tab), window(snapshot, 1)), Command: fmt.Sprintf("tab %d act refused empty", args.Tab)}, nil
	}
	if len(args.Actions) > konst.BrowserBatchMax {
		return turn.Result{}, fmt.Errorf("browser_act: give 1 to %d actions, not %d", konst.BrowserBatchMax, len(args.Actions))
	}
	var report strings.Builder
	var snapshot string
	ran := 0
	loads, changed := false, false
	actions := args.Actions
	if first := actions[0]; args.Tab == 0 && first.Action == "navigate" {
		opened, err := t.session.start(first.Value)
		if err != nil {
			return turn.Result{}, fmt.Errorf("browser_act: %w", err)
		}
		if opened != 0 {
			fmt.Fprintf(&report, "1. %s: opened tofu's own tab %d on it, the tab this task works in\n", first, opened)
			ran, loads, actions = 1, true, actions[1:]
		}
	}
	err := t.session.drive(args.Tab, func(driver *browser.Driver) error {
		for _, step := range actions {
			if changed && step.Target == nil && step.Expect == nil && step.Action != "wait" {
				break
			}
			failed, err := step.Expect.failed(driver, "expect")
			if failed == "" && err == nil {
				failed, err = step.resolve(driver)
			}
			if err != nil {
				return err
			}
			if failed != "" {
				fmt.Fprintf(&report, "%d. %s: %s\n", ran+1, step, failed)
				break
			}
			fingerprint, err := driver.Fingerprint()
			if err != nil {
				return err
			}
			key := step.normalised(driver.Target(step.Ref)) + " on " + fingerprint
			repeats := t.session.repeats(key) + 1
			if repeats >= konst.BrowserRepeatRefuse {
				fmt.Fprintf(&report, "%d. %s: refused, it would be the %dth time on a page that did not change: try another ref, another action, or observe what blocks it\n", ran+1, step, repeats)
				break
			}
			t.session.recent = append(t.session.recent, key)
			t.session.recent = t.session.recent[max(0, len(t.session.recent)-konst.BrowserLoopWindow):]
			moved, err := driver.Do(step.move())
			line := moved.String()
			switch {
			case err != nil:
				line = "failed: " + err.Error()
			case repeats < konst.BrowserRepeatNotice:
			case strings.HasPrefix(line, browser.Unchanged):
				line = fmt.Sprintf("repeated %d times, the page did not change", repeats)
			default:
				line += fmt.Sprintf(", after %d tries on the same page", repeats)
			}
			fmt.Fprintf(&report, "%d. %s: %s\n", ran+1, step, line)
			ran++
			loads = loads || step.Action == "navigate" || step.Action == "back" || step.Action == "wait"
			if err != nil || moved.Covered != "" {
				break
			}
			changed = changed || moved.URLChanged || moved.Opened != 0
			if failed, err = step.ExpectAfter.failed(driver, "expect_after"); err != nil {
				return err
			}
			if failed != "" {
				fmt.Fprintf(&report, "%d. %s: stopped, %s\n", ran, step, failed)
				break
			}
		}
		var err error
		args.Tab = driver.Tab
		if loads {
			snapshot, err = driver.Observe(false)
		} else {
			snapshot, err = driver.ObserveChanges()
		}
		driver.Thinking()
		return err
	})
	if err != nil {
		return turn.Result{}, fmt.Errorf("browser_act: %w", err)
	}
	fmt.Fprintf(&report, "ran %d of %d", ran, len(args.Actions))
	if skipped := len(args.Actions) - ran; skipped > 0 {
		fmt.Fprintf(&report, ", %d skipped: observe the page as it is now and act again", skipped)
	}
	if loads && (strings.Contains(snapshot, `textbox "`) || strings.Contains(snapshot, `combobox "`)) && strings.Contains(snapshot, `button "`) {
		report.WriteString("\nnext: this page shows a form. fill every field and submit it in one guarded batch, by role and name, with an expect_after on the result")
	}
	report.WriteString("\n\n")
	return turn.Result{Content: report.String() + web.Untrusted(fmt.Sprintf("Chrome tab %d", args.Tab), window(snapshot, 1)), Command: fmt.Sprintf("tab %d act %d", args.Tab, ran)}, nil
}

func targets(op browser.Op) bool {
	return op == browser.OpClick || op == browser.OpTypeText || op == browser.OpSelect
}

type browserDo struct {
	session *browserSession
	steps   int
	judge   func() (jevloop.Jev, error)
	model   func() (turn.Model, string, error)
}

func (browserDo) Name() string { return "browser_do" }

func (t browserDo) Definition() llm.Tool {
	return llm.Tool{
		Name: "browser_do",
		Description: "does a whole browsing goal in one call and answers it. call it once with the whole goal, and again on the tab it named if it comes back blocked. " +
			"a url without a tab opens it in a background tab of tofu's own, or reuses the tab tofu already has on that site. a tab alone works in that tab as it is; " +
			"a tab and a url send a tab tofu opened to the url, and a person's tab is never sent anywhere. the result names its tab, so a follow-up passes that tab. " +
			fmt.Sprintf("jev picks each step, a click, typing, choosing an option, a scroll or a wait, until the goal is done, it is blocked, or %d actions ran. ", t.steps) +
			"a tab the page opens is followed. the browser model writes the text a field needs, and reads every page the run saw into the answer. " +
			"values maps a field's label, placeholder or name, in any case, to the exact text to type there instead. " +
			"returns the answer first, then the steps. " + whatTofuReaches,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"goal":   map[string]any{"type": "string"},
				"url":    map[string]any{"type": "string"},
				"tab":    map[string]any{"type": "integer"},
				"values": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			},
			"required": []string{"goal"},
		},
	}
}

func (t browserDo) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		Goal   string      `json:"goal"`
		URL    string      `json:"url"`
		Tab    int         `json:"tab"`
		Values fieldValues `json:"values"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("browser_do: arguments are not the expected shape: %w", err)
	}
	if args.Tab == 0 && args.URL == "" {
		return turn.Result{}, errors.New("browser_do: give a url to open, the tab to work in, or both to send a tab tofu opened to the url")
	}
	model, chosen, err := t.model()
	if err != nil {
		return turn.Result{}, fmt.Errorf("browser_do: no action ran, the %s setting names no model tofu can ask: %w", settings.BrowserModel, err)
	}
	var result jevloop.Result
	closed := false
	err = t.session.with(func(client *browser.Client) error {
		judge, err := t.judge()
		if err != nil {
			return fmt.Errorf("no action ran, jev is not reachable: %w", err)
		}
		id, ours, err := t.session.aim(client, args.Tab, args.URL)
		if err != nil {
			return err
		}
		judge.Decided = map[string]jevloop.Choice{}
		at := id
		result = jevloop.Loop{
			Browser: jevloop.Browser{
				Snapshot: func(ctx context.Context) (browser.Page, error) {
					return browser.SharedTab{Client: client, ID: at}.Snapshot(ctx)
				},
				Act: func(_ context.Context, page browser.Page, action browser.Action) (browser.Stale, error) {
					acted, err := browser.SharedTab{Client: client, ID: at}.Drive(page, action)
					if acted.Opened != 0 {
						at = acted.Opened
					}
					return acted.Stale, err
				},
				Tab: func() int { return at },
			},
			Choose: judge.Choose,
			Write: func(ctx context.Context, goal string, page browser.Page, field browser.Element) (string, error) {
				if text, named := args.Values.named(page, field); named {
					return text, nil
				}
				return askFor(ctx, model, fmt.Sprintf("a browser task has the goal %q. write the exact text to type into the field %q on this page, and nothing else.\n\n%s",
					goal, field.Label, web.Untrusted("the Chrome tab "+page.URL, page.Title+"\n"+page.Text)))
			},
			Actions: t.steps,
		}.Run(ctx, args.Goal)
		args.Tab = at
		if ours && result.Status == jevloop.StatusBlocked && len(result.Steps) == 0 {
			closed = client.CloseTab(id) == nil
			maps.DeleteFunc(t.session.hosts, func(_ string, kept int) bool { return kept == id })
		}
		return nil
	})
	if err != nil {
		return turn.Result{}, fmt.Errorf("browser_do: %w", err)
	}
	var earlier strings.Builder
	for _, page := range result.Pages {
		if page.URL != result.Page.URL || page.Text != result.Page.Text {
			fmt.Fprintf(&earlier, "%s %s\n%s\n\n", page.Title, page.URL, page.Text)
		}
	}
	pages := readOut(args.Tab, result.Page)
	if earlier.Len() > 0 {
		pages = web.Untrusted("the pages this run saw before its last one", earlier.String()) + "\n\nthen the last page: " + pages
	}
	answer, err := askFor(ctx, model, fmt.Sprintf("a browser task with the goal %q ended %s: %s\nfrom the pages below, answer the goal: the data it asks for, or one line saying why it could not be done. write the answer and nothing else.\n\n%s",
		args.Goal, result.Status, result.Reason, pages))
	if err == nil {
		answer = web.Untrusted("the browser model's answer to the goal, read from the Chrome tab "+result.Page.URL, answer)
	} else {
		answer = "the browser model gave no answer: " + err.Error() + "\n" + pages
	}
	var body strings.Builder
	body.WriteString(result.Reason + "\n")
	for i, step := range result.Steps {
		fmt.Fprintf(&body, "%d. %s", i+1, step.Action.Op)
		if targets(step.Action.Op) {
			fmt.Fprintf(&body, " %q", step.Label)
		}
		if step.Action.Value != "" {
			fmt.Fprintf(&body, " %q", step.Action.Value)
		}
		switch {
		case step.Stale != browser.StaleNone:
			fmt.Fprintf(&body, ": did not run, the target is %s\n", step.Stale)
		case step.Opened != 0:
			fmt.Fprintf(&body, ": opened tab %d\n", step.Opened)
		case step.Changed:
			body.WriteString(": changed the page\n")
		default:
			body.WriteString(": unchanged\n")
		}
	}
	if closed {
		fmt.Fprintf(&body, "tab %d is closed: tofu opened it and no step ran there\n", args.Tab)
	}
	return turn.Result{
		Content: fmt.Sprintf("%s\n\ntab %d %s after %d jev decisions and %d steps; the browser model is %s\n%s", answer, args.Tab, result.Status, result.Decisions, len(result.Steps), chosen,
			web.Untrusted("the reason and the steps, named by the page's own labels", strings.TrimSuffix(body.String(), "\n"))),
		Command: fmt.Sprintf("tab %d goal %q", args.Tab, args.Goal),
	}, nil
}

func (s *browserSession) aim(client *browser.Client, tab int, address string) (int, bool, error) {
	if address == "" {
		found, err := driveTab(client, tab)
		return tab, found.Opened, err
	}
	var host string
	if parsed, err := url.Parse(address); err == nil {
		host = parsed.Host
	}
	if tab == 0 {
		if known, err := driveTab(client, s.hosts[host]); err == nil && known.Opened {
			tab = known.ID
		}
	} else if found, err := driveTab(client, tab); err != nil || !found.Opened {
		return 0, false, cmp.Or(err, fmt.Errorf("tab %d is the person's: browser_do sends only a tab tofu opened to a url", tab))
	}
	var err error
	if tab == 0 {
		tab, err = client.Open(address)
	} else {
		err = browser.SharedTab{Client: client, ID: tab}.Navigate(address)
	}
	if err == nil {
		s.hosts[host] = tab
	}
	return tab, true, err
}

func askFor(ctx context.Context, model turn.Model, prompt string) (string, error) {
	decision, err := model.Ask(ctx, llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: prompt}}})
	if err != nil {
		return "", err
	}
	if decision.Outcome != llm.OutcomeMessage || strings.TrimSpace(decision.Content) == "" {
		return "", fmt.Errorf("the browser model answered %s with no text", decision.Outcome)
	}
	return strings.TrimSpace(decision.Content), nil
}

type fieldValues map[string]string

func (values fieldValues) named(page browser.Page, field browser.Element) (string, bool) {
	names := append([]string{field.Label}, page.Names[field.Index]...)
	for key, text := range values {
		if slices.ContainsFunc(names, func(name string) bool { return strings.EqualFold(name, key) }) {
			return text, true
		}
	}
	typeable := 0
	for _, element := range page.Elements {
		if browser.OpTypeText.Accepts(element.Role) && !element.ReadOnly {
			typeable++
		}
	}
	if only := slices.Collect(maps.Values(values)); len(only) == 1 && typeable == 1 {
		return only[0], true
	}
	return "", false
}
