package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"tofu/internal/boardy"
	"tofu/internal/subagent"
)

const (
	replayKey   = "REPLAY"
	replayActor = "replay-lead"
	replayOwner = "replay-person"
	cacheName   = "fixtures.json"
)

type fixture struct {
	Acceptance string `json:"acceptance"`
	Evidence   []int  `json:"evidence"`
	Reason     string `json:"reason"`
}

type verdict struct {
	lines, open, unread bool
}

func main() {
	tofu := flag.String("tofu", "tofu", "the tofu binary that reads the board")
	board := flag.String("board", "TOFU", "the board whose done moves are replayed")
	scratch := flag.String("scratch", filepath.Join(os.TempDir(), "boardy-replay"), "a folder for the fixtures, the fresh home and the replay board")
	workers := flag.Int("workers", 4, "moves replayed at once")
	flag.Parse()
	if err := run(*tofu, *board, *scratch, *workers); err != nil {
		fmt.Fprintln(os.Stderr, "bench boardy replay:", err)
		os.Exit(1)
	}
}

func run(tofu, board, scratch string, workers int) error {
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return err
	}
	fixtures, err := cached(filepath.Join(scratch, cacheName), func() ([]fixture, error) { return read(tofu, board) })
	if err != nil {
		return err
	}
	managed, err := freshHome(tofu, scratch)
	if err != nil {
		return err
	}
	verdicts := make([]verdict, len(fixtures))
	work := make(chan int)
	var wg sync.WaitGroup
	var failed error
	var mu sync.Mutex
	for worker := range workers {
		key := fmt.Sprintf("%s%d", replayKey, worker)
		if _, err := managed.Store.Init(key, key); err != nil {
			return err
		}
		if _, err := managed.AddManager(key, replayActor, replayOwner); err != nil {
			return err
		}
		wg.Go(func() {
			for at := range work {
				v, err := replay(managed, key, fixtures[at])
				mu.Lock()
				verdicts[at], failed = v, errors.Join(failed, err)
				mu.Unlock()
			}
		})
	}
	for at := range fixtures {
		work <- at
	}
	close(work)
	wg.Wait()
	if failed != nil {
		return failed
	}
	report(board, fixtures, verdicts)
	return nil
}

func cached(path string, read func() ([]fixture, error)) ([]fixture, error) {
	var fixtures []fixture
	if raw, err := os.ReadFile(path); err == nil {
		return fixtures, json.Unmarshal(raw, &fixtures)
	}
	fixtures, err := read()
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(fixtures)
	if err == nil {
		err = os.WriteFile(path, raw, 0o644)
	}
	return fixtures, err
}

func tofuJSON(tofu string, into any, args ...string) error {
	cmd := exec.Command(tofu, append(args, "--json")...)
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "CLAUDECODE=") || strings.HasPrefix(kv, "CLAUDE_CODE_SESSION_ID=")
	})
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("%s %s: %w", tofu, strings.Join(args, " "), err)
	}
	envelope := struct {
		OK   bool `json:"ok"`
		Data any  `json:"data"`
	}{Data: into}
	if err := json.Unmarshal(out, &envelope); err != nil || !envelope.OK {
		return fmt.Errorf("%s %s: not ok: %w", tofu, strings.Join(args, " "), err)
	}
	return nil
}

func read(tofu, board string) ([]fixture, error) {
	var list struct {
		Tickets []boardy.Ticket `json:"tickets"`
	}
	if err := tofuJSON(tofu, &list, "boardy", "list", "--board", board, "--all"); err != nil {
		return nil, err
	}
	var fixtures []fixture
	for _, ticket := range list.Tickets {
		var shown struct {
			Thread []boardy.Event `json:"thread"`
		}
		if err := tofuJSON(tofu, &shown, "boardy", "show", ticket.ID, "--thread"); err != nil {
			return nil, err
		}
		inForce := ""
		if len(ticket.Acceptance) > 0 {
			inForce = ticket.Acceptance[len(ticket.Acceptance)-1]
		}
		evidenceBy := func(at time.Time) []int {
			var lines []int
			for _, event := range shown.Thread {
				if event.Kind == boardy.Evidence && !event.At.After(at) {
					lines = append(lines, event.Line)
				}
			}
			return lines
		}
		reasoned := false
		for _, event := range shown.Thread {
			if event.Kind == boardy.Moved && event.To == boardy.Done {
				reasoned = true
				fixtures = append(fixtures, fixture{Acceptance: inForce, Evidence: evidenceBy(event.At), Reason: event.Text})
			}
		}
		if !reasoned && ticket.Status == boardy.Done {
			fixtures = append(fixtures, fixture{Acceptance: inForce, Evidence: evidenceBy(ticket.Updated)})
		}
	}
	return fixtures, nil
}

