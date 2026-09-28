package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"tofu/internal/browser"
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

func NewBrowser(home, mode string) ([]turn.Tool, error) {
	session := &browserSession{home: home, pages: map[int]browser.Page{}}
	switch mode {
	case settings.BrowserOff:
		return nil, nil
	case settings.BrowserRead:
		return []turn.Tool{browserTabs{session}, browserRead{session}}, nil
	case settings.BrowserDrive:
		return []turn.Tool{browserTabs{session}, browserRead{session}, browserAct{session}}, nil
	}
	return nil, fmt.Errorf("the browser setting is %q, and it takes %s, %s or %s", mode, settings.BrowserOff, settings.BrowserRead, settings.BrowserDrive)
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

const theModelNeverWritesScript = "tofu reaches only a Chrome tab the person shared with a click, and never opens, navigates or runs script in one"

type browserTabs struct{ session *browserSession }

func (browserTabs) Name() string { return "browser_tabs" }

func (browserTabs) Definition() llm.Tool {
	return llm.Tool{
		Name:        "browser_tabs",
		Description: "lists the Chrome tabs the person shared with tofu, one a line: id, mode, title and address. a read tab can only be read, a drive tab can also be acted on. " + theModelNeverWritesScript,
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
		return turn.Result{Content: "no tab is shared: the person clicks the tofu icon on a Chrome tab and chooses Read or Drive", Command: "shared tabs"}, nil
	}
	lines := make([]string, len(tabs))
	for i, tab := range tabs {
		lines[i] = fmt.Sprintf("%d %s %s %s", tab.ID, tab.Mode, tab.Title, tab.URL)
	}
	return turn.Result{
		Content: fmt.Sprintf("%d shared tabs\n%s", len(tabs), web.Untrusted("the titles of the shared Chrome tabs", strings.Join(lines, "\n"))),
		Command: "shared tabs",
	}, nil
}

type browserRead struct{ session *browserSession }

func (browserRead) Name() string { return "browser_read" }

func (browserRead) Definition() llm.Tool {
	return llm.Tool{
		Name: "browser_read",
		Description: "reads one shared tab: its visible text, then a numbered table of the controls on screen, each with its role, label, value and state. " +
			"password, file and hidden fields are never listed. browser_act names a control by its number in the latest read of that tab. " +
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
	return turn.Result{
		Content: fmt.Sprintf("tab %d holds %d controls\n%s", args.Tab, len(page.Elements), web.Untrusted("the Chrome tab "+page.URL, body.String())),
		Command: fmt.Sprintf("tab %d %s", args.Tab, page.URL),
	}, nil
}

type browserAct struct{ session *browserSession }

func (browserAct) Name() string { return "browser_act" }

func (browserAct) Definition() llm.Tool {
	return llm.Tool{
		Name: "browser_act",
		Description: "runs one step in a tab shared to drive: CLICK, TYPE_TEXT or SELECT on a control numbered in the latest browser_read of that tab, or SCROLL_UP, SCROLL_DOWN or WAIT. " +
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
	var fresh bool
	err = t.session.with(func(client *browser.Client) error {
		tabs, err := client.Tabs()
		if err != nil {
			return err
		}
		shared := slices.IndexFunc(tabs, func(tab browser.Tab) bool { return tab.ID == args.Tab })
		switch {
		case shared < 0:
			return fmt.Errorf("tab %d is not shared with tofu", args.Tab)
		case tabs[shared].Mode != browser.ModeDrive:
			return fmt.Errorf("tab %d is shared for reading only: the person shares it to Drive to allow a step", args.Tab)
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
		fresh, err = browser.SharedTab{Client: client, ID: args.Tab}.Act(ctx, page, action)
		return err
	})
	if err != nil {
		return turn.Result{}, fmt.Errorf("browser_act: %w", err)
	}
	command := fmt.Sprintf("tab %d %s", args.Tab, op)
	if op == browser.OpClick || op == browser.OpTypeText || op == browser.OpSelect {
		command += fmt.Sprintf(" element %d", args.Element)
	}
	if !fresh {
		return turn.Result{Content: fmt.Sprintf("tab %d changed since browser_read, so %s did not run: read it again", args.Tab, op), Command: command}, nil
	}
	return turn.Result{Content: fmt.Sprintf("tab %d ran %s: read it again to see what changed", args.Tab, op), Command: command}, nil
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
