package main

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/bench/browser/steps"
	"tofu/interface/cli"
	"tofu/internal/browser"
	"tofu/internal/browser/jevloop"
	"tofu/internal/browser/motion"
	"tofu/internal/konst"
	"tofu/internal/recipe"
	"tofu/internal/sys"
	"tofu/internal/widget"
)

type extension struct {
	Installed bool     `json:"installed"`
	Folder    string   `json:"folder"`
	ID        string   `json:"id,omitempty"`
	Host      string   `json:"host,omitempty"`
	Next      []string `json:"next,omitempty"`
	Hint      string   `json:"hint"`
}

type benchReport struct {
	Chooser string `json:"chooser"`
	steps.Summary
	RowsFile string `json:"rows_file"`
}

type browserOutput struct {
	page        cli.Page
	asJSON      bool
	verb        string
	out, errOut io.Writer
}

func hostVerb(origin string, in io.Reader, out, errOut io.Writer) int {
	home, err := os.UserHomeDir()
	if err == nil {
		err = browser.Host(origin, in, out, home)
	}
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu host: %v\n", err)
		return exitVerdict
	}
	return exitOK
}

func browserVerb(args []string, out, errOut io.Writer) int {
	asJSON, all := slices.Contains(args, jsonFlag), slices.Contains(args, "--all")
	args = slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == jsonFlag || arg == "--all" })
	tab, tabErr := 0, error(nil)
	if at := slices.Index(args, "--tab"); at >= 0 {
		tabErr = errors.New("--tab takes a tab id")
		if at+1 < len(args) {
			tab, tabErr = strconv.Atoi(args[at+1])
		}
		args = slices.Delete(args, at, min(at+2, len(args)))
	}
	flags := flag.NewFlagSet("browser", flag.ContinueOnError)
	flags.SetOutput(errOut)
	hostsKey := flags.String("hosts-key", browser.ChromeHostsKey, "")
	err := flags.Parse(args)
	verb, operands := flags.Arg(0), flags.Args()[min(1, flags.NArg()):]
	page := cli.Detect(out, os.Environ())
	o := browserOutput{page: page, asJSON: asJSON, verb: strings.TrimSpace("browser " + verb), out: out, errOut: errOut}
	if err == nil && verb == "bench" {
		return o.bench(operands)
	}
	if err == nil && tabErr == nil && verb == "motion" {
		return o.motion(tab, operands)
	}
	arity, known := map[string][2]int{"": {0, 0}, "tabs": {0, 0}, "build": {0, 0}, "install": {0, 0}, "uninstall": {0, 0}, "open": {1, 1}, "close": {1, 1},
		"recipes": {0, 0}, "observe": {0, 0}, "click": {1, 1}, "fill": {2, 2}, "select": {2, 2}, "press": {1, 1}, "scroll": {0, 2}, "back": {0, 0}}[verb]
	stepVerb := slices.Contains([]string{"observe", "click", "fill", "select", "press", "scroll", "back"}, verb)
	operand := strings.Join(operands, " ")
	tabID, badID := strconv.Atoi(operand)
	if err != nil || tabErr != nil || known && (len(operands) < arity[0] || len(operands) > arity[1]) || verb == "close" && badID != nil || stepVerb && tab == 0 {
		_, _ = fmt.Fprintln(errOut, "usage: tofu browser [install | uninstall | tabs | build | recipes | open <url> | close <tab id> | bench [--jev] [--n 12] [--rows file] | motion capture <scenario.json>] [--json]\n"+
			"       tofu browser observe [--all] | click <ref> | fill <ref> <text> | select <ref> <option> | press <key> | scroll [<ref>] [up|down] | back   --tab <id> [--json]")
		return exitUsage
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return o.fail(err)
	}
	if stepVerb {
		return o.step(home, tab, verb, operands, all)
	}
	switch verb {
	case "recipes":
		dir, err := recipe.Dir()
		var recipes []recipe.Recipe
		if err == nil {
			recipes, err = recipe.List(dir)
		}
		if err != nil {
			return o.fail(err)
		}
		return o.show(struct {
			Folder  string          `json:"folder"`
			Recipes []recipe.Recipe `json:"recipes"`
		}{dir, recipes}, recipesPage(page, dir, recipes))
	case "build":
		build, err := browser.Build()
		if err != nil {
			return o.fail(err)
		}
		return o.show(struct {
			Build string `json:"build"`
		}{build}, []string{build})
	case "", "tabs":
		var tabs []browser.Tab
		var builds *browser.Builds
		err := o.withBrowser(home, func(client *browser.Client) (err error) {
			if tabs, err = client.Tabs(); err == nil {
				builds, err = client.Builds()
			}
			return err
		})
		if err != nil {
			return o.fail(err)
		}
		return o.show(struct {
			Tabs   []browser.Tab   `json:"tabs"`
			Builds *browser.Builds `json:"builds,omitempty"`
		}{append([]browser.Tab{}, tabs...), builds}, tabsPage(page, tabs, builds))
	case "open":
		var tab int
		if err := o.withBrowser(home, func(client *browser.Client) (err error) { tab, err = client.Open(operand); return err }); err != nil {
			return o.fail(err)
		}
		return o.show(struct {
			Tab int    `json:"tab"`
			URL string `json:"url"`
		}{tab, operand}, []string{page.Receipt(cli.Added, "opened tab "+strconv.Itoa(tab), operand)})
	case "close":
		if err := o.withBrowser(home, func(client *browser.Client) error { return client.CloseTab(tabID) }); err != nil {
			return o.fail(err)
		}
		return o.show(struct {
			Tab int `json:"tab"`
		}{tabID}, []string{page.Glyph(cli.Removed) + " closed tab " + strconv.Itoa(tabID)})
	case "install":
		exe, err := os.Executable()
		id := ""
		if err == nil {
			id, err = browser.Install(home, exe, *hostsKey)
		}
		if err != nil {
			return o.fail(err)
		}
		installed := installedExtension(home, exe, id)
		return o.show(installed, extensionPage(page, installed))
	case "uninstall":
		if err := browser.Uninstall(home, *hostsKey); err != nil {
			return o.fail(err)
		}
		removed := removedExtension(home)
		return o.show(removed, extensionPage(page, removed))
	}
	_, _ = fmt.Fprintf(errOut, "tofu browser: unknown argument %q: use install, uninstall, tabs, build, recipes, open, close, observe, click, fill, select, press, scroll, back, bench, motion, or nothing\n", verb)
	return exitUsage
}

