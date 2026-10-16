package guard

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"tofu/internal/sys"
)

var (
	interpreterReadsScript = sys.LazyRegexp(`(?is)\b(python[23]?(\.exe)?|node|perl|ruby|bun|deno|php|powershell|pwsh)\b[^\r\n|]*(<<-?\s*['"]?[A-Za-z_]|\s-\s|\s-[cep]\s|\s-[cep]['"])`)
	scriptWritesGo         = sys.LazyRegexp(`(?is)(\.go['"]\s*,\s*['"][wax]|\.go['"]\s*\)\s*\.\s*write|writeFileSync\s*\(\s*['"][^'"\r\n]*\.go['"]|Set-Content[^\r\n]{0,60}\.go['"]|['"][^'"\r\n]*\.go['"][^\r\n]{0,40}\bmode\s*=\s*['"][wax])`)
	scriptOpensForWrite    = sys.LazyRegexp(`(?is)\bopen\s*\([^)\r\n]{0,120},\s*['"][wax]`)
	goWord                 = sys.LazyRegexp(`(?i)\.go\b`)
	shellWritesSource      = sys.LazyRegexp(`(?i)((^|[;&|(\n]|\$\()\s*((echo|printf|cat)\b[^;&|\n]*)?>>?\s*\S+\.(go|ya?ml|json)\b|\btee\s+(-a\s+)?\S+\.(go|ya?ml|json)\b|\b(cp|mv)\s+\S+\s+\S*\.(go|ya?ml|json)\b)`)
	sourceFile             = sys.LazyRegexp(`(?i)\.(go|ya?ml|json)\b`)
	gitChangesTree         = sys.LazyRegexp(`(?i)` + gitThen + `(reset|checkout|restore|clean|stash|revert|merge|rebase|switch|add|commit|rm|mv|push|pull|filter-branch|gc|prune|update-ref|branch\s+-[dD]|tag\s+-d)` + wordEnds)
	gitDiscardsWork        = sys.LazyRegexp(`(?i)` + gitThen + `(reset\s+(--hard|--merge|--keep)|checkout\s+(--|\.)|restore\b|clean\s+-[a-z]*[fdx]|filter-branch|update-ref)`)
	gitStash               = sys.LazyRegexp(`(?i)` + gitThen + `stash\b`)
	tofuRunsAtCommand      = sys.LazyRegexp(`(?im)(^|[;&|(\x60]|\$\()\s*[^\s;&|]*\b(tofu|boji)(\.exe)?["']?\s+run\b`)
	exeRuns                = sys.LazyRegexp(`(?i)\S+\.exe["']?\s+run\s`)
	wireOpenrouter         = sys.LazyRegexp(`(?i)--wire[=\s]+openrouter`)
	dryRun                 = sys.LazyRegexp(`(?i)--dry-run`)
	liveCost               = sys.LazyRegexp(`(?i)\b(TOFU|BOJI)_LIVE_COST\b`)
	gitWorktree            = sys.LazyRegexp(`(?i)` + gitThen + `worktree\b`)
	goTestPattern          = sys.LazyRegexp(`(?i)go\s+test\b[^&|;]*/\.\.\.`)
	parallelBound          = sys.LazyRegexp(`(?i)-p[= ]\d`)
	treeWide               = []func() *regexp.Regexp{
		sys.LazyRegexp(`(?i)go\s+(test|build|vet)\s+\./\.\.\.`),
		sys.LazyRegexp(`(?i)\bgofmt\s+-w\s+\.`),
		sys.LazyRegexp(`(?i)\bgo\s+run\b.*\bserve\b`),
	}
	gitPublishes = sys.LazyRegexp(`(?i)` + gitThen + `(add|commit|push)` + wordEnds)
)

