package tui

import (
	"strconv"
	"strings"

	"tofu/interface/tui/session"
	"tofu/internal/host"
)

const (
	permissionsRevoke = "/permissions revoke "
	permissionsHead   = "permissions"
	givenLayout       = "2006-01-02 15:04"
	nothingStanding   = "nothing stands in this session, so every call the gate asks about asks you. an answer stands once you press 3 always here or 4 never here on an ask: it then decides every later call on that target unasked, until you revoke it with /permissions revoke <n>"
)

func (a *App) listStanding() {
	standing := a.options.Host.Standing()
	if len(standing) == 0 {
		a.view.Append(session.Entry{Kind: session.Note, Head: permissionsHead, Body: nothingStanding})
		return
	}
	lines := []string{strconv.Itoa(len(standing)) + " standing in session " + a.options.Host.ID() + ", each deciding a call on its target without asking:"}
	for at, one := range standing {
		n := strconv.Itoa(at + 1)
		lines = append(lines,
			n+". "+standingVerb(one)+" "+one.Target,
			"where: "+standingWhere(one),
			"given: "+standingGiven(one)+" · until: "+standingUntil(one),
			"change: /permissions revoke "+n+" asks you again on the next call, and you answer it anew")
	}
	a.view.Append(session.Entry{Kind: session.Note, Head: permissionsHead, Body: strings.Join(lines, "\n")})
}

func (a *App) revokeStanding(which string) {
	standing := a.options.Host.Standing()
	target := which
	if n, err := strconv.Atoi(which); err == nil && n >= 1 && n <= len(standing) {
		target = standing[n-1].Target
	}
	body := "nothing stands on " + which + ": /permissions lists what does, by number"
	if gone, revoked := a.options.Host.Revoke(target); revoked {
		body = "revoked · " + standingVerb(gone) + " " + gone.Target + " · the next call on it asks you again"
	}
	a.view.Append(session.Entry{Kind: session.Note, Head: permissionsHead, Body: body})
}

func standingVerb(one host.StandingAnswer) string {
	switch one.Decision {
	case host.AllowAlways:
		return "always here: allows"
	case host.RejectAlways:
		return "never here: refuses"
	case host.AllowFor:
		return "for a while: allows"
	}
	panic("tui: a standing answer cannot be " + string(one.Decision))
}

func standingWhere(one host.StandingAnswer) string {
	switch one.Kept {
	case host.KeptBesideSession:
		return "this session and its forks, kept beside the session and read back when it reopens"
	case host.KeptUntilRestart:
		return "this session, held in memory only, since inline code is never written down"
	}
	panic("tui: a standing answer cannot be kept as " + string(one.Kept))
}

func standingGiven(one host.StandingAnswer) string {
	if one.Given.IsZero() {
		return "not recorded, it was given before tofu kept the time"
	}
	return one.Given.Local().Format(givenLayout)
}

func standingUntil(one host.StandingAnswer) string {
	switch {
	case one.Decision == host.AllowFor:
		return one.Until.Local().Format(givenLayout) + ", then it asks again"
	case one.Kept == host.KeptUntilRestart:
		return "tofu restarts, or you revoke it"
	}
	return "you revoke it"
}
