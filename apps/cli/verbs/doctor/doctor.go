package doctor

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/agent/loop"
	"tofu/internal/host"
	"tofu/internal/judge/gate"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/llm/accounts"
	"tofu/internal/models/llm/quota"
	"tofu/internal/platform/build"
	"tofu/internal/platform/fs"
	"tofu/internal/platform/project"
	"tofu/internal/project/boardy"
	"tofu/internal/tools/web/browser/driver"
	"tofu/internal/ui/tui/frame/frame"
)

const (
	Usage       = "tofu doctor [--imports] [--fix] [--json]"
	importsFlag = "--imports"
	fixFlag     = "--fix"
	fixHint     = "tofu doctor --fix writes the starter hooks to every board with no rules, and touches no rule that exists"
	gateEnv     = "the environment"
	gateDotEnv  = "an .env file"
	gateStore   = "the credential store"
	gateMissing = "missing"
)

func Verb(args []string, out, errOut io.Writer) int {
	asJSON, imports, fix := output.JSONAsked(args), false, false
	for _, arg := range args {
		switch arg {
		case output.JSONFlag:
		case importsFlag:
			imports = true
		case fixFlag:
			fix = true
		default:
			return output.Verb{Name: "doctor", UsageLine: Usage, AsJSON: asJSON, Out: out, ErrOut: errOut}.Usage(errors.New("unknown argument " + strconv.Quote(arg)))
		}
	}
	if imports {
		return Imports(".", asJSON, out, errOut)
	}
	now := time.Now()
	report := State(now)
	ready := report.Verdict == host.DoctorReady
	unruled, fixed := unruledBoards(report.Root, fix)
	var err error
	if asJSON {
		err = output.WriteJSON(out, cli.Envelope{Verb: "doctor", OK: ready, At: now, Data: report, Problems: append(blockerProblems(report.Blockers), unruled...)})
	} else {
		page := cli.Detect(out, os.Environ())
		err = page.Print(out, append(reportPage(page, report), boardLines(page, unruled, fixed)...))
	}
	switch {
	case err != nil:
		return output.PrintFailure(errOut, output.ExitVerdict, "tofu doctor: "+err.Error(), "")
	case ready:
		return output.ExitOK
	}
	return output.ExitVerdict
}

func blockerProblems(blockers []host.DoctorBlocker) []cli.Problem {
	problems := make([]cli.Problem, len(blockers))
	for i, blocker := range blockers {
		problems[i] = cli.Problem{What: blocker.Label + ": " + blocker.What, Hint: blocker.Command}
	}
	return problems
}

func State(now time.Time) host.DoctorReport {
	root, err := os.Getwd()
	if err != nil {
		root = "."
	}
	located, _ := wiring.LocateGateKey(core.ReadKeyring())
	library, rules := wiring.ReadLibrary()
	for i, point := range rules {
		if point.Point == wiring.ShellSiftPoint {
			rules[i] = wiring.ShellSiftRule(point)
			continue
		}
		rules[i] = withLockStanding(point)
	}
	home, _ := os.UserHomeDir()
	report := host.DoctorReport{
		Version:     frame.Release(build.Version(), build.BuildRevision()),
		Verdict:     host.DoctorReady,
		Blockers:    wiring.DoctorBlockers(),
		Credentials: credentials(now),
		Store:       accounts.DoctorState(now, wiring.Specs()),
		Gate:        host.DoctorGate{Variable: located.Name, Source: gateSource(located), Path: located.Path},
		Library:     library,
		Rules:       rules,
		Calibration: wiring.CalibrationState(),
		Ledger:      wiring.LedgerState(),
		SpendLimit:  quota.SpendLimitLine(),
		Wires:       wireSpends(located.Name),
		Proxy:       proxyState(root),
		Browser:     driver.Hosts(home, driver.ChromeHostsKey),
		Root:        root,
		Go:          build.GoVersion(),
		OS:          fs.OS() + "/" + fs.Arch(),
	}
	stack, err := wiring.StackRules("", root)
	if err != nil {
		report.Unreadable = err.Error()
	}
	for _, found := range stack.Overrides {
		report.Overrides = append(report.Overrides, *found.Listing())
	}
	if len(report.Blockers) > 0 {
		report.Verdict = host.DoctorNotReady
	}
	return report
}

