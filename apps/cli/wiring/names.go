package wiring

import (
	"strconv"
	"strings"

	"tofu/apps/cli/page"
	"tofu/internal/judge/ledger"
)

const roundMark = "-r"

func OwnerName(owner string) string {
	at := strings.LastIndex(owner, roundMark)
	if at < 0 {
		return owner
	}
	if _, err := strconv.Atoi(owner[at+len(roundMark):]); err != nil {
		return owner
	}
	return owner[:at]
}

func FiresVerdict(fired, blocked int) page.Verdict {
	switch {
	case fired == 0:
		return page.Verdict{Mark: page.Done, Text: "nothing fired"}
	case blocked == 0:
		return page.Verdict{Mark: page.Warn, Text: strconv.Itoa(fired) + " fired"}
	}
	return page.Verdict{Mark: page.Fail, Text: strconv.Itoa(fired) + " fired · " + strconv.Itoa(blocked) + " blocked"}
}

func VerdictLook(v ledger.Verdict) page.Verdict {
	switch v {
	case ledger.VerdictAllow:
		return page.Verdict{Mark: page.Done, Text: "ALLOW"}
	case ledger.VerdictAsk:
		return page.Verdict{Mark: page.Warn, Text: "ASK"}
	case ledger.VerdictDeny:
		return page.Verdict{Mark: page.Fail, Text: "DENY"}
	case ledger.VerdictUnset:
		return page.Verdict{Mark: page.Idle, Text: "no verdict"}
	}
	panic("tofu why: unknown verdict " + string(v))
}
