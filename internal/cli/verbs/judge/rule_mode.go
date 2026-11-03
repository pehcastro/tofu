package judge

import (
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/sys"
)

func ResolveRuleMode(r gate.Rule) (gate.Resolution, error) {
	calibDir, err := sys.CalibrationDir()
	if err != nil {
		return gate.Resolution{}, err
	}
	lookup := lookupLock(gate.LockPath(calibDir, r))
	ledgerDir, err := sys.LogDir()
	if err != nil {
		return gate.Resolution{}, err
	}
	current, err := CurrentBuild(ledgerDir, r.Questions)
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

func CurrentBuild(ledgerDir, point string) (gate.Current, error) {
	row, found, err := ledger.NewReader(ledgerDir).Newest(ledger.Filter{Point: point})
	if err != nil || !found {
		return gate.Current{}, err
	}
	return gate.Current{Build: row.Build, QuestionsVersion: row.Version, Known: true}, nil
}
