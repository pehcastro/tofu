package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"tofu/bench/report"
	"tofu/bench/startup"
)

const rounds = 6

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	measured, err := startup.Measure(rounds)
	if err != nil {
		return err
	}
	render := func(machine, date string) (string, error) {
		return startup.Report{
			Date:      date,
			Machine:   machine,
			Rounds:    rounds,
			Shell:     measured.Shell,
			GoProbe:   measured.GoProbe,
			NodeProbe: measured.NodeProbe,
			NodeCold:  measured.NodeCold,
			NodeWarm:  measured.NodeWarm,
		}.Render(), nil
	}
	genErr := report.Generate("startup cost report", render)
	if genErr == nil {
		return nil
	}
	if !strings.Contains(genErr.Error(), "already on disk") {
		return genErr
	}
	return writeOverlapped(render)
}

func writeOverlapped(render func(machine, date string) (string, error)) error {
	date := time.Now().Format("2006-01-02")
	machine, err := os.Hostname()
	if err != nil {
		machine = "unknown"
	}
	body, err := render(machine, date)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("../report-%s-overlapped.md", date)
	if err := report.Write(path, []byte(body), 0o644, "startup cost report"); err != nil {
		return err
	}
	fmt.Println(path, "written")
	return nil
}
