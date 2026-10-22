package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"tofu/internal/boardy"
)

const (
	boardKey   = "DEMO"
	ticketID   = "DEMO-1"
	acceptance = "- a UTF-8 name in a config file parses to the same name\n- go test ./internal/parse passes, with a case for a non-ASCII name\n- demo parse testdata/utf8.toml prints the name as written"
)

type counted struct {
	label                                                 string
	said, evidence, lines                                 int
	threadRunes, engineerRunes, evidenceRunes, proofInLog int
}

func drive(label, tofu, cassette string, proof []string, work string) (counted, error) {
	c := counted{label: label}
	dir := filepath.Join(work, label)
	home, project := filepath.Join(dir, "home"), filepath.Join(dir, "project")
	for _, made := range []string{home, project} {
		if err := os.MkdirAll(made, 0o755); err != nil {
			return c, err
		}
	}
	log, err := os.Create(filepath.Join(dir, "run.log"))
	if err != nil {
		return c, err
	}
	defer func() { _ = log.Close() }()
	env := []string{"HOME=" + home, "USERPROFILE=" + home, "TOFU_DRIVE_CASSETTE=" + cassette}
	for _, pair := range os.Environ() {
		name, _, _ := strings.Cut(strings.ToUpper(pair), "=")
		if !strings.HasPrefix(name, "CLAUDE") && !strings.HasSuffix(name, "_KEY") {
			env = append(env, pair)
		}
	}
	var shown []byte
	for _, args := range [][]string{
		{"boardy", "init", "--key", boardKey, "--name", "demo"},
		{"boardy", "epic", "new", "CFG", "config", "--board", boardKey},
		{"boardy", "new", "--board", boardKey, "--epic", "CFG", "--owns", "internal/parse/**", "--acceptance", acceptance, "parse the config"},
		{"run", "--dir", project, "--no-gate", "--sift", "free", "work " + ticketID},
		{"boardy", "show", ticketID, "--thread", "--json"},
	} {
		cmd := exec.Command(tofu, args...)
		cmd.Dir, cmd.Env, cmd.Stderr = project, env, log
		shown, err = cmd.Output()
		_, _ = fmt.Fprintf(log, "$ tofu %s\n%s\n", strings.Join(args, " "), shown)
		if err != nil {
			return c, fmt.Errorf("tofu %s: %w, its log is %s", args[0], err, log.Name())
		}
	}
	var envelope struct {
		Data struct {
			boardy.Ticket
			Thread []boardy.Event `json:"thread"`
		} `json:"data"`
	}
	if err := json.Unmarshal(shown, &envelope); err != nil {
		return c, err
	}
	ticket := envelope.Data
	c.lines = strings.Count(ticket.Acceptance[len(ticket.Acceptance)-1], "\n- ") + 1
	for _, event := range ticket.Thread {
		runes := utf8.RuneCountInString(event.Text)
		switch event.Kind {
		case boardy.Evidence:
			c.evidence++
			c.evidenceRunes += runes
			continue
		case boardy.Commented:
			c.said++
		}
		c.threadRunes += runes
		if event.Author != nil && event.Author.Kind == boardy.AuthorSubAgent {
			c.engineerRunes += runes
		}
	}
	for _, text := range proof {
		first, _, _ := strings.Cut(text, "\n")
		if strings.Contains(ticket.Log, first) {
			c.proofInLog++
		}
	}
	return c, nil
}

func evidenceIn(cassette string) ([]string, error) {
	raw, err := os.ReadFile(cassette)
	var proof []string
	for line := range strings.Lines(string(raw)) {
		var reply struct {
			Tools []struct {
				Name string
				Args struct{ Kind, Text string }
			}
		}
		if json.Unmarshal([]byte(line), &reply) != nil {
			continue
		}
		for _, tool := range reply.Tools {
			if tool.Name == "ticket_log" && tool.Args.Kind == string(boardy.Evidence) {
				proof = append(proof, tool.Args.Text)
			}
		}
	}
	return proof, err
}

func main() {
	before := flag.String("before", "", "the tofu binary built from the code before the change")
	after := flag.String("after", "", "the tofu binary built with the change")
	cassette := flag.String("cassette", filepath.Join("bench", "thread", "testdata", "run.jsonl"), "the recorded lead and sub-agent run both arms replay")
	flag.Parse()
	if *before == "" || *after == "" {
		fmt.Fprintln(os.Stderr, "bench/thread: name both --before and --after")
		os.Exit(2)
	}
	fail := func(err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, "bench/thread:", err)
			os.Exit(1)
		}
	}
	recorded, err := filepath.Abs(*cassette)
	fail(err)
	proof, err := evidenceIn(recorded)
	fail(err)
	work, err := os.MkdirTemp("", "tofu-bench-thread-")
	fail(err)
	var rows []counted
	for _, arm := range [][2]string{{"before", *before}, {"after", *after}} {
		row, err := drive(arm[0], arm[1], recorded, proof, work)
		fail(err)
		rows = append(rows, row)
	}
	fmt.Printf("bench/thread %s, %s/%s\n", time.Now().Format(time.DateOnly), runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  credential  none: each arm runs tofu run on %s in a fresh home under %s\n\n", recorded, work)
	fmt.Printf("%-7s %-7s %6s %9s %13s %15s %15s %16s %13s\n", "arm", "ticket", "said", "evidence", "thread chars", "engineer chars", "evidence chars", "evidence/lines", "proof in Log")
	for _, r := range rows {
		fmt.Printf("%-7s %-7s %6d %9d %13d %15d %15d %13d/%-2d %10d/%d\n", r.label, ticketID, r.said, r.evidence, r.threadRunes, r.engineerRunes, r.evidenceRunes, r.evidence, r.lines, r.proofInLog, len(proof))
	}
	drop := func(before, after int) string {
		return fmt.Sprintf("%.0f%%, from %d to %d", 100*(1-float64(after)/float64(before)), before, after)
	}
	fmt.Printf("\nthread characters drop %s\nengineer characters drop %s\n", drop(rows[0].threadRunes, rows[1].threadRunes), drop(rows[0].engineerRunes, rows[1].engineerRunes))
}
