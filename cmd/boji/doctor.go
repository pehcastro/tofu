package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"boji/internal/judge/jev"
	"boji/internal/judge/ledger"
	"boji/internal/judge/policy"
	"boji/internal/llm/cred"
	"boji/internal/sys"
)

func doctor(out io.Writer, args ...string) int {
	switch strings.Join(args, " ") {
	case "":
		return doctorEnvironment(out)
	case "--imports":
		return doctorImports(".", out)
	}
	_, _ = fmt.Fprintf(out, "boji doctor: unknown argument %q, the only flag is --imports\n", strings.Join(args, " "))
	return exitUsage
}

func doctorEnvironment(out io.Writer) int {
	_, _ = fmt.Fprintf(out, "go: %s\n", sys.GoVersion())
	_, _ = fmt.Fprintf(out, "os: %s/%s\n", sys.OS(), sys.Arch())

	located, keyErr := jev.Locate(".env")
	_, _ = fmt.Fprintln(out, keyState(located))

	_, _ = fmt.Fprintf(out, "catalog: %s\n", catalogState())
	_, _ = fmt.Fprintf(out, "calibration: %s\n", calibrationState())
	_, _ = fmt.Fprintf(out, "ledger: %s\n", ledgerState())
	_, _ = fmt.Fprintf(out, "credential: %s\n", cred.DoctorState())
	for _, line := range quotaDoctorLines(time.Now()) {
		_, _ = fmt.Fprintln(out, line)
	}
	for _, line := range policyPointLines() {
		_, _ = fmt.Fprintln(out, line)
	}

	if keyErr != nil {
		return exitVerdict
	}
	return exitOK
}

func keyState(located jev.Located) string {
	switch located.Source {
	case jev.SourceEnvironment:
		return located.Name + ": set, from the environment"
	case jev.SourceDotEnv:
		return located.Name + ": set, from .env at " + located.Path
	case jev.SourceMissing:
		return located.Name + ": missing, checked the environment and .env at " + located.Path
	}
	panic("doctor: unknown key source")
}

func catalogState() string {
	dir, err := sys.CatalogDir()
	if err != nil {
		return "unknown: " + err.Error()
	}
	isDir, err := sys.IsDir(dir)
	if err != nil {
		return "unreadable: " + err.Error()
	}
	if !isDir {
		return "missing at " + dir
	}
	if !sys.Readable(dir) {
		return "unreadable at " + dir
	}
	return "readable at " + dir
}

func calibrationState() string {
	dir, err := sys.CalibrationDir()
	if err != nil {
		return "unknown: " + err.Error()
	}
	isDir, err := sys.IsDir(dir)
	if err != nil {
		return "unreadable: " + err.Error()
	}
	if !isDir {
		return "none, no " + dir
	}
	names, err := sys.ListFiles(dir, ".lock")
	if err != nil {
		return "unreadable: " + err.Error()
	}
	if len(names) == 0 {
		return "none"
	}
	var points []string
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		point := strings.SplitN(filepath.Base(name), ".", 2)[0]
		if seen[point] {
			continue
		}
		seen[point] = true
		points = append(points, point)
	}
	return strings.Join(points, " ")
}

func policyPointLines() []string {
	catalogDir, err := sys.CatalogDir()
	if err != nil {
		return []string{"policy: unknown: " + err.Error()}
	}
	policyDir := filepath.Join(catalogDir, "policy")
	names, err := sys.ListFiles(policyDir, ".yaml")
	if err != nil {
		return []string{"policy: unreadable: " + err.Error()}
	}
	lines := make([]string, 0, len(names))
	for _, name := range names {
		lines = append(lines, policyPointLine(filepath.Join(policyDir, name)))
	}
	return lines
}

func policyPointLine(path string) string {
	pol, err := policy.Load(path)
	if err != nil {
		return "policy " + filepath.Base(path) + ": unreadable: " + err.Error()
	}
	point := fmt.Sprintf("%s@%d", pol.Name, pol.PolicyVersion)
	res, err := resolvePolicyMode(pol)
	if err != nil {
		return fmt.Sprintf("policy %s: unknown: %s", point, err.Error())
	}
	if res.Reason == "" {
		return "policy " + point + ": " + res.Mode.String()
	}
	return "policy " + point + ": " + res.Mode.String() + ", " + res.Reason
}

func resolvePolicyMode(pol policy.Policy) (policy.Resolution, error) {
	calibDir, err := sys.CalibrationDir()
	if err != nil {
		return policy.Resolution{}, err
	}
	lookup := lookupLock(policy.LockPath(calibDir, pol))
	ledgerDir, err := ledger.Dir()
	if err != nil {
		return policy.Resolution{}, err
	}
	current, err := currentBuild(ledgerDir, pol.Questions)
	if err != nil {
		return policy.Resolution{}, err
	}
	return policy.Resolve(pol, lookup, current), nil
}

func lookupLock(path string) policy.LockLookup {
	present, err := sys.Exists(path)
	if err != nil {
		return policy.LockLookup{Present: true, Err: err}
	}
	if !present {
		return policy.LockLookup{}
	}
	lock, err := policy.LoadLock(path)
	return policy.LockLookup{Present: true, Lock: lock, Err: err}
}

func currentBuild(ledgerDir, point string) (policy.Current, error) {
	var current policy.Current
	var lastAt time.Time
	_, err := ledger.NewReader(ledgerDir).Each(ledger.Filter{Point: point}, func(row ledger.Row) error {
		if current.Known && row.At.Before(lastAt) {
			return nil
		}
		current = policy.Current{Build: row.Build, QuestionsVersion: row.Version, Known: true}
		lastAt = row.At
		return nil
	})
	if err != nil {
		return policy.Current{}, err
	}
	return current, nil
}

func ledgerState() string {
	dir, err := ledger.Dir()
	if err != nil {
		return "unknown: " + err.Error()
	}
	stats, err := ledger.Summary(dir)
	if err != nil {
		return "unreadable: " + err.Error()
	}
	return stats.String()
}
