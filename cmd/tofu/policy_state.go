package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	catalogpolicy "tofu/catalog/policy"
	"tofu/catalog/questions"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/policy"
	"tofu/internal/judge/question"
	"tofu/internal/sys"
)

func readCatalog() (doctorCatalog, []doctorPolicy) {
	policyDir, refs, err := policyRefs()
	if err != nil {
		return doctorCatalog{Dir: policyDir, Unreadable: err.Error()}, nil
	}
	layers, err := question.DefaultLayers(questions.Files())
	if err != nil {
		return doctorCatalog{Dir: policyDir, Unreadable: err.Error()}, nil
	}
	catalog := doctorCatalog{Dir: policyDir, Points: len(refs)}
	points := make([]doctorPolicy, 0, len(refs))
	for _, ref := range refs {
		point := policyPoint(ref, layers)
		if point.Origin == string(policy.OriginProject) {
			catalog.FromProject++
		}
		points = append(points, point)
	}
	return catalog, points
}

func policyRefs() (string, []string, error) {
	dir, err := sys.CatalogDir()
	if err != nil {
		return "", nil, err
	}
	policyDir := filepath.Join(dir, "policy")
	shipped, err := fs.Glob(catalogpolicy.Files(), "*.yaml")
	if err != nil {
		return policyDir, nil, err
	}
	present, err := sys.Exists(policyDir)
	if err != nil {
		return policyDir, nil, err
	}
	var project []string
	if present {
		project, err = sys.ListFiles(policyDir, ".yaml")
		if err != nil {
			return policyDir, nil, err
		}
	}
	seen := make(map[string]bool, len(shipped)+len(project))
	refs := make([]string, 0, len(shipped)+len(project))
	for _, name := range append(shipped, project...) {
		ref := strings.TrimSuffix(filepath.Base(name), ".yaml")
		if seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return policyDir, refs, nil
}

func calibrationState() string {
	dir, err := sys.CalibrationDir()
	if err != nil {
		return "unknown: " + err.Error()
	}
	isDir, err := sys.IsDir(dir)
	if err != nil {
		return doctorUnreadable + err.Error()
	}
	if !isDir {
		return "none, no lock in " + dir
	}
	names, err := sys.ListFiles(dir, ".lock")
	if err != nil {
		return doctorUnreadable + err.Error()
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

func policyPoint(ref string, layers []question.Layer) doctorPolicy {
	set, _, err := question.Resolve(ref, layers)
	if err != nil {
		return doctorPolicy{Point: ref, Unusable: err.Error()}
	}
	pol, origin, err := policy.LoadPoint(catalogpolicy.Files(), ref, set)
	if err != nil {
		return doctorPolicy{Point: ref, Unusable: err.Error()}
	}
	point := doctorPolicy{
		Point:  fmt.Sprintf("%s@%d", pol.Name, pol.PolicyVersion),
		Origin: string(origin),
		File:   pol.File,
	}
	if pol.Schema != policy.SchemaGate {
		point.Schema = pol.Schema
	}
	if pol.ModeDeclared {
		point.Declared = pol.Mode.String()
	}
	res, err := resolvePolicyMode(pol)
	if err != nil {
		point.Unusable = err.Error()
		return point
	}
	point.Mode = res.Mode.String()
	point.ThresholdsFrom = "the policy"
	if res.Pinned {
		point.ThresholdsFrom = "the lock"
	}
	if point.Schema == "" {
		point.Thresholds = doctorThresholds{
			RiskAskAt:            res.Policy.Thresholds.RiskAskAt,
			RiskDenyAt:           res.Policy.Thresholds.RiskDenyAt,
			UserRequestedRelaxAt: res.Policy.Thresholds.UserRequestedRelaxAt,
			ApprovalRelaxAt:      res.Policy.Thresholds.ApprovalRelaxAt,
			FromUntrustedBlockAt: res.Policy.Thresholds.FromUntrustedBlockAt,
		}
	}
	if point.Declared != point.Mode {
		point.Fallback = res.Reason
	}
	return point
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
		return doctorUnreadable + err.Error()
	}
	return stats.String()
}
