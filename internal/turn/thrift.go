package turn

import (
	"context"
	"fmt"
	"strings"

	"tofu/internal/judge/thrift"
	"tofu/internal/sift"
	"tofu/library/questions"
)

const thriftRuleRef = "config/thrift_rule@1"

var thriftTools = map[string]bool{"read": true, "glob": true, "search": true}

type ThriftScores interface {
	Score(ctx context.Context, tool, command, task string, parts []sift.Part) (map[int]float64, error)
}

type ThriftSift struct {
	Rule   thrift.Rule
	Scores ThriftScores
}

func NewThriftSift(scores ThriftScores) (ThriftSift, error) {
	rule, err := thrift.LoadRule(questions.Files(), thriftRuleRef)
	if err != nil {
		return ThriftSift{}, err
	}
	return ThriftSift{Rule: rule, Scores: scores}, nil
}

func thriftOrNothing(given *ThriftSift) *ThriftSift {
	if given != nil {
		return given
	}
	built, err := NewThriftSift(nil)
	if err != nil {
		return nil
	}
	return &built
}

type ThriftCut struct {
	Text  string
	Saved int
}

func (g gatedCall) cutThriftResult(ctx context.Context, tool string, result Result) ThriftCut {
	if g.thrift == nil || !thriftTools[tool] {
		return ThriftCut{Text: result.Content}
	}
	return g.thrift.Cut(ctx, tool, result.Command, result.Content, g.task)
}

func (s ThriftSift) Cut(ctx context.Context, tool, command, content, task string) ThriftCut {
	if s.Rule.Mode != thrift.ModeEnforced || s.Scores == nil {
		return ThriftCut{Text: content}
	}
	parts, err := sift.Split(content)
	if err != nil || len(parts) < 2 {
		return ThriftCut{Text: content}
	}
	scores, err := s.Scores.Score(ctx, tool, command, task, parts)
	if err != nil {
		return ThriftCut{Text: content}
	}
	marks := make([]sift.Mark, len(parts))
	elided := 0
	for i, part := range parts {
		score, answered := scores[i]
		mark, decErr := thrift.Decide(i, score, answered, s.Rule.KeepAt)
		if decErr != nil {
			mark = thrift.Mark{Keep: true, Reason: "kept, unanswered: " + decErr.Error()}
		}
		marks[i] = sift.Mark{Keep: mark.Keep, Reason: mark.Reason}
		if !mark.Keep {
			elided += len(part.Text)
		}
	}
	if elided == 0 {
		return ThriftCut{Text: content}
	}
	cut := renderThriftCut(parts, marks)
	if len(cut) >= len(content) {
		return ThriftCut{Text: content}
	}
	return ThriftCut{Text: cut, Saved: len(content) - len(cut)}
}

func renderThriftCut(parts []sift.Part, marks []sift.Mark) string {
	var out strings.Builder
	kept, keptBytes, total := 0, 0, 0
	for i, part := range parts {
		total += len(part.Text)
		if marks[i].Keep {
			kept++
			keptBytes += len(part.Text)
			out.WriteString(part.Text)
			out.WriteString(part.Sep)
			continue
		}
		fmt.Fprintf(&out, "[thrift: %d bytes elided, %s]\n", len(part.Text), marks[i].Reason)
	}
	fmt.Fprintf(&out, "[thrift: kept %d of %d units, %d of %d bytes]\n", kept, len(parts), keptBytes, total)
	return out.String()
}
