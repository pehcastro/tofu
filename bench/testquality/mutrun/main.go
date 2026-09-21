package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"tofu/bench/mutate"
)

func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: mutrun <working-dir> <package-pattern>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	outcome, err := mutate.Gremlins.Run(ctx, os.Args[1], os.Args[2])
	if err != nil {
		return err
	}
	score := mutate.Tally(outcome.Mutants)
	fmt.Printf("killed=%d lived=%d not_covered=%d timed_out=%d efficacy=%.2f\n",
		score.Killed, score.Lived, score.NotCovered, score.TimedOut, score.Efficacy())
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
