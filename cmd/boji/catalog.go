package main

import (
	"fmt"
	"io"
	"io/fs"
	"strings"

	shipped "boji/catalog"
	catalogpolicy "boji/catalog/policy"
	"boji/catalog/questions"
	catalogrules "boji/catalog/rules"
	"boji/internal/judge/question"
	"boji/internal/llm/models"
)

const catalogUsage = "usage: boji catalog [resolve <name>]"

func catalogVerb(args []string, out, errOut io.Writer) int {
	switch {
	case len(args) == 0:
		return catalogReport(out, errOut)
	case len(args) == 2 && args[0] == "resolve":
		return catalogResolve(args[1], out, errOut)
	}
	_, _ = fmt.Fprintln(errOut, "boji catalog: "+catalogUsage)
	return exitUsage
}

func catalogResolve(name string, out, errOut io.Writer) int {
	layers, err := question.DefaultLayers(questions.Files())
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "boji catalog: %v\n", err)
		return exitUsage
	}
	_, fields, err := question.Resolve(name, layers)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "boji catalog: %v\n", err)
		return exitUsage
	}
	for _, field := range fields {
		_, _ = fmt.Fprintf(out, "%s = %s  [%s:%d]\n", field.Path, field.Value, field.File, field.Line)
	}
	return exitOK
}

func catalogReport(out, errOut io.Writer) int {
	layers, err := models.Layers(shipped.Files())
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "boji catalog: %v\n", err)
		return exitUsage
	}
	catalog, _ := models.Load(layers)

	loaded := map[string]int{
		"models":        len(catalog.Models),
		"subscriptions": len(catalog.Subscriptions),
		"roles":         len(catalog.Roles),
	}
	for _, contract := range models.Contracts() {
		fields := "required " + strings.Join(contract.Required, ", ")
		if len(contract.Optional) > 0 {
			fields += "   optional " + strings.Join(contract.Optional, ", ")
		}
		_, _ = fmt.Fprintf(out, "%-14s %3d loaded   %s\n", contract.Kind, loaded[contract.Kind], fields)
	}
	for _, kind := range []struct {
		name  string
		files fs.FS
		is    string
	}{
		{"questions", questions.Files(), "the wording a decision point asks, checked by boji catalog resolve"},
		{"policy", catalogpolicy.Files(), "the mode and the thresholds a decision point reads, checked by boji doctor"},
		{"rules", catalogrules.Files(), "the standing rules a turn is held to, checked by boji rules list"},
	} {
		names, _ := fs.Glob(kind.files, "*.yaml")
		_, _ = fmt.Fprintf(out, "%-14s %3d shipped  %s\n", kind.name, len(names), kind.is)
	}

	origins := make([]string, 0, len(layers))
	for _, layer := range layers {
		origins = append(origins, layer.Name+" "+layer.Origin)
	}
	_, _ = fmt.Fprintf(out, "\nlayered %s, the last one winning field by field\n", strings.Join(origins, ", then "))

	if len(catalog.Broken) == 0 {
		return exitOK
	}
	_, _ = fmt.Fprintf(out, "\n%d refused\n", len(catalog.Broken))
	for _, refused := range catalog.Broken {
		_, _ = fmt.Fprintln(out, "  "+refused.Error())
	}
	return exitVerdict
}
