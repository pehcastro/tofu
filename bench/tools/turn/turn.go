package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	toolscorpus "tofu/bench/tools/corpus"
)

type Outcome struct {
	Arm              string
	Hit              bool
	PromptTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	CompletionTokens int
	ToolCalls        int
	Cost             float64
	Elapsed          time.Duration
	Skipped          string
}

func (o Outcome) TotalTokens() int {
	return o.PromptTokens + o.CacheReadTokens + o.CacheWriteTokens + o.CompletionTokens
}

const promptTemplate = "Using only your own read only search tools against this repository, answer with the exact file path and line number that answers this question, in the form path:line. Question: "

func hitsAnswer(text string, q toolscorpus.Question) bool {
	for _, a := range q.AllAnswers() {
		if strings.Contains(text, a.File+":"+strconv.Itoa(a.Line)) {
			return true
		}
	}
	return false
}

func runStdin(ctx context.Context, name string, dir string, args []string, prompt string) (string, time.Duration, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = strings.NewReader(prompt)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	started := time.Now()
	err := cmd.Run()
	return out.String(), time.Since(started), err
}

func RunClaude(ctx context.Context, root string, q toolscorpus.Question) Outcome {
	text, elapsed, err := runStdin(ctx, "claude", root,
		[]string{"-p", "--output-format", "json", "--allowedTools", "Grep,Glob,Read"}, promptTemplate+q.Text)
	if err != nil {
		return Outcome{Arm: "claude -p", Skipped: err.Error(), Elapsed: elapsed}
	}
	var parsed struct {
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			OutputTokens             int `json:"output_tokens"`
		} `json:"usage"`
		NumTurns   int     `json:"num_turns"`
		TotalCost  float64 `json:"total_cost_usd"`
		Result     string  `json:"result"`
		DurationMS int     `json:"duration_ms"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return Outcome{Arm: "claude -p", Skipped: "could not parse claude -p json: " + err.Error(), Elapsed: elapsed}
	}
	return Outcome{
		Arm:              "claude -p",
		Hit:              hitsAnswer(parsed.Result, q),
		PromptTokens:     parsed.Usage.InputTokens,
		CacheReadTokens:  parsed.Usage.CacheReadInputTokens,
		CacheWriteTokens: parsed.Usage.CacheCreationInputTokens,
		CompletionTokens: parsed.Usage.OutputTokens,
		ToolCalls:        parsed.NumTurns,
		Cost:             parsed.TotalCost,
		Elapsed:          time.Duration(parsed.DurationMS) * time.Millisecond,
	}
}

type codexEvent struct {
	Type string `json:"type"`
	Item struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
	Usage struct {
		InputTokens           int `json:"input_tokens"`
		CachedInputTokens     int `json:"cached_input_tokens"`
		CacheWriteInputTokens int `json:"cache_write_input_tokens"`
		OutputTokens          int `json:"output_tokens"`
	} `json:"usage"`
}

func RunCodex(ctx context.Context, root string, q toolscorpus.Question) Outcome {
	text, elapsed, err := runStdin(ctx, "codex", "",
		[]string{"exec", "-s", "read-only", "-C", root, "--skip-git-repo-check", "--json"}, promptTemplate+q.Text)
	if err != nil {
		return Outcome{Arm: "codex exec", Skipped: err.Error(), Elapsed: elapsed}
	}
	var (
		lastAnswer string
		toolCalls  int
		usage      codexEvent
	)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev codexEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		switch {
		case ev.Type == "item.completed" && ev.Item.Type == "command_execution":
			toolCalls++
		case ev.Type == "item.completed" && ev.Item.Type == "agent_message":
			lastAnswer = ev.Item.Text
		case ev.Type == "turn.completed":
			usage = ev
		}
	}
	if lastAnswer == "" && usage.Usage.InputTokens == 0 {
		return Outcome{Arm: "codex exec", Skipped: "no turn.completed event in codex --json output", Elapsed: elapsed}
	}
	return Outcome{
		Arm:              "codex exec",
		Hit:              hitsAnswer(lastAnswer, q),
		PromptTokens:     usage.Usage.InputTokens - usage.Usage.CachedInputTokens,
		CacheReadTokens:  usage.Usage.CachedInputTokens,
		CacheWriteTokens: usage.Usage.CacheWriteInputTokens,
		CompletionTokens: usage.Usage.OutputTokens,
		ToolCalls:        toolCalls,
		Elapsed:          elapsed,
	}
}

var (
	turnLine = regexp.MustCompile(`^turn \S+ outcome (\S+) model \S+ asked_as \S+ .* wall_clock_ms (\d+)$`)
	stepLine = regexp.MustCompile(`^step \d+: stop_reason \S+ in (\d+) out (\d+) cache_read (\d+) cache_write (\d+)$`)
	toolLine = regexp.MustCompile(`^step \d+: tool_call `)
)

func RunTofu(ctx context.Context, dir, wire string, q toolscorpus.Question) Outcome {
	exe, exeErr := filepath.Abs("../../../tofu.exe")
	tree, treeErr := filepath.Abs(dir)
	if err := errors.Join(exeErr, treeErr); err != nil {
		return Outcome{Arm: "tofu run", Skipped: err.Error()}
	}
	cmd := exec.CommandContext(ctx, exe, "run", "--dir", tree, "--wire", wire, promptTemplate+q.Text)
	cmd.Dir = tree
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	started := time.Now()
	err := cmd.Run()
	elapsed := time.Since(started)
	if err != nil {
		return Outcome{Arm: "tofu run", Skipped: err.Error(), Elapsed: elapsed}
	}
	text := out.String()
	result := Outcome{Arm: "tofu run", Elapsed: elapsed}
	wallClockSeen := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := turnLine.FindStringSubmatch(line); m != nil {
			if ms, convErr := strconv.Atoi(m[2]); convErr == nil {
				result.Elapsed = time.Duration(ms) * time.Millisecond
				wallClockSeen = true
			}
			continue
		}
		if m := stepLine.FindStringSubmatch(line); m != nil {
			in, _ := strconv.Atoi(m[1])
			out2, _ := strconv.Atoi(m[2])
			cr, _ := strconv.Atoi(m[3])
			cw, _ := strconv.Atoi(m[4])
			result.PromptTokens += in
			result.CompletionTokens += out2
			result.CacheReadTokens += cr
			result.CacheWriteTokens += cw
			continue
		}
		if toolLine.MatchString(line) {
			result.ToolCalls++
		}
	}
	if !wallClockSeen {
		return Outcome{Arm: "tofu run", Skipped: "no turn summary line in tofu run output", Elapsed: elapsed}
	}
	result.Hit = hitsAnswer(text, q)
	return result
}
