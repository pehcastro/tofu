package sample

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/widget"
)

type reload struct {
	ContextWindows int            `json:"context_windows"`
	Sources        []reloadSource `json:"sources"`
}

type reloadSource struct {
	Source  string        `json:"source"`
	Served  int           `json:"served"`
	Changes []modelChange `json:"changes"`
	Failure string        `json:"failure,omitempty"`
	Hint    string        `json:"hint,omitempty"`
}

type modelChange struct {
	Model  string `json:"model"`
	Change string `json:"change"`
	Detail string `json:"detail"`
}

func (c modelChange) mark() cli.Mark {
	switch c.Change {
	case "added":
		return cli.Added
	case "changed":
		return cli.Changed
	case "removed":
		return cli.Removed
	}
	panic("sample: unknown model change " + c.Change)
}

type accounts struct {
	Subscriptions []subscription `json:"subscriptions"`
	Keys          []key          `json:"keys"`
}

type subscription struct {
	Source   string    `json:"source"`
	Accounts []account `json:"accounts"`
}

type account struct {
	ID      int      `json:"id"`
	Email   string   `json:"email"`
	State   string   `json:"state"`
	Login   string   `json:"login"`
	Windows []window `json:"windows"`
	Hint    string   `json:"hint,omitempty"`
}

type window struct {
	ID       string    `json:"id"`
	Used     float64   `json:"used"`
	ResetsAt time.Time `json:"resets_at"`
	Only     string    `json:"only,omitempty"`
}

type key struct {
	Role    string `json:"role"`
	Service string `json:"service"`
	Key     string `json:"key,omitempty"`
	Use     string `json:"use,omitempty"`
	Hint    string `json:"hint,omitempty"`
}

type extension struct {
	Installed bool     `json:"installed"`
	Folder    string   `json:"folder"`
	ID        string   `json:"id"`
	Host      string   `json:"host"`
	Next      []string `json:"next"`
	Hint      string   `json:"hint"`
}

const (
	stateInUse    = "in use"
	stateStandby  = "standby"
	stateSetAside = "set aside"
)

func reloadData() reload {
	return reload{ContextWindows: 812, Sources: []reloadSource{
		{Source: "claude-sub", Served: 23, Changes: []modelChange{
			{"claude-sonnet-5-5", "added", "allowed"},
			{"claude-opus-5-5", "added", "allowed"},
			{"claude-haiku-4-5", "changed", "context 200k to 1M"},
		}},
		{Source: "codex-sub", Failure: "model list refused (403)", Hint: "tofu login codex-sub"},
	}}
}

func accountsData(now time.Time) accounts {
	now = now.Truncate(time.Second)
	return accounts{
		Subscriptions: []subscription{
			{Source: "claude-sub", Accounts: []account{
				{ID: 1, Email: "ada@example.com", State: stateInUse, Login: "oauth · re-login by 27 Oct", Windows: []window{
					{ID: "5h", ResetsAt: now.Add(4*time.Hour + 28*time.Minute)},
					{ID: "7d", Used: 0.67, ResetsAt: now.Add(22 * time.Hour)},
					{ID: "7d:opus", Only: "opus-5, opus-5-5"},
				}},
				{ID: 2, Email: "lin.marsh@example.org", State: stateStandby, Login: "oauth · re-login by 3 Nov", Windows: []window{
					{ID: "5h", Used: 0.92, ResetsAt: now.Add(51 * time.Minute)},
				}},
				{ID: 3, Email: "ops@example.net", State: stateSetAside, Login: "refresh failed, sign in again", Hint: "tofu login claude-sub"},
			}},
		},
		Keys: []key{
			{Role: "classifier", Service: "OpenRouter", Key: widget.Mask("sk-or-v1-made-up-000000003498"), Use: "judges tool calls with jev-latest"},
			{Role: "classifier", Service: "TypeSafe", Use: "used only without OpenRouter"},
			{Role: "web search", Service: "Brave", Hint: "tofu login brave"},
			{Role: "meta models", Service: "Meta", Hint: "tofu login meta"},
		},
	}
}

func extensionData(home string) extension {
	return extension{
		Installed: true,
		Folder:    filepath.Join(home, ".tofu", "browser", "extension"),
		ID:        "jednanpboiikklhkkkimnmdmjmgjgphh",
		Host:      filepath.Join(home, ".local", "bin", "tofu.exe"),
		Next: []string{
			"open chrome://extensions and turn on Developer mode",
			"Load unpacked, pick the folder above, check the id matches",
			"pin the tofu icon so its badge shows",
		},
		Hint: "after a tofu update: tofu browser install, then reload the tofu card",
	}
}

func Envelopes(home string, now time.Time) []cli.Envelope {
	var problems []cli.Problem
	for _, source := range reloadData().Sources {
		if source.Failure != "" {
			problems = append(problems, cli.Problem{What: source.Source + ": " + source.Failure, Hint: source.Hint})
		}
	}
	return []cli.Envelope{
		{Verb: "models reload", OK: len(problems) == 0, At: now, Data: reloadData(), Problems: problems},
		{Verb: "login --status", OK: true, At: now, Data: accountsData(now)},
		{Verb: "browser install", OK: true, At: now, Data: extensionData(home)},
	}
}

