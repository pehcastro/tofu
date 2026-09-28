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

const ProtocolVersion = 2

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
)

type status string

const (
	statusIdle    status = "idle"
	statusReading status = "reading"
	statusActing  status = "acting"
)

type toExtension struct {
	T     messageType     `json:"t"`
	ID    int64           `json:"id,omitempty"`
	TabID int             `json:"tabId,omitempty"`
	Op    string          `json:"op,omitempty"`
	Args  json.RawMessage `json:"args,omitempty"`
	State status          `json:"state,omitempty"`
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
	opTabs  = "tabs"
	opOpen  = "open"
	opClose = "close"
)

type openArgs struct {
	URL string `json:"url"`
}

func opStatus(op string) (status, error) {
	switch op {
	case "snapshot":
		return statusReading, nil
	case "click", "fill", "select", "scroll", "wait":
		return statusActing, nil
	}
	return "", fmt.Errorf("unknown browser op %q", op)
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
