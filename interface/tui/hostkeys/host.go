package hostkeys

import (
	"maps"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/look"
	"tofu/internal/keymap"
)

type operation string

const (
	apply operation = "apply"
	undo  operation = "undo"
)

const unavailable = "unavailable: "

type result struct {
	status  string
	backups []string
	err     error
}

type Host struct {
	host       keymap.Host
	path       string
	bindings   map[string]string
	collisions map[string]string
	status     string
	pending    operation
	feedback   string
	backups    []string
	busy       bool
	cursor     int
}

func NewHost(bindings map[string]string) Host {
	h := Host{host: keymap.DetectHost(), bindings: maps.Clone(bindings)}
	if !supported(h.host.Name) {
		h.status = "no automatic integration for this host"
		return h
	}
	path, err := keymap.KeymapPath(h.host.Name)
	if err != nil {
		h.status = unavailable + err.Error()
		return h
	}
	h.path = path
	h.collisions = keymap.Collisions(h.host.Name, path, h.bindings)
	h.inspect()
	return h
}

func (h *Host) inspect() {
	state, err := keymap.Inspect(h.host.Name, h.path, h.bindings)
	h.status = string(state)
	if err != nil {
		h.status = unavailable + err.Error()
	}
}

func (h Host) actionable() bool {
	return supported(h.host.Name) && !strings.HasPrefix(h.status, unavailable)
}

func (h Host) labels() []string {
	switch {
	case h.busy:
		return nil
	case h.pending == apply:
		return []string{"Apply to " + string(h.host.Name), "Cancel"}
	case h.pending == undo:
		return []string{"Remove tofu binding", "Cancel"}
	case !h.actionable():
		return []string{"Close"}
	}
	return []string{"Apply tofu keybindings", "Undo tofu keybindings", "Close"}
}

func (h *Host) Key(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	rows := max(1, len(h.labels()))
	switch msg.String() {
	case "esc":
		return nil, !h.busy
	case "up":
		h.cursor = (h.cursor + rows - 1) % rows
	case "down":
		h.cursor = (h.cursor + 1) % rows
	case "enter":
		return h.choose()
	}
	return nil, false
}

func (h *Host) Click(x, y, width, height int) (tea.Cmd, bool) {
	index := choiceAt(h.dialog(width, height), h.labels(), x, y, width, height)
	if index < 0 {
		return nil, false
	}
	h.cursor = index
	return h.choose()
}

func (h *Host) choose() (tea.Cmd, bool) {
	switch {
	case h.busy:
		return nil, false
	case !h.actionable():
		return nil, true
	case h.pending != "" && h.cursor == 1:
		h.pending, h.cursor = "", 0
		return nil, false
	case h.pending != "":
		h.busy = true
		return h.run(), false
	case h.cursor == 0:
		h.pending = apply
	case h.cursor == 1:
		h.pending = undo
	default:
		return nil, true
	}
	h.cursor = 1
	return nil, false
}

func (h Host) run() tea.Cmd {
	op, name, path, bindings := h.pending, h.host.Name, h.path, maps.Clone(h.bindings)
	return func() tea.Msg {
		if op == undo {
			status, backups, err := keymap.Undo(name, path)
			return result{status: status, backups: backups, err: err}
		}
		status, backup, err := keymap.Apply(name, path, bindings)
		r := result{status: status, err: err}
		if backup != "" {
			r.backups = []string{backup}
		}
		return r
	}
}

func (h *Host) Result(msg tea.Msg) bool {
	r, ok := msg.(result)
	if !ok {
		return false
	}
	h.busy, h.pending, h.backups, h.cursor = false, "", r.backups, 0
	h.feedback = r.status
	if r.err != nil {
		h.feedback = "Could not update keymap: " + r.err.Error()
	}
	h.inspect()
	return true
}

func (h Host) Over(base string, width, height int) string {
	return place(base, h.dialog(width, height), width, height)
}