const (
	gitThen  = `\bgit(\s+(-c\s*("[^"]*"|'[^']*'|\S+)|--(git-dir|work-tree|namespace|exec-path|config-env|super-prefix)(=|\s+)\S+|-[-\w]+))*\s+`
	wordEnds = `([^\w-]|$)`
)

func shell(env Env, ev event) (Answer, error) {
	command := ev.ToolInput.Command
	if blank(command) {
		return Answer{}, nil
	}
	stripped := stripHeredocs(command)
	if reason := flowsRefusal(env, stripped); reason != "" {
		return deny(reason), nil
	}
	reason, err := shellRefusal(env, ev, command, stripped)
	if reason != "" {
		return deny(reason), nil
	}
	if err != nil {
		return Answer{}, err
	}
	if gitPublishes().MatchString(stripped) {
		return refuse(0, "NOTE: git add, commit and push need the owner's explicit approval in this turn. No agent, bot or harness is ever a co-author."), nil
	}
	return Answer{}, nil
}

func shellRefusal(env Env, ev event, command, stripped string) (string, error) {
	switch {
	case interpreterReadsScript().MatchString(command) && (scriptWritesGo().MatchString(command) || scriptOpensForWrite().MatchString(command) && goWord().MatchString(command)):
		return interpreterRefusal, nil
	case shellWritesSource().MatchString(stripped) || sedEditsSource(stripped):
		return shellWriteRefusal, nil
	case gitChangesTree().MatchString(stripped):
		if !blank(ev.AgentID) {
			return subagentGitRefusal, nil
		}
		if gitDiscardsWork().MatchString(stripped) || stashes(stripped) {
			return discardRefusal, nil
		}
		return "", nil
	case (tofuRunsAtCommand().MatchString(stripped) || exeRuns().MatchString(stripped)) && wireOpenrouter().MatchString(stripped) && !dryRun().MatchString(stripped):
		return openrouterRefusal, nil
	case liveCost().MatchString(stripped) && !dryRun().MatchString(stripped):
		return liveCostRefusal, nil
	case gitWorktree().MatchString(stripped):
		return worktreeRefusal, nil
	case goTestPattern().MatchString(stripped) && !parallelBound().MatchString(stripped):
		return unboundedTestRefusal, nil
	case anyMatch(treeWide, stripped):
		return treeWideRefusal(env)
	}
	return "", nil
}

func stashes(command string) bool {
	for _, at := range gitStash().FindAllStringIndex(command, -1) {
		rest := command[at[1]:]
		after := strings.ToLower(strings.TrimLeftFunc(rest, unicode.IsSpace))
		readsOnly := len(after) < len(rest) && (strings.HasPrefix(after, "list") || strings.HasPrefix(after, "show"))
		if !readsOnly {
			return true
		}
	}
	return false
}

func sedEditsSource(command string) bool {
	for _, words := range shellWords(command) {
		for at, word := range words {
			if (word == "sed" || strings.HasSuffix(word, "/sed")) && sedTargetsSource(words[at+1:], slices.Contains(words[:at], "xargs")) {
				return true
			}
			if strings.ContainsAny(word, " \t\n") && sedEditsSource(word) {
				return true
			}
		}
	}
	return false
}

func sedTargetsSource(args []string, filesFromStdin bool) bool {
	inPlace, script := false, true
	var files []string
	for i := 0; i < len(args); i++ {
		word := args[i]
		switch {
		case word == "":
		case word == "-e" || word == "-f" || word == "--expression" || word == "--file":
			script = false
			i++
		case strings.HasPrefix(word, "--expression=") || strings.HasPrefix(word, "--file="):
			script = false
		case strings.HasPrefix(word, "--"):
			inPlace = inPlace || strings.HasPrefix(word, "--in-place")
		case strings.HasPrefix(word, "-") && len(word) > 1:
			flags, _, _ := strings.Cut(word[1:], ".")
			inPlace = inPlace || strings.ContainsRune(flags, 'i')
		case script:
			script = false
		default:
			files = append(files, word)
		}
	}
	maybeSource := func(file string) bool {
		return file == "{}" || strings.ContainsAny(file, "$`") || sourceFile().MatchString(file)
	}
	return inPlace && (filesFromStdin && len(files) == 0 || slices.ContainsFunc(files, maybeSource))
}

