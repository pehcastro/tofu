package main

import (
	"fmt"
	"io"

	"boji/catalog/questions"
	"boji/internal/judge/question"
)

func catalogVerb(args []string, out, errOut io.Writer) int {
	if len(args) != 2 || args[0] != "resolve" {
		_, _ = fmt.Fprintln(errOut, "boji catalog: usage: boji catalog resolve <name>")
		return exitUsage
	}
	layers, err := question.DefaultLayers(questions.Files())
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "boji catalog: %v\n", err)
		return exitUsage
	}
	_, fields, err := question.Resolve(args[1], layers)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "boji catalog: %v\n", err)
		return exitUsage
	}
	for _, field := range fields {
		_, _ = fmt.Fprintf(out, "%s = %s  [%s:%d]\n", field.Path, field.Value, field.File, field.Line)
	}
	return exitOK
}
