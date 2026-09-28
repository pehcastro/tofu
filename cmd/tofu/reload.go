package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
)

func runReload(project string) (int, error) {
	rules, _, err := loadRules("", project)
	return len(rules), err
}

func reloadVerb(out, errOut io.Writer) int {
	dir, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu reload: %v\n", err)
		return exitVerdict
	}
	count, err := runReload(dir)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu reload: %v\n", err)
		return exitVerdict
	}
	_, _ = fmt.Fprintf(out, "reloaded %d rules from %s\n", count, dir)
	return exitOK
}

func appReload(project string) func() string {
	return func() string {
		count, err := runReload(project)
		if err != nil {
			return "reload failed: " + err.Error()
		}
		return "reloaded " + strconv.Itoa(count) + " rules"
	}
}
