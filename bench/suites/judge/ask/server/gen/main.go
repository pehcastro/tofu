package main

import (
	"fmt"
	"os"

	"tofu/bench/kit/report"
	"tofu/bench/suites/judge/ask/server"
	"tofu/internal/sys"
)

func main() {
	err := report.Generate("ask/server declared-commands arm", func(machine, date string) (string, error) {
		return server.Render(machine, date, sys.RecordedStateDir("sessions"))
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
