package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/bench/report"
	"tofu/internal/konst"
	"tofu/internal/sys"
)

const packageDir = "bench/rules/batched"

func main() {
	live := flag.Bool("live", false, "ask Jev over the network, one batched request a reply, and record the answers")
	recordFlag := flag.String("record", "", "render from this recorded run, relative to the source root; the newest under runs/ when empty")
	write := flag.Bool("write", false, "also write the dated report into bench/rules")
	flag.Parse()
	if err := run(*live, *recordFlag, *write); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(live bool, recordPath string, write bool) error {
	root := sys.SourceRoot()
	replies, err := loadCorpus(filepath.Join(root, packageDir, "corpus.json"))
	if err != nil {
		return err
	}
	rules, err := loadRules()
	if err != nil {
		return err
	}
	if found := leaks(replies, rules); len(found) > 0 {
		return fmt.Errorf("the corpus leaks, so it cannot separate a rule from its own text:\n%s", strings.Join(found, "\n"))
	}
	var rec record
	if live {
		if rec, err = askAll(context.Background(), replies, rules); err != nil {
			return err
		}
		recordPath = packageDir + "/runs/" + rec.Date + ".json"
		if err := writeRecord(filepath.Join(root, recordPath), rec); err != nil {
			return err
		}
	} else {
		if recordPath == "" {
			recorded, _ := filepath.Glob(filepath.Join(root, packageDir, "runs", "*.json"))
			if len(recorded) == 0 {
				return errors.New("no recorded run under runs/; run with -live first")
			}
			recordPath = packageDir + "/runs/" + filepath.Base(slices.Max(recorded))
		}
		if rec, err = readRecord(filepath.Join(root, recordPath)); err != nil {
			return err
		}
	}
	out, err := render(replies, rules, rec, recordPath)
	if err != nil {
		return err
	}
	fmt.Print(out.body)
	if write {
		path := filepath.Join(root, "bench", "rules", "report-"+time.Now().Format(time.DateOnly)+".md")
		if err := report.Write(path, []byte(out.body), 0o644, "rules batched report"); err != nil {
			return err
		}
	}
	if len(out.oversized) > 0 {
		return fmt.Errorf("%d requests went over %d tokens: %v", len(out.oversized), konst.JudgeRequestTokenCeiling, out.oversized)
	}
	return nil
}
