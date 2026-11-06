package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	turnLine  = regexp.MustCompile(`(?m)^turn \S+ outcome (\S+) model (\S+) asked_as \S+ spend (.*?) wall_clock_ms`)
	stepLine  = regexp.MustCompile(`(?m)^step \d+: stop_reason \S+ in (\d+) out (\d+) cache_read (\d+) cache_write (\d+)`)
	totalLine = regexp.MustCompile(`(?m)^totals requests (\d+) calls (\d+)`)
	gateLine  = regexp.MustCompile(`(?m)^gate decisions \d+ cost \$[\d.]+`)
	raceCall  = regexp.MustCompile(`(?m)^step \d+: tool_call tool=race `)
	raceRun   = regexp.MustCompile(`(?m)^run (\d+): (.*), cost (.*?), (\d+) requests, (\d+) tokens in and (\d+) out(?: on (\S+))?$`)
	raceHead  = regexp.MustCompile(`(?m)^race: .*$`)
)

func number(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func (o *outcome) fromTofuRun(stdout string) {
	calls := 0
	for _, turn := range turnLine.FindAllStringSubmatch(stdout, -1) {
		o.model, o.spend = turn[2], turn[3]
		o.note += "outcome " + turn[1] + ", "
	}
	for _, step := range stepLine.FindAllStringSubmatch(stdout, -1) {
		o.in += number(step[1]) + number(step[3]) + number(step[4])
		o.out += number(step[2])
	}
	for _, total := range totalLine.FindAllStringSubmatch(stdout, -1) {
		o.requests += number(total[1])
		calls += number(total[2])
	}
	o.note += fmt.Sprintf("%d tool calls, %s", calls, strings.Join(gateLine.FindAllString(stdout, -1), ", "))
	if o.arm.side == sideTurn {
		o.note += fmt.Sprintf(", race called %d times, its sub-agents' requests and tokens added from the session's tool results", len(raceCall.FindAllString(stdout, -1)))
	}
}

func (o *outcome) fromRace(text string) {
	o.note = strings.TrimPrefix(o.note+"; "+raceHead.FindString(text), "; ")
	for _, r := range raceRun.FindAllStringSubmatch(text, -1) {
		o.requests += number(r[4])
		o.in += number(r[5])
		o.out += number(r[6])
		o.spend = cmp.Or(o.spend, r[3])
		if r[7] != "" && !strings.Contains(o.model, r[7]) {
			o.model = strings.TrimPrefix(o.model+", "+r[7], ", ")
		}
		o.note += "; run " + r[1] + ": " + r[2]
	}
}

func (o *outcome) addRaces(home string) {
	logs, _ := filepath.Glob(filepath.Join(home, ".tofu", "projects", "orders-*", "sessions", "*", "blobs.jsonl"))
	seen := map[string]bool{}
	for _, path := range logs {
		body, err := os.ReadFile(path)
		if err != nil {
			o.failed = cmp.Or(o.failed, err.Error())
			return
		}
		for line := range strings.Lines(string(body)) {
			var blob struct {
				Body struct {
					Content string `json:"content"`
				} `json:"body"`
			}
			if json.Unmarshal([]byte(line), &blob) == nil && raceHead.MatchString(blob.Body.Content) && !seen[blob.Body.Content] {
				seen[blob.Body.Content] = true
				o.fromRace(blob.Body.Content)
			}
		}
	}
}

func (o *outcome) fromClaude(stdout string) {
	var said struct {
		NumTurns     int     `json:"num_turns"`
		TotalCostUSD float64 `json:"total_cost_usd"`
		IsError      bool    `json:"is_error"`
		ModelUsage   map[string]struct {
			InputTokens              int `json:"inputTokens"`
			OutputTokens             int `json:"outputTokens"`
			CacheReadInputTokens     int `json:"cacheReadInputTokens"`
			CacheCreationInputTokens int `json:"cacheCreationInputTokens"`
		} `json:"modelUsage"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &said); err != nil {
		o.failed = "claude printed no json result: " + err.Error()
		return
	}
	var models []string
	for name, use := range said.ModelUsage {
		models = append(models, name)
		o.in += use.InputTokens + use.CacheReadInputTokens + use.CacheCreationInputTokens
		o.out += use.OutputTokens
	}
	slices.Sort(models)
	o.model, o.requests = strings.Join(models, ", "), said.NumTurns
	o.spend = fmt.Sprintf("subscription, no money; notional total_cost_usd %.4f", said.TotalCostUSD)
	o.note = fmt.Sprintf("num_turns %d, is_error %v; requests is num_turns", said.NumTurns, said.IsError)
}
