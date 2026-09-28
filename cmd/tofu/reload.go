package main

import (
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

	"tofu/internal/keymap"
	"tofu/internal/llm/models"
	settingspkg "tofu/internal/settings"
	"tofu/internal/skill"
	"tofu/internal/subagent"
	"tofu/internal/sys"
)

const reloadFileName = "reload.json"

type reloadPart struct {
	Name   string            `json:"name"`
	Detail string            `json:"detail,omitempty"`
	Items  map[string]string `json:"items"`
}

func runReload(project string) (int, error) {
	rules, _, err := loadRules("", project)
	return len(rules), err
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
		for _, name := range strings.Split(store.Text(settingspkg.InstructionSources), ",") {
			candidates = append(candidates, filepath.Join(folder, strings.TrimSpace(name)))
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

func describeReload(now, last []reloadPart) []string {
	lines := make([]string, 0, len(now))
	for _, part := range now {
		line := part.Name + " " + strconv.Itoa(len(part.Items))
		if part.Detail != "" {
			line += " (" + part.Detail + ")"
		}
		at := slices.IndexFunc(last, func(was reloadPart) bool { return was.Name == part.Name })
		if at < 0 {
			lines = append(lines, line)
			continue
		}
		before := last[at].Items
		var added, removed, changed []string
		for name, fingerprint := range part.Items {
			was, had := before[name]
			switch {
			case !had:
				added = append(added, name)
			case was != fingerprint:
				changed = append(changed, name)
			}
		}
		for name := range before {
			if _, kept := part.Items[name]; !kept {
				removed = append(removed, name)
			}
		}
		for _, change := range []struct {
			mark, word string
			names      []string
		}{{"+", "added", added}, {"-", "removed", removed}, {"", "changed", changed}} {
			if len(change.names) > 0 {
				sort.Strings(change.names)
				line += fmt.Sprintf(", %s%d %s: %s", change.mark, len(change.names), change.word, strings.Join(change.names, ", "))
			}
		}
		lines = append(lines, line)
	}
	return lines
}

func reloadReport(dir string) (string, error) {
	now, err := takeInventory(dir)
	if err != nil {
		return "", err
	}
	state, err := sys.ProjectStateDirAt(dir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(state, reloadFileName)
	header := "re-read " + dir + ", against the last reload"
	var last []reloadPart
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		header = "re-read " + dir + ", the first reload here, so nothing to compare against"
	case err != nil || json.Unmarshal(data, &last) != nil:
		header, last = "re-read "+dir+", and the last reload in "+path+" could not be read, so nothing to compare against", nil
	}
	encoded, err := json.MarshalIndent(now, "", "  ")
	if err != nil {
		return "", err
	}
	if err := sys.WriteFile(path, encoded, 0o600); err != nil {
		return "", err
	}
	return header + "\n" + strings.Join(describeReload(now, last), "\n"), nil
}

func reloadVerb(out, errOut io.Writer) int {
	said := ""
	dir, err := os.Getwd()
	if err == nil {
		said, err = reloadReport(dir)
	}
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu reload: %v\n", err)
		return exitVerdict
	}
	_, _ = fmt.Fprintln(out, said)
	return exitOK
}

func appReload(project string) func() string {
	return func() string {
		said, err := reloadReport(project)
		if err != nil {
			return "reload failed: " + err.Error()
		}
		return said
	}
}
