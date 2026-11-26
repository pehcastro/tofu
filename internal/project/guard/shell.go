package guard

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"tofu/internal/platform/credential"
	"tofu/internal/platform/fs"
)

var (
	scriptWrites       = fs.LazyRegexp(`(?i)\bopen\s*\([^)\r\n]*,\s*(mode\s*=\s*)?[rb]?['"][rb]?[wax+]|\.write_(text|bytes)\s*\(|\bshutil\.(copy\w*|move)\s*\(|\bos\.(rename|replace)\s*\(|\b(writeFile|appendFile|copyFile|rename|createWriteStream|outputFile)(Sync)?\s*\(|\bFile\.(write|binwrite)\b|\bfile_put_contents\s*\(|\bopen\s*\(?[^,\r\n]*,\s*['"]\+?>|\b(Set-Content|Add-Content|Out-File|Copy-Item|Move-Item|New-Item)\b|\[(System\.)?IO\.File\]::(Write|Append|Copy|Move)`)
	powershellRedirect = fs.LazyRegexp(`(?i)(^|[^\w<>=#-])>>?(\s*\$null|[>=&])?`)
	powershellComments = fs.LazyRegexp(`(?s:@'.*?\n'@|@".*?\n"@|<#.*?#>)|(?m:^[ \t]*#[^\n]*)`)
	assignment         = fs.LazyRegexp(`(?i)^\s*(const\s+|let\s+|var\s+|my\s+)?(\$?[A-Za-z_]\w*)\s*=([^=]|$)`)
	identifier         = fs.LazyRegexp(`\$?[A-Za-z_]\w*`)
	underRepo          = fs.LazyRegexp(`(?i)(^|[\s'"(=,\x60])(\./)?(apps|bench|library|internal|\.tofu|\.claude)/`)
	outsideRepo        = fs.LazyRegexp(`(?i)tofu-scratchpad|/te?mp/|AppData/Local/Temp|tempfile|gettempdir|mkdtemp|os\.tmpdir|\$env:|TMPDIR|GetTempPath|[A-Za-z]:/|['"]/[A-Za-z]|~/|\$HOME|os\.environ|expanduser|homedir|Path\.home`)
	scriptName         = fs.LazyRegexp(`(?i)\.` + scriptExtensions + `$`)
	shellWritesSource  = fs.LazyRegexp(`(?i)((^|[;&|(\n]|\$\()\s*((echo|printf|cat)\b[^;&|\n]*)?>>?\s*\S+\.(go|ya?ml|json)\b|\btee\s+(-a\s+)?\S+\.(go|ya?ml|json)\b|\b(cp|mv)\s+\S+\s+\S*\.(go|ya?ml|json)\b)`)
	sourceFile         = fs.LazyRegexp(`(?i)\.(go|ya?ml|json)\b`)
	gitChangesTree     = fs.LazyRegexp(`(?i)` + gitThen + `(reset|checkout|restore|clean|stash|revert|merge|rebase|switch|add|commit|rm|mv|push|pull|filter-branch|gc|prune|update-ref|branch\s+-[dD]|tag\s+-d)` + wordEnds)
	gitDiscardsWork    = fs.LazyRegexp(`(?i)` + gitThen + `(reset\s+(--hard|--merge|--keep)|checkout\s+(--|\.)|restore\b|clean\s+-[a-z]*[fdx]|filter-branch|update-ref)`)
	gitStash           = fs.LazyRegexp(`(?i)` + gitThen + `stash\b`)
	tofuRunsAtCommand  = fs.LazyRegexp(`(?im)(^|[;&|(\x60]|\$\()\s*[^\s;&|]*\b(tofu|boji)(\.exe)?["']?\s+run\b`)
	exeRuns            = fs.LazyRegexp(`(?i)\S+\.exe["']?\s+run\s`)
	wireOpenrouter     = fs.LazyRegexp(`(?i)--wire[=\s]+openrouter`)
	dryRun             = fs.LazyRegexp(`(?i)--dry-run`)
	liveCost           = fs.LazyRegexp(`(?i)\b(TOFU|BOJI)_LIVE_COST\b`)
	gitWorktree        = fs.LazyRegexp(`(?i)` + gitThen + `worktree\b`)
	goTestPattern      = fs.LazyRegexp(`(?i)go\s+test\b[^&|;]*/\.\.\.`)
	parallelBound      = fs.LazyRegexp(`(?i)-p[= ]\d`)
	treeWide           = []func() *regexp.Regexp{
		fs.LazyRegexp(`(?i)go\s+(test|build|vet)\s+\./\.\.\.`),
		fs.LazyRegexp(`(?i)\bgofmt\s+-w\s+\.`),
		fs.LazyRegexp(`(?i)\bgo\s+run\b.*\bserve\b`),
	}
	gitPublishes = fs.LazyRegexp(`(?i)` + gitThen + `(add|commit|push)` + wordEnds)
	inlineScript = fs.LazyRegexp(`(?i)(^|[\s;&|(/\\])` + interpreters + `\s+(-\S+\s+)*(-[a-z]*[ce]|--eval|-p|--print|-command)(\s|['"])`)
	scriptFile   = fs.LazyRegexp(`(?i)>\s*\S+\.` + scriptExtensions + `\b`)
	interpreter  = fs.LazyRegexp(`(?i)^` + interpreters + `$`)
)

