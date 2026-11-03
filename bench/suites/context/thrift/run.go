package thrift

import (
	"sort"

	"tofu/bench/corpus"
	"tofu/internal/konst"
)

const rtkMarginTokensPerCall = 996

type ToolRow struct {
	Tool           string
	Calls          int
	SessionsUsing  int
	RawTokens      int64
	RenderedTokens int64
	PerSession     Spread
}

type Rtk struct {
	MarginTokensPerCall int64
	BashCallsPerSession Spread
	ClaimedPerSession   Spread
	CappedPerSession    Spread
}

type Result struct {
	SessionsDir              string
	ArtifactsDir             string
	Entries                  int
	Sessions                 int
	Skips                    []corpus.SkippedTurn
	ResultBytesCap           int
	BytesPerToken            int
	CallsPerSession          Spread
	CallShape                []Bucket
	RawTokensPerSession      Spread
	RenderedTokensPerSession Spread
	Tools                    []ToolRow
	Rtk                      Rtk
	Prose                    Prose
}

func Run(sessionsDir, artifactsDir string) (Result, error) {
	walked, err := corpus.WalkSessions(sessionsDir)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		SessionsDir:    sessionsDir,
		ArtifactsDir:   artifactsDir,
		Entries:        walked.EntryCount,
		Sessions:       len(walked.Turns),
		Skips:          walked.Skipped,
		ResultBytesCap: konst.TurnResultBytesCap,
		BytesPerToken:  konst.SearchBytesPerToken,
	}
	var calls, rawTokens, renderedTokens, bashCalls, claimed, capped []int64
	perTool := map[string]*ToolRow{}
	perToolSession := map[string][]int64{}
	names := []string{}
	for _, session := range walked.Turns {
		var sessionRaw, sessionRendered, sessionBash, sessionClaimed, sessionCapped int64
		sessionTool := map[string]int64{}
		sessionCalls := sessionCallsOf(session)
		for _, call := range sessionCalls {
			row, ok := perTool[call.Tool]
			if !ok {
				row = &ToolRow{Tool: call.Tool}
				perTool[call.Tool] = row
				names = append(names, call.Tool)
			}
			rendered := call.RenderedBytes / int64(konst.SearchBytesPerToken)
			raw := call.ResultBytes / int64(konst.SearchBytesPerToken)
			row.Calls++
			row.RawTokens += raw
			row.RenderedTokens += rendered
			sessionTool[call.Tool] += rendered
			sessionRaw += raw
			sessionRendered += rendered
			if call.Tool != "bash" {
				continue
			}
			sessionBash++
			sessionClaimed += rtkMarginTokensPerCall
			sessionCapped += min(int64(rtkMarginTokensPerCall), rendered)
		}
		calls = append(calls, int64(len(sessionCalls)))
		rawTokens = append(rawTokens, sessionRaw)
		renderedTokens = append(renderedTokens, sessionRendered)
		bashCalls = append(bashCalls, sessionBash)
		claimed = append(claimed, sessionClaimed)
		capped = append(capped, sessionCapped)
		for tool, tokens := range sessionTool {
			perTool[tool].SessionsUsing++
			perToolSession[tool] = append(perToolSession[tool], tokens)
		}
	}
	result.CallsPerSession = Measure(calls)
	result.CallShape = Histogram(calls)
	result.RawTokensPerSession = Measure(rawTokens)
	result.RenderedTokensPerSession = Measure(renderedTokens)
	result.Rtk = Rtk{
		MarginTokensPerCall: rtkMarginTokensPerCall,
		BashCallsPerSession: Measure(bashCalls),
		ClaimedPerSession:   Measure(claimed),
		CappedPerSession:    Measure(capped),
	}
	sort.Slice(names, func(i, j int) bool {
		return perTool[names[i]].RenderedTokens > perTool[names[j]].RenderedTokens
	})
	for _, name := range names {
		row := *perTool[name]
		row.PerSession = Measure(perToolSession[name])
		result.Tools = append(result.Tools, row)
	}
	result.Prose = MeasureProse(artifactsDir, walked.Turns)
	return result, nil
}

func sessionCallsOf(session corpus.Turn) []corpus.RecordedCall {
	var calls []corpus.RecordedCall
	for _, step := range session.Steps {
		calls = append(calls, step.ToolCalls...)
	}
	return calls
}
