package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"tofu/internal/browser"
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
	takesOperand := verb == "open" || verb == "close"
	tabID, badID := strconv.Atoi(operand)
	if err != nil || flags.NArg() > 2 || takesOperand != (operand != "") || (verb == "close" && badID != nil) {
		_, _ = fmt.Fprintln(errOut, "usage: tofu browser [install | uninstall | open <url> | close <tab id>]")
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
	_, _ = fmt.Fprintf(errOut, "tofu browser: unknown argument %q: use install, uninstall, open, close, or nothing\n", verb)
	return exitUsage
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
