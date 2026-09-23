package candidates

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"tofu/bench/shortlist"
	"tofu/bench/tools"
	toolscorpus "tofu/bench/tools/corpus"
	"tofu/internal/judge/jev"
	tofutools "tofu/internal/turn/tools"
)

const repoFileBytesCap = 200_000

func LoadRepoFiles(root string) ([]shortlist.File, error) {
	paths, err := tofutools.ListPaths(root)
	if err != nil {
		return nil, err
	}
	files := make([]shortlist.File, 0, len(paths))
	for _, rel := range paths {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil || info.Size() > repoFileBytesCap {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		files = append(files, shortlist.File{Path: rel, Content: string(body)})
	}
	return files, nil
}

func fileBytes(files []shortlist.File, path string) int {
	for _, f := range files {
		if f.Path == path {
			return len(f.Content)
		}
	}
	return 0
}

func Bm25Alone(files []shortlist.File, q toolscorpus.Asked) tools.Outcome {
	started := time.Now()
	ranked := shortlist.BM25Rank(q.Text, files)
	elapsed := time.Since(started)
	hit := len(ranked) > 0 && q.MatchesFile(ranked[0].Path)
	bytes := 0
	if len(ranked) > 0 {
		bytes = fileBytes(files, ranked[0].Path)
	}
	return tools.Outcome{Version: "bm25 alone", Hit: hit, Bytes: bytes, Calls: 0, Elapsed: elapsed}
}

func Bm25ThenJevReranks(ctx context.Context, client *jev.Client, files []shortlist.File, q toolscorpus.Asked) tools.Outcome {
	started := time.Now()
	ranked := shortlist.BM25Rank(q.Text, files)
	short := shortlist.Shortlist(files, ranked, shortlist.ShortlistSize)
	answer, err := shortlist.AskJudged(ctx, client, q.Text, short)
	elapsed := time.Since(started)
	if err != nil {
		return tools.Outcome{Version: "bm25 then jev reranks", Skipped: err.Error(), Calls: 1, Elapsed: elapsed}
	}
	hit := len(answer.Ranked) > 0 && q.MatchesFile(answer.Ranked[0].Path)
	bytes := 0
	if len(answer.Ranked) > 0 {
		bytes = fileBytes(files, answer.Ranked[0].Path)
	}
	return tools.Outcome{
		Version: "bm25 then jev reranks",
		Hit:     hit,
		Bytes:   bytes,
		Calls:   2,
		Cost:    answer.Cost,
		Elapsed: elapsed,
	}
}
