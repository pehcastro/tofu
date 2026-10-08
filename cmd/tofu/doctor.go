package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"time"

	"tofu/interface/cli"
	"tofu/interface/tui/frame"
	"tofu/internal/browser"
	"tofu/internal/host"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/method"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/quota"
	"tofu/internal/sift"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

const (
	doctorUsage       = "tofu doctor [--imports] [--json]"
	importsFlag       = "--imports"
	doctorGateEnv     = "the environment"
	doctorGateDotEnv  = "an .env file"
	doctorGateStore   = "the credential store"
	doctorGateMissing = "missing"
	doctorUnreadable  = "unreadable: "
)

type (
	doctorBlocker    = host.DoctorBlocker
	doctorGate       = host.DoctorGate
	doctorLibrary    = host.DoctorLibrary
	doctorThresholds = host.DoctorThresholds
	doctorRule       = host.DoctorRule
	doctorWire       = host.DoctorWire
	doctorProxy      = host.DoctorProxy
	doctorReport     = host.DoctorReport
)

const (
	doctorReady    = host.DoctorReady
	doctorNotReady = host.DoctorNotReady
)

func doctor(args []string, out, errOut io.Writer) int {
	asJSON, imports := jsonAsked(args), false
	for _, arg := range args {
		switch arg {
		case jsonFlag:
		case importsFlag:
			imports = true
		default:
			return verbOutput{verb: "doctor", usageLine: doctorUsage, asJSON: asJSON, out: out, errOut: errOut}.usage(errors.New("unknown argument " + strconv.Quote(arg)))
		}
	}
	if imports {
		return doctorImports(".", asJSON, out, errOut)
	}
	now := time.Now()
	report := doctorState(now)
	ready := report.Verdict == doctorReady
	var err error
	if asJSON {
		err = writeJSON(out, cli.Envelope{Verb: "doctor", OK: ready, At: now, Data: report, Problems: blockerProblems(report.Blockers)})
	} else {
		page := cli.Detect(out, os.Environ())
		err = page.Print(out, doctorPage(page, report))
	}
	switch {
	case err != nil:
		return printFailure(errOut, exitVerdict, "tofu doctor: "+err.Error(), "")
	case ready:
		return exitOK
	}
	return exitVerdict
}

//nolint:unparam
func printFailure(errOut io.Writer, code int, what, hint string) int {
	page := cli.Detect(errOut, os.Environ())
	_ = page.Print(errOut, page.ErrorLine(what, hint))
	return code
}

func blockerProblems(blockers []doctorBlocker) []cli.Problem {
	problems := make([]cli.Problem, len(blockers))
	for i, blocker := range blockers {
		problems[i] = cli.Problem{What: blocker.Label + ": " + blocker.What, Hint: blocker.Command}
	}
	return problems
}

func doctorState(now time.Time) doctorReport {
	root, err := os.Getwd()
	if err != nil {
		root = "."
	}
	located, _ := locateGateKey()
	library, rules := readLibrary()
	for i, point := range rules {
		if point.Point == shellSiftPoint {
			rules[i] = shellSiftRule(point)
		}
	}
	home, _ := os.UserHomeDir()
	report := doctorReport{
		Version:     frame.Release(sys.Version(), sys.BuildRevision()),
		Verdict:     doctorReady,
		Blockers:    doctorBlockers(),
		Credentials: doctorCredentials(now),
		Store:       cred.DoctorState(now),
		Gate:        doctorGate{Variable: located.Name, Source: gateSource(located), Path: located.Path},
		Library:     library,
		Rules:       rules,
		Calibration: calibrationState(),
		Ledger:      ledgerState(),
		SpendLimit:  quota.SpendLimitLine(),
		Wires:       doctorWires(located.Name),
		Proxy:       doctorProxyState(root),
		Browser:     browser.Hosts(home, browser.ChromeHostsKey),
		Root:        root,
		Go:          sys.GoVersion(),
		OS:          sys.OS() + "/" + sys.Arch(),
	}
	stack, err := stackRules("", root)
	if err != nil {
		report.Unreadable = err.Error()
	}
	for _, found := range stack.overrides {
		report.Overrides = append(report.Overrides, *found.listing())
	}
	if len(report.Blockers) > 0 {
		report.Verdict = doctorNotReady
	}
	return report
}

func shellSiftRule(point doctorRule) doctorRule {
	point.Fallback, point.Unusable, point.ThresholdsFrom = "", "", "the rule"
	sifter, _, err := buildShellSift(siftFollowsTheTable)
	if err == nil {
		if chosen, _ := sifter.Methods.Of(sift.ShellSchema); chosen.Method == method.Unwired {
			err = method.UnwiredError{Choice: chosen, File: sifter.Methods.File}
		}
	}
	var contradiction turn.RuleContradictsTheTableError
	var unwired method.UnwiredError
	switch {
	case errors.As(err, &contradiction) || errors.As(err, &unwired):
		point.Mode, point.Fallback = "off", "no shell result is cut: "+err.Error()
	case err != nil:
		point.Mode, point.Unusable = "", err.Error()
	default:
		point.Mode = gate.ModeEnforced.String()
	}
	return point
}

func doctorBlockers() []doctorBlocker {
	var blockers []doctorBlocker
	for _, blocker := range startBlockers() {
		blockers = append(blockers, doctorBlocker{Label: blocker.label, What: blocker.what, Command: blocker.command})
	}
	return blockers
}

func doctorWires(keyName string) []doctorWire {
	names := runWires()
	wires := make([]doctorWire, 0, len(names))
	for _, name := range names {
		spend := "the " + name + " subscription quota, no money"
		if wireSpend(name) == turn.SpendAPIKey {
			spend = "money, every call is billed to the account that issued " + keyName
		}
		wires = append(wires, doctorWire{Name: name, Spend: spend})
	}
	return wires
}

func doctorProxyState(root string) doctorProxy {
	setting := loadProxySetting(root)
	state := doctorProxy{Use: setting.use, From: setting.layer}
	if setting.proxy == nil {
		return state
	}
	version, err := setting.proxy.InstalledVersion(context.Background())
	switch {
	case err != nil:
		state.Unreadable = err.Error()
	case version == "":
		state.Install = rtkInstall()
	}
	state.Version = version
	return state
}

func rtkInstall() string {
	switch sys.OS() {
	case "windows":
		return "winget install rtk-ai.rtk"
	case "darwin":
		return "brew install rtk"
	}
	return "cargo install --git https://github.com/rtk-ai/rtk"
}

func gateSource(located jev.Located) string {
	switch located.Source {
	case jev.SourceEnvironment:
		return doctorGateEnv
	case jev.SourceDotEnv:
		return doctorGateDotEnv
	case jev.SourceDatabase:
		return doctorGateStore
	case jev.SourceMissing:
		return doctorGateMissing
	}
	panic("tofu doctor: unknown key source")
}

func doctorCredentials(now time.Time) []credentialReport {
	results, err := pollCredentials(context.Background(), time.Now, nil)
	if err != nil {
		return []credentialReport{{Provider: "credentials", State: doctorUnreadable + err.Error()}}
	}
	return credentialReports(results, now)
}
