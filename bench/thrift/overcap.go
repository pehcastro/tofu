package thrift

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"tofu/bench/corpus"
	"tofu/internal/judge/jev"
	"tofu/internal/konst"
	"tofu/internal/sift"
)

type OverCapTarget struct {
	Handle     string
	Tool       string
	Turn       string
	Task       string
	Command    string
	Content    string
	Paragraphs int
}

type OverCapRow struct {
	Turn                      string
	Tool                      string
	Command                   string
	RawBytes                  int64
	Paragraphs                int
	FixedBytes                int64
	FixedTokens               int64
	FixedDroppedBytes         int64
	ThriftBytes               int64
	ThriftTokens              int64
	ThriftDroppedBytes        int64
	OverlapBytes              int64
	KeptInFixedDropParagraphs int
	KeptInFixedDropBytes      int64
}

type OverCapJudged struct {
	CallCap   int
	Rows      []OverCapRow
	Skips     []JudgedSkip
	CallsMade int
	CostUSD   float64
}

func OverCapReadAndSearchTargets(artifactsDir string, turns []corpus.Turn) ([]OverCapTarget, []JudgedSkip) {
	seen := map[string]OverCapTarget{}
	for _, session := range turns {
		for _, call := range sessionCallsOf(session) {
			if call.Tool != "read" && call.Tool != "search" {
				continue
			}
			if call.Error != "" || call.ResultHandle == "" {
				continue
			}
			if _, ok := seen[call.ResultHandle]; ok {
				continue
			}
			body, err := os.ReadFile(filepath.Join(artifactsDir, call.ResultHandle+".bin"))
			if err != nil {
				continue
			}
			if len(body) <= konst.TurnResultBytesCap {
				continue
			}
			seen[call.ResultHandle] = OverCapTarget{
				Handle:  call.ResultHandle,
				Tool:    call.Tool,
				Turn:    session.ID,
				Task:    session.Task,
				Command: call.Command,
				Content: string(body),
			}
		}
	}
	var skips []JudgedSkip
	targets := make([]OverCapTarget, 0, len(seen))
	for _, target := range seen {
		parts, err := sift.Split(target.Content)
		if err != nil || len(parts) < 2 {
			skips = append(skips, JudgedSkip{Turn: target.Turn, Reason: fmt.Sprintf("%s %s: not split into more than one paragraph", target.Tool, target.Handle)})
			continue
		}
		target.Paragraphs = len(parts)
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Paragraphs != targets[j].Paragraphs {
			return targets[i].Paragraphs < targets[j].Paragraphs
		}
		return targets[i].Handle < targets[j].Handle
	})
	return targets, skips
}

func RunJudgedOverCap(ctx context.Context, client *jev.Client, jevQuestions []jev.Question, targets []OverCapTarget, callCap int) OverCapJudged {
	judged := OverCapJudged{CallCap: callCap}
	for _, target := range targets {
		if judged.CallsMade >= callCap {
			judged.Skips = append(judged.Skips, JudgedSkip{Turn: target.Turn, Reason: fmt.Sprintf("%s %s: the %d live call cap was already spent", target.Tool, target.Handle, callCap)})
			continue
		}
		row, skip, ok := judgeOverCapOne(ctx, client, jevQuestions, &judged, target, callCap)
		if !ok {
			judged.Skips = append(judged.Skips, skip)
			continue
		}
		judged.Rows = append(judged.Rows, row)
	}
	return judged
}

func judgeOverCapOne(ctx context.Context, client *jev.Client, jevQuestions []jev.Question, judged *OverCapJudged, target OverCapTarget, callCap int) (OverCapRow, JudgedSkip, bool) {
	parts, err := sift.Split(target.Content)
	if err != nil {
		return OverCapRow{}, JudgedSkip{Turn: target.Turn, Reason: fmt.Sprintf("%s %s: %v", target.Tool, target.Handle, err)}, false
	}
	if remaining := callCap - judged.CallsMade; len(parts) > remaining {
		return OverCapRow{}, JudgedSkip{Turn: target.Turn, Reason: fmt.Sprintf("%s %s: %d paragraphs would exceed the %d live call cap, %d left", target.Tool, target.Handle, len(parts), callCap, remaining)}, false
	}
	offsets := make([]int, len(parts))
	offset := 0
	for i, part := range parts {
		offsets[i] = offset
		offset += len(part.Text) + len(part.Sep)
	}
	marks, err := judgeParagraphs(ctx, client, jevQuestions, &judged.CallsMade, &judged.CostUSD, parts, target.Tool, target.Command, target.Task)
	if err != nil {
		return OverCapRow{}, JudgedSkip{Turn: target.Turn, Reason: err.Error()}, false
	}
	judgedText := renderJudgedCut(parts, marks)
	if len(judgedText) >= len(target.Content) {
		judgedText = target.Content
	}
	fixed := target.Content
	fixedLo, fixedHi := 0, 0
	if len(target.Content) > konst.TurnResultBytesCap {
		fixed = truncateFixed(target.Content, konst.TurnResultBytesCap)
		head := konst.TurnResultBytesCap / 2
		fixedLo = head
		fixedHi = len(target.Content) - (konst.TurnResultBytesCap - head)
	}
	var thriftDropped, overlap, keptInFixedDropBytes int64
	keptInFixedDropParas := 0
	for i, part := range parts {
		lo := offsets[i]
		hi := lo + len(part.Text) + len(part.Sep)
		if marks[i].Keep {
			if o := overlapLen(lo, hi, fixedLo, fixedHi); o > 0 {
				keptInFixedDropParas++
				keptInFixedDropBytes += int64(o)
			}
			continue
		}
		thriftDropped += int64(hi - lo)
		overlap += int64(overlapLen(lo, hi, fixedLo, fixedHi))
	}
	return OverCapRow{
		Turn:                      target.Turn,
		Tool:                      target.Tool,
		Command:                   target.Command,
		RawBytes:                  int64(len(target.Content)),
		Paragraphs:                len(parts),
		FixedBytes:                int64(len(fixed)),
		FixedTokens:               int64(len(fixed) / konst.SearchBytesPerToken),
		FixedDroppedBytes:         int64(fixedHi - fixedLo),
		ThriftBytes:               int64(len(judgedText)),
		ThriftTokens:              int64(len(judgedText) / konst.SearchBytesPerToken),
		ThriftDroppedBytes:        thriftDropped,
		OverlapBytes:              overlap,
		KeptInFixedDropParagraphs: keptInFixedDropParas,
		KeptInFixedDropBytes:      keptInFixedDropBytes,
	}, JudgedSkip{}, true
}

func overlapLen(lo, hi, lo2, hi2 int) int {
	start := max(lo, lo2)
	end := min(hi, hi2)
	if end <= start {
		return 0
	}
	return end - start
}
