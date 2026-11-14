package wiring

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"tofu/internal/host"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	question "tofu/internal/judge/questions"
	"tofu/internal/platform/fs"
	"tofu/internal/platform/project"
	shipped "tofu/library"
	"tofu/library/questions"
)

func ReadLibrary(places project.Places, placesErr error) (host.DoctorLibrary, []host.DoctorRule) {
	dir, refs, err := ruleRefs()
	if err != nil {
		return host.DoctorLibrary{Dir: dir, Unreadable: err.Error()}, nil
	}
	layers, err := question.Layers(questions.Files(), "")
	if err != nil {
		return host.DoctorLibrary{Dir: dir, Unreadable: err.Error()}, nil
	}
	library := host.DoctorLibrary{Dir: dir, Points: len(refs)}
	points := make([]host.DoctorRule, 0, len(refs))
	for _, ref := range refs {
		point := rulePoint(ref, layers, places, placesErr)
		if point.Origin == string(gate.OriginProject) {
			library.FromProject++
		}
		points = append(points, point)
	}
	return library, points
}

func ruleRefs() (string, []string, error) {
	dir, err := fs.LibraryDir()
	if err != nil {
		return "", nil, err
	}
	fromBinary, err := gate.Refs(shipped.Files())
	if err != nil {
		return dir, nil, err
	}
	isDir, err := fs.IsDir(dir)
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

func CalibrationState(places project.Places, placesErr error) string {
	if placesErr != nil {
		return "unknown: " + placesErr.Error()
	}
	dir := places.Calibration()
	isDir, err := fs.IsDir(dir)
	if err != nil {
		return Unreadable + err.Error()
	}
	if !isDir {
		return "none, no lock in " + dir
	}
	names, err := fs.ListFiles(dir, ".lock")
	if err != nil {
		return Unreadable + err.Error()
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

func rulePoint(ref string, layers []question.Layer, places project.Places, placesErr error) host.DoctorRule {
	set, _, err := question.Resolve(ref, layers)
	if err != nil {
		return host.DoctorRule{Point: ref, Unusable: err.Error()}
	}
	r, origin, err := gate.LoadPoint(shipped.Files(), ref, set, "")
	if err != nil {
		return host.DoctorRule{Point: ref, Unusable: err.Error()}
	}
	point := host.DoctorRule{
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
	res, err := gate.Resolution{}, placesErr
	if err == nil {
		res, err = ResolveRuleMode(places, r)
	}
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
		point.Thresholds = host.DoctorThresholds{
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

func LedgerState(places project.Places, placesErr error) string {
	if placesErr != nil {
		return "unknown: " + placesErr.Error()
	}
	stats, err := ledger.Summary(places.Log)
	if err != nil {
		return Unreadable + err.Error()
	}
	return stats.String()
}
