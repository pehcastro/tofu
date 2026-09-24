package main

import (
	"fmt"
	"os"
	"path/filepath"

	"tofu/bench/ask/server"
	"tofu/bench/report"
)

func main() {
	err := report.Generate("ask/server declared-commands arm", func(machine, date string) (string, error) {
		return server.Render(machine, date, filepath.Join("..", server.BobSessionsDirFromPackage))
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
