package thrift

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"tofu/bench/corpus"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/judge/question"
	"tofu/internal/judge/thrift"
	"tofu/internal/konst"
	"tofu/internal/sift"
	"tofu/internal/transport"
	"tofu/library/questions"
)

const thriftKeepAt = 0.5

type JudgedRow struct {
	Turn         string
	Tool         string
	RawBytes     int64
	FixedBytes   int64
	ThriftBytes  int64
	FixedTokens  int64
	ThriftTokens int64
}

type JudgedSkip struct {
	Turn   string
	Reason string
}

type Judged struct {
	CallCap   int
	Rows      []JudgedRow
	Skips     []JudgedSkip
	CallsMade int
	CostUSD   float64
}

func NewJevClient(envPath string) (*jev.Client, error) {
	key, err := jev.Key(envPath)
	if err != nil {
		return nil, err
	}
	wire, err := openrouter.New(openrouter.Config{
		Key: key,
		Transport: transport.Config{
			AttemptTimeout: time.Duration(konst.JudgeTimeoutMillis) * time.Millisecond,
			Retries:        konst.JudgeRetries,
			Backoff:        time.Duration(konst.JudgeBackoffMillis) * time.Millisecond,
			Concurrency:    konst.SiftConcurrency,
		},
	})
	if err != nil {
		return nil, err
	}
	return jev.NewClient(jev.Config{Wire: wire})
}

func ThriftQuestions() ([]jev.Question, error) {
	layers, err := question.DefaultLayers(questions.Files())
	if err != nil {
		return nil, err
	}
	set, _, err := question.Resolve("thrift@1", layers)
	if err != nil {
		return nil, err
	}
	out := make([]jev.Question, len(set.Questions))
	for i, q := range set.Questions {
		out[i] = q.ToJev()
	}
	return out, nil
}

func RunJudged(ctx context.Context, client *jev.Client, jevQuestions []jev.Question, artifactsDir string, turns []corpus.Turn, callCap int) Judged {
	judged := Judged{CallCap: callCap}
	for _, session := range turns {
		for _, call := range sessionCallsOf(session) {
			if call.Tool != "read" && call.Tool != "search" {
				continue
			}
			if call.Error != "" || call.ResultHandle == "" {
				continue
			}
			if judged.CallsMade >= callCap {
				judged.Skips = append(judged.Skips, JudgedSkip{Turn: session.ID, Reason: fmt.Sprintf("the %d live call cap was already spent", callCap)})
				continue
			}
			body, err := os.ReadFile(filepath.Join(artifactsDir, call.ResultHandle+".bin"))
			if err != nil {
				judged.Skips = append(judged.Skips, JudgedSkip{Turn: session.ID, Reason: fmt.Sprintf("%s result_handle %s: %v", call.Tool, call.ResultHandle, err)})
				continue
			}
			row, skip, ok := judgeOne(ctx, client, jevQuestions, &judged, session.ID, session.Task, call.Tool, call.Command, string(body), callCap)
			if !ok {
				judged.Skips = append(judged.Skips, skip)
				continue
			}
			judged.Rows = append(judged.Rows, row)
		}
	}
	return judged
}

func judgeOne(ctx context.Context, client *jev.Client, jevQuestions []jev.Question, judged *Judged, turnID, task, tool, command, content string, callCap int) (JudgedRow, JudgedSkip, bool) {
	parts, err := sift.Split(content)
	if err != nil || len(parts) < 2 {
		return JudgedRow{}, JudgedSkip{Turn: turnID, Reason: "not split into more than one paragraph: " + tool}, false
	}
	if remaining := callCap - judged.CallsMade; len(parts) > remaining {
		return JudgedRow{}, JudgedSkip{Turn: turnID, Reason: fmt.Sprintf("%d paragraphs would exceed the %d live call cap, %d left", len(parts), callCap, remaining)}, false
	}
	marks := make([]sift.Mark, len(parts))
	for i, part := range parts {
		state := thrift.BuildState(part.Text, i, len(parts), tool, command, task)
		raw, err := json.Marshal(state)
		if err != nil {
			return JudgedRow{}, JudgedSkip{Turn: turnID, Reason: "state does not marshal: " + err.Error()}, false
		}
		decision, err := client.Ask(ctx, jev.Request{State: raw, Questions: jevQuestions})
		judged.CallsMade++
		if err != nil {
			return JudgedRow{}, JudgedSkip{Turn: turnID, Reason: "jev.Ask: " + err.Error()}, false
		}
		judged.CostUSD += decision.Usage.Cost
		answer, answered := decision.Answers[thrift.NeededQuestion]
		mark, decErr := thrift.Decide(i, answer.Noul, answered, thriftKeepAt)
		if decErr != nil {
			mark = thrift.Mark{Keep: true, Reason: "kept, unanswered"}
		}
		marks[i] = sift.Mark{Keep: mark.Keep, Reason: mark.Reason}
	}
	judgedText := renderJudgedCut(parts, marks)
	if len(judgedText) >= len(content) {
		judgedText = content
	}
	fixed := content
	if len(content) > konst.TurnResultBytesCap {
		fixed = truncateFixed(content, konst.TurnResultBytesCap)
	}
	return JudgedRow{
		Turn:         turnID,
		Tool:         tool,
		RawBytes:     int64(len(content)),
		FixedBytes:   int64(len(fixed)),
		ThriftBytes:  int64(len(judgedText)),
		FixedTokens:  int64(len(fixed) / konst.SearchBytesPerToken),
		ThriftTokens: int64(len(judgedText) / konst.SearchBytesPerToken),
	}, JudgedSkip{}, true
}

func renderJudgedCut(parts []sift.Part, marks []sift.Mark) string {
	var out []byte
	kept := 0
	for i, part := range parts {
		if marks[i].Keep {
			kept++
			out = append(out, part.Text...)
			out = append(out, part.Sep...)
			continue
		}
		out = fmt.Appendf(out, "[thrift: %d bytes elided, %s]\n", len(part.Text), marks[i].Reason)
	}
	return fmt.Sprintf("%s[thrift: kept %d of %d units]\n", out, kept, len(parts))
}

func truncateFixed(content string, bytesCap int) string {
	dropped := len(content) - bytesCap
	head := bytesCap / 2
	marker := fmt.Sprintf("\n...(%d bytes dropped from the middle of this result)...\n", dropped)
	return content[:head] + marker + content[len(content)-(bytesCap-head):]
}