func (o browserOutput) step(home string, tab int, verb string, operands []string, all bool) int {
	move := browser.Move{Kind: browser.MoveKind(verb)}
	switch verb {
	case "click":
		move.Ref = operands[0]
	case "fill", "select":
		move.Ref, move.Value = operands[0], operands[1]
	case "press":
		move.Value = operands[0]
	case "scroll":
		for _, operand := range operands {
			if operand == "up" || operand == "down" {
				move.Value = operand
			} else {
				move.Ref = operand
			}
		}
	}
	var snapshot string
	var moved *browser.Moved
	err := o.withBrowser(home, func(client *browser.Client) error {
		driver := &browser.Driver{Client: client, Tab: tab}
		var err error
		if verb != "observe" {
			if _, err = driver.Observe(true); err == nil {
				var did browser.Moved
				did, err = driver.Do(move)
				moved = &did
			}
		}
		if err == nil {
			snapshot, err = driver.Observe(verb != "observe" || !all)
		}
		tab = driver.Tab
		return err
	})
	if err != nil {
		return o.fail(err)
	}
	var lines []string
	if moved != nil {
		mark := cli.Changed
		if moved.Covered != "" {
			mark = cli.Warn
		}
		lines = append(lines, o.page.Glyph(mark)+" "+strings.TrimSpace(verb+" "+strings.Join(operands, " "))+": "+moved.String(), "")
	}
	lines = append(lines, cli.Indent(strings.Split(strings.TrimSuffix(snapshot, "\n"), "\n")...)...)
	return o.show(struct {
		Tab      int            `json:"tab"`
		Moved    *browser.Moved `json:"moved,omitempty"`
		Snapshot string         `json:"snapshot"`
	}{tab, moved, snapshot}, lines)
}

