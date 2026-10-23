package guard

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/boardy"
	"tofu/internal/sys"
)

const reviewLogFloor = 200

var citedTicket = sys.LazyRegexp(`(TOFU|BOJI|DESK)-\d+(?:-\d+)?`)

func requireTicket(env Env, ev event) (Answer, error) {
	kind := ev.ToolInput.SubagentType
	agents := agentsIn(env.Root)
	grantsPaths := !blank(ev.ToolInput.Ticket) || len(ev.ToolInput.Owns) > 0
	if strings.EqualFold(kind, "Explore") || strings.EqualFold(kind, "Plan") || !grantsPaths && foldContains(agents.readers, kind) {
		return Answer{}, nil
	}
	source := ticketsAt(env)
	why, err := spawnRefusal(agents.names, source, kind, ev.ToolInput.Ticket, ev.ToolInput.Prompt)
	if why == "" {
		return Answer{}, err
	}
	first := "1. Write .local/boji/tickets/doing/TOFU-NNN.md with front matter carrying id, title, epic, size, owns, spec and owner, then Problem, Scope, Acceptance, Notes."
	if _, onBoards := source.(boardTickets); onBoards {
		first = "1. Create it on its board: tofu boardy new --board TOFU --owns a,b --acceptance text --as orchestrator <title>, then tofu boardy assign it to the agent type and tofu boardy move it to doing. Tickets live in boardy now; a file under .local/boji/tickets alone is not a ticket."
	}
	readers := "This project defines no read-only agent, one whose tools hold no edit, so every agent here needs a ticket."
	if len(agents.readers) > 0 {
		readers = "A read-only agent, one whose definition holds no edit tool, is not gated while its spawn names no ticket and no owns, so use one if you need to understand the problem before you can write the ticket: " + strings.Join(agents.readers, ", ") + "."
	}
	return deny(why + `

No ticket, no agent, the ticket's owner: names the agent that runs it, and no two agents on the same paths. All three are gates rather than reminders, and all three are in CLAUDE.md.

Do this instead:
` + first + `
2. Every Acceptance line must be observable by someone who did not do the work: a command that runs, an output that appears, an exit code, a test that fails without the change, a number stated before and after. At least one line on an epic ticket is a number from bench/.
3. spec: must point at a decided document under .local/boji/planning/. A ticket whose only specification is itself is under-specified.
4. owns: lists the paths the agent may write, and must not overlap another ticket in doing/. If it does, send the work to the agent that already holds the path rather than starting a second one.
5. owner: names the agent type this ticket runs as. Spawn that type, or if owner: is orchestrator, spawn general-purpose: PowerShell hooks and other work with no specialized agent are done by a general agent on purpose.
6. Start the agent again with the ticket id and the acceptance list rather than a paraphrase.

` + readers), err
}

func spawnRefusal(known []string, source ticketSource, kind, named, prompt string) (string, error) {
	id := named
	if id == "" {
		id = citedTicket().FindString(prompt)
	}
	if id == "" {
		return "This brief cites no ticket.", nil
	}
	cited, found, err := source.find(id)
	switch {
	case err != nil:
		return "", err
	case !found:
		return id + " is cited but it is not on " + source.where(id) + ".", nil
	case cited.Place == placeClosed:
		return id + " is cited but it is already " + cited.Status + ". That work is finished or abandoned; there is nothing left for an agent to claim.", nil
	case !cited.Acceptance:
		return id + " exists but has no Acceptance, so nothing defines done.", nil
	case len(cited.Owns) == 0:
		return id + " has no owns list, so there is nothing keeping this agent out of another one's files.", nil
	}
	doing, err := source.list(placeDoing)
	for _, other := range doing {
		if strings.EqualFold(other.ID, id) || subTicketOf(other.ID, id) || subTicketOf(id, other.ID) {
			continue
		}
		for _, theirs := range other.Owns {
			for _, mine := range cited.Owns {
				if globsOverlap(mine, theirs) {
					return fmt.Sprintf("%s owns '%s', which overlaps '%s' held by %s in doing.", id, mine, theirs, other.ID), err
				}
			}
		}
	}
	heldBySpawn := cited.Owner == "" || foldSuffix(cited.Owner, "/"+id)
	if named != "" && !blank(kind) && heldBySpawn {
		return "", err
	}
	if cited.Owner == "" {
		return id + " has no owner (assignee on a board), so there is nothing to check this spawn's agent type against.", err
	}
	owner := cited.Owner
	if !foldContains(known, owner) {
		owner = boardy.AgentTypeOf(owner)
	}
	if !foldContains(known, owner) {
		return fmt.Sprintf("%s names owner: %s, which is not a registered agent type. This hook only knows what .tofu/agents/, .agents/agents/, .claude/agents/ and library/*/agents/ define, plus orchestrator and general-purpose: %s.", id, cited.Owner, strings.Join(known, ", ")), err
	}
	allowed := []string{owner}
	if strings.EqualFold(owner, "orchestrator") {
		allowed = []string{"orchestrator", "general-purpose"}
	}
	if !foldContains(allowed, kind) {
		return fmt.Sprintf("%s names owner: %s, which should run as %s, not the requested %s.", id, cited.Owner, strings.Join(allowed, " or "), kind), err
	}
	return "", err
}

