package main

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"tofu/internal/rule"
	"tofu/internal/settings"
	"tofu/internal/sys"
	shipped "tofu/library"
)

func runReload() (settings.ReloadResult, error) {
	fromBinary, err := rule.LoadFS(shipped.Files(), libraryRoot)
	if err != nil {
		return settings.ReloadResult{}, err
	}
	libraryDir, err := sys.LibraryDir()
	if err != nil {
		return settings.ReloadResult{}, err
	}
	return settings.Reload(fromBinary, libraryDir)
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
	return exitOK
}

func appReload() string {
	result, err := runReload()
	if err != nil {
		return "reload failed: " + err.Error()
	}
	return "reloaded " + strconv.Itoa(result.Rules) + " rules"
}