func (h Host) dialog(width, height int) string {
	if height <= compactRows {
		return h.compact(width)
	}
	name := string(h.host.Name)
	plan, skipped := keymap.Plan(h.host.Name, h.bindings, h.collisions)
	body := look.SectionLabel("CURRENT HOST") + "\n" + look.Title(name) + look.Muted("  ·  "+h.host.Source)
	if supported(h.host.Name) {
		body += "\n\n" + look.SectionLabel("TOFU SHORTCUTS · PROPOSED TERMINAL RULES")
		for _, item := range plan {
			body += "\n" + look.Title(strings.ToUpper(item.Key)) + look.Muted("  "+item.Action+" · "+item.Reason)
		}
		for _, item := range skipped {
			body += "\n" + look.Faint("Skipped: "+item)
		}
		body += "\n" + look.Faint("Change: forward these keys only while the terminal is focused.")
	}
	if profile, found := keymap.Lookup(keymap.ProfileID(h.host.Name)); found {
		body += "\n\n" + look.SectionLabel("SHORTCUT CONFLICTS")
		count := 0
		for _, action := range keymap.Actions() {
			for _, conflict := range keymap.Conflicts(profile, map[string]string{action: h.bindings[action]}) {
				body += "\n" + look.Title(strings.ToUpper(conflict.Key)) + look.Muted("  tofu "+action+" · "+conflict.Binding.Owner)
				if conflict.Binding.Status == "possible" {
					body += look.Faint(" (check active keymap)")
				}
				count++
			}
		}
		if count == 0 {
			body += "\n" + look.Muted("No documented default conflict for current tofu bindings.")
		}
		if height >= inspectRows {
			body += "\n" + look.Muted(profile.Inspection)
		}
		if height >= sourceRows {
			body += "\n" + look.Faint("Profile: "+profile.Source)
		}
	} else {
		body += "\n\n" + look.Muted("Host could not be identified. Check editor and terminal shortcuts manually.")
	}
	if supported(h.host.Name) {
		body += "\n\n" + look.SectionLabel(name+" USER KEYMAP") + "\n" + look.Muted("Status: ") + look.Title(h.status)
	}
	if h.feedback != "" {
		body += "\n\n" + look.Muted(h.feedback)
	}
	for _, backup := range h.backups {
		body += "\n" + look.Faint("Backup: "+filepath.Base(backup))
	}
	switch {
	case h.pending != "":
		body += "\n\n" + look.SectionLabel("CONFIRM CHANGE") + "\n" + look.Muted(h.confirmation())
	case supported(h.host.Name):
		body += "\n" + look.Faint("Apply adds terminal-only rules after existing rules; Undo removes tofu's rules.")
	default:
		body += "\n" + look.Faint("No verified live adapter for this host; its settings will not be changed.")
	}
	if h.busy {
		body += "\n\n" + look.Accent("Updating "+name+" keymap…")
	} else {
		body += "\n" + choices(h.labels(), h.cursor)
	}
	return look.DialogPanel(min(hostWidth, width-dialogMargin), "Host integration", "Detected shortcuts and safe actions", body, "↑↓ choose · enter select · esc close")
}

func (h Host) confirmation() string {
	name := string(h.host.Name)
	if h.status == string(keymap.Missing) {
		return "Creates " + name + " keymap; no prior file to back up."
	}
	text := "Only " + name + " user keybindings change. A backup is saved first."
	if h.pending == undo && h.host.Name == keymap.Zed {
		text += " Undo also removes the legacy tofu Ctrl+K block if it is there."
	}
	return text
}

func (h Host) compact(width int) string {
	body := look.SectionLabel("CURRENT HOST") + "\n" + look.Title(string(h.host.Name)) + look.Muted(" · "+h.host.Source)
	if supported(h.host.Name) {
		plan, skipped := keymap.Plan(h.host.Name, h.bindings, h.collisions)
		forwarded := make([]string, 0, len(plan))
		for _, item := range plan {
			forwarded = append(forwarded, look.Title(item.Key)+look.Muted(" "+item.Action))
		}
		body += "\n" + look.SectionLabel("WILL FORWARD IN TERMINAL") + "\n" + strings.Join(forwarded, look.Muted(" · "))
		if len(skipped) > 0 {
			body += "\n" + look.Faint("Unsupported: "+strings.Join(skipped, ", "))
		}
	}
	body += "\n" + look.Muted("Status: "+h.status)
	if h.feedback != "" {
		body += "\n" + look.Muted(h.feedback)
	}
	if h.pending == undo && h.host.Name == keymap.Zed {
		body += "\n" + look.Muted("Also removes legacy Ctrl+K block; backup first.")
	} else if h.pending != "" {
		body += "\n" + look.Muted("Confirm terminal-only changes; backup saved first.")
	}
	if len(h.labels()) > 0 {
		body += "\n" + choices(h.labels(), h.cursor)
	}
	return look.DialogPanel(min(compactWidth, width-dialogMargin), "Host integration", "Terminal shortcut ownership", body, "enter select · esc close")
}
