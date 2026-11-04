package library

import (
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/cli/render"
	"tofu/internal/host"
	question "tofu/internal/judge/questions"
	"tofu/internal/librarian/rules"
	"tofu/internal/models/llm/catalog"
	shipped "tofu/library"
	"tofu/library/questions"
)

const Usage = "tofu library [resolve <name>] [--dir <path>] [--json] | tofu library reach [--never] [--json]"

type Report = host.LibraryReport

type resolvedField struct {
	Path  string `json:"path"`
	Value string `json:"value"`
	File  string `json:"file"`
	Line  int    `json:"line"`
}

func Verb(verbs []string) func(args []string, out, errOut io.Writer) int {
	return func(args []string, out, errOut io.Writer) int {
		o := output.Verb{Name: "library", UsageLine: Usage, AsJSON: slices.Contains(args, output.JSONFlag), Out: out, ErrOut: errOut}
		dir, rest, err := takeDir(slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == output.JSONFlag }))
		switch {
		case err != nil:
			return o.Usage(err)
		case len(rest) == 0:
			return reportVerb(o, dir, verbs)
		case rest[0] == "reach" && dir == "":
			return reachVerb(o, rest[1:], time.Now())
		case len(rest) == 2 && rest[0] == "resolve":
			o.Name = "library resolve"
			return resolve(o, rest[1], dir)
		}
		return o.Usage(errors.New("unknown argument " + strconv.Quote(rest[0])))
	}
}

func takeDir(args []string) (string, []string, error) {
	for i, arg := range args {
		if arg != "--dir" {
			continue
		}
		if i+1 == len(args) {
			return "", nil, errors.New("--dir names no path")
		}
		return args[i+1], slices.Concat(args[:i], args[i+2:]), nil
	}
	return "", args, nil
}

func resolve(o output.Verb, name, dir string) int {
	layers, err := question.Layers(questions.Files(), dir)
	if err != nil {
		return o.Fail(err)
	}
	_, fields, err := question.Resolve(name, layers)
	if err != nil {
		return o.Fail(err)
	}
	resolved := make([]resolvedField, len(fields))
	for i, field := range fields {
		resolved[i] = resolvedField(field)
	}
	data := struct {
		Name   string          `json:"name"`
		Fields []resolvedField `json:"fields"`
	}{name, resolved}
	return output.Show(o.Out, o.AsJSON, cli.Envelope{Verb: o.Name, OK: true, At: time.Now(), Data: data}, func(page cli.Page) []string {
		lines := page.Title("Question set", []string{name}, cli.Verdict{Text: output.Plural(len(fields), "field")})
		for start := 0; start < len(fields); {
			end := start
			var rows []cli.Row
			for ; end < len(fields) && fields[end].File == fields[start].File; end++ {
				rows = append(rows, cli.Row{Cells: []string{strconv.Itoa(fields[end].Line), fields[end].Path}, Detail: strings.Join(strings.Fields(fields[end].Value), " ")})
			}
			lines = append(append(lines, "", page.Section(page.Path(fields[start].File), cli.Verdict{})), page.Rows(rows)...)
			start = end
		}
		return lines
	})
}