func anyMatch(patterns []func() *regexp.Regexp, text string) bool {
	for _, pattern := range patterns {
		if pattern().MatchString(text) {
			return true
		}
	}
	return false
}

func treeWideRefusal(env Env) (string, error) {
	tickets, err := ticketsAt(env).list(placeDoing)
	var goTickets []string
	for _, t := range tickets {
		if ownsGo(t.Owns) {
			goTickets = append(goTickets, t.ID)
		}
	}
	if len(goTickets) == 0 {
		return "", err
	}
	return "A tree-wide check is refused while a ticket in doing/ owns a Go path. Currently: " + strings.Join(goTickets, ", ") + `.

If you are an agent: verify what your ticket owns. go build ./internal/<your package>/... and the same for test and vet. Then prove each acceptance line with a command and its real output.

If you are the orchestrator: move the ticket to review/ first. That is the phase where the whole tree is checked, and an empty doing/, or one holding only non-Go paths, is what makes it safe to run one process over everything. Four agents running suites at once once made this machine unusable.`, err
}

func ownsGo(globs []string) bool {
	for _, glob := range globs {
		first, _, _ := strings.Cut(glob, "/")
		switch strings.ToLower(first) {
		case "cmd", "internal", "interface", "library", "bench":
			return true
		}
	}
	return false
}

func stripHeredocs(command string) string {
	var out strings.Builder
	for at := 0; at < len(command); {
		if end, found := heredocAt(command, at); found {
			out.WriteString("<<HEREDOC>>")
			at = end
			continue
		}
		out.WriteByte(command[at])
		at++
	}
	return out.String()
}

func heredocAt(command string, at int) (int, bool) {
	if !strings.HasPrefix(command[at:], "<<") {
		return 0, false
	}
	next := at + 2
	if next < len(command) && command[next] == '-' {
		next++
	}
	for next < len(command) && isSpace(command[next]) {
		next++
	}
	var quote byte
	if next < len(command) && (command[next] == '\'' || command[next] == '"') {
		quote = command[next]
		next++
	}
	if next >= len(command) || !isWordByte(command[next]) || command[next] >= '0' && command[next] <= '9' {
		return 0, false
	}
	start := next
	for next < len(command) && isWordByte(command[next]) {
		next++
	}
	if quote != 0 {
		if next >= len(command) || command[next] != quote {
			return 0, false
		}
		return heredocCloses(command, next+1, command[start:next])
	}
	for end := next; end > start; end-- {
		if closed, found := heredocCloses(command, end, command[start:end]); found {
			return closed, true
		}
	}
	return 0, false
}

func heredocCloses(command string, from int, token string) (int, bool) {
	for at := from; at < len(command); at++ {
		if command[at] != '\n' {
			continue
		}
		line := at + 1
		for line < len(command) && (command[line] == ' ' || command[line] == '\t') {
			line++
		}
		end := line + len(token)
		if strings.HasPrefix(command[line:], token) && (end == len(command) || !isWordByte(command[end])) {
			return end, true
		}
	}
	return 0, false
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
}

func isWordByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

const interpreterRefusal = `A source file may not be written by a script piped into an interpreter.

The heredoc strip in this hook exists so a ticket can quote a forbidden command without being refused for quoting it. A python, node or shell script read from stdin uses the same shape, and its body is stripped before any rule sees it, so a file write inside it passes every check: the em dash rule, the comment rule and the path ownership rule all run on Write and Edit, and none of them ran on this.

Found on 2026-09-18 by an agent that had already used the route three times and said so.

Use the Write or Edit tool. If a mechanical change really needs a script, write the script to a file first and run it from there, and say in your report that you did.`

