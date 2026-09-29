package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/widget"
)

const migrateUsage = "tofu migrate [--dry-run] [--json]"

type stateMove struct {
	Name    string `json:"name"`
	From    string `json:"from"`
	To      string `json:"to"`
	Files   int    `json:"files"`
	Bytes   int64  `json:"bytes"`
	Failure string `json:"failure,omitempty"`
}

type migratedSession struct {
	ID           string   `json:"id"`
	Turns        int      `json:"turns"`
	Events       int      `json:"events"`
	SubAgentRuns int      `json:"sub_agent_runs"`
	From         []string `json:"from"`
}

type skippedSession struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type migrateReport struct {
	DryRun   bool              `json:"dry_run"`
	From     string            `json:"from"`
	Moves    []stateMove       `json:"moves"`
	Sessions []migratedSession `json:"sessions"`
	Skipped  []skippedSession  `json:"skipped"`
	Head     string            `json:"head,omitempty"`
	Written  int               `json:"written"`
}

func plannedStateMoves(project string) (config string, moves []stateMove, err error) {
	config = sys.StateDir(project)
	state, err := sys.ProjectStateDirAt(project)
	if err != nil {
		return "", nil, err
	}
	quota, err := sys.QuotaDir()
	if err != nil {
		return "", nil, err
	}
	for _, name := range sys.MovedStateNames() {
		move := stateMove{Name: name, From: filepath.Join(config, name), To: filepath.Join(state, name)}
		if name == sys.QuotaDirName {
			move.To = quota
		}
		if _, err := os.Stat(move.From); err == nil && move.From != move.To {
			moves = append(moves, move)
		}
	}
	return config, moves, nil
}

func moveStates(moves []stateMove) []stateMove {
	for i, move := range moves {
		files, size, err := copyTreeInto(os.DirFS(filepath.Dir(move.From)), move.Name, filepath.Dir(move.To))
		if err == nil {
			err = os.RemoveAll(move.From)
		}
		if err != nil {
			moves[i].Failure = err.Error()
			continue
		}
		moves[i].Files, moves[i].Bytes = files, size
	}
	return moves
}

func moveProjectState(out io.Writer, project string) []stateMove {
	page := cli.Detect(out, os.Environ())
	_, moves, err := plannedStateMoves(project)
	if err != nil {
		_ = page.Print(out, page.ErrorLine("the state in "+page.Path(sys.StateDir(project))+" was not moved: "+err.Error(), ""))
		return nil
	}
	moves = moveStates(moves)
	var names, targets []string
	var files int
	var size int64
	var lines []string
	for _, move := range moves {
		if move.Failure != "" {
			lines = append(lines, page.Glyph(cli.Warn)+" "+move.Name+" stays in "+page.Path(filepath.Dir(move.From))+": "+move.Failure+cli.Gap+page.Hint("tofu migrate"))
			continue
		}
		names, files, size = append(names, move.Name), files+move.Files, size+move.Bytes
		if target := page.Path(filepath.Dir(move.To)); !slices.Contains(targets, target) {
			targets = append(targets, target)
		}
	}
	if len(names) > 0 {
		moved := strings.Join([]string{"moved " + strings.Join(names, ", "), plural(files, "file"), widget.Size(int(size))}, " · ")
		lines = append([]string{page.Glyph(cli.Changed) + " " + moved + cli.Gap + page.Label(strings.Join(targets, ", "))}, lines...)
	}
	_ = page.Print(out, lines)
	return moves
}

func migrateVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "migrate", usageLine: migrateUsage, out: out, errOut: errOut}
	report := migrateReport{Moves: []stateMove{}, Sessions: []migratedSession{}, Skipped: []skippedSession{}}
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			report.DryRun = true
		case jsonFlag:
			o.asJSON = true
		default:
			return o.usage(errors.New("unknown argument " + strconv.Quote(arg)))
		}
	}
	project, err := os.Getwd()
	var state string
	var moves []stateMove
	if err == nil {
		report.From, moves, err = plannedStateMoves(project)
	}
	if err == nil {
		state, err = sys.ProjectStateDirAt(project)
	}
	if err != nil {
		return o.fail(err)
	}
	sessions := session.OpenAt(state)
	if report.DryRun {
		for i, move := range moves {
			moves[i].Files, moves[i].Bytes = treeSize(move.From)
			if move.Name == "sessions" {
				sessions = session.NewStore(move.From)
			}
		}
	} else {
		moves = moveStates(moves)
	}
	report.Moves = append(report.Moves, moves...)
	var problems []cli.Problem
	for _, move := range moves {
		if move.Failure != "" {
			problems = append(problems, cli.Problem{What: move.Name + " stays in " + report.From + ": " + move.Failure})
		}
	}
	if len(problems) == 0 {
		problems = convertSessions(sessions, &report)
	}
	if o.asJSON {
		err = writeJSON(out, cli.Envelope{Verb: o.verb, OK: len(problems) == 0, At: time.Now(), Data: report, Problems: problems})
	} else {
		page := cli.Detect(out, os.Environ())
		err = page.Print(out, migrateLines(page, report, len(problems)))
		for _, problem := range problems {
			o.errorLine(problem)
		}
	}
	if err != nil || len(problems) > 0 {
		return exitVerdict
	}
	return exitOK
}

