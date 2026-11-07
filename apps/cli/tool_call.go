package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/apps/cli/output"
	"tofu/apps/cli/wiring"
	"tofu/internal/agent/loop"
	"tofu/internal/models/llm/core"
	platformfs "tofu/internal/platform/fs"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/store/session"
	settingspkg "tofu/internal/store/settings"
	"tofu/internal/tools/contract"
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
	opts = settled(wiring.OnTheBoundWire(opts), notify)
	opts.LeadMayWorkAlone = true
	stateDir, stateErr := platformfs.HomeConfigDir()
	atStart := stateFiles(stateDir)
	home, _ := os.UserHomeDir()
	warm := newWarmProcesses(browser.NewBrowserTabs(home))
	defer warm.Close()
	hand, err := handConfig(opts, recorded, warm, notify)
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
	afterSetup, printed := stateFiles(stateDir), ""
	if stateErr == nil {
		defer func() {
			footer := "tofu tool: under " + stateDir + " the setup " + stateWritten(atStart, afterSetup) + ", and the call " + stateWritten(afterSetup, stateFiles(stateDir))
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
	_, _ = io.WriteString(out, printed)
	if answer.ToolOutcome.Failed() {
		return output.ExitVerdict
	}
	return output.ExitOK
}

type handRun struct {
	config    loop.Config
	prompt    composedRun
	board     leadForeseen
	gateErr   error
	siftErr   error
	unstarted unstartedTools
}

func handConfig(opts wiring.RunOpts, recorded string, warm *warmProcesses, notify func(string)) (handRun, error) {
	var hand handRun
	shell, err := loop.ResolveRunShell(settingspkg.TextSaying(opts.Dir, settingspkg.Shell, notify))
	if err != nil {
		return hand, err
	}
	opts.Shell = shell
	dir := cmp.Or(opts.Dir, ".")
	sessions, err := session.OpenIn(dir)
	if err != nil {
		return hand, err
	}
	if recorded != "" {
		row, _, err := sessionSummary(sessions, recorded)
		if err != nil {
			return hand, err
		}
		opts.Session = row.ID
	}
	places, err := platformproject.ResolvePlaces(dir)
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
	config, spawner, err := runConfig(opts, named, runtime{open: wiring.OpenWire, composed: &hand.prompt, foreseen: &hand.board, sessions: sessions, gate: gate, sift: sifter, scorer: scorer, keys: keys, places: places, notify: notify})
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

func stateFiles(root string) map[string]stateFile {
	files := map[string]stateFile{}
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, err := entry.Info(); err == nil {
			files[filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))] = stateFile{info.Size(), info.ModTime()}
		}
		return nil
	})
	return files
}

func stateWritten(before, after map[string]stateFile) string {
	var written []string
	for path, now := range after {
		if was, existed := before[path]; !existed {
			written = append(written, "created "+path)
		} else if was != now {
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
