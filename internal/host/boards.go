package host

import (
	"cmp"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/llm/quota"
	"tofu/internal/recall"
	"tofu/internal/rule"
	"tofu/internal/session"
	"tofu/internal/sift"
	"tofu/internal/skill"
	"tofu/internal/sys"
	"tofu/internal/turn"
	"tofu/library"
)

const (
	burnLookback       = 7 * 24 * time.Hour
	ledgerSummaryDays  = 7
	classifierLookback = 31 * 24 * time.Hour
	shippedLibraryRoot = "library"
	shellSiftTool      = "bash"
	tokensPerThousand  = 1000
)

type SessionDetail struct {
	Info        SessionInfo      `json:"info"`
	Family      string           `json:"family"`
	Generations []ForkGeneration `json:"generations"`
	Branches    []string         `json:"branches"`
}

type ForkGeneration struct {
	Session      string            `json:"session"`
	Generation   int               `json:"generation"`
	At           time.Time         `json:"at"`
	EndedAt      *time.Time        `json:"ended_at,omitempty"`
	EndReason    session.EndReason `json:"end_reason,omitempty"`
	ForkKind     string            `json:"fork_kind,omitempty"`
	ForkedInto   string            `json:"forked_into,omitempty"`
	TokensBefore int               `json:"tokens_before,omitempty"`
	TokensAfter  int               `json:"tokens_after,omitempty"`
	Turns        int               `json:"turns"`
	Usage        session.Usage     `json:"usage"`
	CostUSD      float64           `json:"cost_usd"`
	Here         bool              `json:"here"`
}

type UsageHistoryParams struct {
	Range   session.UsageSpan `json:"range"`
	Project string            `json:"project,omitempty"`
}

type UsageHistoryReport struct {
	session.UsageHistory
	SiftedTokens int           `json:"sifted_tokens"`
	Skipped      []SessionSkip `json:"skipped"`
}

type LimitsReport struct {
	Burns        []quota.Burn   `json:"burns"`
	TornReadings int            `json:"torn_readings"`
	Order        []AccountOrder `json:"order"`
	Spend        []AccountSpend `json:"spend"`
}

type AccountOrder struct {
	Role     AccountRole `json:"role"`
	Source   string      `json:"source"`
	Accounts []int64     `json:"accounts"`
}

type AccountSpend struct {
	Account int64             `json:"account"`
	Role    session.UsageRole `json:"role"`
	Agent   string            `json:"agent,omitempty"`
	session.UsageTotals
}

type SkillListing = skill.Skill

type SkillsReport struct {
	Skills   []SkillListing `json:"skills"`
	Warnings []string       `json:"warnings"`
}

type LedgerSummaryParams struct {
	Since time.Time `json:"since,omitzero"`
}

type LedgerSummary struct {
	Since   time.Time             `json:"since"`
	Points  []ledger.PointSummary `json:"points"`
	Corrupt int                   `json:"corrupt"`
}

func logDir(project string) (string, error) {
	state, err := sys.ProjectStateDirAt(project)
	return filepath.Join(state, "log"), err
}

func (s *server) sessionDetail(p SessionParams) (any, error) {
	info, err := verbAs[SessionInfo](s, "session", "info", p.Session)
	if err != nil {
		return nil, err
	}
	store, err := session.OpenIn(s.Host.dir)
	if err != nil {
		return nil, err
	}
	listing, err := store.Listing()
	if err != nil {
		return nil, err
	}
	family, _, err := listing.FamilyOf(info.ID)
	if err != nil {
		return nil, err
	}
	detail := SessionDetail{Info: info, Family: family.Family, Branches: append([]string{}, family.Branches...)}
	for i, header := range family.Generations {
		detail.Generations = append(detail.Generations, ForkGeneration{Session: header.ID, Generation: i + 1, At: header.At, EndedAt: header.EndedAt, EndReason: header.EndReason,
			ForkKind: header.ForkKind, ForkedInto: header.ForkedInto, TokensBefore: header.ForkTokensBefore, TokensAfter: header.ForkTokensAfter, Turns: header.Turns,
			Usage: header.Usage, CostUSD: header.CostUSD, Here: header.ID == info.ID})
	}
	return detail, nil
}

func (s *server) usageHistory(p UsageHistoryParams) (any, error) {
	switch p.Range {
	case session.UsageDay, session.UsageWeek, session.UsageMonth:
	default:
		return nil, &Refusal{Code: CodeBadParams, Message: "range is day, week or month, not " + strconv.Quote(string(p.Range))}
	}
	project, now := cmp.Or(p.Project, s.Host.dir), time.Now()
	store, err := session.OpenIn(project)
	if err != nil {
		return nil, err
	}
	dir, err := logDir(project)
	if err != nil {
		return nil, err
	}
	var calls []session.ClassifierCall
	if _, err := ledger.NewReader(dir).Each(ledger.Filter{Since: now.Add(-classifierLookback)}, func(row ledger.Row) error {
		calls = append(calls, session.ClassifierCall{At: row.At, Turn: row.TurnID, CostUSD: row.Cost})
		return nil
	}); err != nil {
		return nil, err
	}
	history, err := store.UsageHistory(p.Range, now, calls)
	if err != nil {
		return nil, err
	}
	cfg, err := recall.LoadConfig()
	if err != nil {
		return nil, err
	}
	report := UsageHistoryReport{UsageHistory: history, SiftedTokens: history.Total.SiftedBytes * tokensPerThousand / cfg.BytesPerThousandTokens, Skipped: []SessionSkip{}}
	for _, skip := range history.Skipped {
		report.Skipped = append(report.Skipped, SessionSkip{Session: skip.ID, Reason: skip.Reason.Error()})
	}
	return report, nil
}

