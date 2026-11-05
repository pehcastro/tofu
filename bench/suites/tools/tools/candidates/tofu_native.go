package candidates

import (
	"context"
	"encoding/json"
	"time"

	"tofu/bench/suites/tools/tools"
	toolscorpus "tofu/bench/suites/tools/tools/corpus"
	searchtool "tofu/internal/tools/code/search"
	"tofu/internal/tools/contract"
)

const nativeToolTimeout = 60 * time.Second

type nativeResult struct {
	result contract.Result
	err    error
}

func TofuSearch(root string, q toolscorpus.Asked) tools.Outcome {
	search, err := searchtool.NewSearch(root)
	if err != nil {
		return tools.Outcome{Version: "tofu search", Skipped: err.Error()}
	}
	args, err := json.Marshal(map[string]string{"pattern": tools.Alternation(tools.ContentWords(q.Text))})
	if err != nil {
		return tools.Outcome{Version: "tofu search", Skipped: err.Error()}
	}

	done := make(chan nativeResult, 1)
	started := time.Now()
	go func() {
		result, err := search.Run(context.Background(), args)
		done <- nativeResult{result: result, err: err}
	}()
	select {
	case ran := <-done:
		if ran.err != nil {
			return tools.Outcome{Version: "tofu search", Skipped: ran.err.Error(), Calls: 1, Elapsed: time.Since(started)}
		}
		return tools.Outcome{
			Version: "tofu search",
			Hit:     q.AnyAnswerFileIn(ran.result.Content),
			Bytes:   len(ran.result.Content),
			Calls:   1,
			Elapsed: time.Since(started),
		}
	case <-time.After(nativeToolTimeout):
		return tools.Outcome{Version: "tofu search", Skipped: "timed out after " + nativeToolTimeout.String(), Calls: 1, Elapsed: time.Since(started)}
	}
}
