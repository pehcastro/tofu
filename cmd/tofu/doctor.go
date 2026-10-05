package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"tofu/interface/cli"
	"tofu/interface/tui/frame"
	"tofu/internal/browser"
	"tofu/internal/judge/jev"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/quota"
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

type doctorVerdict int

const (
	doctorReady doctorVerdict = iota
	doctorNotReady
)

func (v doctorVerdict) String() string {
	switch v {
	case doctorReady:
		return "ready"
	case doctorNotReady:
		return "not ready"
	}
	panic("tofu doctor: unknown verdict")
}

//nolint:unparam
func (v doctorVerdict) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(v.String())), nil
}

func (v *doctorVerdict) UnmarshalJSON(data []byte) error {
	text, err := strconv.Unquote(string(data))
	if err != nil {
		return err
	}
	for _, candidate := range []doctorVerdict{doctorReady, doctorNotReady} {
		if candidate.String() == text {
			*v = candidate
			return nil
		}
	}
	return fmt.Errorf("tofu doctor: unknown verdict %q", text)
}

type doctorBlocker struct {
	Label   string `json:"label"`
	What    string `json:"what"`
	Command string `json:"command"`
}

type doctorGate struct {
	Variable string `json:"variable"`
	Source   string `json:"source"`
	Path     string `json:"path,omitempty"`
}

type doctorLibrary struct {
	Dir         string `json:"dir"`
	Points      int    `json:"points"`
	FromProject int    `json:"from_project"`
	Unreadable  string `json:"unreadable,omitempty"`
}

type doctorThresholds struct {
	RiskAskAt            float64 `json:"risk_ask_at"`
	RiskDenyAt           float64 `json:"risk_deny_at"`
	UserRequestedRelaxAt float64 `json:"user_requested_relax_at"`
	ApprovalRelaxAt      float64 `json:"approval_relax_at"`
	FromUntrustedBlockAt float64 `json:"from_untrusted_block_at"`
}

type doctorRule struct {
	Point          string           `json:"point"`
	Schema         string           `json:"schema,omitempty"`
	Mode           string           `json:"mode"`
	Declared       string           `json:"declared,omitempty"`
	Fallback       string           `json:"fallback,omitempty"`
	Origin         string           `json:"origin,omitempty"`
	File           string           `json:"file,omitempty"`
	ThresholdsFrom string           `json:"thresholds_from,omitempty"`
	Thresholds     doctorThresholds `json:"thresholds"`
	Unusable       string           `json:"unusable,omitempty"`
}

type doctorWire struct {
	Name  string `json:"name"`
	Spend string `json:"spend"`
}

type doctorProxy struct {
	Use        string `json:"use"`
	From       string `json:"from"`
	Version    string `json:"version,omitempty"`
	Install    string `json:"install,omitempty"`
	Unreadable string `json:"unreadable,omitempty"`
}

type doctorReport struct {
	Version     string               `json:"version"`
	Verdict     doctorVerdict        `json:"verdict"`
	Blockers    []doctorBlocker      `json:"blockers,omitempty"`
	Credentials []credentialReport   `json:"credentials"`
	Store       string               `json:"credential_store"`
	Gate        doctorGate           `json:"gate"`
	Library     doctorLibrary        `json:"library"`
	Rules       []doctorRule         `json:"rules"`
	Overrides   []overrideListing    `json:"overrides,omitempty"`
	Unreadable  string               `json:"overrides_unreadable,omitempty"`
	Calibration string               `json:"calibration"`
	Ledger      string               `json:"ledger"`
	SpendLimit  string               `json:"spend_limit"`
	Wires       []doctorWire         `json:"wires"`
	Proxy       doctorProxy          `json:"proxy"`
	Browser     []browser.NativeHost `json:"browser"`
	Root        string               `json:"root"`
	Go          string               `json:"go"`
	OS          string               `json:"os"`
}

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
	home, _ := os.UserHomeDir()
	report := doctorReport{
		Version:     frame.Release(sys.Version(), sys.BuildRevision()),
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
	results, err := pollCredentials(context.Background(), time.Now)
	if err != nil {
		return []credentialReport{{Provider: "credentials", State: doctorUnreadable + err.Error()}}
	}
	return credentialReports(results, now)
}
