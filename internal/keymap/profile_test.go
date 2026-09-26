package keymap

import (
	"strings"
	"testing"
)

func TestProfilesAndConflicts(t *testing.T) {
	if err := validateProfiles(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"zed", "vscode", "jetbrains", "ghostty", "alacritty", "windows_terminal"} {
		if _, ok := Lookup(id); !ok {
			t.Fatalf("missing %s profile", id)
		}
	}
	if _, ok := Lookup("../zed"); ok {
		t.Fatal("accepted path traversal")
	}
	profile, _ := Lookup("vscode")
	conflicts := Conflicts(profile, map[string]string{"Search": "ctrl+k", "Commands": "alt+k"})
	if len(conflicts) != 1 || conflicts[0].Action != "Search" {
		t.Fatalf("unexpected conflicts: %+v", conflicts)
	}
	profile, _ = Lookup("ghostty")
	if len(Conflicts(profile, map[string]string{"Search": "ctrl+k"})) != 0 {
		t.Fatal("invented Ghostty default conflict")
	}
}

func TestRuleEncodesConfigurableTerminalShortcuts(t *testing.T) {
	for _, tc := range []struct{ key, sequence string }{
		{"ctrl+k", `"\u000b"`},
		{"ctrl+l", `"\f"`},
		{"alt+k", `"\u001bk"`},
	} {
		rule, ok := vscodeRule(tc.key)
		if !ok || !strings.Contains(rule, `"when":"terminalFocus"`) || !strings.Contains(rule, tc.sequence) {
			t.Fatalf("invalid %s rule: %s", tc.key, rule)
		}
	}
	if _, ok := vscodeRule("ctrl+shift+k"); ok {
		t.Fatal("claimed a key without a safe terminal encoding")
	}
}

func TestClassifyTerminalHost(t *testing.T) {
	for _, tc := range []struct {
		name      string
		markers   map[string]string
		ancestors []string
		want      HostName
	}{
		{"zed marker", map[string]string{"TERM_PROGRAM": "zed"}, nil, Zed},
		{"zed ancestor", nil, []string{"powershell.exe", "Zed.exe"}, Zed},
		{"vscode marker", map[string]string{"TERM_PROGRAM": "vscode"}, nil, VSCode},
		{"vscode overrides inherited Zed marker", map[string]string{"TERM_PROGRAM": "vscode", "ZED_TERM": "true"}, nil, VSCode},
		{"ghostty marker", map[string]string{"TERM_PROGRAM": "ghostty"}, nil, Ghostty},
		{"alacritty parent", nil, []string{"pwsh.exe", "alacritty.exe"}, Alacritty},
		{"jetbrains parent", nil, []string{"pwsh.exe", "idea64.exe"}, JetBrains},
		{"windows terminal", map[string]string{"WT_SESSION": "abc"}, nil, WindowsTerminal},
		{"zed child inherits terminal marker", map[string]string{"WT_SESSION": "abc"}, []string{"powershell.exe", "Zed.exe", "WindowsTerminal.exe"}, Zed},
		{"unknown", nil, []string{"powershell.exe"}, UnknownHost},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyHost(tc.markers, tc.ancestors); got.Name != tc.want {
				t.Fatalf("got %q, want %q", got.Name, tc.want)
			}
		})
	}
}
