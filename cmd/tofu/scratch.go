package main

import (
	"context"
	"fmt"
	"io"
	"slices"
	"time"

	"tofu/interface/cli"
	"tofu/internal/konst"
	"tofu/internal/session"
	settingspkg "tofu/internal/settings"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

const (
	scratchUsage          = "tofu scratch [clean [--session X] [--cache] [--dry-run] | open] [--json]"
	scratchCleanupSetting = "scratchCleanupDays"
	scratchMaxGBSetting   = "scratchMaxGB"
)

type scratchCleaned struct {
	Root    string              `json:"root"`
	DryRun  bool                `json:"dry_run"`
	Removed []sys.ScratchFolder `json:"removed"`
}

func runScratch(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "scratch", usageLine: scratchUsage, asJSON: jsonAsked(args), out: out, errOut: errOut}
	args = withoutJSON(args)
	report, err := sys.ReadScratch(".")
	if err != nil {
		return o.fail(err)
	}
	if len(args) == 0 {
		return o.done(true, report, func(page cli.Page) []string { return scratchLines(report) })
	}
	o.verb = "scratch " + args[0]
	switch args[0] {
	case "open":
		return o.done(true, report.Root, func(cli.Page) []string { return []string{report.Root} })
	case "clean":
	default:
		return o.usage(fmt.Errorf("there is no subcommand %q", args[0]))
	}
	sweep, dryRun := scratchSweep("."), false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--cache":
			sweep.Cache = true
		case "--dry-run":
			dryRun = true
		case "--session":
			if sweep.Session, err = nextArg(args, &i, "--session"); err != nil {
				return o.usage(err)
			}
		default:
			return o.usage(fmt.Errorf("unknown flag %q", args[i]))
		}
	}
	sweep.Leftovers = sweep.Session == ""
	cleaned := scratchCleaned{Root: report.Root, DryRun: dryRun, Removed: report.Removable(sweep)}
	if !dryRun {
		if err := removeScratch(cleaned.Removed); err != nil {
			return o.fail(err)
		}
	}
	return o.done(true, cleaned, func(cli.Page) []string {
		said := []string{"removed"}
		if dryRun {
			said = []string{"would remove"}
		}
		for _, folder := range cleaned.Removed {
			said = append(said, fmt.Sprintf("  %s  %s  %s", folder.Kind, sizeOf(folder.Bytes), folder.Path))
		}
		if len(cleaned.Removed) == 0 {
			said = append(said, "  nothing")
		}
		return said
	})
}

func scratchLines(report sys.ScratchReport) []string {
	lines := []string{"root " + report.Root}
	for _, folder := range report.Folders {
		name := string(folder.Kind)
		if folder.Session != "" {
			name += " " + folder.Session
		}
		lines = append(lines, fmt.Sprintf("  %-40s %9s  touched %s  %s", name, sizeOf(folder.Bytes), folder.Touched.Format(time.DateTime), folder.Path))
	}
	sweep := scratchSweep(".")
	next := report.Removable(sweep)
	lines = append(lines, fmt.Sprintf("cleanup removes sessions untouched for %d days and the oldest past %d GB; next: %d folders", sweep.CleanupDays, sweep.MaxGB, len(next)))
	if slices.ContainsFunc(report.Folders, func(folder sys.ScratchFolder) bool { return folder.Kind == sys.ScratchKindLeftover }) {
		lines = append(lines, "leftovers sit in this repository's .tofu/scratch: tofu scratch clean removes them")
	}
	return lines
}

func sizeOf(bytes int64) string {
	for _, unit := range []string{"B", "KB", "MB"} {
		if bytes < 1<<10 {
			return fmt.Sprintf("%d %s", bytes, unit)
		}
		bytes >>= 10
	}
	return fmt.Sprintf("%d GB", bytes)
}

func scratchSweep(dir string) sys.ScratchSweep {
	store, _ := openSettings(dir)
	setting := func(key string, fallback int) int {
		if store == nil || !slices.ContainsFunc(store.Table(), func(spec settingspkg.Spec) bool { return spec.Key == key }) {
			return fallback
		}
		return store.Int(key)
	}
	sessions, err := session.OpenIn(dir)
	return sys.ScratchSweep{
		CleanupDays: setting(scratchCleanupSetting, konst.ScratchCleanupDays),
		MaxGB:       setting(scratchMaxGBSetting, konst.ScratchMaxGB),
		Now:         time.Now(),
		Live:        func(id string) bool { return err != nil || sessions.Busy(id) != nil },
	}
}

func removeScratch(folders []sys.ScratchFolder) error {
	registry, err := launchShellRegistry(".")
	if err != nil {
		return err
	}
	return sys.RemoveScratch(folders, func(dir string) { registry.StopUnder(dir) })
}

func leadScratch(ctx context.Context, config turn.Config, errOut io.Writer) (context.Context, error) {
	leading, err := turn.WithLeadScratch(ctx, config)
	if err != nil {
		return ctx, fmt.Errorf("the scratchpad was not made: %w", err)
	}
	place, _ := sys.ScratchOf(leading)
	report, err := sys.ReadScratch(config.Project)
	sweep := scratchSweep(config.Project)
	sweep.Keep = place.Session
	if removable := report.Removable(sweep); err == nil && len(removable) > 0 {
		if err := removeScratch(removable); err != nil {
			_, _ = fmt.Fprintln(errOut, "tofu run: scratch cleanup: "+err.Error())
		}
	}
	return leading, nil
}
