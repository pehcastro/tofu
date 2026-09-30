package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tofu/bench/browser/airbnb"
	"tofu/bench/browser/tasks"
	"tofu/internal/browser"
	"tofu/internal/session"
	"tofu/internal/shell"
	"tofu/internal/sys"
)

func main() {
	arm := flag.String("arm", "", "A, B1, B2, B3, B4 or C")
	browserModel := flag.String("browser-model", "", "a browser sub-agent arm on this source/model instead of a named arm")
	out := flag.String("out", "", "the folder each run writes into")
	tofu := flag.String("tofu", "tofu", "the installed tofu binary")
	again := flag.String("rescore", "", "a run folder to score again from its sessions, running nothing")
	maxWall := flag.Duration("max-wall", 20*time.Minute, "end the tofu run process tree at this wall time and score what it reached")
	taskName := flag.String("task", "airbnb", "airbnb, books, herokuapp, wikipedia, flights, youtube, npm or imdb")
	seed := flag.Int64("seed", 0, "draws the task's values; airbnb with seed 0 is the fixed task")
	flag.Parse()
	task, err := taskNamed(*taskName, *seed, time.Now())
	switch {
	case *again != "":
		err = rescore(*taskName, *again)
	case err != nil:
	case *browserModel != "" && *arm != "":
		err = fmt.Errorf("-arm %s and -browser-model %s both name the arm: give one", *arm, *browserModel)
	case *browserModel != "":
		err = run(task, airbnb.BrowserArm(*browserModel), *out, *tofu, *maxWall)
	default:
		err = run(task, airbnb.Arm(*arm), *out, *tofu, *maxWall)
	}
	if err != nil {
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

func tofuAt(tofu, project string, log io.Writer, args ...string) error {
	command := exec.Command(tofu, args...)
	command.Dir, command.Stdout, command.Stderr = project, io.MultiWriter(os.Stdout, log), io.MultiWriter(os.Stderr, log)
	return command.Run()
}

const pipesDrainAfterTheTreeEnds = 5 * time.Second

func tofuCapped(tofu, project string, log io.Writer, maxWall time.Duration, args ...string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), maxWall)
	defer cancel()
	command := exec.CommandContext(ctx, tofu, args...)
	command.Dir, command.Stdout, command.Stderr = project, io.MultiWriter(os.Stdout, log), io.MultiWriter(os.Stderr, log)
	command.WaitDelay = pipesDrainAfterTheTreeEnds
	var tree shell.Tree
	endTree := sync.OnceFunc(func() { tree.Release() })
	command.Cancel = func() error {
		endTree()
		return nil
	}
	tree, err := shell.StartTracked(command)
	if err != nil {
		return false, err
	}
	defer endTree()
	err = command.Wait()
	return errors.Is(ctx.Err(), context.DeadlineExceeded), err
}

func configure(arm airbnb.Arm, tofuIn func(args ...string) error) error {
	settings, err := arm.Settings()
	if err != nil {
		return err
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
	return nil
}

func run(task benchTask, arm airbnb.Arm, out, tofu string, maxWall time.Duration) error {
	if _, err := arm.Settings(); err != nil {
		return err
	}
	if out == "" {
		return fmt.Errorf("-out names the folder the run writes into")
	}
	started := time.Now()
	folder := arm.Folder() + "-" + started.Format("20060102-150405")
	if task.name != "airbnb" {
		folder = task.name + "-" + folder
	}
	if task.seed != 0 {
		folder += fmt.Sprintf("-seed%d", task.seed)
	}
	dir, err := filepath.Abs(filepath.Join(out, folder))
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
	tofuIn := func(args ...string) error { return tofuAt(tofu, project, log, args...) }
	if err := configure(arm, tofuIn); err != nil {
		return err
	}
	closed, err := closeTofuTabs()
	if err != nil {
		return fmt.Errorf("closing the tabs an earlier arm opened, before arm %s: %w", arm, err)
	}
	capped, ranErr := tofuCapped(tofu, project, log, maxWall, "run", "--dir", project, "--model", airbnb.MainModel, task.prompt)
	machine, _ := os.Hostname()
	if err := score(task, dir, airbnb.Run{Arm: arm, Seed: task.seed, TabsClosed: closed, Capped: capped, Conditions: airbnb.Conditions{Date: started.Format(time.DateOnly), Machine: machine}}); err != nil {
		return fmt.Errorf("tofu run: %v, then %w", ranErr, err)
	}
	fmt.Printf("tofu run exit: %v, capped at %s: %v\n", ranErr, maxWall, capped)
	return nil
}

type benchTask struct {
	name   string
	seed   int64
	prompt string
	score  func(recorded airbnb.Run, lineage []string) (airbnb.Row, error)
}

func taskNamed(name string, seed int64, drawnOn time.Time) (benchTask, error) {
	if name == "airbnb" {
		task, err := airbnb.Load()
		if seed != 0 {
			task = airbnb.Draw(seed, drawnOn)
		}
		return benchTask{name, seed, task.Prompt, func(recorded airbnb.Run, _ []string) (airbnb.Row, error) { return airbnb.Score(task, recorded), nil }}, err
	}
	task, err := tasks.Named(name, seed, drawnOn)
	return benchTask{name, seed, task.Prompt, func(recorded airbnb.Run, lineage []string) (airbnb.Row, error) {
		evidence, err := tasks.Read(recorded.Arm, lineage...)
		evidence.Run = recorded
		return tasks.Score(task, evidence), err
	}}, err
}

func rescore(name, dir string) error {
	before, err := airbnb.LoadRun(dir)
	if err != nil {
		return err
	}
	drawnOn, err := time.Parse(time.DateOnly, before.Conditions.Date)
	if err != nil {
		return fmt.Errorf("%s: the run's date %q draws no task: %w", dir, before.Conditions.Date, err)
	}
	task, err := taskNamed(name, before.Seed, drawnOn)
	if err != nil {
		return err
	}
	return score(task, dir, before)
}

func score(task benchTask, dir string, before airbnb.Run) error {
	state, err := sys.ProjectStateDirAt(filepath.Join(dir, "project"))
	if err != nil {
		return err
	}
	lineage, err := airbnb.Lineage(session.SessionsDir(state))
	if err != nil {
		return fmt.Errorf("the run left no session to read: %w", err)
	}
	recorded, err := airbnb.RunFromEvents(before.Arm, lineage...)
	if err != nil {
		return err
	}
	recorded.TabsClosed, recorded.Capped, recorded.Seed = before.TabsClosed, before.Capped, before.Seed
	if err := before.Arm.Stamp(&recorded, before.Conditions.Date, before.Conditions.Machine); err != nil {
		return err
	}
	if err := airbnb.SaveRun(dir, recorded); err != nil {
		return err
	}
	row, err := task.score(recorded, lineage)
	if err != nil {
		return err
	}
	table, err := airbnb.Render([]airbnb.Row{row})
	if err != nil {
		return err
	}
	fmt.Printf("task %s, run folder %s, %d sessions read, root first:\n%s\n\n%s", task.name, dir, len(lineage), strings.Join(lineage, "\n"), table)
	return nil
}
