package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

type arm string

const (
	armFloor arm = "floor"
	armToday arm = "today"
)

type options struct {
	corpus   string
	arms     []arm
	chains   int
	tofu     string
	wire     string
	model    string
	ceiling  int
	spend    bool
	cassette string
}

type printer func(format string, args ...any)

func parse(args []string) (options, error) {
	opts := options{}
	set := flag.NewFlagSet("bench/memory", flag.ContinueOnError)
	set.StringVar(&opts.corpus, "corpus", "fork30", "the corpus under bench/memory/testdata")
	arms := set.String("arms", "floor,today", "floor: the questions alone in a fresh session; today: the whole chain on today's carry")
	set.IntVar(&opts.chains, "chains", 0, "run the first N chains, 0 for all")
	set.StringVar(&opts.tofu, "tofu", os.Getenv("TOFU_BIN"), "the tofu binary to drive, TOFU_BIN by default")
	set.StringVar(&opts.wire, "wire", "claude-sub", "the source that pays for the lead")
	set.StringVar(&opts.model, "model", "claude-sub/claude-sonnet-5", "the lead's model, as source/model")
	set.IntVar(&opts.ceiling, "ceiling", 32000, "TOFU_CONTEXT_CEILING for every driven chain, low enough that a chain forks at least twice")
	set.BoolVar(&opts.spend, "spend", false, "drive tofu live on the wire, which spends the subscription")
	set.StringVar(&opts.cassette, "cassette", "", "drive tofu on this cassette in a fresh home, with no network")
	if err := set.Parse(args); err != nil {
		return options{}, err
	}
	for _, name := range strings.Split(*arms, ",") {
		if !slices.Contains([]arm{armFloor, armToday}, arm(name)) {
			return options{}, fmt.Errorf("--arms names %q, and the arms are floor and today", name)
		}
		opts.arms = append(opts.arms, arm(name))
	}
	if opts.spend && opts.wire != "claude-sub" {
		return options{}, fmt.Errorf("--wire %s: this bench runs live on claude-sub alone", opts.wire)
	}
	if opts.cassette != "" {
		full, err := filepath.Abs(opts.cassette)
		opts.cassette = full
		return opts, err
	}
	return opts, nil
}

func run(opts options, say printer) error {
	c, err := loadCorpus(opts.corpus)
	if err != nil {
		return err
	}
	found := c.leaks()
	printLeaks(say, c, found)
	if len(found) > 0 {
		return fmt.Errorf("%d leaks in corpus %s, so no arm runs on it", len(found), c.Name)
	}
	if !opts.spend && opts.cassette == "" {
		say("no arm ran: each one drives tofu serve on claude-sub, which needs the network and spends the subscription. --spend runs them live, --cassette FILE runs the harness offline\n")
		return nil
	}
	if opts.tofu == "" {
		return fmt.Errorf("name the tofu binary with --tofu or TOFU_BIN")
	}
	chains := c.Chains
	if opts.chains > 0 {
		chains = chains[:min(opts.chains, len(chains))]
	}
	work, err := os.MkdirTemp("", "tofu-bench-memory-")
	if err != nil {
		return err
	}
	env := os.Environ()
	credential := "claude-sub, a subscription, through the home tofu is signed in from"
	if opts.cassette != "" {
		home := filepath.Join(work, "home")
		if err := os.MkdirAll(home, 0o755); err != nil {
			return err
		}
		env = append(env, "HOME="+home, "USERPROFILE="+home)
		credential = "none, cassette " + opts.cassette + " in the fresh home " + home
	}
	version, _ := exec.Command(opts.tofu, "version").Output()
	say("bench/memory %s, %s\n", c.Name, time.Now().Format(time.DateOnly))
	say("  machine     %s/%s, %d cpus\n", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
	say("  tofu        %s (%s)\n", strings.Join(strings.Fields(string(version)), " "), opts.tofu)
	say("  credential  %s\n", credential)
	say("  lead        %s asked on %s, the model each response reported is in the arm rows\n", opts.model, opts.wire)
	say("  ceiling     TOFU_CONTEXT_CEILING=%d\n", opts.ceiling)
	say("  chains      %d of %d, %d questions\n", len(chains), len(c.Chains), len(chains)*questionsPerChain)
	say("  warm up     none, no call is discarded\n")
	say("  scratch     %s\n\n", work)
	var scores []armScore
	for _, a := range opts.arms {
		score := armScore{arm: a}
		for _, ch := range chains {
			driven, err := driveChain(opts, env, filepath.Join(work, string(a), ch.ID), a, ch)
			if err != nil {
				return fmt.Errorf("arm %s, chain %s: %w", a, ch.ID, err)
			}
			say("%s", score.add(driven))
		}
		scores = append(scores, score)
		say("\n")
	}
	say("%s", table(scores))
	return nil
}

func main() {
	opts, err := parse(os.Args[1:])
	if err == nil {
		err = run(opts, func(format string, args ...any) { _, _ = fmt.Printf(format, args...) })
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "bench/memory:", err)
		os.Exit(1)
	}
}