func withLockStanding(point host.DoctorRule) host.DoctorRule {
	if point.Fallback != "" || point.Unusable != "" {
		return point
	}
	r, _, err := wiring.LoadRulePoint(point.Point, "")
	dir, dirErr := project.CalibrationDir()
	if err != nil || dirErr != nil {
		return point
	}
	if present, _ := fs.Exists(gate.LockPath(dir, r)); !present {
		return point
	}
	res, err := wiring.ResolveRuleMode(r)
	if err != nil {
		point.Unusable = err.Error()
		return point
	}
	point.Fallback = res.Reason
	return point
}

func unruledBoards(root string, fix bool) (unruled []cli.Problem, fixed []string) {
	store, err := boardy.OpenStore(root)
	boards, listed := store.Boards()
	if err = errors.Join(err, listed); err != nil {
		unruled = append(unruled, cli.Problem{What: "boards: " + err.Error()})
	}
	ids, err := boardy.StarterHookIDs()
	if err != nil {
		unruled = append(unruled, cli.Problem{What: "starter hooks: " + err.Error()})
	}
	hooks := strings.Join(ids, ", ")
	for _, board := range boardy.Unruled(boards) {
		rules := filepath.Join(board.Dir, boardy.RulesDirName)
		if !fix {
			unruled = append(unruled, cli.Problem{What: "board " + board.Key + " has no rules, so " + hooks + " never fire", Hint: fixHint})
			continue
		}
		if err := store.WriteStarterHooks(board.Key); err != nil {
			unruled = append(unruled, cli.Problem{What: "board " + board.Key + ": its starter hooks were not all written to " + rules + ": " + err.Error()})
			continue
		}
		fixed = append(fixed, "board "+board.Key+": wrote "+hooks+" to "+rules)
	}
	return unruled, fixed
}

func wireSpends(keyName string) []host.DoctorWire {
	names := wiring.RunWires()
	wires := make([]host.DoctorWire, 0, len(names))
	for _, name := range names {
		spend := "the " + name + " subscription quota, no money"
		if wiring.WireSpend(name) == loop.SpendAPIKey {
			spend = "money, every call is billed to the account that issued " + keyName
		}
		wires = append(wires, host.DoctorWire{Name: name, Spend: spend})
	}
	return wires
}

func proxyState(root string) host.DoctorProxy {
	setting := wiring.LoadProxySetting(root)
	state := host.DoctorProxy{Use: setting.Use, From: setting.Layer}
	if setting.Proxy == nil {
		return state
	}
	version, err := setting.Proxy.InstalledVersion(context.Background())
	switch {
	case err != nil:
		state.Unreadable = err.Error()
	case version == "":
		state.Install = RtkInstall()
	}
	state.Version = version
	return state
}

func RtkInstall() string {
	switch fs.OS() {
	case "windows":
		return "winget install rtk-ai.rtk"
	case "darwin":
		return "brew install rtk"
	}
	return "cargo install --git https://github.com/rtk-ai/rtk"
}

func gateSource(located core.Located) string {
	switch located.Source {
	case core.SourceEnvironment:
		return gateEnv
	case core.SourceDotEnv:
		return gateDotEnv
	case core.SourceDatabase:
		return gateStore
	case core.SourceMissing:
		return gateMissing
	}
	panic("tofu doctor: unknown key source")
}

func credentials(now time.Time) []host.CredentialReport {
	results, err := wiring.PollCredentials(context.Background(), time.Now, nil)
	if err != nil {
		return []host.CredentialReport{{Provider: "credentials", State: wiring.Unreadable + err.Error()}}
	}
	return wiring.CredentialReports(results, now)
}
