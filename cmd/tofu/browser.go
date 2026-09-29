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
	asJSON := slices.Contains(args, jsonFlag)
	args = slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == jsonFlag })
	flags := flag.NewFlagSet("browser", flag.ContinueOnError)
	flags.SetOutput(errOut)
	hostsKey := flags.String("hosts-key", browser.ChromeHostsKey, "")
	err := flags.Parse(args)
	verb, operand := flags.Arg(0), flags.Arg(1)
	page := cli.Detect(out, os.Environ())
	o := browserOutput{page: page, asJSON: asJSON, verb: strings.TrimSpace("browser " + verb), out: out, errOut: errOut}
	if err == nil && verb == "bench" {
		return o.bench(flags.Args()[1:])
	}
	takesOperand := verb == "open" || verb == "close"
	tabID, badID := strconv.Atoi(operand)
	if err != nil || flags.NArg() > 2 || takesOperand != (operand != "") || (verb == "close" && badID != nil) {
		_, _ = fmt.Fprintln(errOut, "usage: tofu browser [install | uninstall | open <url> | close <tab id> | bench [--jev] [--n 12] [--rows file]] [--json]")
		return exitUsage
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return o.fail(err)
	}
	switch verb {
	case "":
		var tabs []browser.Tab
		var builds *browser.Builds
		err := withBrowser(home, func(client *browser.Client) (err error) {
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
		if err := withBrowser(home, func(client *browser.Client) (err error) { tab, err = client.Open(operand); return err }); err != nil {
			return o.fail(err)
		}
		return o.show(struct {
			Tab int    `json:"tab"`
			URL string `json:"url"`
		}{tab, operand}, []string{page.Receipt(cli.Added, "opened tab "+strconv.Itoa(tab), operand)})
	case "close":
		if err := withBrowser(home, func(client *browser.Client) error { return client.CloseTab(tabID) }); err != nil {
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
	_, _ = fmt.Fprintf(errOut, "tofu browser: unknown argument %q: use install, uninstall, open, close, bench, or nothing\n", verb)
	return exitUsage
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
	err = withBrowser(home, func(client *browser.Client) error {
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

func withBrowser(home string, use func(*browser.Client) error) error {
	client, err := browser.Dial(home)
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
