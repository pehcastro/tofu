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
	hosts  map[string]int
}

type BrowserSettings struct {
	Home    string
	Mode    string
	Chooser string
	Steps   int
	Judge   func() (jevloop.Jev, error)
	Model   func() (turn.Model, string, error)
}

func NewBrowser(config BrowserSettings) ([]turn.Tool, error) {
	session := &browserSession{home: config.Home, pages: map[int]browser.Page{}, hosts: map[string]int{}}
	switch config.Mode {
	case settings.BrowserOff:
		return nil, nil
	case settings.BrowserRead:
		return []turn.Tool{browserTabs{session}, browserRead{session}}, nil
	case settings.BrowserDrive:
		switch config.Chooser {
		case settings.ChooserJev:
			return []turn.Tool{browserTabs{session}, browserRead{session}, browserDo{session, config.Steps, config.Judge, config.Model}}, nil
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

type browserAct struct{ session *browserSession }

func (browserAct) Name() string { return "browser_act" }

func (browserAct) Definition() llm.Tool {
	return llm.Tool{
		Name: "browser_act",
		Description: "runs one step in a Chrome tab: CLICK, TYPE_TEXT or SELECT on a control numbered in the latest browser_read of that tab, or SCROLL_UP, SCROLL_DOWN or WAIT. " +
			"text is what TYPE_TEXT types, or the option SELECT picks. every step changes the page, so read the tab again before the next one. " +
			whatTofuReaches,
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
		if _, err := driveTab(client, args.Tab); err != nil {
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
