package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func signedIn(exe, home, dir string, env []string, batch int) error {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	for _, step := range []struct {
		stdin string
		args  []string
	}{
		{mockLoginCode + "\n", []string{"login", "llm", "claude-sub", "--paste"}},
		{"", []string{"models", "bind", "classifier", "typesafe/jev-latest"}},
		{"", []string{"models", "bind", "orchestrator", model}},
		{"", []string{"settings", "set", "subAgentsPerTurn", strconv.Itoa(batch)}},
	} {
		if err := tofu(exe, dir, env, step.stdin, step.args...); err != nil {
			return err
		}
	}
	return nil
}

func live(args []string) error {
	flags := flag.NewFlagSet(liveArm, flag.ExitOnError)
	exe := flags.String("tofu", "", "the tofu under measurement; built from this repository into -work when empty")
	work := flags.String("work", "", "a short folder the build, the fresh home, the project, the profiles and the rows go under; a fresh temp folder when empty")
	minutes := flags.Int("minutes", 3, "the window of the message phase and of the work phase")
	settle := flags.Duration("settle", 30*time.Second, "time from launch before the seed phase opens, not measured")
	out := flags.String("out", "", "the jsonl the rows are appended to; live450.jsonl under -work when empty")
	_ = flags.Parse(args)
	if *work == "" {
		made, err := os.MkdirTemp("", "ls")
		if err != nil {
			return err
		}
		*work = made
	}
	if *exe == "" {
		built, err := build(*work)
		if err != nil {
			return err
		}
		*exe = built
	}
	if *out == "" {
		*out = filepath.Join(*work, liveArm+".jsonl")
	}
	home, dir := filepath.Join(*work, "template"), filepath.Join(*work, "project")
	if err := project(dir); err != nil {
		return err
	}
	server, err := startStub(0, 0, 0, workPace)
	if err != nil {
		return err
	}
	if err := signedIn(*exe, home, dir, freshEnv(home, server.env()...), spawnBatch); err != nil {
		return err
	}
	fmt.Printf("%s: tofu %s, fresh home %s signed in to a mock claude-sub with the orchestrator bound to %s, project %s\n", liveArm, *exe, home, model, dir)
	if err := measure([]string{"-arm", liveArm, "-tofu", *exe, "-template", home, "-project", dir, "-work", *work, "-phases", "seed,message,work",
		"-minutes", strconv.Itoa(*minutes), "-settle", settle.String(), "-out", *out}); err != nil {
		return err
	}
	return report([]string{*out})
}

func build(work string) (string, error) {
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("live450 builds tofu from the repository it runs in: %w", err)
	}
	exe := filepath.Join(work, "tofu.exe")
	cmd := exec.Command("go", "build", "-trimpath", "-p", buildProcs, "-o", exe, "./cmd/tofu")
	cmd.Dir, cmd.Env = strings.TrimSpace(string(root)), append(os.Environ(), "GOMAXPROCS="+buildProcs)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build ./cmd/tofu: %v: %s", err, out)
	}
	return exe, nil
}
