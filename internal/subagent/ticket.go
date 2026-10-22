package subagent

import "strings"

const SessionEnv = "TOFU_SESSION"

func InForce(revisions []string) []string {
	if len(revisions) == 0 {
		return nil
	}
	return acceptanceBullets(strings.Split(revisions[len(revisions)-1], "\n"))
}
