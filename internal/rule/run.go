package rule

import (
	"fmt"
	"time"
)

type Fire struct {
	RuleID     string
	Mode       Mode
	Target     string
	Findings   []Finding
	Blocked    bool
	Overridden bool
	At         time.Time
}

func Run(r Rule, checkers map[string]Checker, a Artifact, target string, now time.Time) (Fire, error) {
	checker, ok := checkers[r.Checker]
	if !ok {
		return Fire{}, fmt.Errorf("rule %q names checker %q, which is not registered", r.ID, r.Checker)
	}
	findings, err := checker(a)
	if err != nil {
		return Fire{}, err
	}
	for i := range findings {
		findings[i].RuleID = r.ID
	}
	return Fire{
		RuleID:   r.ID,
		Mode:     r.Mode,
		Target:   target,
		Findings: findings,
		Blocked:  r.Mode == ModeEnforced && len(findings) > 0,
		At:       now,
	}, nil
}
