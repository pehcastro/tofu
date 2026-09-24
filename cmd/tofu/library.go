package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"

	"tofu/internal/judge/question"
	"tofu/internal/llm/models"
	"tofu/internal/rule"
	"tofu/internal/sys"
	shipped "tofu/library"
	"tofu/library/questions"
)

const libraryUsage = "usage: tofu library [resolve <name>] [--dir <path>]"

func libraryVerb(args []string, out, errOut io.Writer) int {
	dir, rest, err := takeDir(args)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu library: %v\n", err)
		return exitUsage
	}
	switch {
	case len(rest) == 0:
		return libraryReport(dir, out, errOut)
	case len(rest) == 2 && rest[0] == "resolve":
		return libraryResolve(rest[1], dir, out, errOut)
	}
	_, _ = fmt.Fprintln(errOut, "tofu library: "+libraryUsage)
	return exitUsage
}

func takeDir(args []string) (string, []string, error) {
	for i, arg := range args {
		if arg != "--dir" {
			continue
		}
		if i+1 == len(args) {
			return "", nil, errors.New("--dir names no path")
		}
		return args[i+1], slices.Concat(args[:i], args[i+2:]), nil
	}
	return "", args, nil
}

func libraryResolve(name, dir string, out, errOut io.Writer) int {
	layers, err := question.Layers(questions.Files(), dir)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu library: %v\n", err)
		return exitUsage
	}
	_, fields, err := question.Resolve(name, layers)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu library: %v\n", err)
		return exitUsage
	}
	for _, field := range fields {
		_, _ = fmt.Fprintf(out, "%s = %s  [%s:%d]\n", field.Path, field.Value, field.File, field.Line)
	}
	return exitOK
}

func librarySource() (fs.FS, string, string) {
	dir, err := sys.LibraryDir()
	if err != nil {
		return shipped.Files(), libraryRoot, rulesFromTheBinary
	}
	isDir, err := sys.IsDir(dir)
	if err != nil || !isDir {
		return shipped.Files(), libraryRoot, rulesFromTheBinary
	}
	return os.DirFS(dir), dir, rulesFromTheProject
}

func printDomains(out io.Writer, domains []rule.Domain) []error {
	var refused []error
	for _, domain := range domains {
		_, _ = fmt.Fprintf(out, "  %-9s rules %2d   thresholds %2d   skills %2d   agents %2d   references %2d\n",
			domain.Name, len(domain.Rules), len(domain.Thresholds), len(domain.Skills), len(domain.Agents), len(domain.References))
		refused = append(refused, domain.Refused...)
		for _, unreachable := range domain.Unreachable() {
			refused = append(refused, errors.New(unreachable))
		}
	}
	return refused
}

func libraryReport(dir string, out, errOut io.Writer) int {
	layers, err := models.Layers(shipped.Files(), dir)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu library: %v\n", err)
		return exitUsage
	}
	library, _ := models.Load(layers)

	loaded := map[string]int{
		"models":        len(library.Models),
		"subscriptions": len(library.Subscriptions),
		"roles":         len(library.Roles),
	}
	for _, contract := range models.Contracts() {
		fields := "required " + strings.Join(contract.Required, ", ")
		if len(contract.Optional) > 0 {
			fields += "   optional " + strings.Join(contract.Optional, ", ")
		}
		_, _ = fmt.Fprintf(out, "%-14s %3d loaded   %s\n", contract.Kind, loaded[contract.Kind], fields)
	}

	sets, refused := question.LoadAll(questions.Files(), libraryRoot+"/questions")
	_, _ = fmt.Fprintf(out, "%-14s %3d loaded   the wording a decision point asks, checked by tofu library resolve\n", "questions", len(sets))

	proxy := loadProxySetting(dir)
	refused = append(refused, proxy.refused...)
	_, _ = fmt.Fprintf(out, "%-14s use %-3s   from %s   required use, timeout_ms\n", "proxy", proxy.use, proxy.layer)

	files, dir, origin := librarySource()
	domains, err := rule.LoadDomains(files, libraryRoot)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu library: %v\n", err)
		return exitUsage
	}
	_, _ = fmt.Fprintf(out, "\ndomains from %s, %s\n", origin, dir)
	refused = append(refused, printDomains(out, domains)...)

	origins := make([]string, 0, len(layers))
	for _, layer := range layers {
		origins = append(origins, layer.Name+" "+layer.Origin)
	}
	_, _ = fmt.Fprintf(out, "\nlayered %s, the last one winning field by field\n", strings.Join(origins, ", then "))

	for _, broken := range library.Broken {
		refused = append(refused, broken)
	}
	if len(refused) == 0 {
		return exitOK
	}
	_, _ = fmt.Fprintf(out, "\n%d refused\n", len(refused))
	for _, one := range refused {
		_, _ = fmt.Fprintln(out, "  "+one.Error())
	}
	return exitVerdict
}
