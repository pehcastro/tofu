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
}

func Resolve(pol Policy, lookup LockLookup, current Current) Resolution {
	point := fmt.Sprintf("%s@%d", pol.Name, pol.PolicyVersion)
	if !pol.ModeDeclared {
		return Resolution{Mode: ModeShadow, Reason: "the policy declares no mode"}
	}
	if pol.Mode == ModeShadow {
		return Resolution{Mode: ModeShadow, Reason: "declared shadow in " + pol.File}
	}
	if !lookup.Present {
		reason := "no lock file for " + point
		if current.Known {
			reason += " at build " + current.Build
		}
		return Resolution{Mode: ModeShadow, Reason: reason}
	}
	if lookup.Err != nil {
		return Resolution{Mode: ModeShadow, Reason: fmt.Sprintf("lock file for %s is unreadable: %v", point, lookup.Err)}
	}
	if !current.Known {
		return Resolution{Mode: ModeShadow, Reason: fmt.Sprintf("no rows recorded for %s yet, the current build is unknown", point)}
	}
	lock := lookup.Lock
	if lock.Policy != pol.Name || lock.PolicyVersion != pol.PolicyVersion || lock.Questions != pol.Questions {
		return Resolution{Mode: ModeShadow, Reason: fmt.Sprintf("lock names %s@%d over %s, not %s", lock.Policy, lock.PolicyVersion, lock.Questions, point)}
	}
	if lock.Build != current.Build {
		return Resolution{Mode: ModeShadow, Reason: fmt.Sprintf("lock build %s does not match current build %s", lock.Build, current.Build)}
	}
	if lock.QuestionsVersion != current.QuestionsVersion {
		return Resolution{Mode: ModeShadow, Reason: fmt.Sprintf("lock questions_version %d does not match current questions_version %d", lock.QuestionsVersion, current.QuestionsVersion)}
	}
	n := lock.NFit
	if lock.NVerify < n {
		n = lock.NVerify
	}
	if n < pol.SampleFloor {
		return Resolution{Mode: ModeShadow, Reason: fmt.Sprintf("sample size %d is below the floor %d, .local/research/calibration-design.md §3", n, pol.SampleFloor)}
	}
	return Resolution{Mode: ModeEnforced}
}

func ExitCode(mode Mode, verdict Verdict) int {
	if mode == ModeEnforced && verdict == VerdictDeny {
		return 1
	}
	return 0
}