func subTicketOf(child, parent string) bool {
	ref, err := boardy.SplitID(strings.ToUpper(child))
	return err == nil && strings.EqualFold(ref.Parent(), parent)
}

func globsOverlap(a, b string) bool {
	trim := func(glob string) string { return strings.TrimRight(strings.TrimRight(glob, "*"), "/") }
	pa, pb := trim(a), trim(b)
	return pa == "" || pb == "" || foldPrefix(pa, pb) || foldPrefix(pb, pa)
}

var (
	agentName  = sys.LazyRegexp(`(?im)^name:\s*(\S+)\s*$`)
	agentTools = sys.LazyRegexp(`(?im)^tools:[ \t]*(\S.*?)[ \t]*$`)
)

type agentCatalog struct {
	names   []string
	readers []string
}

func agentsIn(root string) agentCatalog {
	files, _ := filepath.Glob(filepath.Join(root, "library", "*", "agents", "*.md"))
	for _, folder := range []string{".tofu", ".agents", ".claude"} {
		found, _ := filepath.Glob(filepath.Join(root, folder, "agents", "*.md"))
		files = append(files, found...)
	}
	editors := []string{"orchestrator", "general-purpose"}
	var readers []string
	for _, file := range files {
		data, _ := os.ReadFile(file)
		name := agentName().FindStringSubmatch(string(data))
		if name == nil {
			continue
		}
		tools := agentTools().FindStringSubmatch(string(data))
		if tools == nil || slices.ContainsFunc(strings.Split(tools[1], ","), editsFiles) {
			editors = append(editors, name[1])
		} else {
			readers = append(readers, name[1])
		}
	}
	readers = slices.DeleteFunc(readers, func(name string) bool { return foldContains(editors, name) })
	return agentCatalog{names: sortedNames(append(editors, readers...)), readers: sortedNames(readers)}
}

func editsFiles(tool string) bool {
	tool = strings.ToLower(tool)
	return strings.Contains(tool, "edit") || strings.Contains(tool, "patch")
}