func (s *server) limits(NoParams) (any, error) {
	now := time.Now()
	dir, err := sys.QuotaDir()
	if err != nil {
		return nil, err
	}
	readings, torn, err := quota.ReadReadings(dir, now.Add(-burnLookback))
	if err != nil {
		return nil, err
	}
	report := LimitsReport{Burns: append([]quota.Burn{}, quota.Burns(readings)...), TornReadings: torn, Order: []AccountOrder{}, Spend: []AccountSpend{}}
	accounts, err := verbAs[Accounts](s, "login", "--status")
	if err != nil {
		return nil, err
	}
	for _, held := range accounts.Subscriptions {
		order := AccountOrder{Role: held.Role, Source: held.Source, Accounts: []int64{}}
		for _, account := range held.Accounts {
			order.Accounts = append(order.Accounts, account.ID)
		}
		report.Order = append(report.Order, order)
	}
	store, err := session.OpenIn(s.Host.dir)
	if err != nil {
		return nil, err
	}
	history, err := store.UsageHistory(session.UsageDay, now, nil)
	if err != nil {
		return nil, err
	}
	at := map[AccountSpend]int{}
	for _, row := range history.Spenders {
		if row.Account == 0 {
			continue
		}
		key := AccountSpend{Account: row.Account, Role: row.Role, Agent: row.Agent}
		if _, seen := at[key]; !seen {
			at[key] = len(report.Spend)
			report.Spend = append(report.Spend, key)
		}
		report.Spend[at[key]].Add(row.UsageTotals)
	}
	return report, nil
}

func (s *server) skills(NoParams) (any, error) {
	shipped, err := skill.Shipped(library.Files(), shippedLibraryRoot)
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	found := skill.Discover(s.Host.dir, home)
	return SkillsReport{Skills: append(shipped, found.Skills...), Warnings: append([]string{}, found.Warnings...)}, nil
}

func (s *server) ledgerSummary(p LedgerSummaryParams) (any, error) {
	now := time.Now()
	since := p.Since
	if since.IsZero() {
		since = now.AddDate(0, 0, -ledgerSummaryDays)
	}
	dir, err := logDir(s.Host.dir)
	if err != nil {
		return nil, err
	}
	points, report, err := ledger.NewReader(dir).Points(since, now)
	return LedgerSummary{Since: since, Points: points, Corrupt: len(report.Corrupt)}, err
}

func (s *server) withSubjects(report LedgerReport) (LedgerReport, error) {
	dir, err := logDir(s.Host.dir)
	if err != nil {
		return report, err
	}
	reader := ledger.NewReader(dir)
	for i, row := range report.Rows {
		body, err := reader.State(row.Row)
		if err != nil {
			return report, err
		}
		if len(body) == 0 {
			continue
		}
		subject, err := state.SubjectOf(body)
		if err != nil {
			return report, err
		}
		if builder, _, _ := strings.Cut(row.StateBuilder, "@"); builder == sift.ShellSchema {
			subject.Tool = shellSiftTool
		}
		report.Rows[i].Subject = &subject
	}
	return report, nil
}

func (s *server) withRuleText(report RuleListReport) (RuleListReport, error) {
	shipped, err := rule.LoadFS(library.Files(), shippedLibraryRoot)
	if err != nil {
		return report, err
	}
	store, err := session.OpenIn(s.Host.dir)
	if err != nil {
		return report, err
	}
	fires, _, err := store.RuleFires(time.Now())
	if err != nil {
		return report, err
	}
	layered := map[string][]rule.Rule{}
	for i := range report.Rules {
		listing := &report.Rules[i]
		known := shipped
		if dir := filepath.Dir(listing.File); listing.File != "" && listing.Override == nil {
			if _, read := layered[dir]; !read {
				if layered[dir], err = rule.LoadFS(os.DirFS(dir), dir); err != nil {
					return report, err
				}
			}
			known = layered[dir]
		}
		for _, one := range known {
			if one.ID == listing.ID {
				listing.Text, listing.Trigger = one.Text, one.Trigger.String()
			}
		}
		if listing.Override != nil && listing.Override.Text != "" {
			listing.Text = listing.Override.Text
		}
		listing.Fires, listing.FiresWeek = fires[listing.ID].Fires, fires[listing.ID].Week
	}
	return report, nil
}

func (s *server) withItems(report ContextReport) (ContextReport, error) {
	if report.Session == "" {
		return report, nil
	}
	store, err := session.OpenIn(s.Host.dir)
	if err != nil {
		return report, err
	}
	header, err := store.Header(report.Session)
	if err != nil {
		return report, err
	}
	events, err := store.Body(header.ID)
	if err != nil {
		return report, err
	}
	conversation, err := turn.RecordedContext(events)
	if err != nil {
		return report, err
	}
	cfg, err := recall.LoadConfig()
	if err != nil {
		return report, err
	}
	bands := recall.ShippedBands()
	if o := report.Occupancy; o != nil {
		bands = recall.Bands{Identity: o.Identity.Cap, Facts: o.Facts.Cap, WorkingSet: o.WorkingSet.Cap, Recent: o.Recent.Cap}
	}
	report.Items = recall.Items(cfg.OnWire(header.Wire), bands, conversation)
	return report, nil
}