func reportVerb(o output.Verb, dir string, verbs []string) int {
	layers, err := catalog.Layers(shipped.Files(), dir)
	if err != nil {
		return o.Fail(err)
	}
	library, _ := catalog.Load(layers)
	sets, refused := question.LoadAll(questions.Files(), wiring.LibraryRoot+"/questions")
	proxy := wiring.LoadProxySetting(dir)
	corpus, missing := DocsCoverage(verbs)
	report := Report{
		Models: len(library.Models), Subscriptions: len(library.Subscriptions), Roles: len(library.Roles), Questions: len(sets),
		DocPages: len(corpus.Pages), DocEntries: len(corpus.Entries), Proxy: proxy.Use, ProxyFrom: proxy.Layer,
	}
	refused = slices.Concat(refused, proxy.Refused, missing)
	for _, layer := range layers {
		report.Layers = append(report.Layers, host.LibraryLayer{Name: layer.Name, Origin: layer.Origin})
	}
	files, domainsDir, origin := wiring.LibrarySource()
	domains, err := rules.LoadDomains(files, wiring.LibraryRoot)
	if err != nil {
		return o.Fail(err)
	}
	report.DomainsFrom, report.DomainsDir = origin, domainsDir
	for _, domain := range domains {
		refusedHere := len(domain.Refused) + len(domain.Unreachable())
		refused = append(refused, domain.Refused...)
		for _, unreachable := range domain.Unreachable() {
			refused = append(refused, errors.New(unreachable))
		}
		report.Domains = append(report.Domains, host.LibraryDomain{Name: domain.Name, Rules: len(domain.Rules), Thresholds: len(domain.Thresholds),
			Skills: len(domain.Skills), Agents: len(domain.Agents), References: len(domain.References), Refused: refusedHere})
	}
	for _, broken := range library.Broken {
		refused = append(refused, broken)
	}
	problems := make([]cli.Problem, len(refused))
	for i, one := range refused {
		problems[i] = cli.Problem{What: one.Error()}
	}
	return output.Show(o.Out, o.AsJSON, cli.Envelope{Verb: o.Name, OK: len(problems) == 0, At: time.Now(), Data: report, Problems: problems},
		func(page cli.Page) []string { return reportLines(page, report, problems) })
}

func reportLines(page cli.Page, report Report, problems []cli.Problem) []string {
	verdict := cli.Verdict{Mark: cli.Done, Text: "nothing refused"}
	if len(problems) > 0 {
		verdict = cli.Verdict{Mark: cli.Fail, Text: strconv.Itoa(len(problems)) + " refused"}
	}
	proxyFrom := report.ProxyFrom
	for _, layer := range report.Layers {
		if proxyFrom == layer.Name+" "+layer.Origin {
			proxyFrom = layer.Name
		}
	}
	lines := append(page.Title("Library", []string{output.Plural(len(report.Layers), "layer")}, verdict), "")
	lines = append(lines, cli.Indent(page.Facts([]cli.Fact{
		{Label: "models", Text: output.NonZero(report.Models)},
		{Label: "subscriptions", Text: output.NonZero(report.Subscriptions)},
		{Label: "roles", Text: output.NonZero(report.Roles)},
		{Label: "questions", Text: output.NonZero(report.Questions)},
		{Label: "docs", Text: output.Plural(report.DocPages, "page") + " · " + strconv.Itoa(report.DocEntries) + " index entries"},
		{Label: "proxy", Text: report.Proxy + " · " + proxyFrom},
	})...)...)
	facts := make([]cli.Fact, len(report.Layers))
	for i, layer := range report.Layers {
		facts[i] = cli.Fact{Label: layer.Name, Text: page.Path(layer.Origin)}
	}
	lines = append(append(lines, "", page.Section("layers", cli.Verdict{})), cli.Indent(page.Facts(facts)...)...)
	rows := make([]cli.Row, len(report.Domains))
	for i, domain := range report.Domains {
		var said []string
		counts := []int{domain.Rules, domain.Thresholds, domain.Skills, domain.Agents, domain.References}
		for i, noun := range []string{"rule", "threshold", "skill", "agent", "reference"} {
			if counts[i] > 0 {
				said = append(said, output.Plural(counts[i], noun))
			}
		}
		rows[i] = cli.Row{Mark: cli.Done, Cells: []string{domain.Name}, Detail: strings.Join(said, " · ")}
		if domain.Refused > 0 {
			rows[i].Mark = cli.Fail
		}
	}
	from := report.DomainsFrom
	if from == wiring.RulesFromTheProject {
		from = page.Path(report.DomainsDir)
	}
	lines = append(append(lines, "", page.Section("domains · "+from, cli.Verdict{})), cli.Indent(page.Rows(rows)...)...)
	if len(problems) == 0 {
		return lines
	}
	lines = append(lines, "", page.Section("refused", cli.Verdict{}))
	for _, problem := range problems {
		under := "    "
		for i, wrapped := range render.Wrap(problem.What, page.Width-len(under)) {
			lead := under
			if i == 0 {
				lead = "  " + page.Glyph(cli.Fail) + " "
			}
			lines = append(lines, lead+wrapped)
		}
	}
	return lines
}
