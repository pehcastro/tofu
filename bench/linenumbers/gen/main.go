package main

import (
	"fmt"
	"os"

	"tofu/bench/linenumbers"
	"tofu/bench/report"
	"tofu/internal/sys"
)

func main() {
	err := report.Generate("read tool line-number bytes", func(machine, date string) (string, error) {
		result, err := linenumbers.Run(sys.RecordedStateDir("sessions"))
		if err != nil {
			return "", err
		}
		return linenumbers.Render(machine, date, result), nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
