package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	shipped "tofu/catalog"
	"tofu/catalog/questions"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/sys"
)

func readCatalog() (doctorCatalog, []doctorRule) {
	dir, refs, err := ruleRefs()
	if err != nil {
		return doctorCatalog{Dir: dir, Unreadable: err.Error()}, nil
	}
	layers, err := question.DefaultLayers(questions.Files())
	if err != nil {
		return doctorCatalog{Dir: dir, Unreadable: err.Error()}, nil
	}
	catalog := doctorCatalog{Dir: dir, Points: len(refs)}
	points := make([]doctorRule, 0, len(refs))
	for _, ref := range refs {
		point := rulePoint(ref, layers)
		if point.Origin == string(gate.OriginProject) {
			catalog.FromProject++
		}
		points = append(points, point)
	}
	return catalog, points
}

func ruleRefs() (string, []string, error) {
	dir, err := sys.CatalogDir()
	if err != nil {
		return "", nil, err
	}
	fromBinary, err := gate.Refs(shipped.Files())
	if err != nil {
		return dir, nil, err
	}
	isDir, err := sys.IsDir(dir)
	if err != nil {
		return dir, nil, err
	}
	var fromProject []string
	if isDir {
		if fromProject, err = gate.Refs(os.DirFS(dir)); err != nil {
			return dir, nil, err
		}
	}
	seen := make(map[string]bool, len(fromBinary)+len(fromProject))
	refs := make([]string, 0, len(fromBinary)+len(fromProject))
	for _, ref := range append(fromBinary, fromProject...) {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return dir, refs, nil
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

func rulePoint(ref string, layers []question.Layer) doctorRule {
	set, _, err := question.Resolve(ref, layers)
	if err != nil {
		return doctorRule{Point: ref, Unusable: err.Error()}
	}
	r, origin, err := gate.LoadPoint(shipped.Files(), ref, set)
	if err != nil {
		return doctorRule{Point: ref, Unusable: err.Error()}
	}
	point := doctorRule{
		Point:  fmt.Sprintf("%s@%d", r.Name, r.RuleVersion),
		Origin: string(origin),
		File:   r.File,
	}
	if r.Schema != gate.SchemaGate {
		point.Schema = r.Schema
	}
	if r.ModeDeclared {
		point.Declared = r.Mode.String()
	}
	res, err := resolveRuleMode(r)
	if err != nil {
		point.Unusable = err.Error()
		return point
	}
	point.Mode = res.Mode.String()
	point.ThresholdsFrom = "the rule"
	if res.Pinned {
		point.ThresholdsFrom = "the lock"
	}
	if point.Schema == "" {
		point.Thresholds = doctorThresholds{
			RiskAskAt:            res.Rule.Thresholds.RiskAskAt,
			RiskDenyAt:           res.Rule.Thresholds.RiskDenyAt,
			UserRequestedRelaxAt: res.Rule.Thresholds.UserRequestedRelaxAt,
			ApprovalRelaxAt:      res.Rule.Thresholds.ApprovalRelaxAt,
			FromUntrustedBlockAt: res.Rule.Thresholds.FromUntrustedBlockAt,
		}
	}
	if point.Declared != point.Mode {
		point.Fallback = res.Reason
	}
	return point
}

func resolveRuleMode(r gate.Rule) (gate.Resolution, error) {
	calibDir, err := sys.CalibrationDir()
	if err != nil {
		return gate.Resolution{}, err
	}
	lookup := lookupLock(gate.LockPath(calibDir, r))
	ledgerDir, err := sys.LogDir()
	if err != nil {
		return gate.Resolution{}, err
	}
	current, err := currentBuild(ledgerDir, r.Questions)
	if err != nil {
		return gate.Resolution{}, err
	}
	return gate.Resolve(r, lookup, current), nil
}

func lookupLock(path string) gate.LockLookup {
	present, err := sys.Exists(path)
	if err != nil {
		return gate.LockLookup{Present: true, Err: err}
	}
	if !present {
		return gate.LockLookup{}
	}
	lock, err := gate.LoadLock(path)
	return gate.LockLookup{Present: true, Lock: lock, Err: err}
}

func currentBuild(ledgerDir, point string) (gate.Current, error) {
	var current gate.Current
	var lastAt time.Time
	_, err := ledger.NewReader(ledgerDir).Each(ledger.Filter{Point: point}, func(row ledger.Row) error {
		if current.Known && row.At.Before(lastAt) {
			return nil
		}
		current = gate.Current{Build: row.Build, QuestionsVersion: row.Version, Known: true}
		lastAt = row.At
		return nil
	})
	if err != nil {
		return gate.Current{}, err
	}
	return current, nil
}

func ledgerState() string {
	dir, err := sys.LogDir()
	if err != nil {
		return "unknown: " + err.Error()
	}
	stats, err := ledger.Summary(dir)
	if err != nil {
		return doctorUnreadable + err.Error()
	}
	return stats.String()
}
