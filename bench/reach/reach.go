package main

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type kind string

const (
	kindRule      kind = "rule"
	kindReference kind = "reference"
	kindSkill     kind = "skill"
	kindTool      kind = "tool"
)

func kindOrder() []kind { return []kind{kindRule, kindReference, kindSkill, kindTool} }

type item struct {
	kind  kind
	key   string
	lines int
}

type reached map[kind]map[string]bool

func (r reached) add(k kind, key string) {
	if r[k] == nil {
		r[k] = map[string]bool{}
	}
	r[k][key] = true
}

func libraryItems(root string) ([]item, error) {
	var items []item
	err := filepath.WalkDir(filepath.Join(root, "library"), func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, file)
		rel = filepath.ToSlash(rel)
		name, folder := path.Base(rel), path.Base(path.Dir(rel))
		body, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		lines := strings.Count(string(body), "\n")
		switch {
		case folder == "rules" && path.Ext(name) == ".yaml":
			items = append(items, item{kindRule, rel, lines})
		case folder == "references" && path.Ext(name) == ".md":
			items = append(items, item{kindReference, strings.TrimSuffix(name, ".md"), lines})
		case folder == "skills" && path.Ext(name) == ".md":
			items = append(items, item{kindSkill, strings.TrimSuffix(name, ".md"), lines})
		case folder == "agents" && path.Ext(name) == ".md":
			for _, tool := range agentTools(string(body)) {
				items = append(items, item{kind: kindTool, key: tool})
			}
		}
		return nil
	})
	return items, err
}

func agentTools(definition string) []string {
	for line := range strings.Lines(definition) {
		if listed, ok := strings.CutPrefix(strings.TrimSpace(line), "tools:"); ok {
			return strings.Split(strings.ReplaceAll(listed, " ", ""), ",")
		}
	}
	return nil
}

func parseReached(prompt string) reached {
	ruleLine := regexp.MustCompile(`^part \S+\s+\d+ bytes  from the rule \S+ in (library/\S+)$`)
	loadedLine := regexp.MustCompile(`^the skill (\S+), loaded before your task:$`)
	listLine := regexp.MustCompile(`^- ([\w-]+): `)
	toolLine := regexp.MustCompile(`^tool (\S+)$`)
	found := reached{}
	var list kind
	tools, previous := false, ""
	for line := range strings.Lines(prompt) {
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			list = ""
		case strings.HasPrefix(line, "your references"):
			list = kindReference
		case line == "[skills, from tofu itself]":
			list = kindSkill
		case strings.HasPrefix(line, "offered tools, "):
			tools = true
		}
		if match := listLine.FindStringSubmatch(line); match != nil && list != "" {
			found.add(list, match[1])
		}
		if match := ruleLine.FindStringSubmatch(line); match != nil {
			found.add(kindRule, match[1])
		}
		if match := loadedLine.FindStringSubmatch(line); match != nil {
			found.add(kindSkill, match[1])
		}
		if match := toolLine.FindStringSubmatch(line); match != nil && tools && previous == "" {
			found.add(kindTool, match[1])
		}
		previous = line
	}
	return found
}

func universe(library []item, runs []run) []item {
	all := slices.Clone(library)
	for _, each := range runs {
		for tool := range each.reached[kindTool] {
			all = append(all, item{kind: kindTool, key: tool})
		}
	}
	slices.SortStableFunc(all, func(a, b item) int {
		if order := slices.Index(kindOrder(), a.kind) - slices.Index(kindOrder(), b.kind); order != 0 {
			return order
		}
		return strings.Compare(a.key, b.key)
	})
	return slices.CompactFunc(all, func(a, b item) bool { return a.kind == b.kind && a.key == b.key })
}
