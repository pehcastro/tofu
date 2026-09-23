package sift

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/sift"
)

type armTotal struct {
	name      string
	kept      int
	before    int
	after     int
	calls     int
	state     int
	cost      float64
	elapsed   time.Duration
	latencies []time.Duration
	builds    map[string]int
}

func (a *armTotal) add(reading Reading) {
	if reading.NeedleKept {
		a.kept++
	}
	a.before += reading.Before
	a.after += reading.After
}

func (a *armTotal) addJev(answered Answered) {
	a.calls += len(answered.Scores) + answered.Errors
	a.state += answered.StateBytes
	a.cost += answered.Cost
	a.latencies = append(a.latencies, answered.Latencies...)
	if a.builds == nil {
		a.builds = map[string]int{}
	}
	for build, n := range answered.Builds {
		a.builds[build] += n
	}
}

func (a armTotal) spread() string {
	if len(a.latencies) == 0 {
		return "no wire call"
	}
	sorted := append([]time.Duration(nil), a.latencies...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return fmt.Sprintf("p50 %s, p95 %s, worst %s, builds %v",
		sorted[len(sorted)/2].Round(time.Millisecond),
		sorted[(len(sorted)*95)/100].Round(time.Millisecond),
		sorted[len(sorted)-1].Round(time.Millisecond), a.builds)
}

func (a armTotal) row(rows int) string {
	money, calls, state := "none", "0", "none"
	if a.cost > 0 {
		money = fmt.Sprintf("$%.5f", a.cost)
	}
	if a.calls > 0 {
		calls = strconv.Itoa(a.calls)
		state = strconv.Itoa(a.state)
	}
	return fmt.Sprintf("| %s | %d of %d | %.1f%% | %s | %s | %s | %s |",
		a.name, a.kept, rows, 100*float64(a.before-a.after)/float64(a.before),
		calls, state, money, a.elapsed.Round(time.Millisecond))
}

func TestFourMethodsOverTheSameThirtyFourShellResults(t *testing.T) {
	rtkOnPath(t)
	asker := liveClient(t)
	pol := shippedRule(t)
	rows := corpusRows(t)

	for run := 1; run <= 2; run++ {
		t.Logf("run %d of 2", run)
		fourMethods(t, asker, pol.KeepAt, rows)
	}
}

func fourMethods(t *testing.T, asker Asker, keepAt float64, rows []Row) {
	regex := &armTotal{name: "regex on error lines"}
	judged := &armTotal{name: "jev"}
	proxy := &armTotal{name: "rtk"}
	chained := &armTotal{name: "rtk then jev"}
	var lost []string

	for i, row := range rows {
		planted, err := Plant(row, i)
		if err != nil {
			t.Fatalf("Plant: %v", err)
		}
		run, err := RunRtk(context.Background(), row.Command, planted.Shell.Stdout)
		if err != nil {
			t.Fatalf("row %d: rtk pipe: %v", i, err)
		}
		thinned := planted.Shell
		thinned.Stdout = run.Output

		asked := asker.Ask(context.Background(), planted.Shell, planted.Units, row.Task)
		after := asker.Ask(context.Background(), thinned, sift.SplitShell(thinned), row.Task)

		regex.add(Free(row, planted))
		judged.addJev(asked)
		judged.add(Read(row, planted, asked.Cut(keepAt)))
		proxy.elapsed += run.Elapsed
		proxy.add(Read(row, planted, run.Output))
		chained.elapsed += run.Elapsed
		chained.addJev(after)
		reading := Read(row, planted, after.Cut(keepAt))
		chained.add(reading)
		if !reading.NeedleKept {
			lost = append(lost, fmt.Sprintf("row %d filter %q", i, run.Filter))
		}
	}

	t.Log("| method | needles kept | bytes saved | jev calls | jev input bytes | money | rtk ms |")
	t.Log("|---|---|---|---|---|---|---|")
	for _, arm := range []*armTotal{regex, judged, proxy, chained} {
		t.Log(arm.row(len(rows)))
	}

	t.Logf("jev after rtk sees %d of %d state bytes, %.1f%%, over %d of %d calls, %.1f%%",
		chained.state, judged.state, 100*float64(chained.state)/float64(judged.state),
		chained.calls, judged.calls, 100*float64(chained.calls)/float64(judged.calls))
	t.Logf("money: jev alone $%.5f, rtk then jev $%.5f, %.1f%% of it, $%.5f this run",
		judged.cost, chained.cost, 100*chained.cost/judged.cost, judged.cost+chained.cost)
	t.Logf("rtk then jev lost the needle on %s", strings.Join(lost, ", "))
	t.Logf("jev alone latency: %s", judged.spread())
	t.Logf("jev after rtk latency: %s", chained.spread())
	t.Logf("keep_at %.2f, %d rows, no call discarded as a warm up", keepAt, len(rows))
}
