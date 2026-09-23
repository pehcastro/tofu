package main

import (
	"fmt"
	"os"

	"tofu/bench/report"
	"tofu/bench/schemas"
	"tofu/bench/tokens"
)

const testFunctionCount = 10

func main() {
	err := report.Generate("tokens prose report", func(machine, date string) (string, error) {
		defs := schemas.FullRegistry(".").Definitions()
		whole := schemas.Measure(defs)
		prose := tokens.MeasureProse(defs, whole)
		return tokens.ProseReport{
			Date:      date,
			Machine:   machine,
			BuildNote: "tofu's own claude-sub credential, wire anthropic",
			ToolCount: schemas.FullToolCount,
			Whole:     whole,
			Prose:     prose,
			TestCount: testFunctionCount,
		}.Render(), nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
