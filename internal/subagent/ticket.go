package subagent

import (
	"os"
	"strings"
)

const SessionEnv = "TOFU_SESSION"

func Session() string { return os.Getenv(SessionEnv) }

func TicketContract(id string, revisions []string, report string, stopped bool) Contract {
	if len(revisions) == 0 {
		return Contract{NoTicket: true}
	}
	return contractOf(id, acceptanceBullets(strings.Split(revisions[len(revisions)-1], "\n")), report, stopped)
}
