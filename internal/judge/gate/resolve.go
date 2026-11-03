package gate

import (
	"fmt"
	"path/filepath"
)

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
	if !r.ModeDeclared {
		return ModeShadow, "the rule declares no mode"
	}
	held := lockHeldBack(r, lookup, current)
	switch {
	case r.Mode != ModeShadow && held != "":
		return ModeShadow, held
	case r.Mode != ModeShadow:
		return ModeEnforced, ""
	case !lookup.Present:
		return ModeShadow, "declared shadow in " + r.File
	case lookup.Err != nil:
		return ModeShadow, "declared shadow in " + r.File + "; " + held
	case held != "":
		return ModeShadow, fmt.Sprintf("declared shadow in %s; the lock %s fits risk_ask_at %g and would still hold it in shadow: %s", r.File, filepath.Base(lookup.Lock.File), lookup.Lock.Thresholds.RiskAskAt, held)
	}
	return ModeShadow, fmt.Sprintf("declared shadow in %s; the lock %s fits risk_ask_at %g and holds, so declaring enforced would enforce it", r.File, filepath.Base(lookup.Lock.File), lookup.Lock.Thresholds.RiskAskAt)
}

func lockHeldBack(r Rule, lookup LockLookup, current Current) string {
	point := fmt.Sprintf("%s@%d", r.Name, r.RuleVersion)
	if !lookup.Present {
		reason := "no lock file for " + point
		if current.Known {
			reason += " at build " + current.Build
		}
		return reason
	}
	if lookup.Err != nil {
		return fmt.Sprintf("lock file for %s is unreadable: %v", point, lookup.Err)
	}
	if !current.Known {
		return fmt.Sprintf("no rows recorded for %s yet, the current build is unknown", point)
	}
	lock := lookup.Lock
	if lock.Rule != r.Name || lock.RuleVersion != r.RuleVersion || lock.Questions != r.Questions {
		return fmt.Sprintf("lock names %s@%d over %s, not %s", lock.Rule, lock.RuleVersion, lock.Questions, point)
	}
	if lock.Build != current.Build {
		return fmt.Sprintf("lock build %s does not match current build %s", lock.Build, current.Build)
	}
	if lock.QuestionsVersion != current.QuestionsVersion {
		return fmt.Sprintf("lock questions_version %d does not match current questions_version %d", lock.QuestionsVersion, current.QuestionsVersion)
	}
	if n := min(lock.NFit, lock.NVerify); n < r.SampleFloor {
		return fmt.Sprintf("sample size %d is below the floor %d, see tofu docs calibration", n, r.SampleFloor)
	}
	return ""
}

func ExitCode(mode Mode, verdict Verdict) int {
	if mode == ModeEnforced && verdict == VerdictDeny {
		return 1
	}
	return 0
}
