package wiring

import (
	"strconv"
	"strings"

	"tofu/internal/cli/page"
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

func FiresVerdict(fired, blocked int) cli.Verdict {
	switch {
	case fired == 0:
		return cli.Verdict{Mark: cli.Done, Text: "nothing fired"}
	case blocked == 0:
		return cli.Verdict{Mark: cli.Warn, Text: strconv.Itoa(fired) + " fired"}
	}
	return cli.Verdict{Mark: cli.Fail, Text: strconv.Itoa(fired) + " fired · " + strconv.Itoa(blocked) + " blocked"}
}
