package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

type GitHubPRDiff struct {
	root turn.Root
}

func NewGitHubPRDiff(dir string) (GitHubPRDiff, error) {
	root, err := turn.NewRoot(dir)
	return GitHubPRDiff{root: root}, err
}

func (GitHubPRDiff) Name() string { return "github_pr_diff" }

func (GitHubPRDiff) Definition() llm.Tool {
	return llm.Tool{
		Name: "github_pr_diff",
		Description: "returns the unified diff of a github pull request by running the gh command line, which already holds the owner's credential: " +
			"this tool never calls the github api itself and never holds a token. " +
			"name the pull request by its number, by its full github.com pull request url, or leave it out for the pull request open on the current branch. " +
			"gh missing and gh not signed in each come back as a refusal naming which one it was and what to run, rather than a wall of standard error. " +
			"a diff too large for one result is handed back as an artifact instead of being cut",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pr": map[string]any{
					"type":        "string",
					"description": "the pull request's number or its full github.com url; leave it out for the pull request open on the current branch",
				},
			},
		},
	}
}

func (t GitHubPRDiff) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		PR string `json:"pr"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return turn.Result{}, fmt.Errorf("github_pr_diff: arguments are not the expected shape: %w", err)
		}
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return turn.Result{}, errors.New("github_pr_diff: gh is not installed: install the GitHub CLI, github.com/cli/cli, so this tool can shell out to it")
	}

	pr := strings.TrimSpace(args.PR)
	words := []string{"pr", "diff", "--color", "never"}
	if pr != "" {
		words = append(words, pr)
	}
	cmd := exec.CommandContext(ctx, "gh", words...)
	cmd.Dir, cmd.Env = string(t.root), append(os.Environ(), sys.ScratchEnv(ctx)...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	command := "gh " + strings.Join(words, " ")

	if runErr != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if strings.Contains(message, "gh auth login") || strings.Contains(message, "not logged into any GitHub hosts") {
			return turn.Result{}, errors.New("github_pr_diff: gh is not authenticated: run gh auth login")
		}
		if message == "" {
			message = runErr.Error()
		}
		return turn.Result{}, fmt.Errorf("github_pr_diff: %s failed: %s", command, message)
	}

	diff := stdout.String()
	if strings.TrimSpace(diff) == "" {
		return turn.Result{Content: command + " printed no diff", Command: command}, nil
	}
	return turn.Result{
		Content: fmt.Sprintf("%s printed a %d byte diff\n\n%s", command, len(diff), diff),
		Command: command,
	}, nil
}
