package sift

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"tofu/internal/judge/jev"
	"tofu/internal/konst"
)

const batchModel = "~typesafe/jev-latest"

func batchCaps() jev.WireCaps {
	return jev.WireCaps{Name: "test", MaxRequestTokens: konst.JudgeRequestTokenCeiling, MaxStateTokens: konst.JudgeStateTokenCeiling}
}

func batchRubric() jev.Question {
	return jev.Question{ID: NeededQuestion, Kind: jev.QuestionNoul, Instructions: "keep what the task needs", True: "the task needs it", False: "filler"}
}

func linesOf(chunks, lines, width int) string {
	var out strings.Builder
	for chunk := range chunks {
		for line := range lines {
			fmt.Fprintf(&out, "chunk %d line %d %s\n", chunk, line, strings.Repeat("x", width))
		}
		out.WriteString("\n")
	}
	return out.String()
}

func TestOneShellResultIsOneRequestWithTheRubricInState(t *testing.T) {
	result := Shell{Command: "go test ./...", Stdout: linesOf(12, 8, 10), Stderr: "a warning\n", ExitCode: 1}
	units := SplitShell(result)
	batch, err := BuildShellBatch(batchRubric(), batchCaps(), batchModel, result, units, "find what failed")
	if err != nil {
		t.Fatalf("BuildShellBatch: %v", err)
	}
	var candidates []int
	for _, unit := range units {
		if unit.Held == NotHeld {
			candidates = append(candidates, unit.Index)
		}
	}
	if len(candidates) < 2 || fmt.Sprint(batch.Indexes) != fmt.Sprint(candidates) || len(batch.Request.Questions) != len(candidates) {
		t.Fatalf("%d units with candidates %v became indexes %v and %d questions", len(units), candidates, batch.Indexes, len(batch.Request.Questions))
	}
	state, ok := batch.Request.State.(ShellBatchState)
	if !ok || len(state.Chunks) != len(units) || state.ExitCode != 1 || state.Task != "find what failed" {
		t.Fatalf("the state is %#v", batch.Request.State)
	}
	rubric := batchRubric()
	if state.Rubric != (ShellRubric{Instructions: rubric.Instructions, True: rubric.True, False: rubric.False}) {
		t.Fatalf("the rubric in state is %+v", state.Rubric)
	}
	answers := map[string]jev.Answer{}
	for i, question := range batch.Request.Questions {
		chunk := fmt.Sprintf("chunks[%d]", batch.Indexes[i])
		if !strings.HasSuffix(question.ID, fmt.Sprintf("_%d", batch.Indexes[i])) || !strings.Contains(question.Instructions, chunk) || !strings.Contains(question.True, chunk) || !strings.Contains(question.False, chunk) {
			t.Fatalf("question %d asks about %+v and should name %s", i, question, chunk)
		}
		if i > 0 {
			answers[question.ID] = jev.Answer{Noul: float64(batch.Indexes[i]) / 100}
		}
	}
	scores := batch.Scores(jev.Decision{Answers: answers})
	if _, scored := scores[batch.Indexes[0]]; scored || len(scores) != len(candidates)-1 {
		t.Fatalf("an unanswered chunk was scored, or an answered one dropped: %v", scores)
	}
	for _, index := range batch.Indexes[1:] {
		if scores[index] != float64(index)/100 {
			t.Fatalf("chunk %d scored %.2f, the answer about it was %.2f", index, scores[index], float64(index)/100)
		}
	}
}

func TestAShellResultOverTheCeilingIsRefusedSoTheCallerAsksPerChunk(t *testing.T) {
	result := Shell{Command: "cat big.log", Stdout: linesOf(40, 20, 200)}
	_, err := BuildShellBatch(batchRubric(), batchCaps(), batchModel, result, SplitShell(result), "find what failed")
	var over ShellBatchOverCeilingError
	if !errors.As(err, &over) {
		t.Fatalf("a %d byte result built a batch, err %v", len(result.Stdout), err)
	}
	t.Log(err)
}

func TestAShellResultUnderTheTokenCeilingsAndOverTheWireByteCapIsRefused(t *testing.T) {
	result := Shell{Command: "cat small.log", Stdout: linesOf(6, 8, 60)}
	caps := batchCaps()
	caps.MaxRequestBytes = len(result.Stdout) / 2
	_, err := BuildShellBatch(batchRubric(), caps, batchModel, result, SplitShell(result), "find what failed")
	var over ShellBatchOverCeilingError
	if !errors.As(err, &over) {
		t.Fatalf("a %d byte result built a batch against a %d byte cap, err %v", len(result.Stdout), caps.MaxRequestBytes, err)
	}
	t.Log(err)
}

func TestAShellResultWithEveryChunkHeldAsksNothing(t *testing.T) {
	result := Shell{Command: "ls", Stdout: "a\n\nb\n"}
	batch, err := BuildShellBatch(batchRubric(), batchCaps(), batchModel, result, SplitShell(result), "list")
	if err != nil || len(batch.Indexes) != 0 || len(batch.Request.Questions) != 0 {
		t.Fatalf("a result with every chunk held built %d questions, err %v", len(batch.Request.Questions), err)
	}
}