func convertSessions(sessions *session.Store, report *migrateReport) []cli.Problem {
	plan, err := sessions.PlanConversion()
	if err != nil {
		return []cli.Problem{{What: "the sessions in the old layout were not read: " + err.Error()}}
	}
	for _, skipped := range plan.Skipped {
		report.Skipped = append(report.Skipped, skippedSession{ID: skipped.ID, Reason: skipped.Reason.Error()})
	}
	for _, converted := range plan.Sessions {
		header := converted.Header
		report.Sessions = append(report.Sessions, migratedSession{ID: header.ID, Turns: header.Turns, Events: len(converted.Events), SubAgentRuns: len(header.Agents), From: converted.From})
	}
	report.Head = plan.Head
	if report.DryRun || len(plan.Sessions) == 0 {
		return nil
	}
	report.Written, err = sessions.Convert(plan)
	if err != nil {
		return []cli.Problem{{What: err.Error()}}
	}
	return nil
}

func migrateLines(page cli.Page, report migrateReport, failed int) []string {
	facts := []string{page.Path(report.From)}
	moveSection, sessionSection, verdict := "moved", "converted", cli.Verdict{Mark: cli.Done}
	var said []string
	if report.DryRun {
		facts = append(facts, "dry run")
		moveSection, sessionSection, verdict = "would move", "would convert", cli.Verdict{Mark: cli.Idle}
	}
	var moved []cli.Row
	for _, move := range report.Moves {
		if move.Failure == "" {
			moved = append(moved, cli.Row{Mark: cli.Changed, Cells: []string{move.Name, plural(move.Files, "file"), widget.Size(int(move.Bytes))}, Detail: page.Path(move.To)})
		}
	}
	if len(moved) > 0 {
		said = append(said, strconv.Itoa(len(moved))+" "+moveSection)
	}
	if len(report.Sessions) > 0 {
		said = append(said, strconv.Itoa(len(report.Sessions))+" "+sessionSection)
	}
	verdict.Text = strings.Join(said, " · ")
	switch {
	case failed > 0:
		verdict = cli.Verdict{Mark: cli.Fail, Text: strconv.Itoa(failed) + " failed"}
	case len(said) == 0:
		verdict = cli.Verdict{Mark: cli.Done, Text: "nothing to migrate"}
	}
	lines := page.Title("Migrate", facts, verdict)
	if len(moved) > 0 {
		lines = append(append(lines, "", page.Section(moveSection, cli.Verdict{})), cli.Indent(page.Rows(moved)...)...)
	}
	var rows []cli.Row
	for _, converted := range report.Sessions {
		detail := "from " + strings.Join(converted.From, " ")
		if converted.SubAgentRuns > 0 {
			detail = plural(converted.SubAgentRuns, "sub-agent run") + " · " + detail
		}
		rows = append(rows, cli.Row{Mark: cli.Added, Cells: []string{converted.ID, plural(converted.Turns, "turn"), plural(converted.Events, "event")}, Detail: detail})
	}
	for _, skipped := range report.Skipped {
		rows = append(rows, cli.Row{Mark: cli.Warn, Cells: []string{skipped.ID}, Detail: strings.ReplaceAll(skipped.Reason, "\n", "; ")})
	}
	if len(rows) == 0 {
		return lines
	}
	lines = append(append(lines, "", page.Section("sessions", cli.Verdict{})), cli.Indent(page.Rows(rows)...)...)
	return append(lines, cli.Indent(page.Facts([]cli.Fact{{Label: "head", Text: report.Head}})...)...)
}

func treeSize(root string) (files int, size int64) {
	_ = filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err == nil {
			files, size = files+1, size+info.Size()
		}
		return err
	})
	return files, size
}