const (
	gitThen          = `\bgit(\s+(-c\s*("[^"]*"|'[^']*'|\S+)|--(git-dir|work-tree|namespace|exec-path|config-env|super-prefix)(=|\s+)\S+|-[-\w]+))*\s+`
	wordEnds         = `([^\w-]|$)`
	interpreters     = `(python[23]?|py|node|bash|sh|zsh|perl|ruby|bun|deno|php|powershell|pwsh)(\.exe)?`
	scriptBytes      = 64 << 10
	scriptExtensions = `(py|sh|bash|js|mjs|cjs|ts|rb|pl|php|ps1)`
)

func shell(ctx context.Context, env Env, ev event) (Answer, error) {
	command := ev.ToolInput.Command
	if blank(command) {
		return Answer{}, nil
	}
	stripped := stripHeredocs(command)
	if reason := flowsRefusal(ctx, env, stripped); reason != "" {
		return deny(reason), nil
	}
	var code strings.Builder
	for _, s := range scripts(env.Root, command) {
		if line := repoWrite(env.Root, s); line != "" {
			return deny(interpreterRefusal(line)), nil
		}
		code.WriteString(scriptCode(s.text()) + "\n")
	}
	git := withoutQuotedText(stripped) + "\n" + code.String()
	reason, err := shellRefusal(env, ev, stripped+"\n"+code.String(), git)
	if reason != "" {
		return deny(reason), nil
	}
	if err != nil {
		return Answer{}, err
	}
	if gitPublishes().MatchString(git) {
		return refuse(0, "NOTE: git add, commit and push need the owner's explicit approval in this turn. No agent, bot or harness is ever a co-author."), nil
	}
	return Answer{}, nil
}

func shellRefusal(env Env, ev event, checked, git string) (string, error) {
	switch {
	case shellWritesSource().MatchString(checked) || sedEditsSource(checked):
		return shellWriteRefusal, nil
	case gitChangesTree().MatchString(git):
		if !blank(ev.AgentID) {
			return subagentGitRefusal, nil
		}
		if gitDiscardsWork().MatchString(git) || stashes(git) {
			return discardRefusal, nil
		}
		return "", nil
	case (tofuRunsAtCommand().MatchString(checked) || exeRuns().MatchString(checked)) && wireOpenrouter().MatchString(checked) && !dryRun().MatchString(checked):
		return openrouterRefusal, nil
	case liveCost().MatchString(checked) && !dryRun().MatchString(checked):
		return liveCostRefusal, nil
	case gitWorktree().MatchString(git):
		return worktreeRefusal, nil
	case goTestPattern().MatchString(checked) && !parallelBound().MatchString(checked):
		return unboundedTestRefusal, nil
	case anyMatch(treeWide, checked):
		return treeWideRefusal(env)
	}
	return "", nil
}

type script struct {
	code       string
	powershell bool
	fromFile   bool
}

func (s script) text() string {
	if s.powershell {
		return powershellComments().ReplaceAllString(s.code, "")
	}
	return s.code
}

func scripts(root, command string) []script {
	var found []script
	for at := 0; at < len(command); at++ {
		end, isHeredoc := heredocAt(command, at)
		if !isHeredoc {
			continue
		}
		segment := command[strings.LastIndexAny(command[:at], ";&|\n(")+1 : at]
		words := shellWords(segment)[0]
		if scriptFile().MatchString(segment) || len(words) > 0 && interpreter().MatchString(filepath.Base(words[0])) {
			body := command[at+2 : end]
			found = append(found, script{code: body, powershell: len(words) > 0 && isPowershell(words[0])})
			found = append(found, scripts(root, body)...)
		}
		at = end - 1
	}
	stripped := stripHeredocs(command)
	if inline := inlineScript().FindStringSubmatch(stripped); inline != nil {
		found = append(found, script{code: stripped, powershell: isPowershell(inline[2])})
	}
	dir := root
	for _, words := range shellWords(stripped) {
		if len(words) > 1 && words[0] == "cd" {
			dir = resolved(root, words[1])
		}
		if s, named := scriptArgument(dir, words); named {
			found = append(found, s)
		}
	}
	return found
}

