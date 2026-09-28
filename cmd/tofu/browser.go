package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"tofu/bench/browser/steps"
	"tofu/internal/browser"
	"tofu/internal/browser/jevloop"
	"tofu/internal/sys"
)

const browserInstallSteps = `
Then, once, by hand:
  1. open chrome://extensions and turn on Developer mode
  2. choose Load unpacked, pick the folder above, and check the id matches
  3. pin the tofu icon; on a tab you want to share, click it and choose Read or Drive
`

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
	flags := flag.NewFlagSet("browser", flag.ContinueOnError)
	flags.SetOutput(errOut)
	hostsKey := flags.String("hosts-key", browser.ChromeHostsKey, "")
	err := flags.Parse(args)
	verb, operand := flags.Arg(0), flags.Arg(1)
	if err == nil && verb == "bench" {
		return browserBench(flags.Args()[1:], out, errOut)
	}
	takesOperand := verb == "open" || verb == "close"
	tabID, badID := strconv.Atoi(operand)
	if err != nil || flags.NArg() > 2 || takesOperand != (operand != "") || (verb == "close" && badID != nil) {
		_, _ = fmt.Fprintln(errOut, "usage: tofu browser [install | uninstall | open <url> | close <tab id> | bench [--jev] [--n 12] [--rows file]]")
		return exitUsage
	}
	home, err := os.UserHomeDir()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu browser: %v\n", err)
		return exitVerdict
	}
	switch verb {
	case "":
		return withBrowser(home, errOut, func(client *browser.Client) error {
			tabs, err := client.Tabs()
			if len(tabs) == 0 && err == nil {
				_, _ = fmt.Fprintln(out, "the extension is connected and no tab is shared: click the tofu icon on a tab and choose Read or Drive")
			}
			for _, tab := range tabs {
				_, _ = fmt.Fprintf(out, "%-8d %-5s  %s  %s\n", tab.ID, tab.Mode, tab.Title, tab.URL)
			}
			return err
		})
	case "open":
		return withBrowser(home, errOut, func(client *browser.Client) error {
			tab, err := client.Open(operand)
			if err == nil {
				_, _ = fmt.Fprintln(out, tab)
			}
			return err
		})
	case "close":
		return withBrowser(home, errOut, func(client *browser.Client) error { return client.CloseTab(tabID) })
	case "install":
		exe, err := os.Executable()
		id := ""
		if err == nil {
			id, err = browser.Install(home, exe, *hostsKey)
		}
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "tofu browser install: %v\n", err)
			return exitVerdict
		}
		_, _ = fmt.Fprintf(out, "installed the Chrome native host for %s\n  extension folder  %s\n  extension id      %s\n%s",
			exe, browser.ExtensionDir(home), id, browserInstallSteps)
		return exitOK
	case "uninstall":
		if err := browser.Uninstall(home, *hostsKey); err != nil {
			_, _ = fmt.Fprintf(errOut, "tofu browser uninstall: %v\n", err)
			return exitVerdict
		}
		_, _ = fmt.Fprintln(out, "removed the Chrome native host and the unpacked extension; remove the extension from chrome://extensions too")
		return exitOK
	}
	_, _ = fmt.Fprintf(errOut, "tofu browser: unknown argument %q: use install, uninstall, open, close, bench, or nothing\n", verb)
	return exitUsage
}

func browserBench(args []string, out, errOut io.Writer) int {
	moves := steps.Script()
	flags := flag.NewFlagSet("browser bench", flag.ContinueOnError)
	flags.SetOutput(errOut)
	withJev := flags.Bool("jev", false, "")
	n := flags.Int("n", len(moves), "")
	rowsPath := flags.String("rows", "", "")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 || *n < 1 || *n > len(moves) {
		_, _ = fmt.Fprintf(errOut, "usage: tofu browser bench [--jev] [--n 1 to %d] [--rows file]\n", len(moves))
		return exitUsage
	}
	home, err := os.UserHomeDir()
	var judge *jevloop.Jev
	if err == nil && *withJev {
		judge = &jevloop.Jev{}
		*judge, err = browserJudge(".")
	}
	var url string
	stop := func() error { return nil }
	if err == nil {
		url, stop, err = steps.Serve()
	}
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu browser bench: %v\n", err)
		return exitVerdict
	}
	defer func() { _ = stop() }()
	if *rowsPath == "" {
		*rowsPath = filepath.Join(home, sys.StateDirName, "bench", "browser-steps-"+time.Now().Format("20060102-150405")+".jsonl")
	}
	return withBrowser(home, errOut, func(client *browser.Client) error {
		tab, err := client.Open(url)
		if err != nil {
			return err
		}
		rows := steps.Run(context.Background(), browser.SharedTab{Client: client, ID: tab}, judge, moves[:*n])
		var written bytes.Buffer
		err = errors.Join(client.CloseTab(tab), steps.Write(&written, rows))
		if err == nil {
			err = sys.WriteFile(*rowsPath, written.Bytes(), 0o644)
		}
		if err == nil {
			err = steps.Table(out, rows)
		}
		_, _ = fmt.Fprintf(out, "rows in %s\n", *rowsPath)
		return err
	})
}

func withBrowser(home string, errOut io.Writer, use func(*browser.Client) error) int {
	client, err := browser.Dial(home)
	if err == nil {
		defer func() { _ = client.Close() }()
		err = use(client)
	}
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu browser: %v\n", err)
		return exitUsage
	}
	return exitOK
}
