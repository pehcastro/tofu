package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"tofu/internal/browser"
	"tofu/internal/browser/jevloop"
	"tofu/internal/llm"
	"tofu/internal/settings"
	"tofu/internal/turn"
	"tofu/internal/web"
)

type browserSession struct {
	home   string
	mu     sync.Mutex
	client *browser.Client
	pages  map[int]browser.Page
}

type BrowserSettings struct {
	Home    string
	Mode    string
	Chooser string
	Steps   int
	Judge   func() (jevloop.Jev, error)
}

func NewBrowser(config BrowserSettings) ([]turn.Tool, error) {
	session := &browserSession{home: config.Home, pages: map[int]browser.Page{}}
	switch config.Mode {
	case settings.BrowserOff:
		return nil, nil
	case settings.BrowserRead:
		return []turn.Tool{browserTabs{session}, browserRead{session}}, nil
	case settings.BrowserDrive:
		switch config.Chooser {
		case settings.ChooserJev:
			return []turn.Tool{browserTabs{session}, browserRead{session}, browserDo{session, config.Steps, config.Judge}}, nil
		case settings.ChooserModel:
			return []turn.Tool{browserTabs{session}, browserRead{session}, browserAct{session}}, nil
		}
		return nil, fmt.Errorf("the browserChooser setting is %q, and it takes %s or %s", config.Chooser, settings.ChooserJev, settings.ChooserModel)
	}
	return nil, fmt.Errorf("the browser setting is %q, and it takes %s, %s or %s", config.Mode, settings.BrowserOff, settings.BrowserRead, settings.BrowserDrive)
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

const theModelNeverWritesScript = "tofu reaches the person's ordinary Chrome tabs, never a chrome:// page, DevTools, an extension or the web store, and never opens, navigates or runs script in one"

type browserTabs struct{ session *browserSession }

func (browserTabs) Name() string { return "browser_tabs" }

func (browserTabs) Definition() llm.Tool {
	return llm.Tool{
		Name:        "browser_tabs",
		Description: "lists the Chrome tabs tofu can reach, one a line: id, title and address. " + theModelNeverWritesScript,
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
		if err == nil {
			t.session.pages[args.Tab] = page
		}
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
		for _, option := range element.Options {
			fmt.Fprintf(&body, " option %q", option.Label)
		}
	}
	return fmt.Sprintf("tab %d holds %d controls\n%s", tab, len(page.Elements), web.Untrusted("the Chrome tab "+page.URL, body.String()))
}

func driveTab(client *browser.Client, id int) error {
	tabs, err := client.Tabs()
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(tabs, func(tab browser.Tab) bool { return tab.ID == id }) {
		return fmt.Errorf("tofu cannot reach tab %d: browser_tabs lists the tabs it can", id)
	}
	return nil
}

type browserAct struct{ session *browserSession }

func (browserAct) Name() string { return "browser_act" }

func (browserAct) Definition() llm.Tool {
	return llm.Tool{
		Name: "browser_act",
		Description: "runs one step in a Chrome tab: CLICK, TYPE_TEXT or SELECT on a control numbered in the latest browser_read of that tab, or SCROLL_UP, SCROLL_DOWN or WAIT. " +
			"text is what TYPE_TEXT types, or the option SELECT picks. every step changes the page, so read the tab again before the next one. " +
			theModelNeverWritesScript,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tab":     map[string]any{"type": "integer"},
				"element": map[string]any{"type": "integer"},
				"op":      map[string]any{"type": "string", "enum": []string{"CLICK", "TYPE_TEXT", "SELECT", "SCROLL_UP", "SCROLL_DOWN", "WAIT"}},
				"text":    map[string]any{"type": "string"},
			},
			"required": []string{"tab", "op"},
		},
	}
}

type browserStep struct {
	Tab     int    `json:"tab"`
	Element int    `json:"element"`
	Op      string `json:"op"`
	Text    string `json:"text"`
}

func (t browserAct) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	var args browserStep
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("browser_act: arguments are not the expected shape: %w", err)
	}
	op, err := browser.ParseOp(args.Op)
	if err != nil {
		return turn.Result{}, fmt.Errorf("browser_act: %w", err)
	}
	var stale browser.Stale
	err = t.session.with(func(client *browser.Client) error {
		if err := driveTab(client, args.Tab); err != nil {
			return err
		}
		page, read := t.session.pages[args.Tab]
		if !read {
			return fmt.Errorf("tab %d has no browser_read since its last step: read it, then act on that table", args.Tab)
		}
		action, err := stepOn(page, op, args)
		if err != nil {
			return err
		}
		delete(t.session.pages, args.Tab)
		stale, err = browser.SharedTab{Client: client, ID: args.Tab}.Act(ctx, page, action)
		return err
	})
	if err != nil {
		return turn.Result{}, fmt.Errorf("browser_act: %w", err)
	}
	command := fmt.Sprintf("tab %d %s", args.Tab, op)
	if targets(op) {
		command += fmt.Sprintf(" element %d", args.Element)
	}
	if stale != browser.StaleNone {
		return turn.Result{Content: fmt.Sprintf("tab %d: %s did not run, the target is %s: read it again", args.Tab, op, stale), Command: command}, nil
	}
	return turn.Result{Content: fmt.Sprintf("tab %d ran %s: read it again to see what changed", args.Tab, op), Command: command}, nil
}