func (o browserOutput) bench(args []string) int {
	moves := steps.Script()
	flags := flag.NewFlagSet("browser bench", flag.ContinueOnError)
	flags.SetOutput(o.errOut)
	withJev := flags.Bool("jev", false, "")
	n := flags.Int("n", len(moves), "")
	rowsPath := flags.String("rows", "", "")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 || *n < 1 || *n > len(moves) {
		_, _ = fmt.Fprintf(o.errOut, "usage: tofu browser bench [--jev] [--n 1 to %d] [--rows file] [--json]\n", len(moves))
		return exitUsage
	}
	report := benchReport{Chooser: "scripted", RowsFile: *rowsPath}
	home, err := os.UserHomeDir()
	var judge *jevloop.Jev
	if err == nil && *withJev {
		report.Chooser, judge = "jev", &jevloop.Jev{}
		*judge, err = browserJudge(".")
	}
	var address string
	stop := func() error { return nil }
	if err == nil {
		address, stop, err = steps.Serve()
	}
	if err != nil {
		return o.fail(err)
	}
	defer func() { _ = stop() }()
	if report.RowsFile == "" {
		report.RowsFile = filepath.Join(home, sys.StateDirName, "bench", "browser-steps-"+time.Now().Format("20060102-150405")+".jsonl")
	}
	err = o.withBrowser(home, func(client *browser.Client) error {
		tab, err := client.Open(address)
		if err != nil {
			return err
		}
		rows := steps.Run(context.Background(), browser.SharedTab{Client: client, ID: tab}, judge, moves[:*n])
		report.Summary = steps.Summarise(rows)
		var written bytes.Buffer
		err = errors.Join(client.CloseTab(tab), steps.Write(&written, rows))
		if err == nil {
			err = sys.WriteFile(report.RowsFile, written.Bytes(), 0o644)
		}
		return err
	})
	if err != nil {
		return o.fail(err)
	}
	return o.show(report, benchPage(o.page, report))
}

type motionTake struct {
	ID      string `json:"take_id"`
	Dir     string `json:"dir"`
	Frames  int    `json:"frames"`
	Samples int    `json:"samples"`
	Event   string `json:"trigger_event"`
}

func (o browserOutput) motion(tab int, args []string) int {
	flags := flag.NewFlagSet("browser motion capture", flag.ContinueOnError)
	flags.SetOutput(o.errOut)
	takes := flags.Int("takes", konst.MotionTakesDefault, "")
	label := flags.String("label", "take", "")
	if len(args) < 2 || args[0] != "capture" || flags.Parse(args[2:]) != nil || flags.NArg() > 0 || *takes < 1 {
		_, _ = fmt.Fprintln(o.errOut, "usage: tofu browser motion capture <scenario.json> [--takes 3] [--label take] [--tab <id>] [--json]")
		return exitUsage
	}
	scenario, err := browser.ReadScenario(args[1])
	home := ""
	if err == nil {
		home, err = os.UserHomeDir()
	}
	root := filepath.Join(home, sys.StateDirName, "motion")
	var saved []motion.Take
	if err == nil {
		err = o.withBrowser(home, func(client *browser.Client) (err error) {
			own := tab == 0
			if own {
				if tab, err = client.Open(scenario.URL); err != nil {
					return err
				}
			}
			saved, err = browser.Capture(&browser.Driver{Client: client, Tab: tab}, scenario, *takes, *label, root)
			if own {
				err = errors.Join(err, client.CloseTab(tab))
			}
			return err
		})
	}
	if err != nil {
		return o.fail(err)
	}
	summary := make([]motionTake, len(saved))
	rows := make([]cli.Row, len(saved))
	for i, take := range saved {
		summary[i] = motionTake{take.Manifest.TakeID, take.Dir, take.Manifest.FrameCount, take.Manifest.TraceSampleCount, take.Manifest.Trigger.Event}
		window := ""
		if len(take.Frames) > 0 {
			window = fmt.Sprintf("%+.1f to %+.1f ms", *take.Frames[0].MsFromTrigger, *take.Frames[len(take.Frames)-1].MsFromTrigger)
		}
		rows[i] = cli.Row{Mark: cli.Done, Cells: []string{take.Manifest.TakeID, strconv.Itoa(summary[i].Frames) + " frames", strconv.Itoa(summary[i].Samples) + " samples", window}}
	}
	lines := append(o.page.Title("Motion capture", []string{scenario.Name, strconv.Itoa(len(saved)) + " takes", o.page.Path(root)}, cli.Verdict{Mark: cli.Done, Text: "captured"}), "")
	return o.show(struct {
		Root  string       `json:"root"`
		Takes []motionTake `json:"takes"`
	}{root, summary}, append(lines, cli.Indent(o.page.Rows(rows)...)...))
}

