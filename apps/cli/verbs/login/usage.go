package login

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/cli/render"
	"tofu/internal/host"
	"tofu/internal/judge/ledger"
	"tofu/internal/models/llm/quota"
	"tofu/internal/platform/fs"
	"tofu/internal/platform/project"
	"tofu/internal/store/session"
	"tofu/internal/store/session/drain"
)

const (
	UsageFlags        = "tofu usage [--history] [--json]"
	historyFlag       = "--history"
	usageDay          = 24 * time.Hour
	usageRunOutFormat = "Mon 2 Jan 15:04"
	windowScopeMark   = ":"
)

func UsageVerb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "usage", UsageLine: UsageFlags, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	for _, arg := range args {
		if arg != output.JSONFlag && arg != historyFlag {
			return o.Usage(errors.New("unknown argument " + strconv.Quote(arg)))
		}
	}
	if slices.Contains(args, historyFlag) {
		return usageHistoryVerb(out, errOut, o.AsJSON)
	}
	now := time.Now()
	report, err := readUsage(now)
	if err != nil {
		return o.Fail(err)
	}
	classifier, err := readClassifierSpend(now)
	if err != nil {
		return o.Fail(err)
	}
	answer := usageAnswer{UsageReport: report, Classifier: classifier}
	return o.Done(true, answer, func(page cli.Page) []string {
		lines := append(usagePage(page, report, now), "", page.Section("classifier", cli.Verdict{}))
		return append(lines, cli.Indent(page.Facts([]cli.Fact{
			{Label: "calls", Text: strconv.Itoa(classifier.Calls) + " in the last " + classifier.Window},
			{Label: "spend", Text: fmt.Sprintf("USD %.5f charged, USD %.5f at list price, %d unpriced", classifier.ChargedUSD, classifier.ListUSD, classifier.Unpriced)},
		})...)...)
	})
}

type classifierSpend struct {
	Window     string  `json:"window"`
	Calls      int     `json:"calls"`
	ChargedUSD float64 `json:"charged_usd"`
	ListUSD    float64 `json:"list_usd"`
	Unpriced   int     `json:"unpriced_calls"`
}

type usageAnswer struct {
	host.UsageReport
	Classifier classifierSpend `json:"classifier"`
}

func readClassifierSpend(now time.Time) (classifierSpend, error) {
	dir, err := project.LogDir()
	if err != nil {
		return classifierSpend{}, err
	}
	spend := classifierSpend{Window: strconv.Itoa(int(usageDay.Hours())) + "h"}
	_, err = ledger.NewReader(dir).Each(ledger.Filter{Since: now.Add(-usageDay), Until: now}, func(row ledger.Row) error {
		if row.ReplayOf != "" {
			return nil
		}
		spend.Calls++
		if row.Charge == nil {
			spend.Unpriced++
			return nil
		}
		switch row.Charge.Source {
		case ledger.ChargeProvider:
			spend.ChargedUSD += row.Charge.USD
		case ledger.ChargeList:
			spend.ListUSD += row.Charge.USD
		case ledger.ChargeNone:
			spend.Unpriced++
		default:
			return fmt.Errorf("ledger row %s carries the unknown charge source %q", row.ID, row.Charge.Source)
		}
		return nil
	})
	return spend, err
}

func usageFail(errOut io.Writer, err error) int {
	return output.PrintFailure(errOut, output.ExitVerdict, "tofu usage: "+err.Error(), "")
}

func readUsage(now time.Time) (host.UsageReport, error) {
	results, err := wiring.PollCredentials(context.Background(), time.Now, nil)
	if err != nil {
		return host.UsageReport{}, err
	}
	answer := host.UsageReport{State: host.UsageServing, Providers: wiring.CredentialReports(results, now), SpendLimit: quota.SpendLimitLine()}
	if len(results) == 0 {
		answer.State, answer.Missing = host.UsageNone, wiring.DoctorBlockers()
		return answer, nil
	}
	fullest := -1.0
	for _, provider := range answer.Providers {
		if provider.State != wiring.UsageServingState {
			answer.State = host.UsageAttention
		}
		for _, window := range provider.Windows {
			if window.Reported && window.Used > fullest {
				fullest, answer.Fullest = window.Used, provider.Provider+" "+window.ID
			}
		}
	}
	answer.Spends, answer.Unreadable, err = windowSpends(results, now)
	return answer, err
}