func targets(op browser.Op) bool {
	return op == browser.OpClick || op == browser.OpTypeText || op == browser.OpSelect
}

func stepOn(page browser.Page, op browser.Op, args browserStep) (browser.Action, error) {
	switch op {
	case browser.OpScrollUp, browser.OpScrollDown, browser.OpWait:
		return browser.Action{Op: op}, nil
	case browser.OpDone, browser.OpBlocked:
		return browser.Action{}, fmt.Errorf("%s ends a browser task and is not a step a tab can run", op)
	case browser.OpClick, browser.OpTypeText, browser.OpSelect:
	}
	element, found := page.Element(args.Element)
	switch {
	case !found:
		return browser.Action{}, fmt.Errorf("tab %d has no element %d in its last read", args.Tab, args.Element)
	case !op.Accepts(element.Role):
		return browser.Action{}, fmt.Errorf("%s does not take a %s, and element %d is one", op, element.Role, args.Element)
	case op == browser.OpTypeText && element.ReadOnly:
		return browser.Action{}, fmt.Errorf("element %d is read-only", args.Element)
	case op == browser.OpTypeText:
		return browser.Action{Op: op, Element: element.Index, Value: args.Text}, nil
	case op == browser.OpSelect:
		labels := make([]string, len(element.Options))
		for i, option := range element.Options {
			if args.Text == option.Label || args.Text == option.Value {
				return browser.Action{Op: op, Element: element.Index, Value: option.Value}, nil
			}
			labels[i] = fmt.Sprintf("%q", option.Label)
		}
		return browser.Action{}, fmt.Errorf("element %d offers %s, not %q", args.Element, strings.Join(labels, ", "), args.Text)
	}
	return browser.Action{Op: op, Element: element.Index}, nil
}

type browserDo struct {
	session *browserSession
	steps   int
	judge   func() (jevloop.Jev, error)
}

func (browserDo) Name() string { return "browser_do" }

func (t browserDo) Definition() llm.Tool {
	return llm.Tool{
		Name: "browser_do",
		Description: fmt.Sprintf("runs a whole task in a Chrome tab: jev picks each step, a click, typing, choosing an option, a scroll or a wait, until the goal is done, it is blocked, or %d actions ran. ", t.steps) +
			"values maps a field's label, placeholder or name, in any case, to the text to type there, and one value fills a page's only text field; when jev picks a field values does not name, the task stops blocked and names the field, so call again with it. " +
			"returns every step and a final read of the tab. " + theModelNeverWritesScript,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tab":    map[string]any{"type": "integer"},
				"goal":   map[string]any{"type": "string"},
				"values": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			},
			"required": []string{"tab", "goal"},
		},
	}
}

func (t browserDo) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		Tab    int         `json:"tab"`
		Goal   string      `json:"goal"`
		Values fieldValues `json:"values"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("browser_do: arguments are not the expected shape: %w", err)
	}
	var result jevloop.Result
	err := t.session.with(func(client *browser.Client) error {
		if err := driveTab(client, args.Tab); err != nil {
			return err
		}
		judge, err := t.judge()
		if err != nil {
			return fmt.Errorf("no action ran, jev is not reachable: %w", err)
		}
		judge.Decided = map[string]jevloop.Choice{}
		tab := browser.SharedTab{Client: client, ID: args.Tab}
		result = jevloop.Loop{
			Browser: jevloop.Browser{Snapshot: tab.Snapshot, Act: tab.Act},
			Choose:  judge.Choose,
			Write:   args.Values.write,
			Actions: t.steps,
		}.Run(ctx, args.Goal)
		return nil
	})
	if err != nil {
		return turn.Result{}, fmt.Errorf("browser_do: %w", err)
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
		case step.Changed:
			body.WriteString(": changed the page\n")
		default:
			body.WriteString(": unchanged\n")
		}
	}
	return turn.Result{
		Content: fmt.Sprintf("tab %d %s after %d jev decisions and %d steps\n%s\n%s", args.Tab, result.Status, result.Decisions, len(result.Steps),
			web.Untrusted("the reason and the steps, named by the page's own labels", strings.TrimSuffix(body.String(), "\n")), readOut(args.Tab, result.Page)),
		Command: fmt.Sprintf("tab %d goal %q", args.Tab, args.Goal),
	}, nil
}

type fieldValues map[string]string

func (values fieldValues) write(_ context.Context, _ string, page browser.Page, field browser.Element) (string, error) {
	names := append([]string{field.Label}, page.Names[field.Index]...)
	for key, text := range values {
		if slices.ContainsFunc(names, func(name string) bool { return strings.EqualFold(name, key) }) {
			return text, nil
		}
	}
	typeable := 0
	for _, element := range page.Elements {
		if browser.OpTypeText.Accepts(element.Role) && !element.ReadOnly {
			typeable++
		}
	}
	if only := slices.Collect(maps.Values(values)); len(only) == 1 && typeable == 1 {
		return only[0], nil
	}
	return "", fmt.Errorf("values names no text for element %d, so call browser_do again with values naming %q", field.Index, field.Label)
}