func (o browserOutput) withBrowser(home string, use func(*browser.Client) error) error {
	client, err := browser.Dial(home)
	if errors.Is(err, browser.ErrRelayRestarting) && !o.asJSON {
		_, _ = fmt.Fprintln(o.out, o.page.Glyph(cli.Changed)+" updating: the relay restarts on this build")
	}
	restarting := errors.Is(err, browser.ErrRelayRestarting)
	for until := time.Now().Add(konst.BrowserCallTimeoutMillis * time.Millisecond); restarting && err != nil && time.Now().Before(until); {
		time.Sleep(konst.BrowserDialTimeoutMillis * time.Millisecond)
		client, err = browser.Dial(home)
	}
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	return use(client)
}

func (o browserOutput) show(data any, lines []string) int {
	var err error
	if o.asJSON {
		err = writeJSON(o.out, cli.Envelope{Verb: o.verb, OK: true, At: time.Now(), Data: data})
	} else {
		err = o.page.Print(o.out, lines)
	}
	if err != nil {
		return exitVerdict
	}
	return exitOK
}

func (o browserOutput) fail(err error) int {
	what, hint := err.Error(), ""
	if errors.Is(err, browser.ErrNotConnected) {
		what, hint = browser.NotConnected, browser.InstallHint
	}
	if o.asJSON {
		_ = writeJSON(o.out, cli.Envelope{Verb: o.verb, At: time.Now(), Problems: []cli.Problem{{What: err.Error(), Hint: hint}}})
		return exitVerdict
	}
	page := cli.Detect(o.errOut, os.Environ())
	_ = page.Print(o.errOut, page.ErrorLine(what, hint))
	return exitVerdict
}

func tabsPage(page cli.Page, tabs []browser.Tab, builds *browser.Builds) []string {
	facts, opened := []string{strconv.Itoa(len(tabs)) + " reachable"}, 0
	rows := make([]cli.Row, len(tabs))
	for i, tab := range tabs {
		site := tab.URL
		if parsed, err := url.Parse(tab.URL); err == nil && parsed.Host != "" {
			site = parsed.Host
		}
		rows[i] = cli.Row{Mark: cli.Idle, Cells: []string{strconv.Itoa(tab.ID), site}, Detail: tab.Title}
		if tab.Opened {
			rows[i].Mark = cli.Active
			opened++
		}
	}
	if opened > 0 {
		facts = append(facts, strconv.Itoa(opened)+" opened by tofu")
	}
	verdict, hint := cli.Verdict{Mark: cli.Done, Text: "connected"}, ""
	if builds != nil {
		extensionBuild := cmp.Or(builds.Extension, "unknown")
		facts = append(facts, "extension "+extensionBuild, "tofu "+builds.Tofu)
		switch {
		case builds.Problem != "":
			verdict, hint = cli.Verdict{Mark: cli.Fail, Text: "stale"}, builds.Problem
		case builds.Extension == "":
			verdict, hint = cli.Verdict{Mark: cli.Warn, Text: "stale"}, "this extension cannot update itself: tofu browser install, then reload the tofu card once"
		case builds.UpdatedFrom != "":
			verdict = cli.Verdict{Mark: cli.Done, Text: "updated from " + builds.UpdatedFrom}
		}
	}
	lines := append(page.Title("Chrome tabs", facts, verdict), "")
	if hint != "" {
		lines = append(append(lines, cli.Indent(page.Hint(hint))...), "")
	}
	if len(tabs) == 0 {
		return append(lines, cli.Indent(page.Label("chrome:// pages, DevTools, extensions and the web store are never listed"))...)
	}
	return append(lines, cli.Indent(page.Rows(rows)...)...)
}

