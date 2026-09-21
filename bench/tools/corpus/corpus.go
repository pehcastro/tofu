package corpus

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	questionsFile = "testdata/questions.jsonl"

	BandNamed     = "named"
	BandDescribed = "described"
	BandIntent    = "intent"

	ScoreAll = "all"

	TreeTofu = "tofu"
)

type Answer struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

type Question struct {
	ID          string   `json:"id"`
	Text        string   `json:"text"`
	Tree        string   `json:"tree"`
	Band        string   `json:"band"`
	File        string   `json:"file"`
	Line        int      `json:"line"`
	Answers     []Answer `json:"answers,omitempty"`
	ScoreRule   string   `json:"score_rule,omitempty"`
	Identifiers []string `json:"identifiers,omitempty"`
	Source      string   `json:"source"`
	Note        string   `json:"note"`
}

func (q Question) AllAnswers() []Answer {
	if len(q.Answers) > 0 {
		return q.Answers
	}
	if q.File == "" {
		return nil
	}
	return []Answer{{File: q.File, Line: q.Line}}
}

func (q Question) MatchesFile(path string) bool {
	for _, a := range q.AllAnswers() {
		if a.File == path {
			return true
		}
	}
	return false
}

func (q Question) AnyAnswerFileIn(content string) bool {
	for _, a := range q.AllAnswers() {
		if strings.Contains(content, a.File) {
			return true
		}
	}
	return false
}

func TreeRoot(tofuRoot, tree string) string {
	if tree == "" || tree == TreeTofu {
		return tofuRoot
	}
	return tofuRoot + "/.local/sources/" + tree
}

func ReadQuestions(path string) ([]Question, error) {
	if path == "" {
		path = questionsFile
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var out []Question
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var q Question
		if err := json.Unmarshal([]byte(line), &q); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, q)
	}
	return out, scanner.Err()
}