func sortedNames(names []string) []string {
	slices.SortFunc(names, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return slices.Compact(names)
}

func foldContains(names []string, want string) bool {
	return slices.ContainsFunc(names, func(name string) bool { return strings.EqualFold(name, want) })
}

var gatedRoots = []string{"cmd/", "internal/", "interface/", "bench/", "library/"}

func ownPaths(env Env, ev event) (Answer, error) {
	target := ev.ToolInput.FilePath
	if blank(target) {
		return Answer{}, nil
	}
	rel, inside := underRoot(env.Root, target)
	gated := slices.ContainsFunc(gatedRoots, func(root string) bool { return foldPrefix(rel, root) })
	if !inside || !gated || blank(ev.AgentID) {
		return Answer{}, nil
	}
	doing, err := ticketsAt(env).list(placeDoing)
	if len(doing) == 0 {
		return deny("'" + rel + `' cannot be written: the board holds no work.

No ticket is in doing, so no ticket owns any path right now. A subagent writes only inside the owns list of a ticket that is actually being worked, and an empty doing is not a gap to write through, it is the absence of any ticket that could own this path.

If this is your ticket's file and it has not reached doing yet, wait for it to be moved there. If your ticket was accepted and moved to done, its work is finished and this write does not belong to it.`), err
	}
	var held []string
	for _, t := range doing {
		if ownsMatch(rel, t.Owns) {
			return Answer{}, err
		}
		held = append(held, "  "+t.ID+": "+strings.Join(t.Owns, ", "))
	}
	return deny("'" + rel + `' is not owned by any ticket in doing.

Currently held:
` + strings.Join(held, "\n") + `

Work happens only inside the owns: list of the ticket you were given. This is how two agents stay out of each other's files in one tree, because there are no worktrees here.

If this file genuinely belongs to your ticket, the ticket's owns: is wrong: stop, say so in your report, and let the orchestrator correct it. If it belongs to someone else's work, report what you found with the file and the line and do not fix it.`), err
}

var (
	logHeredocOpener = sys.LazyRegexp(`^<<'?(\w+)'?\r?\n`)
	movedTicketFile  = sys.LazyRegexp(`(?i)(TOFU|BOJI|DESK)-\d+(?:-\d+)?\.md`)
	moveCommand      = sys.LazyRegexp(`(?i)\b(mv|move|move-item|git\s+mv)\b`)
	reviewFolder     = sys.LazyRegexp(`(?i)review[\\/]`)
	boardyMove       = sys.LazyRegexp(`(?i)\bboardy\s+move\s+((?:TOFU|BOJI|DESK)-\d+(?:-\d+)?)\s+([A-Za-z-]+)`)
)

func ticketLog(env Env, ev event) (Answer, error) {
	command := stripLogHeredocs(ev.ToolInput.Command)
	var empty []string
	how := "the ticket file's ## Log section"
	switch {
	case env.Boardy:
		how = `tofu boardy log <ID> "<one line>" --as orchestrator, one line per piece of evidence`
		store, err := boardy.OpenStore(env.Root)
		if err != nil {
			return Answer{}, err
		}
		for _, move := range boardyMove().FindAllStringSubmatch(command, -1) {
			id := strings.ToUpper(move[1])
			moved, err := store.Get(id)
			if err != nil || moved.Status != boardy.Review || strings.EqualFold(move[2], "review") {
				continue
			}
			if length := utf16Len(strings.TrimSpace(moved.Log)); length < reviewLogFloor {
				empty = append(empty, fmt.Sprintf("%s has %d characters in its Log", id, length))
			}
		}
	case moveCommand().MatchString(command) && reviewFolder().MatchString(command):
		for _, name := range movedTicketFile().FindAllString(command, -1) {
			path := filepath.Join(env.Root, ".local", "boji", "tickets", "review", name)
			if _, err := os.Stat(path); err != nil {
				continue
			}
			switch moved := readFolderTicket(path); {
			case !moved.HasLog:
				empty = append(empty, name+" has no ## Log section at all")
			case utf16Len(moved.Log) < reviewLogFloor:
				empty = append(empty, fmt.Sprintf("%s has %d characters under ## Log", name, utf16Len(moved.Log)))
			}
		}
	}
	if len(empty) == 0 {
		return Answer{}, nil
	}
	return deny(`A ticket may not leave review with an empty Log.

  ` + strings.Join(empty, "\n  ") + `

What goes there is what you verified yourself: the command you ran, its real
output, the number the acceptance asked for, and the one line saying which
acceptance line each piece of evidence satisfies. Not a summary of an agent's
report, and never the agent's word. Write it with ` + how + `.

The reason this exists: on 2026-09-21 six tickets in review/ and three already
accepted into done/ all had an empty ## Log, and three versions of the road
could not be called finished because their evidence had only ever existed in a
chat message. A number nobody can go back and read is the same as no number.

Paste it, then move it.`), nil
}

func stripLogHeredocs(command string) string {
	var out strings.Builder
	for at := 0; at < len(command); {
		opener := logHeredocOpener().FindStringSubmatch(command[at:])
		if opener != nil {
			body := at + len(opener[0])
			if closing := strings.Index(command[body:], "\n"+opener[1]); closing >= 0 {
				out.WriteString("<<HEREDOC")
				at = body + closing + 1 + len(opener[1])
				continue
			}
		}
		out.WriteByte(command[at])
		at++
	}
	return out.String()
}