func recipesPage(page cli.Page, dir string, recipes []recipe.Recipe) []string {
	rows := make([]cli.Row, len(recipes))
	for i, known := range recipes {
		mark, state := cli.Active, "in use"
		if known.Aside {
			mark, state = cli.Idle, "set aside"
		}
		templates := make([]string, len(known.Pages))
		for j, learned := range known.Pages {
			templates[j] = learned.Template
		}
		rows[i] = cli.Row{Mark: mark, Cells: []string{known.Host, state, strconv.Itoa(known.Uses) + " uses", strconv.Itoa(known.FailedInARow) + " failed in a row"},
			Detail: strings.Join(templates, "\n")}
	}
	lines := append(page.Title("Browser recipes", []string{strconv.Itoa(len(recipes)) + " learned", dir}, cli.Verdict{Mark: cli.Done, Text: "listed"}), "")
	if len(recipes) == 0 {
		return append(lines, cli.Indent(page.Label("a successful browser sub-agent run teaches its site a recipe"))...)
	}
	return append(lines, cli.Indent(page.Rows(rows)...)...)
}

func installedExtension(home, exe, id string) extension {
	return extension{
		Installed: true,
		Folder:    browser.ExtensionDir(home),
		ID:        id,
		Host:      exe,
		Next: []string{
			"open chrome://extensions and turn on Developer mode",
			"Load unpacked, pick the folder above, check the id matches",
			"pin the tofu icon so its badge shows",
		},
		Hint: "after a tofu update: tofu browser install, then reload the tofu card",
	}
}

func removedExtension(home string) extension {
	return extension{Folder: browser.ExtensionDir(home), Hint: "remove the tofu card in chrome://extensions"}
}

func extensionPage(page cli.Page, data extension) []string {
	verdict := cli.Verdict{Mark: cli.Idle, Text: "removed"}
	if data.Installed {
		verdict = cli.Verdict{Mark: cli.Done, Text: "installed"}
	}
	lines := append(page.Title("Chrome extension", nil, verdict), "")
	lines = append(lines, cli.Indent(page.Facts([]cli.Fact{{Label: "folder", Text: page.Path(data.Folder)}, {Label: "id", Text: data.ID}, {Label: "host", Text: page.Path(data.Host)}})...)...)
	if len(data.Next) > 0 {
		lines = append(lines, "", page.Section("next, once in Chrome", cli.Verdict{}))
		lines = append(lines, page.Steps(data.Next)...)
	}
	return append(append(lines, ""), cli.Indent(page.Hint(data.Hint))...)
}

func benchPage(page cli.Page, report benchReport) []string {
	facts := []string{strconv.Itoa(report.Steps) + " steps", report.Chooser}
	if report.DidNotRun > 0 {
		facts = append(facts, strconv.Itoa(report.DidNotRun)+" did not run")
	}
	verdict := cli.Verdict{Mark: cli.Done, Text: "none failed"}
	if report.Failed > 0 {
		verdict = cli.Verdict{Mark: cli.Fail, Text: strconv.Itoa(report.Failed) + " failed"}
	}
	numbers := make([][2]string, len(report.Phases))
	width := len("p50")
	for i, phase := range report.Phases {
		numbers[i] = [2]string{strconv.FormatFloat(phase.P50, 'f', 2, 64), strconv.FormatFloat(phase.P90, 'f', 2, 64)}
		width = max(width, len(numbers[i][0]), len(numbers[i][1]))
	}
	rows := []cli.Row{{Cells: []string{page.Label("phase"), page.Label(widget.Lead("p50", width)), page.Label(widget.Lead("p90", width))}}}
	for i, phase := range report.Phases {
		rows = append(rows, cli.Row{Cells: []string{phase.Name, widget.Lead(numbers[i][0], width), widget.Lead(numbers[i][1], width)}})
	}
	untimed := ""
	if report.Untimed > 0 {
		untimed = page.Glyph(cli.Warn) + " " + strconv.Itoa(report.Untimed) + " steps: evaluate, settle and act read 0"
	}
	lines := append(append(page.Title("Browser bench", facts, verdict), ""), cli.Indent(page.Rows(rows)...)...)
	return append(append(lines, ""), cli.Indent(page.Facts([]cli.Fact{{Label: "untimed", Text: untimed}, {Label: "rows", Text: page.Path(report.RowsFile)}})...)...)
}