func windowSpends(results []wiring.PollResult, now time.Time) ([]host.WindowSpend, int, error) {
	var spends []host.WindowSpend
	earliest := now
	for _, result := range results {
		for _, window := range result.Report.Windows {
			since := window.ResetsAt.Add(-window.Duration)
			if window.Duration == 0 || window.ResetsAt.IsZero() || !window.Used.Reported || !since.Before(now) {
				continue
			}
			if spends = append(spends, host.WindowSpend{Account: wiring.QuotaAccount(result.Row), Window: window.ID, Since: since, Row: result.Row}); since.Before(earliest) {
				earliest = since
			}
		}
	}
	if len(spends) == 0 {
		return nil, 0, nil
	}
	readings, err := fs.QuotaDir()
	if err != nil {
		return nil, 0, err
	}
	recorded, tornReadings, err := quota.ReadReadings(readings, earliest)
	if err != nil {
		return nil, 0, err
	}
	for _, burn := range quota.Burns(recorded) {
		at := slices.IndexFunc(spends, func(spend host.WindowSpend) bool { return spend.Row == burn.Account && spend.Window == burn.Window })
		if at >= 0 && burn.PerHour != nil {
			perDay := *burn.PerHour * usageDay.Hours()
			spends[at].UsedPerDay, spends[at].RunsOutAt = &perDay, burn.FullAt
		}
	}
	tornEvents, err := tofuSpend(spends, earliest, now)
	return spends, tornReadings + tornEvents, err
}

func tofuSpend(spends []host.WindowSpend, earliest, now time.Time) (int, error) {
	registry, err := wiring.ModelRegistry()
	if err != nil {
		return 0, err
	}
	weights, err := wiring.PricedByProvider(registry)
	if err != nil {
		return 0, err
	}
	home, err := fs.HomeConfigDir()
	if err != nil {
		return 0, err
	}
	states, err := project.ProjectGlob(filepath.Join(home, fs.ProjectsDirName), "*")
	if err != nil {
		return 0, err
	}
	written := make([][]string, len(states))
	inParallel(len(states), func(at int) {
		store := session.OpenAt(states[at])
		ids, _ := os.ReadDir(session.SessionsDir(states[at]))
		for _, id := range ids {
			if info, err := os.Stat(store.EventsPath(id.Name())); err == nil && !info.ModTime().Before(earliest) {
				written[at] = append(written[at], store.EventsPath(id.Name()))
			}
		}
	})
	paths, torn := slices.Concat(written...), 0
	var adding sync.Mutex
	inParallel(len(paths), func(at int) {
		events, unreadable := requestEvents(paths[at], earliest)
		reads := make([]drain.Report, len(spends))
		for i, spend := range spends {
			reads[i] = drain.Read(paths[at], spentIn(events, spend, now), drain.Input{Weights: weights})
		}
		adding.Lock()
		defer adding.Unlock()
		torn += unreadable
		for i, read := range reads {
			spends[i].Requests += read.Total.Calls + read.Memory.Calls
			spends[i].Weighted += read.Total.Weighted + read.Memory.Weighted
		}
	})
	for i := range spends {
		spends[i].PerDay = spends[i].Weighted / (float64(now.Sub(spends[i].Since)) / float64(usageDay))
	}
	return torn, nil
}

func inParallel(count int, do func(at int)) {
	next := make(chan int)
	var working sync.WaitGroup
	for range goruntime.NumCPU() {
		working.Go(func() {
			for at := range next {
				do(at)
			}
		})
	}
	for at := range count {
		next <- at
	}
	close(next)
	working.Wait()
}

type accountEvent struct {
	session.Event
	account int64
	model   string
}

func requestEvents(path string, since time.Time) ([]accountEvent, int) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 1
	}
	var kept []accountEvent
	torn, account := 0, map[string]int64{}
	for line := range bytes.SplitSeq(raw, []byte("\n")) {
		_, kind, _ := bytes.Cut(line, []byte(`"kind":"`))
		kind, _, _ = bytes.Cut(kind, []byte(`"`))
		switch session.EventKind(kind) {
		case session.EventTurnStart, session.EventRequest, session.EventMemoryRequest:
		default:
			continue
		}
		var event accountEvent
		var body struct {
			Model   string `json:"model"`
			Account int64  `json:"account"`
		}
		if json.Unmarshal(line, &event.Event) != nil || json.Unmarshal(event.Body, &body) != nil {
			torn++
			continue
		}
		if event.Kind == session.EventTurnStart {
			account[event.Agent] = body.Account
		}
		if event.account, event.model = account[event.Agent], body.Model; event.Kind == session.EventTurnStart || !event.At.Before(since) {
			kept = append(kept, event)
		}
	}
	return kept, torn
}

