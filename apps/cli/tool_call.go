package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/apps/cli/output"
	"tofu/apps/cli/wiring"
	"tofu/internal/agent/cron"
	"tofu/internal/agent/hooks"
	"tofu/internal/agent/loop"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	platformfs "tofu/internal/platform/fs"
	platformproject "tofu/internal/platform/project"
	platformscratch "tofu/internal/platform/scratch"
	"tofu/internal/store/session"
	settingspkg "tofu/internal/store/settings"
	crontool "tofu/internal/tools/agents/cron"
	"tofu/internal/tools/contract"
	tools "tofu/internal/tools/registry"
	"tofu/internal/tools/web/browser"
)

const toolUsage = "usage: tofu tool list [--dir <dir>] [--agent <name>] [--session <name|id|head>], or tofu tool <name> <json> [--dir <dir>] [--agent <name>] [--session <name|id|head>] [--sift free|judged]"

const toolListTask = "run one tool call by hand"

const toolCallID = "tofu_tool"

func toolVerb(args []string, out, errOut io.Writer) int {
	runArgs, words, recorded := []string{"--show-prompt", "--dir", "."}, []string{}, ""
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--dir" || arg == "--agent" || arg == "--session" || arg == "--sift":
			value, err := output.NextArg(args, &i, arg)
			if err != nil {
				return refuseTool(errOut, err.Error())
			}
			if arg == "--session" {
				recorded = value
				continue
			}
			runArgs = append(runArgs, arg, value)
		case strings.HasPrefix(arg, "--"):
			return refuseTool(errOut, "unknown argument "+strconv.Quote(arg)+"\n"+toolUsage)
		default:
			words = append(words, arg)
		}
	}
	listing := len(words) == 1 && words[0] == "list"
	if !listing && (len(words) != 2 || words[0] == "list") {
		return refuseTool(errOut, toolUsage)
	}
	var call core.ToolCall
	if !listing {
		call = core.ToolCall{ID: toolCallID, Name: words[0], Arguments: json.RawMessage(words[1])}
		var syntax *json.SyntaxError
		if err := json.Unmarshal(call.Arguments, &call.Arguments); errors.As(err, &syntax) {
			return refuseTool(errOut, fmt.Sprintf("the arguments are not JSON: %v, at byte %d of %s", err, syntax.Offset, words[1]))
		}
	}
	opts, err := parseRunArgs(append(runArgs, toolListTask))
	if err != nil {
		return refuseTool(errOut, err.Error())
	}
	say := writeNotice(errOut)
	notify := func(notice string) {
		if call.Name == loop.RememberToolName || !strings.HasPrefix(notice, loop.RememberToolName+" ") {
			say(notice)
		}
	}
	opts = wiring.OnTheBoundWire(opts)
	opts.LeadMayWorkAlone = true
	stateDir, stateErr := platformfs.HomeConfigDir()
	places, _ := platformproject.ResolvePlaces(cmp.Or(opts.Dir, "."))
	since := time.Now()
	atStart := stateFiles(stateDir, places.State, since, callFolders{})
	var hand handRun
	if lone, built := loneTool(opts, call.Name); built {
		hand.config, err = loneConfig(opts, recorded, lone, call.Arguments, notify)
	} else {
		home, _ := os.UserHomeDir()
		warm := newWarmProcesses(browser.NewBrowserTabs(home))
		defer warm.Close()
		hand, err = handConfig(settled(opts, notify), recorded, warm, notify)
	}
	if err != nil {
		return refuseTool(errOut, err.Error())
	}
	if listing {
		for _, tool := range hand.config.OfferedTools() {
			_, _ = fmt.Fprintln(out, tool.Name)
		}
		if len(hand.unstarted) > 0 {
			_, _ = fmt.Fprintln(out, hand.unstarted.Error())
		}
		return output.ExitOK
	}
	if hand.siftErr != nil && (call.Name == "bash" || call.Name == loop.ShellToolName) {
		notify("no shell result is cut: " + hand.siftErr.Error())
	}
	own := callFoldersOf(hand.config)
	afterSetup, printed := stateFiles(stateDir, places.State, since, own), ""
	if stateErr == nil {
		defer func() {
			footer := "tofu tool: under " + stateDir + " the setup " + stateWritten(atStart, afterSetup, since) + ", and the call " + stateWritten(afterSetup, stateFiles(stateDir, places.State, since, own), since) + " (its scratch folder is not checked)"
			if printed != "" && !strings.HasSuffix(printed, "\n") {
				footer = "\n" + footer
			}
			_, _ = fmt.Fprintln(errOut, footer)
		}()
	}
	answer, err := hand.config.CallByHand(context.Background(), call)
	if err != nil {
		return refuseTool(errOut, err.Error())
	}
	printed = answer.Content
	if call.Name == crontool.CronToolName && hand.cron.Live() > 0 {
		id := hand.config.Session
		if _, err := hand.config.Sessions.Header(id); err != nil {
			said := "the cron job is kept in session " + id + ": tofu tool cron with --session " + id + " reads it again, and tofu --continue fires it"
			if err := hand.config.Sessions.Write(session.Header{ID: id, Root: id, At: time.Now()}, nil); err != nil {
				said = "the cron job is kept, and tofu --continue does not find its session: " + err.Error()
			}
			notify(said)
		}
	}
	_, _ = io.WriteString(out, printed)
	if answer.ToolOutcome.Failed() {
		return output.ExitVerdict
	}
	return output.ExitOK
}

