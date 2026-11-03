package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

type measureSpec struct {
	Key      string  `json:"key"`
	Name     string  `json:"name"`
	Baseline string  `json:"baseline"`
	Cite     string  `json:"cite"`
	Bound    string  `json:"bound"`
	Target   float64 `json:"target"`
	Least    int     `json:"least"`
}

type baselines struct {
	Spec           string `json:"spec"`
	BaselineWindow struct {
		From time.Time `json:"from"`
		To   time.Time `json:"to"`
		Cite string    `json:"cite"`
	} `json:"baselineWindow"`
	Since struct {
		At   time.Time `json:"at"`
		Cite string    `json:"cite"`
	} `json:"since"`
	Patterns map[string]string `json:"patterns"`
	Repeat   struct {
		Similarity float64 `json:"similarity"`
		MinWords   int     `json:"minWords"`
		MinLetters int     `json:"minLetters"`
	} `json:"repeat"`
	Points []struct {
		Point    int           `json:"point"`
		Title    string        `json:"title"`
		Measures []measureSpec `json:"measures"`
	} `json:"points"`
}

func main() {
	baselinePath := flag.String("baselines", "bench/review/baselines.json", "the baseline file written from analysis.md")
	since := flag.String("since", "", "read from this RFC3339 time instead of the baseline file's install time")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: go run ./bench/review [-baselines FILE] [-since RFC3339] EXPORT...\nan EXPORT is a file or folder of tofu --json output: boardy list --all, boardy show ID --thread, session transcript NAME, rules why SESSION, rules list")
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*baselinePath, *since, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "bench/review:", err)
		os.Exit(1)
	}
}

func run(baselinePath, since string, exports []string) error {
	raw, err := os.ReadFile(baselinePath)
	if err != nil {
		return err
	}
	var base baselines
	if err := json.Unmarshal(raw, &base); err != nil {
		return fmt.Errorf("%s: %w", baselinePath, err)
	}
	after := window{from: base.Since.At, to: time.Now().Add(time.Hour)}
	if since != "" {
		if after.from, err = time.Parse(time.RFC3339, since); err != nil {
			return err
		}
	}
	found, err := compile(base)
	if err != nil {
		return err
	}
	in, err := load(exports)
	if err != nil {
		return err
	}
	b := bench{in: in, detectors: found}
	baseWindow := window{from: base.BaselineWindow.From, to: base.BaselineWindow.To}
	fmt.Println("| point | measure | baseline, analysis.md | same rule, baseline window | reading | n | target | met |")
	fmt.Println("|---|---|---|---|---|---|---|---|")
	var verdicts []string
	for _, point := range base.Points {
		var unmet []string
		rests := 0
		for _, spec := range point.Measures {
			now, err := b.measure(spec.Key, after)
			if err != nil {
				return err
			}
			then, err := b.measure(spec.Key, baseWindow)
			if err != nil {
				return err
			}
			if spec.Key == "rules_dropped_unlisted" {
				then = reading{missing: "rules why rows carry no date"}
			}
			met, why, err := judge(spec, now)
			if err != nil {
				return err
			}
			if !met {
				unmet = append(unmet, spec.Key+" "+why)
			}
			rests += now.n
			fmt.Printf("| %d | %s | %s (%s) | %s | %s | %d | %s %g | %s |\n", point.Point, spec.Name, spec.Baseline, spec.Cite, shown(then), shown(now), now.n, spec.Bound, spec.Target, why)
		}
		verdict := fmt.Sprintf("point %d, %s: done, rests on %d", point.Point, point.Title, rests)
		if len(unmet) > 0 {
			verdict = fmt.Sprintf("point %d, %s: not done, rests on %d: %s", point.Point, point.Title, rests, strings.Join(unmet, "; "))
		}
		verdicts = append(verdicts, verdict)
	}
	fmt.Println()
	for _, verdict := range verdicts {
		fmt.Println(verdict)
	}
	host, _ := os.Hostname()
	var read []string
	for what, count := range in.read {
		read = append(read, fmt.Sprintf("%d %s", count, what))
	}
	slices.Sort(read)
	fmt.Printf("\nconditions: run %s on %s %s/%s; reading from %s (%s); baseline window %s to %s (%s); inputs %s; no model call, no credential\n",
		time.Now().Format(time.RFC3339), host, runtime.GOOS, runtime.GOARCH, after.from.Format(time.RFC3339), base.Since.Cite,
		baseWindow.from.Format(time.RFC3339), baseWindow.to.Format(time.RFC3339), base.BaselineWindow.Cite, strings.Join(read, ", "))
	return nil
}

func compile(base baselines) (detectors, error) {
	d := detectors{docsOnly: base.Patterns["docsOnly"], decisionKind: base.Patterns["decisionKind"], similarity: base.Repeat.Similarity, minWords: base.Repeat.MinWords, minLetters: base.Repeat.MinLetters}
	for name, into := range map[string]**regexp.Regexp{"limit": &d.limit, "limitSaid": &d.limitSaid, "restate": &d.restate, "reviewer": &d.reviewer, "ownerDefect": &d.ownerDefect,
		"regression": &d.regression, "openPart": &d.openPart, "qaCite": &d.qaCite, "beforeCapture": &d.beforeCapture} {
		pattern, ok := base.Patterns[name]
		if !ok {
			return d, fmt.Errorf("baseline file has no pattern %q", name)
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return d, fmt.Errorf("pattern %q: %w", name, err)
		}
		*into = compiled
	}
	return d, nil
}

func judge(spec measureSpec, r reading) (bool, string, error) {
	met, known := map[string]bool{"most": r.value <= spec.Target, "least": r.value >= spec.Target, "above": r.value > spec.Target, "below": r.value < spec.Target, "none": true}[spec.Bound]
	switch {
	case !known:
		return false, "", fmt.Errorf("measure %s has bound %q, want most, least, above, below or none", spec.Key, spec.Bound)
	case r.missing != "":
		return false, r.missing, nil
	case spec.Bound == "none":
		return true, "no target in analysis.md, read only", nil
	case r.n < spec.Least:
		return false, fmt.Sprintf("too few to read: %d of the %d it needs", r.n, spec.Least), nil
	case met:
		return true, "yes", nil
	}
	return false, fmt.Sprintf("no: %.4g against %s %g", r.value, spec.Bound, spec.Target), nil
}

func shown(r reading) string {
	if r.missing != "" {
		return r.missing
	}
	return fmt.Sprintf("%.4g: %s", r.value, r.text)
}
