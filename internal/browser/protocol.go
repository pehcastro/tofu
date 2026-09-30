package browser

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/sys"
)

const (
	ProtocolVersion = 2
	relayRestarting = "the relay restarts on the dialling tofu's build"
)

type Tab struct {
	ID     int    `json:"id"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Opened bool   `json:"opened,omitempty"`
}

type messageType string

const (
	messageCall       messageType = "call"
	messageStatus     messageType = "status"
	messageHello      messageType = "hello"
	messageTabUpdated messageType = "tabUpdated"
	messageTabRemoved messageType = "tabRemoved"
	messageResult     messageType = "result"
	messageReload     messageType = "reload"
)

type Builds struct {
	Extension   string `json:"extension"`
	Tofu        string `json:"tofu"`
	UpdatedFrom string `json:"updated_from,omitempty"`
	Problem     string `json:"problem,omitempty"`
}

func (c *Client) Builds() (*Builds, error) {
	raw, err := c.Call(0, opBuilds, nil)
	if err != nil {
		return nil, err
	}
	var builds *Builds
	return builds, json.Unmarshal(raw, &builds)
}

type status string

const (
	statusIdle    status = "idle"
	statusReading status = "reading"
	statusActing  status = "acting"
)

type toExtension struct {
	Cursor bool            `json:"cursor,omitempty"`
	T      messageType     `json:"t"`
	ID     int64           `json:"id,omitempty"`
	TabID  int             `json:"tabId,omitempty"`
	Op     string          `json:"op,omitempty"`
	Args   json.RawMessage `json:"args,omitempty"`
	State  status          `json:"state,omitempty"`
}

type Timing struct {
	EvaluateMS float64 `json:"evaluate_ms"`
	SettleMS   float64 `json:"settle_ms"`
	ActMS      float64 `json:"act_ms"`
}

type result struct {
	ID     int64           `json:"id"`
	OK     bool            `json:"ok"`
	Value  json.RawMessage `json:"value,omitempty"`
	Error  string          `json:"error,omitempty"`
	Timing *Timing         `json:"timing,omitempty"`
	Host   time.Duration   `json:"host_ns,omitempty"`
}

type extensionMessage struct {
	T       messageType `json:"t"`
	Version int         `json:"version"`
	Build   string      `json:"build"`
	Tabs    []Tab       `json:"tabs"`
	Tab     Tab         `json:"tab"`
	TabID   int         `json:"tabId"`
	result
}

func parseExtensionMessage(raw []byte) (extensionMessage, error) {
	var message extensionMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		return extensionMessage{}, fmt.Errorf("the extension sent %s: %w", raw, err)
	}
	switch message.T {
	case messageHello:
		if message.Version != ProtocolVersion {
			return extensionMessage{}, fmt.Errorf("the extension speaks protocol %d and this tofu speaks %d: run tofu browser install and reload the extension", message.Version, ProtocolVersion)
		}
	case messageTabUpdated, messageTabRemoved, messageResult:
	default:
		return extensionMessage{}, fmt.Errorf("the extension sent the unknown message type %q", message.T)
	}
	return message, nil
}

func reachable(address string) bool {
	parsed, err := url.Parse(address)
	if err != nil {
		return false
	}
	switch parsed.Scheme {
	case "chrome", "devtools", "edge", "view-source", "chrome-extension", "chrome-untrusted", "chrome-search":
		return false
	}
	return parsed.Host != "chromewebstore.google.com" && (parsed.Host != "chrome.google.com" || !strings.HasPrefix(parsed.Path, "/webstore"))
}

const (
	opTabs   = "tabs"
	opBuilds = "builds"
	opOpen   = "open"
	opClose  = "close"
	opBack   = "back"
	opHello  = "hello"
	opCDP    = "cdp"
)

type openArgs struct {
	URL string `json:"url"`
}

func opStatus(req request) (status, error) {
	switch req.Op {
	case "snapshot":
		return statusReading, nil
	case "click", "fill", "select", "scroll", "wait":
		return statusActing, nil
	case opCDP:
		return cdpStatus(req.Args)
	}
	return "", fmt.Errorf("unknown browser op %q", req.Op)
}

type cdpCall struct {
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type cdpArgs struct {
	Calls []cdpCall    `json:"calls"`
	Act   bool         `json:"act,omitempty"`
	Point *cursorPoint `json:"point,omitempty"`
}

type cursorPoint struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Label string  `json:"label"`
}

type cdpAnswer struct {
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

func cdpStatus(raw json.RawMessage) (status, error) {
	var args cdpArgs
	if err := json.Unmarshal(raw, &args); err != nil || (len(args.Calls) == 0 && args.Point == nil) {
		return "", fmt.Errorf("a cdp op carries no calls: %s", raw)
	}
	now := statusReading
	if args.Act {
		now = statusActing
	}
	for _, call := range args.Calls {
		switch call.Method {
		case "Accessibility.getFullAXTree", "Accessibility.getPartialAXTree", "Page.getFrameTree",
			"DOM.getDocument", "DOM.querySelectorAll", "DOM.describeNode", "DOM.resolveNode", "DOM.getBoxModel", "DOM.scrollIntoViewIfNeeded",
			"Runtime.evaluate", "Runtime.callFunctionOn", "Emulation.setFocusEmulationEnabled", "Page.setWebLifecycleState":
		case "Input.dispatchMouseEvent", "Input.dispatchKeyEvent", "Input.insertText":
			now = statusActing
		default:
			return "", fmt.Errorf("tofu does not pass the CDP method %q to Chrome", call.Method)
		}
	}
	return now, nil
}

type request struct {
	ID   int64           `json:"id"`
	Op   string          `json:"op"`
	Tab  int             `json:"tab,omitempty"`
	Args json.RawMessage `json:"args,omitempty"`
}

func socketPath(home string) (string, error) {
	path := filepath.Join(home, sys.StateDirName, "browser", "relay.sock")
	if len(path) > konst.BrowserSocketPathMaxBytes {
		return "", fmt.Errorf("the browser socket %s is %d bytes, over the %d byte limit of an AF_UNIX path: use a shorter home", path, len(path), konst.BrowserSocketPathMaxBytes)
	}
	return path, nil
}

func ExtensionDir(home string) string {
	extensionDir, _ := installPaths(home)
	return extensionDir
}