type handRun struct {
	config    loop.Config
	cron      *cron.Book
	prompt    composedRun
	board     leadForeseen
	gateErr   error
	siftErr   error
	unstarted unstartedTools
}

func loneTool(opts wiring.RunOpts, name string) (contract.Tool, bool) {
	leads := leadToolConstructors()
	at := slices.IndexFunc(leads, func(lead leadTool) bool { return lead.name == name })
	if at < 0 || opts.Agent != "" || opts.ToolSet == toolSetThree || settingspkg.TextSaying(opts.Dir, settingspkg.Shell, nil) != "" {
		return nil, false
	}
	tool, err := leads[at].construct(opts.Dir)
	if err != nil {
		return nil, false
	}
	return tools.NewMemo().Wrap([]contract.Tool{tool})[0], true
}

func loneConfig(opts wiring.RunOpts, recorded string, lone contract.Tool, args json.RawMessage, notify func(string)) (loop.Config, error) {
	opts, sessions, places, err := handStores(opts, recorded)
	if err != nil {
		return loop.Config{}, err
	}
	keys := &storedKeys{}
	redactor := keys.redactor()
	turnID := cmp.Or(opts.TurnID, loop.NewID(time.Now()))
	var leadToolUnsifted loop.ShellSift
	config := loop.Config{Places: places, Redactor: &redactor, Tools: loop.NewRegistry(lone), Sift: &leadToolUnsifted, Project: cmp.Or(opts.Dir, "."), SessionSource: hooks.SessionStartup, Task: opts.Task, Wire: opts.Wire,
		ResultBytesCap: konst.TurnResultBytesCap, Sessions: sessions, Session: cmp.Or(opts.Session, opts.TurnID, session.NewEventID()), Proxy: wiring.LoadProxySetting(opts.Dir).Proxy, Notify: notify,
		NewID: func() string { return turnID }}
	if opts.GateArm != gateOff && loop.PersonOnly(config.Project, loop.GateRequest{Tool: lone.Name(), Args: args}) {
		if gate, _ := toolGateOn(opts.Dir, places.Log, keys.keyring()); gate != nil {
			config.Gate, config.GateMode = gate, gateMode(opts.GateArm)
		}
	}
	return config, nil
}

func handStores(opts wiring.RunOpts, recorded string) (wiring.RunOpts, *session.Store, platformproject.Places, error) {
	dir := cmp.Or(opts.Dir, ".")
	sessions, err := session.OpenIn(dir)
	if err != nil {
		return opts, nil, platformproject.Places{}, err
	}
	if recorded != "" {
		row, _, err := sessionSummary(sessions, recorded)
		if err != nil {
			return opts, nil, platformproject.Places{}, err
		}
		opts.Session = row.ID
	}
	places, err := platformproject.ResolvePlaces(dir)
	return opts, sessions, places, err
}

