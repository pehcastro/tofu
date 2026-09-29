package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"tofu/bench/browser/airbnb"
	"tofu/internal/browser"
	"tofu/internal/session"
	"tofu/internal/sys"
)

func main() {
	arm := flag.String("arm", "", "A, B1, B2 or C")
	out := flag.String("out", "", "the folder each run writes into")
	tofu := flag.String("tofu", "tofu", "the installed tofu binary")
	flag.Parse()
	if err := run(airbnb.Arm(*arm), *out, *tofu); err != nil {
		fmt.Fprintln(os.Stderr, "airbnb bench:", err)
		os.Exit(1)
	}
}

func closeTofuTabs() (int, error) {
	home, err := os.UserHomeDir()
	var client *browser.Client
	if err == nil {
		client, err = browser.Dial(home)
	}
	if err != nil {
		return 0, err
	}
	defer func() { _ = client.Close() }()
	tabs, err := client.Tabs()
	closed := 0
	for _, tab := range tabs {
		if err == nil && tab.Opened {
			if err = client.CloseTab(tab.ID); err == nil {
				closed++
			}
		}
	}
	return closed, err
}

func run(arm airbnb.Arm, out, tofu string) error {
	settings, err := arm.Settings()
	if err != nil {
		return err
	}
	task, err := airbnb.Load()
	if err != nil {
		return err
	}
	if out == "" {
		return fmt.Errorf("-out names the folder the run writes into")
	}
	started := time.Now()
	dir, err := filepath.Abs(filepath.Join(out, string(arm)+"-"+started.Format("20060102-150405")))
	if err != nil {
		return err
	}
	project := filepath.Join(dir, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		return err
	}
	log, err := os.Create(filepath.Join(dir, "tofu.log"))
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()
	tofuIn := func(args ...string) error {
		command := exec.Command(tofu, args...)
		command.Dir, command.Stdout, command.Stderr = project, io.MultiWriter(os.Stdout, log), io.MultiWriter(os.Stderr, log)
		return command.Run()
	}
	set := [][2]string{{"browser", "drive"}, {"browserDriver", settings.Driver}}
	if settings.BrowserModel != "" {
		set = append(set, [2]string{"browserModel", settings.BrowserModel})
	}
	for _, setting := range set {
		if err := tofuIn("settings", "set", "--scope", "project", setting[0], setting[1]); err != nil {
			return fmt.Errorf("setting %s in the project: %w", setting[0], err)
		}
	}
	closed, err := closeTofuTabs()
	if err != nil {
		return fmt.Errorf("closing the tabs an earlier arm opened, before arm %s: %w", arm, err)
	}
	ranErr := tofuIn("run", "--dir", project, "--model", airbnb.MainModel, task.Prompt)
	state, err := sys.ProjectStateDirAt(project)
	if err != nil {
		return err
	}
	store := session.OpenAt(state)
	head, err := store.Head()
	if err != nil {
		return fmt.Errorf("the run left no session (tofu run: %v): %w", ranErr, err)
	}
	recorded, err := airbnb.RunFromEvents(arm, filepath.Join(store.Dir(head.ID), "events.jsonl"))
	if err != nil {
		return err
	}
	recorded.TabsClosed = closed
	machine, _ := os.Hostname()
	if err := arm.Stamp(&recorded, started.Format(time.DateOnly), machine); err != nil {
		return err
	}
	if err := airbnb.SaveRun(dir, recorded); err != nil {
		return err
	}
	table, err := airbnb.Render([]airbnb.Row{airbnb.Score(task, recorded)})
	if err != nil {
		return err
	}
	fmt.Printf("session %s, run folder %s, tofu run exit: %v\n\n%s", head.ID, dir, ranErr, table)
	return nil
}
