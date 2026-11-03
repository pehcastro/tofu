package keymap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	terminalContext = "Terminal"
	zedForward      = "terminal::SendKeystroke"
	terminalFocus   = "terminalFocus"
	vscodeForward   = "workbench.action.terminal.sendSequence"
)

type PlanItem struct {
	Action string
	Key    string
	Effect string
	Reason string
	Rule   string
}

func KeymapPath(host HostName) (string, error) {
	var relative string
	base, err := os.UserConfigDir()
	switch {
	case host == Zed && runtime.GOOS == "windows":
		relative = filepath.Join("Zed", "keymap.json")
	case host == Zed:
		base, err = os.UserHomeDir()
		relative = filepath.Join(".config", "zed", "keymap.json")
	case host == VSCode:
		product := "Code"
		if strings.EqualFold(os.Getenv("TERM_PROGRAM"), "vscode-insiders") || strings.Contains(strings.ToLower(os.Getenv("VSCODE_IPC_HOOK_CLI")), "insiders") {
			product = "Code - Insiders"
		}
		relative = filepath.Join(product, "User", "keybindings.json")
	default:
		return "", fmt.Errorf("no automatic keymap integration for %s", host)
	}
	if err != nil {
		return "", err
	}
	return filepath.Join(base, relative), nil
}

func vscodeRule(key string) (string, bool) {
	modifier, letter, ok := strings.Cut(key, "+")
	if !ok || len(letter) != 1 || letter[0] < 'a' || letter[0] > 'z' {
		return "", false
	}
	var sequence string
	switch modifier {
	case "ctrl":
		sequence = string(rune(letter[0] - 'a' + 1))
	case "alt":
		sequence = "\x1b" + letter
	default:
		return "", false
	}
	rule := struct {
		Key     string `json:"key"`
		Command string `json:"command"`
		Args    struct {
			Text string `json:"text"`
		} `json:"args"`
		When string `json:"when"`
	}{Key: key, Command: vscodeForward, When: terminalFocus}
	rule.Args.Text = sequence
	encoded, err := json.Marshal(rule)
	return string(encoded), err == nil
}

func Plan(host HostName, shortcuts, collisions map[string]string) (items []PlanItem, skipped []string) {
	profile, _ := Lookup(ProfileID(host))
	for _, action := range Actions() {
		key := strings.ToLower(shortcuts[action])
		if key == "" {
			continue
		}
		item := PlanItem{Action: action, Key: key, Reason: "ensure tofu receives this shortcut"}
		for _, conflict := range Conflicts(profile, map[string]string{action: key}) {
			item.Reason = conflict.Binding.Owner
		}
		if existing := collisions[key]; existing != "" {
			item.Reason = "takes priority over user rule: " + existing
		}
		switch host {
		case Zed:
			zedKey := strings.ReplaceAll(key, "+", "-")
			item.Effect = "forward in Terminal context"
			item.Rule = `"` + zedKey + `": ["` + zedForward + `", "` + zedKey + `"]`
		case VSCode:
			rule, ok := vscodeRule(key)
			if !ok {
				skipped = append(skipped, action+" "+key+" (not safely encodable)")
				continue
			}
			item.Effect, item.Rule = "send to terminalFocus", rule
		default:
			continue
		}
		items = append(items, item)
	}
	return items, skipped
}

func elements(host HostName, shortcuts map[string]string) []string {
	items, _ := Plan(host, shortcuts, nil)
	if host != Zed {
		rules := make([]string, 0, len(items))
		for _, item := range items {
			rules = append(rules, item.Rule)
		}
		return rules
	}
	if len(items) == 0 {
		return nil
	}
	bindings := map[string][]string{}
	for _, item := range items {
		key := strings.ReplaceAll(item.Key, "+", "-")
		bindings[key] = []string{zedForward, key}
	}
	encoded, _ := json.Marshal(struct {
		Context  string              `json:"context"`
		Bindings map[string][]string `json:"bindings"`
	}{terminalContext, bindings})
	return []string{string(encoded)}
}

func Collisions(host HostName, path string, shortcuts map[string]string) map[string]string {
	found := map[string]string{}
	source, err := os.ReadFile(path)
	if err != nil {
		return found
	}
	array, err := parseJSONC(source)
	if err != nil {
		return found
	}
	wanted := map[string]bool{}
	for _, key := range shortcuts {
		if key != "" {
			wanted[strings.ToLower(key)] = true
		}
	}
	switch host {
	case VSCode:
		var rules []struct{ Key, Command, When string }
		if json.Unmarshal(array.clean, &rules) != nil {
			return found
		}
		for _, rule := range rules {
			key := strings.ToLower(rule.Key)
			if wanted[key] && (rule.When == "" || strings.Contains(rule.When, terminalFocus)) && rule.Command != vscodeForward {
				found[key] = rule.Command
			}
		}
	case Zed:
		for _, entry := range array.entries {
			if entry.Context != "" && !strings.Contains(entry.Context, terminalContext) {
				continue
			}
			for key, action := range entry.Bindings {
				tofuKey := strings.ReplaceAll(strings.ToLower(key), "-", "+")
				if wanted[tofuKey] && !strings.Contains(string(action), zedForward) {
					found[tofuKey] = "Zed " + entry.Context + " action"
				}
			}
		}
	}
	return found
}

func Inspect(host HostName, path string, shortcuts map[string]string) (State, error) {
	state, err := inspectManaged(path, ProfileID(host), elements(host, shortcuts))
	if err != nil || state != NotConfigured || host != Zed {
		return state, err
	}
	legacy, err := legacyState(path)
	if legacy == Managed {
		legacy = LegacyOnly
	}
	return legacy, err
}

func Apply(host HostName, path string, shortcuts map[string]string) (status, backup string, err error) {
	return applyManaged(path, ProfileID(host), elements(host, shortcuts))
}

func Undo(host HostName, path string) (status string, backups []string, err error) {
	status, backup, err := undoManaged(path, ProfileID(host))
	backups = nonEmpty(backups, backup)
	if err != nil || host != Zed {
		return status, backups, err
	}
	if legacy, stateErr := legacyState(path); stateErr != nil || legacy != Managed {
		return status, backups, nil
	}
	status, backup, err = legacyUndo(path)
	return status, nonEmpty(backups, backup), err
}

func nonEmpty(backups []string, backup string) []string {
	if backup == "" {
		return backups
	}
	return append(backups, backup)
}