func spentIn(events []accountEvent, spend host.WindowSpend, now time.Time) []session.Event {
	_, scope, scoped := strings.Cut(spend.Window, windowScopeMark)
	var kept []session.Event
	for _, event := range events {
		if event.Kind == session.EventTurnStart || event.account == spend.Row && !event.At.Before(spend.Since) && !event.At.After(now) &&
			(!scoped || strings.Contains(strings.ToLower(event.model), strings.ToLower(scope))) {
			kept = append(kept, event.Event)
		}
	}
	return kept
}

func windowPace(page cli.Page, spends []host.WindowSpend, account, window string) (cli.Fact, bool) {
	at := slices.IndexFunc(spends, func(spend host.WindowSpend) bool { return spend.Account == account && spend.Window == window })
	if at < 0 {
		return cli.Fact{}, false
	}
	spend := spends[at]
	text := "rate not known yet"
	switch {
	case spend.UsedPerDay == nil:
	case spend.RunsOutAt == nil:
		text = render.Percent(*spend.UsedPerDay) + "/day, lasts to the reset"
	default:
		text = render.Percent(*spend.UsedPerDay) + "/day, out " + spend.RunsOutAt.Local().Format(usageRunOutFormat)
	}
	if spend.Requests == 0 {
		return cli.Fact{Text: page.Label(text + "; tofu spent nothing")}, true
	}
	return cli.Fact{Text: page.Label(text + "; tofu " + render.Weighted(spend.Weighted) + " weighted, " + render.Weighted(spend.PerDay) + "/day")}, true
}

func usagePage(page cli.Page, report host.UsageReport, now time.Time) []string {
	verdict := cli.Verdict{Mark: cli.Done, Text: string(report.State)}
	switch report.State {
	case host.UsageServing:
	case host.UsageAttention:
		verdict.Mark = cli.Warn
	case host.UsageNone:
		lines := append(page.Title("Usage", nil, cli.Verdict{Mark: cli.Idle, Text: "none signed in"}), "")
		return append(lines, wiring.BlockerRows(page, report.Missing)...)
	}
	lines := page.Title("Usage", []string{strconv.Itoa(len(report.Providers)) + " signed in"}, verdict)
	for i, provider := range report.Providers {
		lines = append(lines, "")
		if i == 0 || provider.Provider != report.Providers[i-1].Provider {
			lines = append(lines, page.Section(provider.Provider, cli.Verdict{}))
		}
		cardVerdict := cli.Verdict{Mark: cli.Active, Text: wiring.UsageServingState}
		var facts []cli.Fact
		if provider.State != wiring.UsageServingState {
			cardVerdict = cli.Verdict{Mark: cli.Warn, Text: string(host.UsageAttention)}
			facts = append(facts, cli.Fact{Label: "state", Text: provider.State})
		}
		for _, window := range provider.Windows {
			text := page.Label(wiring.WindowPercent(window))
			if window.Reported {
				text = page.Bar(window.Used)
				if left := window.ResetsAt.Sub(now); left > 0 {
					text += cli.Gap + page.Label("resets in "+render.Until(left))
				}
			}
			facts = append(facts, cli.Fact{Label: window.ID, Text: text})
			if pace, known := windowPace(page, report.Spends, provider.Account, window.ID); known {
				facts = append(facts, pace)
			}
		}
		if !provider.ReadAt.IsZero() {
			text := provider.ReadAt.Format(time.RFC3339) + ", " + render.Until(now.Sub(provider.ReadAt)) + " ago, from the " + provider.Source
			if provider.Stale {
				text += ", stale"
			}
			facts = append(facts, cli.Fact{Label: "read", Text: page.Label(text)})
		}
		if left := provider.RetryAt.Sub(now); left > 0 {
			facts = append(facts, cli.Fact{Label: "retry", Text: page.Label("in " + render.Until(left))})
		}
		lines = append(lines, page.Card(page.Subject(cmp.Or(provider.Plan, "plan not reported")), cardVerdict, page.Facts(facts))...)
	}
	return lines
}
