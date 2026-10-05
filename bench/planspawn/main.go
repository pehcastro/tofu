package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tofu/internal/session"
)

type trace struct {
	Data struct {
		Agents   []session.AgentRun `json:"agents"`
		Requests []struct {
			Request string        `json:"request"`
			Agent   string        `json:"agent"`
			Usage   session.Usage `json:"usage"`
		} `json:"requests"`
		Calls []struct {
			Call  string `json:"call"`
			Agent string `json:"agent"`
			Tool  string `json:"tool"`
		} `json:"calls"`
	} `json:"data"`
}

func lead(agent string) bool {
	return agent == "" || agent == session.AuthorOrchestrator
}

type request struct {
	task         string
	passed       bool
	check        string
	leadRequests int
	leadTokens   int
	leadWrites   int
	subAgents    int
	subTokens    int
	stray        []string
}

func total(u session.Usage) int {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheWriteTokens
}

func readArm(dir string) ([]request, error) {
	var rows []request
	seen, seenAgents, seenPaths := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for n := 1; ; n++ {
		prefix := filepath.Join(dir, fmt.Sprintf("r%d", n))
		raw, err := os.ReadFile(prefix + ".trace.json")
		if os.IsNotExist(err) {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		var t trace
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, fmt.Errorf("%s.trace.json: %w", prefix, err)
		}
		check, err := os.ReadFile(prefix + ".check")
		if err != nil {
			return nil, err
		}
		status, err := os.ReadFile(prefix + ".status")
		if err != nil {
			return nil, err
		}
		verdict := strings.TrimSpace(string(check))
		verdict = verdict[strings.LastIndex(verdict, "\n")+1:]
		task, rest, _ := strings.Cut(verdict, " ")
		row := request{task: task, passed: strings.HasPrefix(rest, "PASS"), check: verdict}
		for _, r := range t.Data.Requests {
			if seen[r.Request] {
				continue
			}
			seen[r.Request] = true
			if lead(r.Agent) {
				row.leadRequests++
				row.leadTokens += total(r.Usage)
			} else {
				row.subTokens += total(r.Usage)
			}
		}
		for _, c := range t.Data.Calls {
			if !seen[c.Call] && lead(c.Agent) && (c.Tool == "write" || c.Tool == "edit") {
				row.leadWrites++
			}
			seen[c.Call] = true
		}
		for _, a := range t.Data.Agents {
			if !seenAgents[a.Agent] {
				seenAgents[a.Agent] = true
				row.subAgents++
			}
		}
		for line := range strings.Lines(string(status)) {
			path := strings.TrimSpace(line[min(3, len(line)):])
			if path == "" || seenPaths[path] {
				continue
			}
			seenPaths[path] = true
			if !strings.HasPrefix(path, "packages/") || strings.HasSuffix(path, ".md") {
				row.stray = append(row.stray, path)
			}
		}
		rows = append(rows, row)
	}
}

const barWidth = 40

func bar(tokens, most int) string {
	return strings.Repeat("#", max(1, tokens*barWidth/max(1, most)))
}

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: go run ./bench/planspawn <old arm dir> <new arm dir> <report.md>")
		os.Exit(2)
	}
	arms := []string{"old", "new"}
	rows := map[string][]request{}
	most := 0
	for i, arm := range arms {
		got, err := readArm(os.Args[1+i])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		rows[arm] = got
		for _, r := range got {
			most = max(most, r.leadTokens)
		}
	}
	var b strings.Builder
	b.WriteString("# plan_before_spawn: old wording against new\n\nOne app session per arm on claude-sub, one request after another (the first fresh, each next one through tofu --continue). Lead tokens are input, output, cache read and cache write summed over the orchestrator's requests in that request. Stray files are new paths outside packages/, or any new .md file.\n")
	for _, arm := range arms {
		fmt.Fprintf(&b, "\n## %s wording\n\n| # | task | sub-agents | lead requests | lead tokens | sub-agent tokens | lead writes and edits | stray files | check |\n|---|---|---|---|---|---|---|---|---|\n", arm)
		for i, r := range rows[arm] {
			fmt.Fprintf(&b, "| %d | %s | %d | %d | %d | %d | %d | %s | %s |\n", i+1, r.task, r.subAgents, r.leadRequests, r.leadTokens, r.subTokens, r.leadWrites,
				strings.Join(append([]string{fmt.Sprint(len(r.stray))}, r.stray...), " "), strings.ReplaceAll(r.check, "|", "/"))
		}
	}
	b.WriteString("\n## Lead tokens per request, in sequence\n\n```\n")
	for i := range max(len(rows["old"]), len(rows["new"])) {
		for _, arm := range arms {
			if i >= len(rows[arm]) {
				continue
			}
			r := rows[arm][i]
			fmt.Fprintf(&b, "r%d %-14s %s %-*s %d\n", i+1, r.task, arm, barWidth, bar(r.leadTokens, most), r.leadTokens)
		}
	}
	b.WriteString("```\n\n## Degrade check\n\n")
	degraded := 0
	for i, r := range rows["new"] {
		if i < len(rows["old"]) && rows["old"][i].passed && !r.passed {
			degraded++
			fmt.Fprintf(&b, "- r%d %s passed under the old wording and fails under the new: %s\n", i+1, r.task, r.check)
		}
	}
	if degraded == 0 {
		b.WriteString("No request's check fails under the new wording that passed under the old.\n")
	}
	if err := os.WriteFile(os.Args[3], []byte(b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(b.String())
}
