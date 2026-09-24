package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"tofu/bench/prefix"
	"tofu/bench/report"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	rules, err := prefix.RealRules()
	if err != nil {
		return err
	}
	system, composed, err := prefix.ComposeSystem(rules)
	if err != nil {
		return err
	}
	builtinOnly, _, err := prefix.ComposeSystem(nil)
	if err != nil {
		return err
	}

	withBilling, err := prefix.MeasureRewrite(system, true)
	if err != nil {
		return err
	}
	noBilling, err := prefix.MeasureRewrite(system, false)
	if err != nil {
		return err
	}
	builtin, err := prefix.MeasureRewrite(builtinOnly, true)
	if err != nil {
		return err
	}
	repeat, err := prefix.MeasureRewrite(system, true)
	if err != nil {
		return err
	}

	render := func(machine, date string) (string, error) {
		return prefix.Report{
			Date:        date,
			Machine:     machine,
			Task:        prefix.RealTask,
			FiredRules:  prefix.FiredRuleIDs(composed),
			WithBilling: withBilling,
			NoBilling:   noBilling,
			BuiltinOnly: builtin,
			Reproduced:  [2]prefix.RewriteFigure{withBilling, repeat},
		}.Render(), nil
	}

	genErr := report.Generate("prefix rewrite report", render)
	if genErr == nil {
		return nil
	}
	if !strings.Contains(genErr.Error(), "already on disk") {
		return genErr
	}
	return writeCorrected(render)
}

func writeCorrected(render func(machine, date string) (string, error)) error {
	date := time.Now().Format("2006-01-02")
	machine, err := os.Hostname()
	if err != nil {
		machine = "unknown"
	}
	body, err := render(machine, date)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("../report-%s-corrected.md", date)
	if err := report.Write(path, []byte(body), 0o644, "prefix rewrite report"); err != nil {
		return err
	}
	fmt.Println(path, "written")
	return nil
}
