package policy

import "fmt"

type Current struct {
	Build            string
	QuestionsVersion int
	Known            bool
}

type LockLookup struct {
	Present bool
	Lock    Lock
	Err     error
}

type Resolution struct {
	Mode   Mode
	Reason string
	Policy Policy
	Pinned bool
}

func Resolve(pol Policy, lookup LockLookup, current Current) Resolution {
	mode, reason := resolveMode(pol, lookup, current)
	if mode != ModeEnforced || !lookup.Lock.PinsThresholds {
		return Resolution{Mode: mode, Reason: reason, Policy: pol}
	}
	pol.Thresholds = lookup.Lock.Thresholds
	return Resolution{Mode: mode, Reason: reason, Policy: pol, Pinned: true}
}

func resolveMode(pol Policy, lookup LockLookup, current Current) (Mode, string) {
	point := fmt.Sprintf("%s@%d", pol.Name, pol.PolicyVersion)
	if !pol.ModeDeclared {
		return ModeShadow, "the policy declares no mode"
	}
	if pol.Mode == ModeShadow {
		return ModeShadow, "declared shadow in " + pol.File
	}
	if !lookup.Present {
		reason := "no lock file for " + point
		if current.Known {
			reason += " at build " + current.Build
		}
		return ModeShadow, reason
	}
	if lookup.Err != nil {
		return ModeShadow, fmt.Sprintf("lock file for %s is unreadable: %v", point, lookup.Err)
	}
	if !current.Known {
		return ModeShadow, fmt.Sprintf("no rows recorded for %s yet, the current build is unknown", point)
	}
	lock := lookup.Lock
	if lock.Policy != pol.Name || lock.PolicyVersion != pol.PolicyVersion || lock.Questions != pol.Questions {
		return ModeShadow, fmt.Sprintf("lock names %s@%d over %s, not %s", lock.Policy, lock.PolicyVersion, lock.Questions, point)
	}
	if lock.Build != current.Build {
		return ModeShadow, fmt.Sprintf("lock build %s does not match current build %s", lock.Build, current.Build)
	}
	if lock.QuestionsVersion != current.QuestionsVersion {
		return ModeShadow, fmt.Sprintf("lock questions_version %d does not match current questions_version %d", lock.QuestionsVersion, current.QuestionsVersion)
	}
	n := lock.NFit
	if lock.NVerify < n {
		n = lock.NVerify
	}
	if n < pol.SampleFloor {
		return ModeShadow, fmt.Sprintf("sample size %d is below the floor %d, .local/research/calibration-design.md §3", n, pol.SampleFloor)
	}
	return ModeEnforced, ""
}

func ExitCode(mode Mode, verdict Verdict) int {
	if mode == ModeEnforced && verdict == VerdictDeny {
		return 1
	}
	return 0
}
