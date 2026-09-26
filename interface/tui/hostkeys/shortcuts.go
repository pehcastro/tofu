package hostkeys

import (
	"fmt"
	"maps"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/look"
	"tofu/internal/keymap"
)

type Shortcuts struct {
	path     string
	host     keymap.HostName
	bindings map[string]string
	cursor   int
	capture  string
	feedback string
}

func NewShortcuts(path string, bindings map[string]string) Shortcuts {
	return Shortcuts{path: path, host: keymap.DetectHost().Name, bindings: maps.Clone(bindings)}
}

func (s Shortcuts) Bindings() map[string]string { return maps.Clone(s.bindings) }

func (s *Shortcuts) Key(msg tea.KeyPressMsg) (changed bool, closed bool) {
	actions := keymap.Actions()
	if action := s.capture; action != "" {
		s.capture = ""
		if msg.String() == "esc" {
			s.feedback = "Change cancelled"
			return false, false
		}
		return s.set(action, msg.Key().Keystroke()), false
	}
	switch msg.String() {
	case "esc":
		return false, true
	case "up":
		s.cursor = (s.cursor + len(actions) - 1) % len(actions)
	case "down":
		s.cursor = (s.cursor + 1) % len(actions)
	case "enter":
		s.capture = actions[s.cursor]
		s.feedback = "Press a shortcut, Esc to cancel, Backspace to clear."
	case "backspace", "delete":
		return s.set(actions[s.cursor], ""), false
	}
	return false, false
}

func (s *Shortcuts) Click(x, y, width, height int) bool {
	if index := choiceAt(s.dialog(width), s.labels(), x, y, width, height); index >= 0 {
		s.cursor = index
		s.Key(tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	return false
}

func (s Shortcuts) Over(base string, width, height int) string {
	return place(base, s.dialog(width), width, height)
}

func (s *Shortcuts) set(action, key string) bool {
	if key == "backspace" || key == "delete" {
		key = ""
	}
	if err := keymap.ValidShortcut(key); err != nil {
		s.feedback = err.Error()
		return false
	}
	for other, assigned := range s.bindings {
		if key != "" && other != action && assigned == key {
			s.feedback = key + " is already assigned to " + other
			return false
		}
	}
	previous := s.bindings[action]
	s.bindings[action] = key
	if err := keymap.SaveShortcuts(s.path, s.bindings); err != nil {
		s.bindings[action] = previous
		s.feedback = "Could not save shortcut: " + err.Error()
		return false
	}
	s.feedback = action + " is now " + key
	if key == "" {
		s.feedback = action + " shortcut cleared"
	}
	if previous == key {
		return false
	}
	if supported(s.host) {
		s.feedback += "; check Host integration"
	}
	return true
}

func (s Shortcuts) labels() []string {
	var labels []string
	for _, action := range keymap.Actions() {
		key := s.bindings[action]
		if key == "" {
			key = "not set"
		}
		labels = append(labels, fmt.Sprintf("%-*s  %s", actionColumn, action, key))
	}
	return labels
}

func (s Shortcuts) dialog(width int) string {
	body := look.SectionLabel("TOFU ACTIONS") + "\n" + look.Muted("These work after the terminal sends the key to tofu.") + "\n" + choices(s.labels(), s.cursor)
	if s.capture != "" {
		body += "\n\n" + look.Accent("Press a shortcut for "+s.capture)
	}
	if s.feedback != "" {
		body += "\n" + look.Muted(s.feedback)
	}
	body += "\n\n" + look.Faint("Host editors may still intercept a chosen key.")
	return look.DialogPanel(min(compactWidth, width-dialogMargin), "Keybindings", "Tofu shortcuts", body, "↑↓ choose · enter edit · bksp clear · esc")
}
