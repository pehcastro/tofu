package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"tofu/bench/harness"
	"tofu/internal/sys"
)

func main() {
	principlesMode := flag.Bool("principles", false, "measure each pstack principle, rule-on against rule-off, one claude -p run at a time")
	dryRun := flag.Bool("dry-run", false, "print every run and its leakage check, run nothing")
	flag.Parse()
	if !*principlesMode {
		fmt.Fprintln(os.Stderr, "bench/rules: -principles is the only mode here; the fire report is go run . from bench/rules/gen")
		os.Exit(2)
	}
	if err := measure(*dryRun); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func measure(dryRun bool) error {
	root := filepath.Join(sys.SourceRoot(), "bench", "rules", "principles")
	sets, err := loadSets(root)
	if err != nil {
		return err
	}
	printLeakage(sets)
	runs := plan(sets, filepath.Join(root, "runs"))
	kept := make([]string, len(sets))
	for i, set := range sets {
		kept[i] = fmt.Sprint(set.kept())
	}
	fmt.Printf("%d principles x 2 arms x %s tasks = %d runs, one at a time, rule-on then rule-off per task\n", len(sets), strings.Join(kept, "/"), len(runs))
	fmt.Printf("each run: %s, in a fresh temp copy of the task seed, credential the claude subscription, then go test ./... with the check/ files added\n", harness.Shell(append([]string{"claude"}, claudeArgs("<prompt>")...)))
	run := runAll
	if dryRun {
		run = printPlan
	}
	if err := run(runs); err != nil {
		return err
	}
	return summarize(sets, runs)
}

func printPlan(runs []plannedRun) error {
	for i, r := range runs {
		_, recorded, err := readRecord(r.record)
		if err != nil {
			return err
		}
		state := "would run"
		if recorded {
			state = "recorded"
		}
		fmt.Printf("%03d %s %s %s %s\n", i+1, r.principle, r.task.name, r.arm, state)
	}
	return nil
}

func runAll(runs []plannedRun) error {
	for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"} {
		if os.Getenv(name) != "" {
			return fmt.Errorf("%s is set, and claude would bill it instead of the subscription; unset it and run again", name)
		}
	}
	version, err := exec.Command("claude", "--version").Output()
	if err != nil {
		return fmt.Errorf("claude --version: %w", err)
	}
	machine, _ := os.Hostname()
	for i, r := range runs {
		_, recorded, err := readRecord(r.record)
		if err != nil {
			return err
		}
		if recorded {
			continue
		}
		rec, err := execute(r, strings.TrimSpace(string(version)), machine)
		if err != nil {
			return err
		}
		if err := writeRecord(r.record, rec); err != nil {
			return err
		}
		fmt.Printf("%03d %s %s %s passed=%t diff=%d wall=%dms models=%v\n", i+1, r.principle, r.task.name, r.arm, rec.Passed, rec.DiffLines, rec.WallMS, rec.Models)
	}
	return nil
}

type armScore struct {
	run, passed int
	diffs       []int
	models      []string
}

func summarize(sets []principleSet, runs []plannedRun) error {
	for _, set := range sets {
		scores := map[arm]*armScore{armOn: {}, armOff: {}}
		for _, r := range runs {
			if r.principle != set.name {
				continue
			}
			rec, recorded, err := readRecord(r.record)
			if err != nil {
				return err
			}
			if !recorded {
				continue
			}
			s := scores[r.arm]
			s.run++
			if rec.Passed {
				s.passed++
			}
			s.diffs = append(s.diffs, rec.DiffLines)
			s.models = append(s.models, rec.Models...)
		}
		fmt.Printf("%s: rule-on %s; rule-off %s\n", set.name, scores[armOn], scores[armOff])
	}
	return nil
}

func (s *armScore) String() string {
	if s.run == 0 {
		return "0 tasks run"
	}
	slices.Sort(s.diffs)
	slices.Sort(s.models)
	return fmt.Sprintf("%d of %d tasks run passed, median diff %d lines, models %v", s.passed, s.run, s.diffs[len(s.diffs)/2], slices.Compact(s.models))
}
