package browser

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"tofu/internal/konst"
	"tofu/internal/sys"
)

const ProtocolVersion = 1

type Mode string

const (
	ModeRead  Mode = "read"
	ModeDrive Mode = "drive"
)

func (m *Mode) UnmarshalJSON(raw []byte) error {
	var name string
	if err := json.Unmarshal(raw, &name); err != nil {
		return err
	}
	switch Mode(name) {
	case ModeRead, ModeDrive:
		*m = Mode(name)
		return nil
	}
	return fmt.Errorf("unknown tab mode %q", name)
}

type Tab struct {
	ID     int    `json:"id"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Mode   Mode   `json:"mode,omitempty"`
	Opened bool   `json:"opened,omitempty"`
}

type messageType string

const (
	messageCall       messageType = "call"
	messageHello      messageType = "hello"
	messageShared     messageType = "shared"
	messageUnshared   messageType = "unshared"
	messageTabUpdated messageType = "tabUpdated"
	messageResult     messageType = "result"
)

type extensionCall struct {
	T     messageType     `json:"t"`
	ID    int64           `json:"id"`
	TabID int             `json:"tabId"`
	Op    string          `json:"op"`
	Args  json.RawMessage `json:"args,omitempty"`
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
	Mode    Mode        `json:"mode"`
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
		for _, tab := range message.Tabs {
			if tab.Mode == "" {
				return extensionMessage{}, fmt.Errorf("the extension said hello with tab %d and no mode", tab.ID)
			}
		}
	case messageShared:
		if message.Mode == "" {
			return extensionMessage{}, fmt.Errorf("the extension shared tab %d with no mode", message.Tab.ID)
		}
	case messageUnshared, messageTabUpdated, messageResult:
	default:
		return extensionMessage{}, fmt.Errorf("the extension sent the unknown message type %q", message.T)
	}
	return message, nil
}

const (
	opTabs  = "tabs"
	opOpen  = "open"
	opClose = "close"
)

type openArgs struct {
	URL string `json:"url"`
}

func opMode(op string) (Mode, error) {
	switch op {
	case "snapshot":
		return ModeRead, nil
	case "click", "fill", "select", "scroll", "wait":
		return ModeDrive, nil
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
