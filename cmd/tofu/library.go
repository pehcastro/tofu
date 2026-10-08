package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/host"
	"tofu/internal/judge/question"
	"tofu/internal/llm/models"
	"tofu/internal/rule"
	"tofu/internal/sys"
	"tofu/internal/widget"
	shipped "tofu/library"
	"tofu/library/questions"
)

const libraryUsage = "tofu library [resolve <name>] [--dir <path>] [--json]"

type (
	libraryLayer  = host.LibraryLayer
	libraryDomain = host.LibraryDomain
	libraryReport = host.LibraryReport
)

type resolvedField struct {
	Path  string `json:"path"`
	Value string `json:"value"`
	File  string `json:"file"`
	Line  int    `json:"line"`
}

func libraryVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "library", usageLine: libraryUsage, asJSON: slices.Contains(args, jsonFlag), out: out, errOut: errOut}
	dir, rest, err := takeDir(slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == jsonFlag }))
	switch {
	case err != nil:
		return o.usage(err)
	case len(rest) == 0:
		return libraryReportVerb(o, dir)
	case len(rest) == 2 && rest[0] == "resolve":
		o.verb = "library resolve"
		return libraryResolve(o, rest[1], dir)
	}
	return o.usage(errors.New("unknown argument " + strconv.Quote(rest[0])))
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

func libraryResolve(o verbOutput, name, dir string) int {
	layers, err := question.Layers(questions.Files(), dir)
	if err != nil {
		return o.fail(err)
	}
	_, fields, err := question.Resolve(name, layers)
	if err != nil {
		return o.fail(err)
	}
	resolved := make([]resolvedField, len(fields))
	for i, field := range fields {
		resolved[i] = resolvedField(field)
	}
	data := struct {
		Name   string          `json:"name"`
		Fields []resolvedField `json:"fields"`
	}{name, resolved}
	return show(o.out, o.asJSON, cli.Envelope{Verb: o.verb, OK: true, At: time.Now(), Data: data}, func(page cli.Page) []string {
		lines := page.Title("Question set", []string{name}, cli.Verdict{Text: plural(len(fields), "field")})
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

func librarySource() (fs.FS, string, string) {
	dir, err := sys.LibraryDir()
	if err != nil {
		return shipped.Files(), libraryRoot, rulesFromTheBinary
	}
	isDir, err := sys.IsDir(dir)
	if err != nil || !isDir {
		return shipped.Files(), libraryRoot, rulesFromTheBinary
	}
	return os.DirFS(dir), dir, rulesFromTheProject
}

func libraryReportVerb(o verbOutput, dir string) int {
	layers, err := models.Layers(shipped.Files(), dir)
	if err != nil {
		return o.fail(err)
	}
	library, _ := models.Load(layers)
	sets, refused := question.LoadAll(questions.Files(), libraryRoot+"/questions")
	proxy := loadProxySetting(dir)
	corpus, missing := docsCoverage(usageVerbs())
	report := libraryReport{
		Models: len(library.Models), Subscriptions: len(library.Subscriptions), Roles: len(library.Roles), Questions: len(sets),
		DocPages: len(corpus.Pages), DocEntries: len(corpus.Entries), Proxy: proxy.use, ProxyFrom: proxy.layer,
	}
	refused = slices.Concat(refused, proxy.refused, missing)
	for _, layer := range layers {
		report.Layers = append(report.Layers, libraryLayer{Name: layer.Name, Origin: layer.Origin})
	}
	files, domainsDir, origin := librarySource()
	domains, err := rule.LoadDomains(files, libraryRoot)
	if err != nil {
		return o.fail(err)
	}
	report.DomainsFrom, report.DomainsDir = origin, domainsDir
	for _, domain := range domains {
		refusedHere := len(domain.Refused) + len(domain.Unreachable())
		refused = append(refused, domain.Refused...)
		for _, unreachable := range domain.Unreachable() {
			refused = append(refused, errors.New(unreachable))
		}
		report.Domains = append(report.Domains, libraryDomain{Name: domain.Name, Rules: len(domain.Rules), Thresholds: len(domain.Thresholds),
			Skills: len(domain.Skills), Agents: len(domain.Agents), References: len(domain.References), Refused: refusedHere})
	}
	for _, broken := range library.Broken {
		refused = append(refused, broken)
	}
	problems := make([]cli.Problem, len(refused))
	for i, one := range refused {
		problems[i] = cli.Problem{What: one.Error()}
	}
	return show(o.out, o.asJSON, cli.Envelope{Verb: o.verb, OK: len(problems) == 0, At: time.Now(), Data: report, Problems: problems},
		func(page cli.Page) []string { return libraryLines(page, report, problems) })
}

func libraryLines(page cli.Page, report libraryReport, problems []cli.Problem) []string {
	verdict := cli.Verdict{Mark: cli.Done, Text: "nothing refused"}
	if len(problems) > 0 {
		verdict = cli.Verdict{Mark: cli.Fail, Text: strconv.Itoa(len(problems)) + " refused"}
	}
	number := func(count int) string {
		if count == 0 {
			return ""
		}
		return strconv.Itoa(count)
	}
	proxyFrom := report.ProxyFrom
	for _, layer := range report.Layers {
		if proxyFrom == layer.Name+" "+layer.Origin {
			proxyFrom = layer.Name
		}
	}
	lines := append(page.Title("Library", []string{plural(len(report.Layers), "layer")}, verdict), "")
	lines = append(lines, cli.Indent(page.Facts([]cli.Fact{
		{Label: "models", Text: number(report.Models)},
		{Label: "subscriptions", Text: number(report.Subscriptions)},
		{Label: "roles", Text: number(report.Roles)},
		{Label: "questions", Text: number(report.Questions)},
		{Label: "docs", Text: plural(report.DocPages, "page") + " · " + strconv.Itoa(report.DocEntries) + " index entries"},
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
				said = append(said, plural(counts[i], noun))
			}
		}
		rows[i] = cli.Row{Mark: cli.Done, Cells: []string{domain.Name}, Detail: strings.Join(said, " · ")}
		if domain.Refused > 0 {
			rows[i].Mark = cli.Fail
		}
	}
	from := report.DomainsFrom
	if from == rulesFromTheProject {
		from = page.Path(report.DomainsDir)
	}
	lines = append(append(lines, "", page.Section("domains · "+from, cli.Verdict{})), cli.Indent(page.Rows(rows)...)...)
	if len(problems) == 0 {
		return lines
	}
	lines = append(lines, "", page.Section("refused", cli.Verdict{}))
	for _, problem := range problems {
		under := "    "
		for i, wrapped := range widget.Wrap(problem.What, page.Width-len(under)) {
			lead := under
			if i == 0 {
				lead = "  " + page.Glyph(cli.Fail) + " "
			}
			lines = append(lines, lead+wrapped)
		}
	}
	return lines
}