func handConfig(opts wiring.RunOpts, recorded string, warm *warmProcesses, notify func(string)) (handRun, error) {
	var hand handRun
	shell, err := loop.ResolveRunShell(context.Background(), settingspkg.TextSaying(opts.Dir, settingspkg.Shell, notify))
	if err != nil {
		return hand, err
	}
	opts.Shell = shell
	opts, sessions, places, err := handStores(opts, recorded)
	if err != nil {
		return hand, err
	}
	named, _, err := buildRunToolsForRun(opts.Dir, opts.ToolSet, readsWhen(opts.ReadBeforeEdit, contract.NewReadLedger()), warm, shell)
	if hand.unstarted, err = unstartedIn(err); err != nil {
		return hand, err
	}
	sayUnstarted(hand.unstarted, notify)
	if hand.prompt, err = composeRun(opts, named, runtime{notify: notify, open: wiring.OpenWire}); err != nil {
		return hand, err
	}
	keys := &storedKeys{}
	var gate *toolGate
	if opts.GateArm != gateOff {
		gate, hand.gateErr = toolGateOn(opts.Dir, places.Log, keys.keyring())
	}
	sifter, scorer, siftErr := wiring.ShellSiftOn(opts.SiftArm, places.Log, keys.keyring())
	hand.siftErr = siftErr
	hand.cron = &cron.Book{}
	config, spawner, err := runConfig(opts, named, runtime{open: wiring.OpenWire, composed: &hand.prompt, foreseen: &hand.board, sessions: sessions, gate: gate, sift: sifter, scorer: scorer, keys: keys, places: places, notify: notify, cron: hand.cron, now: time.Now})
	if err == nil {
		err = hand.cron.Load(filepath.Join(sessions.Dir(config.Session), konst.CronBookFile))
	}
	if err != nil || opts.Agent == "" {
		hand.config = config
		return hand, err
	}
	if spawner == nil {
		return hand, errors.New("--agent " + opts.Agent + " runs as a spawn would, and this run offers no spawn tool")
	}
	agent, err := hand.prompt.subAgents.Named(opts.Agent)
	if err != nil {
		return hand, err
	}
	hand.board = leadForeseen{}
	hand.config, err = spawner.RecordedSpawn(agent, opts.Task)
	return hand, err
}

func refuseTool(errOut io.Writer, said string) int {
	_, _ = fmt.Fprintln(errOut, "tofu tool: "+said)
	return output.ExitUsage
}

type stateFile struct {
	size     int64
	modified time.Time
}

type callFolders struct {
	session string
	scratch string
}

func callFoldersOf(config loop.Config) callFolders {
	tag := session.FamilyTag(config.Session)
	if identity, err := config.Sessions.Identity(config.Session); err == nil {
		tag = identity.Tag
	}
	folders := callFolders{session: config.Sessions.Dir(config.Session)}
	if scratch, err := platformscratch.ScratchFor(config.Project, config.Session, "", tag); err == nil {
		folders.scratch = scratch.SessionDir()
	}
	return folders
}

func stateFiles(root, project string, since time.Time, own callFolders) map[string]stateFile {
	files := map[string]stateFile{}
	var walk func(dir string, whole bool)
	walk = func(dir string, whole bool) {
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			switch {
			case !entry.IsDir():
				files[filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))] = stateFile{info.Size(), info.ModTime()}
			case path != own.scratch && (whole || dir == project || !info.ModTime().Before(since)):
				walk(path, whole)
			}
		}
	}
	walk(root, false)
	walk(project, false)
	if own.session != "" {
		walk(own.session, true)
	}
	return files
}

func stateWritten(before, after map[string]stateFile, since time.Time) string {
	var written []string
	for path, now := range after {
		was, existed := before[path]
		switch {
		case !existed && !now.modified.Before(since):
			written = append(written, "created "+path)
		case existed && was != now:
			written = append(written, "changed "+path)
		}
	}
	for path := range before {
		if _, kept := after[path]; !kept {
			written = append(written, "removed "+path)
		}
	}
	if len(written) == 0 {
		return "wrote nothing"
	}
	slices.Sort(written)
	return strings.Join(written, ", ")
}