func scriptArgument(dir string, words []string) (script, bool) {
	at := slices.IndexFunc(words, func(word string) bool { return interpreter().MatchString(filepath.Base(word)) })
	if at < 0 {
		return script{}, false
	}
	for _, arg := range words[at+1:] {
		if scriptName().MatchString(arg) {
			data, _ := credential.ReadCredential(resolved(dir, arg))
			return script{code: string(data[:min(len(data), scriptBytes)]), powershell: isPowershell(words[at]), fromFile: true}, true
		}
	}
	return script{}, false
}

func isPowershell(word string) bool {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(word)), ".exe")
	return name == "powershell" || name == "pwsh"
}

func repoWrite(root string, s script) string {
	lines := newline().Split(s.text(), -1)
	assigned := map[string][]string{}
	for _, line := range lines {
		if m := assignment().FindStringSubmatch(line); m != nil {
			name := strings.ToLower(m[2])
			assigned[name] = append(assigned[name], line)
		}
	}
	repo := strings.ToLower(strings.ReplaceAll(root, `\`, "/"))
	for _, line := range lines {
		hit := writeAt(line, s.powershell)
		if hit < 0 || s.fromFile && s.powershell && strings.Count(line[:hit], "'")%2 == 1 {
			continue
		}
		target := line
		for _, id := range identifier().FindAllString(line, -1) {
			target += "\n" + strings.Join(assigned[strings.ToLower(id)], "\n")
		}
		target = strings.ReplaceAll(target, `\`, "/")
		if repo != "" && strings.Contains(strings.ToLower(target), repo) || underRepo().MatchString(target) || !outsideRepo().MatchString(target) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func writeAt(line string, powershell bool) int {
	hit := -1
	if at := scriptWrites().FindStringIndex(line); at != nil {
		hit = at[0]
	}
	if !powershell {
		return hit
	}
	for _, at := range powershellRedirect().FindAllStringSubmatchIndex(line, -1) {
		if at[4] >= 0 {
			continue
		}
		if hit < 0 || at[3] < hit {
			hit = at[3]
		}
		break
	}
	return hit
}

func resolved(dir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(dir, path)
}

func scriptCode(text string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune("\"'`,[](){}", r) {
			return ' '
		}
		return r
	}, text)
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

func withoutQuotedText(command string) string {
	var out, word strings.Builder
	var words []string
	kept, inWord := 0, false
	endWord := func() {
		if inWord && (len(words) > 0 || !strings.Contains(word.String(), "=")) {
			words = append(words, word.String())
		}
		word.Reset()
		inWord = false
	}
	for at := 0; at < len(command); at++ {
		b := command[at]
		switch {
		case b == '\'' || b == '"':
			end := at + 1
			for end < len(command) && command[end] != b {
				if b == '"' && command[end] == '\\' {
					end++
				}
				end++
			}
			end = min(end, len(command))
			body := command[at+1 : end]
			word.WriteString(body)
			inWord = true
			if quotesText(words) && (b == '\'' || !strings.ContainsAny(body, "$`")) {
				out.WriteString(command[kept : at+1])
				kept = end
			}
			at = end
		case b == '\\' && at+1 < len(command):
			at++
			word.WriteByte(command[at])
			inWord = true
		case strings.IndexByte(";&|\n()`", b) >= 0:
			endWord()
			words = nil
		case isSpace(b):
			endWord()
		default:
			word.WriteByte(b)
			inWord = true
		}
	}
	return out.String() + command[kept:]
}

func quotesText(words []string) bool {
	if len(words) == 0 {
		return false
	}
	program := strings.ToLower(words[0])
	program = strings.TrimSuffix(program[strings.LastIndexAny(program, `/\`)+1:], ".exe")
	switch program {
	case "grep", "rg":
		return true
	case "tofu", "boji":
		return len(words) > 1 && words[1] == "boardy"
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
	return slices.ContainsFunc(globs, func(glob string) bool {
		return slices.ContainsFunc(fs.GoRoots(), func(root string) bool {
			return strings.EqualFold(glob, root) || foldPrefix(glob, root+"/")
		})
	})
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

func interpreterRefusal(line string) string {
	return `A repository file may not be written by a script run through an interpreter, inline or from a file.

This line writes under the repository, or to a path the hook cannot place outside it:

  ` + line + `

A python, node, perl or powershell script reaches the disk without passing the checks that run on Write and Edit: the em dash rule, the comment rule and the path ownership rule. On 2026-11-15 an agent wrote a library yaml file this way, and another ran a refused build through a script file.

Use the Write or Edit tool for every file under apps/, bench/, library/, internal/, .tofu/, .claude/ or the repository root. A script may write only under E:/tofu-scratchpad or a temp directory.`
}

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

The OpenRouter key in this repository is not spent on a model. The owner's ruling, 2026-09-18, after the model spend reached $1.08 on Claude Fable and $0.07 on Opus through it; Jev runs on the classifier key of whichever provider the classifier is bound to, never on an llm key, so nothing in tofu's own work spends that key on a model.

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
