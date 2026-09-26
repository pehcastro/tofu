package main

import (
	"fmt"
	"os"

	"tofu/bench/ask/server"
	"tofu/bench/report"
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
