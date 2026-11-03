package main

import (
	"regexp"
	"slices"
	"strings"
)

type bucket string

const (
	bucketAPI       bucket = "API calls"
	bucketBuild     bucket = "builds"
	bucketTest      bucket = "tests"
	bucketScript    bucket = "scripts and app runs"
	bucketOtherBash bucket = "other bash"
	bucketBashRead  bucket = "bash read and search"
	bucketWait      bucket = "sleep, poll and unmatched shell waits"
	bucketSearch    bucket = "native search, glob, grep"
	bucketGit       bucket = "git"
	bucketFile      bucket = "read, edit, write"
	bucketWeb       bucket = "web and browser"
	bucketOtherTool bucket = "other tools"
	bucketSubAgent  bucket = "waiting on sub-agents, background tasks, wakeups"
	bucketPerson    bucket = "waiting on the person"
	bucketIdle      bucket = "idle gaps over 15 minutes"
)

var counted = []bucket{bucketAPI, bucketBuild, bucketTest, bucketScript, bucketOtherBash, bucketBashRead, bucketWait, bucketSearch, bucketGit, bucketFile, bucketWeb, bucketOtherTool}

var excluded = []bucket{bucketSubAgent, bucketPerson, bucketIdle}

type commandKind struct {
	kind    bucket
	pattern *regexp.Regexp
}

var commandKinds = []commandKind{
	{bucketTest, regexp.MustCompile(`^(go test|cargo (test|nextest)|npm (run )?test|pnpm (run )?test|yarn test|bun test|pytest|python -m pytest|vitest|jest|npx (vitest|jest|playwright))\b`)},
	{bucketBuild, regexp.MustCompile(`^(go (build|vet|install|generate|mod|tool golangci-lint|tool deadcode)|golangci-lint|gofmt|cargo (build|check|clippy|fmt)|npm (run )?build|pnpm (run )?build|bun (run )?build|tsc|make|cmake|dotnet build|staticcheck)\b`)},
	{bucketScript, regexp.MustCompile(`^((go run|cargo run|node|python3?|pwsh|powershell|bash|sh|npx|npm (run|start|exec)|pnpm|bun|deno|tofu|curl)\b|\./|\.\\|[^ ]+\.(exe|sh|ps1|py|js)\b)`)},
	{bucketGit, regexp.MustCompile(`^(git|gh)\b`)},
	{bucketWait, regexp.MustCompile(`^(sleep|start-sleep|timeout /t|while|until|wait)\b`)},
	{bucketBashRead, regexp.MustCompile(`^((cat|head|tail|sed -n|grep|rg|find|ls|dir|wc|awk|tree|stat|file|diff|less|more|echo|printf|pwd|which|where|type|get-content|get-childitem|select-string)\b|test -|\[)`)},
}

var commandPrefix = regexp.MustCompile(`^(rtk (proxy )?|timeout \d+[smh]? |time |env |[a-z_][a-z0-9_]*=[^ ]* )`)

var commandSeparator = regexp.MustCompile(`&&|\|\||;|\n`)

func commandBucket(command string) bucket {
	heaviest := len(commandKinds)
	for _, part := range commandSeparator.Split(strings.ToLower(command), -1) {
		part = strings.TrimSpace(strings.SplitN(part, "|", 2)[0])
		for trimmed := strings.TrimSpace(commandPrefix.ReplaceAllString(part, "")); trimmed != part; trimmed = strings.TrimSpace(commandPrefix.ReplaceAllString(part, "")) {
			part = trimmed
		}
		if at := slices.IndexFunc(commandKinds, func(known commandKind) bool { return known.pattern.MatchString(part) }); at >= 0 {
			heaviest = min(heaviest, at)
		}
	}
	if heaviest == len(commandKinds) {
		return bucketOtherBash
	}
	return commandKinds[heaviest].kind
}

func toolBucket(tool string) bucket {
	name := strings.ToLower(tool)
	switch name {
	case "read", "edit", "write", "multiedit", "notebookedit", "quote", "artifact_fetch":
		return bucketFile
	case "search", "glob", "grep", "symbols", "zoom", "gosymbol", "toolsearch":
		return bucketSearch
	case "test":
		return bucketTest
	case "typecheck":
		return bucketBuild
	case "fetch", "web_search", "webfetch", "websearch", "github_pr_diff":
		return bucketWeb
	case "spawn", "ask", "task", "agent", "taskoutput":
		return bucketSubAgent
	case "ask_person", "askuserquestion":
		return bucketPerson
	case "bashoutput":
		return bucketWait
	case "killshell", "killbash":
		return bucketOtherBash
	}
	if strings.HasPrefix(name, "browser") {
		return bucketWeb
	}
	return bucketOtherTool
}
