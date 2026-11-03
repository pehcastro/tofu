package shells

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"tofu/internal/agent/loop"
	"tofu/internal/cli/output"
	"tofu/internal/cli/page"
	"tofu/internal/host"
	"tofu/internal/konst"
	"tofu/internal/store/session"
	settingspkg "tofu/internal/store/settings"
	"tofu/internal/sys"
)

const (
	ScratchUsage          = "tofu scratch [clean [--session X] [--cache] [--dry-run] | open] [--json]"
	scratchCleanupSetting = "scratchCleanupDays"
	scratchMaxGBSetting   = "scratchMaxGB"
)

func Scratch(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "scratch", UsageLine: ScratchUsage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	args = output.WithoutJSON(args)
	report, err := sys.ReadScratch(".")
	if err != nil {
		return o.Fail(err)
	}
	if len(args) == 0 {
		return o.Done(true, report, func(page cli.Page) []string { return scratchLines(report) })
	}
	o.Name = "scratch " + args[0]
	switch args[0] {
	case "open":
		return o.Done(true, report.Root, func(cli.Page) []string { return []string{report.Root} })
	case "clean":
	default:
		return o.Usage(fmt.Errorf("there is no subcommand %q", args[0]))
	}
	sweep, dryRun := scratchSweep(".", busyIn(session.OpenIn("."))), false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--cache":
			sweep.Cache = true
		case "--dry-run":
			dryRun = true
		case "--session":
			if sweep.Session, err = output.NextArg(args, &i, "--session"); err != nil {
				return o.Usage(err)
			}
		default:
			return o.Usage(fmt.Errorf("unknown flag %q", args[i]))
		}
	}
	sweep.Leftovers = sweep.Session == ""
	cleaned := host.ScratchCleaned{Root: report.Root, DryRun: dryRun, Removed: append([]sys.ScratchFolder{}, report.Removable(sweep)...)}
	if !dryRun {
		if err := removeScratch(".", cleaned.Removed); err != nil {
			return o.Fail(err)
		}
	}
	return o.Done(true, cleaned, func(cli.Page) []string {
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
	sweep := scratchSweep(".", busyIn(session.OpenIn(".")))
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

func scratchSweep(dir string, live func(string) bool) sys.ScratchSweep {
	store, _ := settingspkg.OpenFor(dir)
	setting := func(key string, fallback int) int {
		if store == nil || !slices.ContainsFunc(store.Table(), func(spec settingspkg.Spec) bool { return spec.Key == key }) {
			return fallback
		}
		return store.Int(key)
	}
	return sys.ScratchSweep{
		CleanupDays: setting(scratchCleanupSetting, konst.ScratchCleanupDays),
		MaxGB:       setting(scratchMaxGBSetting, konst.ScratchMaxGB),
		Now:         time.Now(),
		Live:        live,
	}
}

func busyIn(sessions *session.Store, openErr error) func(string) bool {
	return func(id string) bool { return openErr != nil || sessions.Busy(id) != nil }
}

func SweepScratch(dir string, say func(string), stop <-chan struct{}) {
	sessions, openErr := session.OpenIn(dir)
	live := func(id string) bool {
		if openErr != nil || sessions.Busy(id) != nil {
			return true
		}
		_, locked := os.Stat(filepath.Join(sessions.Dir(id), sys.SessionLockFile))
		return !errors.Is(locked, fs.ErrNotExist)
	}
	ticker := time.NewTicker(konst.ScratchSweepHours * time.Hour)
	defer ticker.Stop()
	for {
		report, err := sys.ReadScratch(dir)
		sweep := scratchSweep(dir, live)
		if removable := report.Removable(sweep); err == nil && len(removable) > 0 {
			if err = removeScratch(dir, removable); err == nil {
				say(fmt.Sprintf("the scratch sweep removed %s, past %d GB or untouched for %d days", output.Plural(len(removable), "folder"), sweep.MaxGB, sweep.CleanupDays))
			}
		}
		if err != nil {
			say("the scratch sweep failed and runs again on the next tick: " + err.Error())
		}
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
	}
}

func removeScratch(dir string, folders []sys.ScratchFolder) error {
	registry, err := LaunchShellRegistry(dir)
	if err != nil {
		return err
	}
	return sys.RemoveScratch(folders, func(dir string) { registry.StopUnder(dir) }, konst.ScratchRemoveTries, konst.ScratchRemoveRetryMillis*time.Millisecond)
}

func LeadScratch(ctx context.Context, config turn.Config, errOut io.Writer) (context.Context, error) {
	leading, err := turn.WithLeadScratch(ctx, config)
	if err != nil {
		return ctx, fmt.Errorf("the scratchpad was not made: %w", err)
	}
	place, _ := sys.ScratchOf(leading)
	report, err := sys.ReadScratch(config.Project)
	sweep := scratchSweep(config.Project, busyIn(session.OpenIn(config.Project)))
	sweep.Keep = place.Session
	if removable := report.Removable(sweep); err == nil && len(removable) > 0 {
		if err := removeScratch(config.Project, removable); err != nil {
			_, _ = fmt.Fprintln(errOut, "tofu run: scratch cleanup: "+err.Error())
		}
	}
	return leading, nil
}
