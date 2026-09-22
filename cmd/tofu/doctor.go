package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"tofu/interface/tui/frame"
	"tofu/internal/judge/jev"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/quota"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

const (
	doctorFlags       = "the flags are --json and --imports"
	doctorGateDecides = " is the point the gate decides through"
	doctorGateEnv     = "the environment"
	doctorGateDotEnv  = "an .env file"
	doctorGateMissing = "missing"

	doctorWindowColumn = 14
	doctorUnreadable   = "unreadable: "
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

type doctorReport struct {
	Version     string             `json:"version"`
	Verdict     doctorVerdict      `json:"verdict"`
	Blockers    []doctorBlocker    `json:"blockers,omitempty"`
	Credentials []credentialReport `json:"credentials"`
	Store       string             `json:"credential_store"`
	Gate        doctorGate         `json:"gate"`
	Library     doctorLibrary      `json:"library"`
	Rules       []doctorRule       `json:"rules"`
	Calibration string             `json:"calibration"`
	Ledger      string             `json:"ledger"`
	SpendLimit  string             `json:"spend_limit"`
	Wires       []doctorWire       `json:"wires"`
	Root        string             `json:"root"`
	Go          string             `json:"go"`
	OS          string             `json:"os"`
}

func doctor(out io.Writer, shade palette, args ...string) int {
	joined := strings.Join(args, " ")
	if joined == "--imports" {
		return doctorImports(".", out)
	}
	if joined != "" && joined != jsonFlag {
		_, _ = fmt.Fprintf(out, "tofu doctor: unknown argument %q, %s\n", joined, doctorFlags)
		return exitUsage
	}
	report := doctorState(time.Now())
	if joined == jsonFlag {
		if err := writeJSON(out, report); err != nil {
			_, _ = fmt.Fprintf(out, "tofu doctor: %v\n", err)
			return exitVerdict
		}
	} else {
		_, _ = fmt.Fprint(out, doctorText(report, shade))
	}
	if report.Verdict == doctorNotReady {
		return exitVerdict
	}
	return exitOK
}

func doctorState(now time.Time) doctorReport {
	root, err := os.Getwd()
	if err != nil {
		root = "."
	}
	located, _ := locateGateKey()
	library, rules := readLibrary()
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
		Root:        root,
		Go:          sys.GoVersion(),
		OS:          sys.OS() + "/" + sys.Arch(),
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

func gateSource(located jev.Located) string {
	switch located.Source {
	case jev.SourceEnvironment:
		return doctorGateEnv
	case jev.SourceDotEnv:
		return doctorGateDotEnv
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
