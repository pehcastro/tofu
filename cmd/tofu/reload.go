package main

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/keymap"
	"tofu/internal/llm/models"
	settingspkg "tofu/internal/settings"
	"tofu/internal/skill"
	"tofu/internal/subagent"
	"tofu/internal/sys"
)

const (
	reloadFileName = "reload.json"
	reloadUsage    = "usage: tofu reload [--json]"
)

type reloadPart struct {
	Name   string            `json:"name"`
	Detail string            `json:"detail,omitempty"`
	Items  map[string]string `json:"items"`
}

type partDiff struct {
	Name    string   `json:"name"`
	Detail  string   `json:"detail,omitempty"`
	Count   int      `json:"count"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Changed []string `json:"changed,omitempty"`
}

type reloadDiff struct {
	Project    string     `json:"project"`
	First      bool       `json:"first"`
	Unreadable string     `json:"last_unreadable,omitempty"`
	Parts      []partDiff `json:"parts"`
}

func digest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:6])
}

func takeInventory(dir string) ([]reloadPart, error) {
	store, err := openSettings(dir)
	if err != nil {
		return nil, err
	}
	rules, _, err := loadRules("", dir)
	if err != nil {
		return nil, err
	}
	library, err := modelLibrary(dir)
	var broken *models.BrokenLibrary
	if err != nil && !errors.As(err, &broken) {
		return nil, err
	}
	agents, err := agentsIn(dir)
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	shortcuts, _ := keymap.ShortcutsPath()

	settingsIn := [2]map[string]string{{}, {}}
	for _, spec := range store.Table() {
		scope, fromFile := store.Source(spec.Key)
		if !fromFile {
			continue
		}
		value := strconv.Itoa(store.Int(spec.Key))
		if spec.Kind == settingspkg.Text {
			value = store.Text(spec.Key)
		}
		settingsIn[scope][spec.Key] = value
	}
	ruleItems := map[string]string{}
	for _, one := range rules {
		ruleItems[one.ID] = digest(string(one.Mode), one.Text)
	}
	skills := map[string]string{}
	for _, one := range skill.Discover(dir, home).Skills {
		body, _ := os.ReadFile(one.File)
		skills[one.Name] = digest(one.File, string(body))
	}
	defined, brokenAgents := map[string]string{}, map[string]string{}
	for _, one := range agents.Definitions {
		defined[one.Name] = digest(one.Path, string(one.Runs), one.Model, one.Description)
	}
	for _, one := range agents.Broken {
		brokenAgents[one.Path] = one.Reason
	}
	all, usable, layers := map[string]string{}, map[string]string{}, map[string]int{}
	for _, one := range library.Models {
		all[one.Slug()] = one.Layer + " " + string(one.Use)
		layers[one.Layer]++
		if one.Use != models.UseExcluded {
			usable[one.Slug()] = one.Layer
		}
	}
	var byLayer []string
	for layer, count := range layers {
		byLayer = append(byLayer, layer+" "+strconv.Itoa(count))
	}
	sort.Strings(byLayer)
	roles, tiers := map[string]string{}, map[string]string{}
	for _, one := range library.Roles {
		roles[string(one.ID)] = one.Model.Slug()
	}
	for _, tier := range subagent.Tiers() {
		if slug := strings.TrimSpace(store.Text(tier.Setting())); slug != "" {
			tiers["@"+string(tier)] = slug
		}
	}
	instructions := map[string]string{}
	candidates := []string{filepath.Join(sys.StateDir(home), "AGENTS.md")}
	for _, folder := range skill.ProjectFolders(dir, home) {
		for _, name := range settingspkg.InstructionFiles(store.Text(settingspkg.InstructionSources)) {
			candidates = append(candidates, filepath.Join(folder, name))
		}
	}
	for _, path := range candidates {
		if body, err := os.ReadFile(path); err == nil {
			instructions[path] = digest(string(body))
		}
	}
	bound := map[string]string{}
	for action, key := range keymap.LoadShortcuts(shortcuts) {
		if key != "" {
			bound[action] = key
		}
	}
	brokenFiles := map[string]string{}
	for _, one := range library.Broken {
		brokenFiles[one.File] = one.Error()
	}
	return []reloadPart{
		{Name: "global settings", Items: settingsIn[settingspkg.Global]},
		{Name: "project settings", Items: settingsIn[settingspkg.Project]},
		{Name: "rules", Items: ruleItems},
		{Name: "skills", Items: skills},
		{Name: "sub-agents", Items: defined},
		{Name: "broken sub-agents", Items: brokenAgents},
		{Name: "models", Detail: strings.Join(byLayer, ", "), Items: all},
		{Name: "usable models", Items: usable},
		{Name: "roles", Items: roles},
		{Name: "tiers", Items: tiers},
		{Name: "instruction files", Items: instructions},
		{Name: "keymap", Items: bound},
		{Name: "broken library files", Items: brokenFiles},
	}, nil
}

func describeReload(now, last []reloadPart) []partDiff {
	diffs := make([]partDiff, 0, len(now))
	for _, part := range now {
		diff := partDiff{Name: part.Name, Detail: part.Detail, Count: len(part.Items)}
		if at := slices.IndexFunc(last, func(was reloadPart) bool { return was.Name == part.Name }); at >= 0 {
			before := last[at].Items
			for name, fingerprint := range part.Items {
				was, had := before[name]
				switch {
				case !had:
					diff.Added = append(diff.Added, name)
				case was != fingerprint:
					diff.Changed = append(diff.Changed, name)
				}
			}
			for name := range before {
				if _, kept := part.Items[name]; !kept {
					diff.Removed = append(diff.Removed, name)
				}
			}
		}
		sort.Strings(diff.Added)
		sort.Strings(diff.Removed)
		sort.Strings(diff.Changed)
		diffs = append(diffs, diff)
	}
	return diffs
}

func reloadReport(dir string) (reloadDiff, error) {
	now, err := takeInventory(dir)
	if err != nil {
		return reloadDiff{}, err
	}
	state, err := sys.ProjectStateDirAt(dir)
	if err != nil {
		return reloadDiff{}, err
	}
	path := filepath.Join(state, reloadFileName)
	report := reloadDiff{Project: dir}
	var last []reloadPart
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		report.First = true
	case err != nil || json.Unmarshal(data, &last) != nil:
		report.Unreadable, last = path, nil
	}
	encoded, err := json.MarshalIndent(now, "", "  ")
	if err != nil {
		return reloadDiff{}, err
	}
	if err := sys.WriteFile(path, encoded, 0o600); err != nil {
		return reloadDiff{}, err
	}
	report.Parts = describeReload(now, last)
	return report, nil
}

type partSide struct {
	mark  cli.Mark
	sign  string
	names []string
}

func (p partDiff) sides() []partSide {
	return []partSide{{cli.Added, "+", p.Added}, {cli.Removed, "-", p.Removed}, {cli.Changed, "~", p.Changed}}
}

func (d reloadDiff) lines(page cli.Page) []string {
	title := func(verdict cli.Verdict) []string {
		return page.Title("Reload", []string{page.Path(d.Project)}, verdict)
	}
	if d.First || d.Unreadable != "" {
		verdict := cli.Verdict{Mark: cli.Done, Text: "first reload"}
		if !d.First {
			verdict = cli.Verdict{Mark: cli.Warn, Text: "last reload unreadable"}
		}
		var counted []cli.Fact
		for _, part := range d.Parts {
			counted = append(counted, cli.Fact{Label: part.Name, Text: nonZero(part.Count)})
		}
		return append(append(title(verdict), ""), cli.Indent(page.Facts(counted)...)...)
	}
	var body []string
	counts := map[cli.Mark]int{}
	for _, part := range d.Parts {
		var rows []cli.Row
		for _, side := range part.sides() {
			counts[side.mark] += len(side.names)
			for _, name := range side.names {
				rows = append(rows, cli.Row{Mark: side.mark, Cells: []string{name}})
			}
		}
		if len(rows) > 0 {
			body = append(append(body, "", page.Section(part.Name, cli.Verdict{})), cli.Indent(page.Rows(rows)...)...)
		}
	}
	said := countsSaid(counts, []markNoun{{cli.Added, "added"}, {cli.Removed, "removed"}, {cli.Changed, "changed"}})
	return append(title(cli.Verdict{Mark: cli.Done, Text: cmp.Or(said, "nothing changed")}), body...)
}

func (d reloadDiff) note() string {
	switch {
	case d.First:
		return "reloaded, the first reload here"
	case d.Unreadable != "":
		return "reloaded, the last reload could not be read"
	}
	var parts []string
	for _, part := range d.Parts {
		said := ""
		for _, side := range part.sides() {
			if len(side.names) > 0 {
				said += " " + side.sign + strconv.Itoa(len(side.names))
			}
		}
		if said != "" {
			parts = append(parts, part.Name+said)
		}
	}
	if len(parts) == 0 {
		return "reloaded, nothing changed"
	}
	return "reloaded: " + strings.Join(parts, " · ")
}

func reloadVerb(args []string, out, errOut io.Writer) int {
	asJSON := false
	for _, arg := range args {
		if arg != jsonFlag {
			_, _ = fmt.Fprintf(errOut, "tofu reload: unknown flag %q, %s\n", arg, reloadUsage)
			return exitUsage
		}
		asJSON = true
	}
	var report reloadDiff
	dir, err := os.Getwd()
	if err == nil {
		report, err = reloadReport(dir)
	}
	if err != nil {
		return verbFailed(out, errOut, "reload", asJSON, err)
	}
	return show(out, asJSON, cli.Envelope{Verb: "reload", OK: true, At: time.Now(), Data: report}, report.lines)
}

func appReload(project string) func() string {
	return func() string {
		report, err := reloadReport(project)
		if err != nil {
			return "reload failed: " + err.Error()
		}
		return report.note()
	}
}