const shellWriteRefusal = `Writing a source file through the shell is refused. Use the Write or Edit tool.

A redirect, a heredoc, tee, sed -i, cp and mv all reach the disk without passing the three checks that run on Write and Edit: the em dash block, the comment block, and the path ownership check. Four files went in that way today carrying comments the rule forbids, and nothing noticed until the next Write was refused.

The tools are not a formality. They are where the rules live.`

const subagentGitRefusal = `Git may not change this repository from a subagent.

Every agent works in one shared tree. There are no worktrees, on purpose: parallelism here is disjoint path ownership, and that only protects files, not git state. A single git command that rewrites the index or the working tree reaches every other agent's work at once, whatever their owns: list says.

On 2026-09-18 a git reset --hard ran mid-session and destroyed the uncommitted work of three accepted tickets: the ledger's new fields, the turn row's schema, and a bench verb's wiring. Untracked files survived, tracked edits did not, and nothing could recover them because they had never been committed.

Read-only git is fine: status, log, diff, show, ls-files, rev-parse, check-ignore.

If you need a clean tree, clone to a temporary directory. If you need something discarded or committed, say so in your report and the orchestrator does it.`

const discardRefusal = `A command that discards uncommitted work is refused, including for the orchestrator.

Two of these ran on 2026-09-18. A git reset --hard destroyed three accepted tickets' uncommitted work. A git stash -u, run by an agent that only wanted a baseline test count, reverted every uncommitted file in the tree and then failed to pop cleanly. Neither was recoverable, because none of it had been committed.

git stash is on this list for that reason. It reads as a safe parking spot and it is a tree-wide revert. Only git stash list and git stash show are allowed.

Commit first, then discard. If a single file must go, name it and run the narrow form by hand.`

const openrouterRefusal = `tofu run --wire openrouter spends real money and is refused.

OpenRouter is for Jev and nothing else. The owner's ruling, 2026-09-18, after the model spend reached $1.08 on Claude Fable and $0.07 on Opus through it, against $0.08 for every Jev call this project has ever made.

The default arm is the subscription and it is allowed: it spends quota the owner already pays for. This refusal is only the off arm, which exists so the subscription has something to be measured against, and running it needs the owner to say so in the turn that runs it.

tofu run --dry-run is allowed on either arm and spends nothing.`

const liveCostRefusal = `A command setting TOFU_LIVE_COST or BOJI_LIVE_COST is refused: it turns on live tests that spend money on the API key.

No model runs on the API key at all, opus included. The key is for Jev. A comparison against a frontier model is the vendor's own command line on the owner's subscription, driven the way a person drives it, and read from its raw output.

A ticket that believes it needs a paid model call says so in its report and stops. It does not ask an agent to decide.`

const worktreeRefusal = `git worktree is refused in this repository.

Parallelism here is disjoint path ownership, declared in a ticket's owns: list and checked before an agent starts. A worktree makes a second copy of the tree that nothing reconciles, and the owner's standing rule is against it.

If two pieces of work collide, the answer is to split the paths or to send the second piece to the agent that already holds them.`

const unboundedTestRefusal = `A go test over a /... pattern is refused without -p.

Measured on this machine on 2026-09-25, over ./bench/...:

  default   108.6s   21 test binaries at once
  -p 8      106.2s    9
  -p 4      142.5s    5

GOMAXPROCS is unset, so go test defaults -p to NumCPU, which is 32 here. That is 32 test binaries at once across 128 packages, several of them spawning real shells. On 2026-09-25 that put the machine at 85% CPU and 90% memory and dropped the owner's game to 5 frames a second while he was playing on the other screen.

-p 8 removes more than half the concurrency for about 2 percent of the wall clock. Add -p 8 and run it again.

This is a bound on test binaries, not on goroutines: each one still gets GOMAXPROCS 32.`
