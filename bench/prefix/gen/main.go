package main

import (
	"fmt"
	"os"

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

	return report.Generate("prefix rewrite report", func(machine, date string) (string, error) {
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
	})
}