func freshHome(tofu, scratch string) (boardy.Managed, error) {
	home, err := os.MkdirTemp(scratch, "home-")
	project := filepath.Join(home, "project")
	for _, name := range []string{"HOME", "USERPROFILE"} {
		err = errors.Join(err, os.Setenv(name, home))
	}
	if err = errors.Join(err, os.MkdirAll(project, 0o755)); err != nil {
		return boardy.Managed{}, err
	}
	if out, err := exec.Command(tofu, "models", "bind", "classifier", "typesafe/jev-latest").CombinedOutput(); err != nil {
		return boardy.Managed{}, fmt.Errorf("binding the classifier: %w: %s", err, out)
	}
	store, err := boardy.OpenStore(project)
	return boardy.Managed{Local: boardy.Local{Store: store}, Person: replayOwner}, err
}

func replay(m boardy.Managed, key string, f fixture) (verdict, error) {
	lines := subagent.InForce([]string{f.Acceptance})
	if len(lines) == 0 {
		lines = []string{"no acceptance line was written"}
	}
	evidenced := func(evidence []int) (boardy.Ticket, error) {
		made, err := m.Create(key, boardy.Ticket{Front: boardy.Front{Title: "replayed done move"}, Problem: "replayed", Scope: "replayed",
			Before: "replayed", Driven: "replayed", StillWorks: "replayed", ErrorCase: "replayed", Feasibility: "replayed", Acceptance: []string{"- " + strings.Join(lines, "\n- ")}}, replayActor)
		for _, line := range evidence {
			if err == nil && line <= len(lines) {
				_, err = m.Evidence(made.ID, "replayed evidence", replayActor, line)
			}
		}
		return made, err
	}
	var v verdict
	historical, err := evidenced(f.Evidence)
	if err != nil {
		return v, err
	}
	_, err = m.Move(historical.ID, boardy.Done, "", replayActor)
	v.lines = refused(err)
	if strings.TrimSpace(f.Reason) == "" {
		return v, nil
	}
	every := make([]int, len(lines))
	for i := range every {
		every[i] = i + 1
	}
	proved, err := evidenced(every)
	if err != nil {
		return v, err
	}
	_, err = m.Move(proved.ID, boardy.Done, f.Reason, replayActor)
	var waiting boardy.WaitingError
	v.open, v.unread = refused(err), errors.As(err, &waiting)
	return v, nil
}

func refused(err error) bool {
	var refusal boardy.RefusedError
	return errors.As(err, &refusal)
}

func report(board string, fixtures []fixture, verdicts []verdict) {
	count := func(keep func(fixture, verdict) bool) int {
		n := 0
		for i, f := range fixtures {
			if keep(f, verdicts[i]) {
				n++
			}
		}
		return n
	}
	share := func(n, of int) string {
		return fmt.Sprintf("%d of %d, %.1f%%", n, of, 100*float64(n)/float64(max(of, 1)))
	}
	all := len(fixtures)
	reasoned := count(func(f fixture, _ verdict) bool { return strings.TrimSpace(f.Reason) != "" })
	fmt.Printf("bench boardy replay: %s, %d done moves, %s\n\n", board, all, time.Now().Format(time.DateOnly))
	fmt.Println("| gate | refused |")
	fmt.Println("|---|---|")
	fmt.Printf("| an acceptance line with no evidence entry | %s |\n", share(count(func(_ fixture, v verdict) bool { return v.lines }), all))
	fmt.Printf("| the reason names an open part (Jev done_reason@1) | %s reasoned moves |\n", share(count(func(_ fixture, v verdict) bool { return v.open }), reasoned))
	fmt.Printf("| Jev could not read the reason, held for the person | %s reasoned moves |\n", share(count(func(_ fixture, v verdict) bool { return v.unread }), reasoned))
	fmt.Printf("| either gate | %s |\n", share(count(func(_ fixture, v verdict) bool { return v.lines || v.open }), all))
	fmt.Printf("| either gate, moves with evidence entries only | %s |\n", share(
		count(func(f fixture, v verdict) bool { return len(f.Evidence) > 0 && (v.lines || v.open) }),
		count(func(f fixture, _ verdict) bool { return len(f.Evidence) > 0 })))
}
