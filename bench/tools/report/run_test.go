package report

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"tofu/bench/shortlist"
	"tofu/bench/tools"
	"tofu/bench/tools/candidates"
	toolscorpus "tofu/bench/tools/corpus"
	"tofu/internal/judge/jev"
)

const (
	tofuRoot            = "../../.."
	wantCorpusQuestions = 100
)

func liveClient(t *testing.T) *jev.Client {
	t.Helper()
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to put the pattern writing versions to the real jev route")
	}
	jev.AllowLiveCredential(t)
	key, err := jev.Key(tofuRoot + "/.env")
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	wire, err := shortlist.NewWire(key)
	if err != nil {
		t.Fatalf("NewWire: %v", err)
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func loadQuestions(t *testing.T) []toolscorpus.Question {
	t.Helper()
	qs, err := toolscorpus.ReadQuestions("../testdata/questions.jsonl")
	if err != nil {
		t.Fatalf("ReadQuestions: %v", err)
	}
	if len(qs) != wantCorpusQuestions {
		t.Fatalf("the corpus carries %d questions, want %d", len(qs), wantCorpusQuestions)
	}
	return qs
}

func questionsByTree(qs []toolscorpus.Question) map[string][]toolscorpus.Question {
	byTree := map[string][]toolscorpus.Question{}
	for _, q := range qs {
		byTree[q.Tree] = append(byTree[q.Tree], q)
	}
	return byTree
}

type tally struct {
	version string
	hits    int
	total   int
	bytes   int64
	cost    float64
	calls   int
	skips   int
	elapsed []time.Duration
}

func (a *tally) add(o tools.Outcome) {
	a.total++
	if o.Skipped != "" {
		a.skips++
		return
	}
	if o.Hit {
		a.hits++
	}
	a.bytes += int64(o.Bytes)
	a.cost += o.Cost
	a.calls += o.Calls
	a.elapsed = append(a.elapsed, o.Elapsed)
}

func (a *tally) median() time.Duration {
	if len(a.elapsed) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), a.elapsed...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}

func (a *tally) worst() time.Duration {
	worst := time.Duration(0)
	for _, e := range a.elapsed {
		if e > worst {
			worst = e
		}
	}
	return worst
}

func (a *tally) log(t *testing.T, label string) {
	avgBytes := int64(0)
	if a.total-a.skips > 0 {
		avgBytes = a.bytes / int64(a.total-a.skips)
	}
	t.Logf("%-28s %-11s %2d/%-2d correct  median %10s  worst %10s  bytes %8d  cost $%.6f  calls %d  skips %d",
		a.version, label, a.hits, a.total, a.median().Round(time.Millisecond), a.worst().Round(time.Millisecond),
		avgBytes, a.cost, a.calls, a.skips)
}

func missReason(o tools.Outcome) string {
	switch {
	case o.Skipped != "":
		return "errored: " + o.Skipped
	case o.Bytes == 0:
		return "matched nothing"
	default:
		return "matched, but not the right line"
	}
}

type scoreboard struct {
	version string
	overall *tally
	byTree  map[string]*tally
	byBand  map[string]*tally
	misses  map[string]int
}

func newScoreboard(version string) *scoreboard {
	return &scoreboard{
		version: version,
		overall: &tally{version: version},
		byTree:  map[string]*tally{},
		byBand:  map[string]*tally{},
		misses:  map[string]int{},
	}
}

func (s *scoreboard) add(q toolscorpus.Question, o tools.Outcome) {
	s.overall.add(o)
	if s.byTree[q.Tree] == nil {
		s.byTree[q.Tree] = &tally{version: s.version}
	}
	s.byTree[q.Tree].add(o)
	if s.byBand[q.Band] == nil {
		s.byBand[q.Band] = &tally{version: s.version}
	}
	s.byBand[q.Band].add(o)
	if o.Skipped == "" && !o.Hit {
		s.misses[missReason(o)]++
	}
}

func (s *scoreboard) log(t *testing.T) {
	s.overall.log(t, "overall")
	for _, band := range []string{toolscorpus.BandNamed, toolscorpus.BandDescribed, toolscorpus.BandIntent} {
		if a := s.byBand[band]; a != nil {
			a.log(t, band)
		}
	}
	trees := make([]string, 0, len(s.byTree))
	for tree := range s.byTree {
		trees = append(trees, tree)
	}
	sort.Strings(trees)
	for _, tree := range trees {
		s.byTree[tree].log(t, tree)
	}
	for reason, count := range s.misses {
		t.Logf("%-28s miss reason: %-40s %d", s.version, reason, count)
	}
}

