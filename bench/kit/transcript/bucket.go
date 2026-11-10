package transcript

import (
	"regexp"
	"slices"
	"strings"
)

type Bucket string

const (
	Test      Bucket = "tests"
	Build     Bucket = "builds"
	Script    Bucket = "scripts and app runs"
	Git       Bucket = "git"
	Wait      Bucket = "sleep, poll and unmatched shell waits"
	BashRead  Bucket = "bash read and search"
	OtherBash Bucket = "other bash"
)

type commandKind struct {
	kind    Bucket
	pattern *regexp.Regexp
}

var commandKinds = []commandKind{
	{Test, regexp.MustCompile(`^(go test|cargo (test|nextest)|npm (run )?test|pnpm (run )?test|yarn test|bun test|pytest|python -m pytest|vitest|jest|npx (vitest|jest|playwright))\b`)},
	{Build, regexp.MustCompile(`^(go (build|vet|install|generate|mod|tool golangci-lint|tool deadcode)|golangci-lint|gofmt|cargo (build|check|clippy|fmt)|npm (run )?build|pnpm (run )?build|bun (run )?build|(bun x |bunx |npx )?tsc|make|cmake|dotnet build|staticcheck)\b`)},
	{Script, regexp.MustCompile(`^((go run|cargo run|node|python3?|pwsh|powershell|bash|sh|npx|npm (run|start|exec)|pnpm|bun|deno|tofu|curl)\b|\./|\.\\|[^ ]+\.(exe|sh|ps1|py|js)\b)`)},
	{Git, regexp.MustCompile(`^(git|gh)\b`)},
	{Wait, regexp.MustCompile(`^(sleep|start-sleep|timeout /t|while|until|wait)\b`)},
	{BashRead, regexp.MustCompile(`^((cat|head|tail|sed -n|grep|rg|find|ls|dir|wc|awk|tree|stat|file|diff|less|more|echo|printf|pwd|which|where|type|get-content|get-childitem|select-string)\b|test -|\[)`)},
}

var commandPrefix = regexp.MustCompile(`^(rtk (proxy )?|timeout \d+[smh]? |time |env |[a-z_][a-z0-9_]*=[^ ]* )`)

var commandSeparator = regexp.MustCompile(`&&|\|\||;|\n`)

func CommandBucket(command string) Bucket {
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
		return OtherBash
	}
	return commandKinds[heaviest].kind
}