func Pages(page cli.Page, now time.Time) [][]string {
	return [][]string{reloadPage(page, reloadData()), accountsPage(page, accountsData(now), now), extensionPage(page, extensionData(page.Home))}
}

func reloadPage(page cli.Page, data reload) []string {
	counts := map[cli.Mark]int{}
	for _, source := range data.Sources {
		for _, change := range source.Changes {
			counts[change.mark()]++
		}
		if source.Failure != "" {
			counts[cli.Fail]++
		}
	}
	verdict := cli.Verdict{Mark: cli.Done, Text: "nothing changed"}
	if counts[cli.Fail] > 0 {
		verdict.Mark = cli.Warn
	}
	var parts []string
	for _, count := range []struct {
		mark cli.Mark
		noun string
	}{{cli.Added, "new"}, {cli.Changed, "changed"}, {cli.Removed, "removed"}, {cli.Fail, "failed"}} {
		if counts[count.mark] > 0 {
			parts = append(parts, strconv.Itoa(counts[count.mark])+" "+count.noun)
		}
	}
	if len(parts) > 0 {
		verdict.Text = strings.Join(parts, " · ")
	}
	lines := append(page.Title("Model reload", nil, verdict), "", page.Status("models.dev", cli.Verdict{Mark: cli.Done, Text: strconv.Itoa(data.ContextWindows) + " context windows"}))
	for _, source := range data.Sources {
		lines = append(lines, "")
		if source.Failure != "" {
			lines = append(lines, page.Section(source.Source, cli.Verdict{Mark: cli.Fail, Text: source.Failure}))
			lines = append(lines, cli.Indent(page.Hint(source.Hint))...)
			continue
		}
		rows := make([]cli.Row, len(source.Changes))
		for i, change := range source.Changes {
			rows[i] = cli.Row{Mark: change.mark(), Cells: []string{change.Model}, Detail: change.Detail}
		}
		lines = append(lines, page.Section(source.Source, cli.Verdict{Mark: cli.Done, Text: strconv.Itoa(source.Served) + " served"}))
		lines = append(lines, cli.Indent(page.Rows(rows)...)...)
	}
	return lines
}

func accountsPage(page cli.Page, data accounts, now time.Time) []string {
	signedIn, attention := 0, 0
	var body []string
	for _, sub := range data.Subscriptions {
		body = append(body, "", page.Section(sub.Source, cli.Verdict{}))
		for i, account := range sub.Accounts {
			signedIn++
			if i > 0 {
				body = append(body, "")
			}
			verdict := cli.Verdict{Text: account.State}
			switch account.State {
			case stateInUse:
				verdict.Mark = cli.Active
			case stateStandby:
				verdict.Mark = cli.Idle
			case stateSetAside:
				verdict.Mark = cli.Fail
				attention++
			default:
				panic("sample: unknown account state " + account.State)
			}
			facts := []cli.Fact{{Label: "login", Text: account.Login}}
			for _, w := range account.Windows {
				detail := "only " + w.Only
				if w.Only == "" {
					detail = "resets in " + widget.Until(w.ResetsAt.Sub(now))
				}
				facts = append(facts, cli.Fact{Label: w.ID, Text: page.Bar(w.Used) + cli.Gap + page.Label(detail)})
			}
			lines := page.Facts(facts)
			if account.Hint != "" {
				lines = append(lines, page.Hint(account.Hint))
			}
			body = append(body, page.Card(page.Label("#"+strconv.Itoa(account.ID))+cli.Gap+page.Subject(account.Email), verdict, lines)...)
		}
	}
	rows := make([]cli.Row, len(data.Keys))
	for i, k := range data.Keys {
		rows[i] = cli.Row{Mark: cli.Done, Cells: []string{k.Role, k.Service, k.Key}, Detail: k.Use, Hint: k.Hint}
		if k.Key == "" {
			rows[i].Mark, rows[i].Cells[2] = cli.Idle, "not set"
		}
	}
	verdict := cli.Verdict{Mark: cli.Done, Text: "all ready"}
	if attention > 0 {
		verdict = cli.Verdict{Mark: cli.Warn, Text: strconv.Itoa(attention) + " needs attention"}
	}
	lines := append(page.Title("Accounts", []string{strconv.Itoa(signedIn) + " signed in"}, verdict), body...)
	lines = append(lines, "", page.Section("keys", cli.Verdict{}))
	return append(lines, cli.Indent(page.Rows(rows)...)...)
}

func extensionPage(page cli.Page, data extension) []string {
	verdict := cli.Verdict{Mark: cli.Fail, Text: "not installed"}
	if data.Installed {
		verdict = cli.Verdict{Mark: cli.Done, Text: "installed"}
	}
	lines := append(page.Title("Chrome extension", nil, verdict), "")
	lines = append(lines, cli.Indent(page.Facts([]cli.Fact{{Label: "folder", Text: page.Path(data.Folder)}, {Label: "id", Text: data.ID}, {Label: "host", Text: page.Path(data.Host)}})...)...)
	lines = append(lines, "", page.Section("next, once in Chrome", cli.Verdict{}))
	lines = append(lines, page.Steps(data.Next)...)
	return append(append(lines, ""), cli.Indent(page.Hint(data.Hint))...)
}
