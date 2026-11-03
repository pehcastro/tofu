package keymap

import (
	"os"
	"slices"
	"strings"
)

type HostName string

const (
	Zed             HostName = "Zed"
	VSCode          HostName = "VS Code"
	JetBrains       HostName = "JetBrains IDE"
	Ghostty         HostName = "Ghostty"
	Alacritty       HostName = "Alacritty"
	WindowsTerminal HostName = "Windows Terminal"
	UnknownHost     HostName = "Unknown"
)

type Host struct {
	Name   HostName
	Source string
}

const (
	fromEnvironment = "terminal environment"
	fromParent      = "parent process"
	maxAncestors    = 12
)

func DetectHost() Host {
	markers := map[string]string{}
	for _, key := range []string{"TERM_PROGRAM", "TERMINAL_EMULATOR", "ZED_TERM", "VSCODE_PID", "WT_SESSION"} {
		markers[key] = os.Getenv(key)
	}
	return classifyHost(markers, ancestorNames())
}

func classifyHost(markers map[string]string, ancestors []string) Host {
	switch program := strings.ToLower(markers["TERM_PROGRAM"]); {
	case program == "zed":
		return Host{Zed, fromEnvironment}
	case program == "vscode", program == "vscode-insiders":
		return Host{VSCode, fromEnvironment}
	case program == "ghostty":
		return Host{Ghostty, fromEnvironment}
	case program == "alacritty":
		return Host{Alacritty, fromEnvironment}
	case strings.Contains(strings.ToLower(markers["TERMINAL_EMULATOR"]), "jetbrains"):
		return Host{JetBrains, fromEnvironment}
	}
	for _, name := range ancestors {
		switch name = strings.ToLower(name); {
		case name == "zed.exe", name == "zed", name == "zed-editor":
			return Host{Zed, fromParent}
		case name == "code.exe", name == "code - insiders.exe":
			return Host{VSCode, fromParent}
		case slices.Contains([]string{"idea64.exe", "idea.exe", "pycharm64.exe", "webstorm64.exe", "rider64.exe", "goland64.exe", "clion64.exe", "datagrip64.exe"}, name):
			return Host{JetBrains, fromParent}
		case name == "ghostty.exe", name == "ghostty":
			return Host{Ghostty, fromParent}
		case name == "alacritty.exe", name == "alacritty":
			return Host{Alacritty, fromParent}
		case name == "windowsterminal.exe":
			return Host{WindowsTerminal, fromParent}
		}
	}
	switch {
	case markers["VSCODE_PID"] != "":
		return Host{VSCode, fromEnvironment}
	case strings.EqualFold(markers["ZED_TERM"], "true"):
		return Host{Zed, fromEnvironment}
	case markers["WT_SESSION"] != "":
		return Host{WindowsTerminal, fromEnvironment}
	}
	return Host{UnknownHost, "no terminal marker found"}
}

func ProfileID(name HostName) string {
	switch name {
	case Zed:
		return "zed"
	case VSCode:
		return "vscode"
	case JetBrains:
		return "jetbrains"
	case Ghostty:
		return "ghostty"
	case Alacritty:
		return "alacritty"
	case WindowsTerminal:
		return "windows_terminal"
	}
	return ""
}