func runFreeToolArm(questions []toolscorpus.Question, board *scoreboard, run func(root string, q toolscorpus.Question) tools.Outcome) {
	for _, q := range questions {
		root := toolscorpus.TreeRoot(tofuRoot, q.Tree)
		board.add(q, run(root, q))
	}
}

const largeTreeSampleSize = 3

const historicalTofuGrepRow = "tofu grep is history, not an arm: the tool was removed on 2026-09-21 by TOFU-284. " +
	"its last measurement over .local/sources/jev-r2 was 48.5 s, 133 MB returned and 14 of 30 right, " +
	"against tofu search at 1.36 s, 17 KB and 13 of 30 on the same tree, " +
	"and a line cap on grep scored 0 of 30 and then 10 of 30, so it could not be bounded without losing half its correct answers. " +
	"every row in bench/tools naming tofu grep before that date stands as recorded and is not rerunnable."

func toolTableSample(qs []toolscorpus.Question, full bool) []toolscorpus.Question {
	if full {
		return qs
	}
	byTree := questionsByTree(qs)
	var sample []toolscorpus.Question
	for tree, pool := range byTree {
		if tree == toolscorpus.TreeTofu || len(pool) <= largeTreeSampleSize {
			sample = append(sample, pool...)
			continue
		}
		step := len(pool) / largeTreeSampleSize
		if step == 0 {
			step = 1
		}
		taken := 0
		for i := 0; i < len(pool) && taken < largeTreeSampleSize; i += step {
			sample = append(sample, pool[i])
			taken++
		}
	}
	return sample
}

func TestToolAgainstTool(t *testing.T) {
	t.Log("this table compares tools: one call in, raw matches out. It is never the same unit as a turn, which searches, reads and decides.")
	full := os.Getenv("TOFU_TOOLS_LARGE_TREES") == "full"
	questions := toolTableSample(loadQuestions(t), full)
	if !full {
		t.Logf("questions over the four non tofu trees are sampled to %d each: this machine's disk under .local/sources answers a single git grep --no-index over jev-r2 in tens of seconds, so 20 questions times several arms times five trees does not fit this run. Set TOFU_TOOLS_LARGE_TREES=full for the exhaustive run.", largeTreeSampleSize)
	}
	byTree := questionsByTree(questions)

	raw := newScoreboard("ripgrep raw")
	quoted := newScoreboard("ripgrep quoted")
	tSearch := newScoreboard("tofu search")

	runFreeToolArm(questions, raw, candidates.RipgrepRaw)
	runFreeToolArm(questions, quoted, candidates.RipgrepQuoted)
	runFreeToolArm(questions, tSearch, candidates.TofuSearch)

	for _, b := range []*scoreboard{raw, quoted, tSearch} {
		b.log(t)
	}
	t.Log(historicalTofuGrepRow)

	tofuQuestions := byTree[toolscorpus.TreeTofu]
	files, err := candidates.LoadRepoFiles(tofuRoot)
	if err != nil {
		t.Fatalf("LoadRepoFiles: %v", err)
	}
	t.Logf("bm25 alone ran only over the tofu tree, %d questions: loading and scoring full file content for the other four trees is outside this run's time budget", len(tofuQuestions))
	bm25 := newScoreboard("bm25 alone")
	for _, q := range tofuQuestions {
		bm25.add(q, candidates.Bm25Alone(files, q))
	}
	bm25.log(t)

	if os.Getenv("TOFU_LIVE") != "1" {
		t.Log("jevgrep, jevgrep triple, jevgrep narrowed, jevgrep answer and bm25 then jev reranks did not run: set TOFU_LIVE=1")
		return
	}
	client := liveClient(t)
	ctx := context.Background()

	jg := newScoreboard("jevgrep")
	jt := newScoreboard("jevgrep triple")
	jn := newScoreboard("jevgrep narrowed")
	ja := newScoreboard("jevgrep answer")
	for _, q := range questions {
		root := toolscorpus.TreeRoot(tofuRoot, q.Tree)
		jg.add(q, candidates.Jevgrep(ctx, client, root, q))
		jt.add(q, candidates.JevgrepTriple(ctx, client, root, q))
		jn.add(q, candidates.JevgrepNarrowed(ctx, client, root, q))
		ja.add(q, candidates.JevgrepAnswer(ctx, client, root, q))
	}
	for _, b := range []*scoreboard{jg, jt, jn, ja} {
		b.log(t)
	}

	jr := newScoreboard("bm25 then jev reranks")
	for _, q := range tofuQuestions {
		jr.add(q, candidates.Bm25ThenJevReranks(ctx, client, files, q))
	}
	jr.log(t)
}
