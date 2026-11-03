package corpus

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	BandNamed     = "named"
	BandDescribed = "described"
	BandIntent    = "intent"

	ScoreAll = "all"

	TreeTofu = "tofu"

	fingerprintHexDigits = 12
)

type Answer struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

type LineFingerprint string

func fingerprintOf(line string) LineFingerprint {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(line), " ")))
	return LineFingerprint(hex.EncodeToString(sum[:])[:fingerprintHexDigits])
}

type Pin struct {
	Answer
	Fingerprint LineFingerprint `json:"fingerprint"`
}

var (
	ErrPinContentChanged = errors.New("pinned content changed")
	ErrPinAmbiguous      = errors.New("pinned content now matches more than one line")
)

type PinResolution struct {
	Line  int
	Moved bool
}

func ResolvePin(lines []string, p Pin) (PinResolution, error) {
	if p.Line >= 1 && p.Line <= len(lines) && fingerprintOf(lines[p.Line-1]) == p.Fingerprint {
		return PinResolution{Line: p.Line}, nil
	}
	var matches []int
	for i, l := range lines {
		if fingerprintOf(l) == p.Fingerprint {
			matches = append(matches, i+1)
		}
	}
	switch len(matches) {
	case 0:
		return PinResolution{}, fmt.Errorf("%w: line %d no longer carries fingerprint %s and no other line in the file does either", ErrPinContentChanged, p.Line, p.Fingerprint)
	case 1:
		return PinResolution{Line: matches[0], Moved: matches[0] != p.Line}, nil
	default:
		return PinResolution{}, fmt.Errorf("%w: fingerprint %s now names lines %v", ErrPinAmbiguous, p.Fingerprint, matches)
	}
}

type Question struct {
	ID          string          `json:"id"`
	Text        string          `json:"text"`
	Tree        string          `json:"tree"`
	Band        string          `json:"band"`
	File        string          `json:"file"`
	Line        int             `json:"line"`
	Fingerprint LineFingerprint `json:"fingerprint,omitempty"`
	Answers     []Pin           `json:"answers,omitempty"`
	ScoreRule   string          `json:"score_rule,omitempty"`
	Identifiers []string        `json:"identifiers,omitempty"`
	Source      string          `json:"source"`
	Note        string          `json:"note"`
}

func (q Question) Pins() []Pin {
	if len(q.Answers) > 0 {
		return q.Answers
	}
	if q.File == "" {
		return nil
	}
	return []Pin{{Answer: Answer{File: q.File, Line: q.Line}, Fingerprint: q.Fingerprint}}
}

type Asked struct {
	ID        string
	Text      string
	Tree      string
	Band      string
	ScoreRule string
	answers   []Answer
}

func (q Question) Asked() Asked {
	pins := q.Pins()
	answers := make([]Answer, len(pins))
	for i, p := range pins {
		answers[i] = p.Answer
	}
	return Asked{ID: q.ID, Text: q.Text, Tree: q.Tree, Band: q.Band, ScoreRule: q.ScoreRule, answers: answers}
}

func (a Asked) AllAnswers() []Answer {
	return a.answers
}

func (a Asked) MatchesFile(path string) bool {
	for _, answer := range a.answers {
		if answer.File == path {
			return true
		}
	}
	return false
}

func (a Asked) AnyAnswerFileIn(content string) bool {
	for _, answer := range a.answers {
		if strings.Contains(content, answer.File) {
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
