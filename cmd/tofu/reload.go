package main

import (
	"fmt"
	"io"
	"os"
	"strconv"

	shipped "tofu/catalog"
	"tofu/internal/rule"
	"tofu/internal/settings"
	"tofu/internal/sys"
)

func runReload() (settings.ReloadResult, error) {
	fromBinary, err := rule.LoadFS(shipped.Files(), catalogRoot)
	if err != nil {
		return settings.ReloadResult{}, err
	}
	catalogDir, err := sys.CatalogDir()
	if err != nil {
		return settings.ReloadResult{}, err
	}
	return settings.Reload(fromBinary, catalogDir)
}

func reloadVerb(out, errOut io.Writer) int {
	dir, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu reload: %v\n", err)
		return exitVerdict
	}
	result, err := runReload()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu reload: %v\n", err)
		return exitVerdict
	}
	_, _ = fmt.Fprintf(out, "reloaded %d rules from %s\n", result.Rules, dir)
	for _, skipped := range result.Skipped {
		_, _ = fmt.Fprintln(out, "not reloaded: "+skipped)
	}
	return exitOK
}

func appReload() string {
	result, err := runReload()
	if err != nil {
		return "reload failed: " + err.Error()
	}
	summary := "reloaded " + strconv.Itoa(result.Rules) + " rules"
	for _, skipped := range result.Skipped {
		summary += "; not reloaded: " + skipped
	}
	return summary
}
