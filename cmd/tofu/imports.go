package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type importRule struct {
	from  string
	scope string
	allow []string
}

func importRules() []importRule {
	return []importRule{
		{from: "tofu/cmd", scope: "tofu/bench"},
		{from: "tofu/interface", scope: "tofu/bench"},
		{from: "tofu/internal", scope: "tofu/bench"},
		{from: "tofu/internal", scope: "tofu/cmd"},
		{from: "tofu/internal", scope: "tofu/interface"},
		{from: "tofu/catalog", scope: "tofu"},
		{from: "tofu/internal/konst", scope: "tofu"},
		{from: "tofu/internal/sys", scope: "tofu"},
		{
			from:  "tofu/internal/judge",
			scope: "tofu/internal",
			allow: []string{"tofu/internal/judge", "tofu/internal/sys", "tofu/internal/konst", "tofu/internal/transport"},
		},
		{
			from:  "tofu/internal/point",
			scope: "tofu/internal",
			allow: []string{"tofu/internal/judge", "tofu/internal/rule", "tofu/internal/transform", "tofu/internal/sys", "tofu/internal/konst"},
		},
	}
}

func (r importRule) String() string {
	if len(r.allow) == 0 {
		return "nothing under " + r.from + " imports " + r.scope
	}
	return "under " + r.scope + ", a package in " + r.from + " imports only " + strings.Join(r.allow, ", ")
}

func (r importRule) broken(from, to string) bool {
	if !under(from, r.from) || !under(to, r.scope) {
		return false
	}
	for _, allowed := range r.allow {
		if under(to, allowed) {
			return false
		}
	}
	return true
}

func under(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

type importEdge struct {
	from string
	to   string
	rule importRule
}

type packageImports struct {
	ImportPath string
	Imports    []string
}

func moduleImports(dir string) ([]packageImports, error) {
	list := exec.Command("go", "list", "-e", "-json=ImportPath,Imports", "./...")
	list.Dir = dir
	var stderr strings.Builder
	list.Stderr = &stderr
	stdout, err := list.Output()
	if err != nil {
		return nil, fmt.Errorf("go list in %s: %w: %s", dir, err, strings.TrimSpace(stderr.String()))
	}
	var pkgs []packageImports
	decoder := json.NewDecoder(bytes.NewReader(stdout))
	for decoder.More() {
		var pkg packageImports
		if err := decoder.Decode(&pkg); err != nil {
			return nil, err
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, nil
}

func brokenEdges(pkgs []packageImports, rules []importRule) []importEdge {
	var broken []importEdge
	for _, pkg := range pkgs {
		for _, imported := range pkg.Imports {
			for _, rule := range rules {
				if rule.broken(pkg.ImportPath, imported) {
					broken = append(broken, importEdge{from: pkg.ImportPath, to: imported, rule: rule})
				}
			}
		}
	}
	return broken
}

func doctorImports(dir string, out io.Writer) int {
	pkgs, err := moduleImports(dir)
	if err != nil {
		_, _ = fmt.Fprintf(out, "imports: unreadable: %v\n", err)
		return exitUsage
	}
	rules := importRules()
	for _, rule := range rules {
		_, _ = fmt.Fprintf(out, "rule: %s\n", rule)
	}
	broken := brokenEdges(pkgs, rules)
	for _, edge := range broken {
		_, _ = fmt.Fprintf(out, "violation: %s imports %s, and %s\n", edge.from, edge.to, edge.rule)
	}
	_, _ = fmt.Fprintf(out, "imports: %d packages, %d rules, %d violations\n", len(pkgs), len(rules), len(broken))
	if len(broken) > 0 {
		return exitVerdict
	}
	return exitOK
}
