package gate

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
	Rule   Rule
	Pinned bool
}

func Resolve(r Rule, lookup LockLookup, current Current) Resolution {
	mode, reason := resolveMode(r, lookup, current)
	if mode != ModeEnforced || !lookup.Lock.PinsThresholds {
		return Resolution{Mode: mode, Reason: reason, Rule: r}
	}
	r.Thresholds = lookup.Lock.Thresholds
	return Resolution{Mode: mode, Reason: reason, Rule: r, Pinned: true}
}

func resolveMode(r Rule, lookup LockLookup, current Current) (Mode, string) {
	point := fmt.Sprintf("%s@%d", r.Name, r.RuleVersion)
	if !r.ModeDeclared {
		return ModeShadow, "the rule declares no mode"
	}
	if r.Mode == ModeShadow {
		return ModeShadow, "declared shadow in " + r.File
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
	if lock.Rule != r.Name || lock.RuleVersion != r.RuleVersion || lock.Questions != r.Questions {
		return ModeShadow, fmt.Sprintf("lock names %s@%d over %s, not %s", lock.Rule, lock.RuleVersion, lock.Questions, point)
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
	if n < r.SampleFloor {
		return ModeShadow, fmt.Sprintf("sample size %d is below the floor %d, .local/research/calibration-design.md §3", n, r.SampleFloor)
	}
	return ModeEnforced, ""
}

func ExitCode(mode Mode, verdict Verdict) int {
	if mode == ModeEnforced && verdict == VerdictDeny {
		return 1
	}
	return 0
}
