package main

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/widget"
)

func whyPage(page cli.Page, report whyReport, now time.Time) []string {
	var lines []string
	if report.Call != nil {
		lines = callLines(page, *report.Call)
	}
	for _, listing := range report.Rows {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, decisionLines(page, listing, now)...)
	}
	return lines
}

func decisionLines(page cli.Page, listing whyListing, now time.Time) []string {
	row := listing.decision()
	ago := func(at time.Time) string { return widget.Until(now.Sub(at)) + " ago" }
	lines := append(page.Title(row.Point, []string{row.ID, ago(row.At)}, verdictLook(row.Verdict)), "")
	var answers []cli.Fact
	for _, answer := range row.Answers {
		if answer.Kind == ledger.AnswerNoul {
			answers = append(answers, cli.Fact{Label: answer.Question, Text: page.Bar(answer.Noul)})
			continue
		}
		chosen := answer.Choice
		if answer.Kind == ledger.AnswerScore {
			chosen = strconv.FormatFloat(answer.Score, 'g', -1, 64)
		}
		answers = append(answers, cli.Fact{Label: answer.Question, Text: "chose " + chosen})
		for _, slice := range answer.Dist {
			answers = append(answers, cli.Fact{Label: cli.Gap + slice.Option, Text: page.Bar(slice.P)})
		}
	}
	if len(answers) > 0 {
		lines = append(append(lines, cli.Indent(page.Facts(answers)...)...), "")
	}
	replay := ""
	if listing.Chain != nil {
		replay = listing.ID + " · " + ago(listing.At)
	}
	state, body, hint := stateFacts(page, listing)
	facts := append([]cli.Fact{
		{Label: "replayed as", Text: replay},
		{Label: "threshold", Text: thresholdText(row)},
		{Label: "ambiguous", Text: ambiguousText(row)},
		{Label: "authority", Text: authorityText(row, listing.BlockedBy)},
		{Label: "mode", Text: modeText(row)},
		{Label: "outcome", Text: outcomeText(row.Outcome)},
		{Label: "questions", Text: row.Questions + "@" + strconv.Itoa(row.Version)},
		{Label: "builder", Text: builderText(row)},
		{Label: "build", Text: row.Build},
	}, state...)
	lines = append(lines, cli.Indent(page.Facts(facts)...)...)
	if body != "" {
		lines = append(lines, cli.Indent(cli.Indent(body)...)...)
	}
	if hint != "" {
		lines = append(lines, cli.Indent(page.Hint(hint))...)
	}
	if len(listing.Precedents) == 0 {
		return lines
	}
	rows := make([]cli.Row, len(listing.Precedents))
	for i, precedent := range listing.Precedents {
		look := verdictLook(precedent.Verdict)
		rows[i] = cli.Row{Mark: look.Mark, Cells: []string{precedent.ID, look.Text, precedent.Why}, Detail: ago(precedent.At)}
		if precedent.Outcome != nil {
			rows[i].Detail += " · " + outcomeText(precedent.Outcome)
		}
	}
	lines = append(lines, "", page.Subject("precedents")+page.Label(" · "+strconv.Itoa(len(rows))+" at this point, nearest first"))
	return append(lines, cli.Indent(page.Rows(rows)...)...)
}

func verdictLook(v ledger.Verdict) cli.Verdict {
	switch v {
	case ledger.VerdictAllow:
		return cli.Verdict{Mark: cli.Done, Text: "ALLOW"}
	case ledger.VerdictAsk:
		return cli.Verdict{Mark: cli.Warn, Text: "ASK"}
	case ledger.VerdictDeny:
		return cli.Verdict{Mark: cli.Fail, Text: "DENY"}
	case ledger.VerdictUnset:
		return cli.Verdict{Mark: cli.Idle, Text: "no verdict"}
	}
	panic("tofu why: unknown verdict " + string(v))
}

func precedentWhy(found ledger.Precedent) string {
	if found.SameFingerprint {
		return "the same call"
	}
	if !found.Comparable {
		return "no question in common"
	}
	return fmt.Sprintf("answers %.3f away", found.Distance)
}

func outcomeText(outcome *ledger.Outcome) string {
	if outcome == nil {
		return ""
	}
	return strings.TrimSpace(outcome.Kind + " " + outcome.Detail)
}

func thresholdText(row ledger.Row) string {
	r := row.Reason
	switch {
	case r == nil:
		return "none recorded"
	case gate.IsUnavailable(r.Comparison):
		return "not compared · " + r.Comparison
	}
	return fmt.Sprintf("%s %.2f vs %s %.2f · %s@%d", r.Question, r.Value, r.Comparison, r.Threshold, row.Policy, row.PolicyVersion)
}

func ambiguousText(row ledger.Row) string {
	if row.Reason == nil || row.Reason.Ambiguous == "" {
		return ""
	}
	return row.Reason.Ambiguous + " in the dead band"
}

func authorityText(row ledger.Row, blockedBy string) string {
	r := row.Reason
	switch {
	case r == nil:
		return ""
	case r.Blocked && blockedBy == "":
		return fmt.Sprintf("blocked by the from-untrusted question of %s@%d", row.Policy, row.PolicyVersion)
	case r.Blocked:
		return "blocked by " + questionWithValue(row, blockedBy)
	case r.RelaxedBy != "":
		return "relaxed by " + questionWithValue(row, r.RelaxedBy)
	}
	return ""
}

func questionWithValue(row ledger.Row, question string) string {
	for _, answer := range row.Answers {
		if answer.Question == question && answer.Kind == ledger.AnswerNoul {
			return fmt.Sprintf("%s %.2f", question, answer.Noul)
		}
	}
	return question
}

func modeText(row ledger.Row) string {
	r := row.Reason
	if r == nil || r.Mode == ledger.ModeUnknown {
		return ""
	}
	if r.ModeReason != nil && *r.ModeReason != "" {
		return string(r.Mode) + " · " + *r.ModeReason
	}
	return string(r.Mode)
}

func builderText(row ledger.Row) string {
	switch {
	case row.StateBuilder != "":
		return row.StateBuilder
	case row.Schema < ledger.StateBuilderSchema:
		return "absent, schema " + strconv.Itoa(row.Schema) + " predates it"
	}
	return "absent"
}

func stateFacts(page cli.Page, listing whyListing) ([]cli.Fact, string, string) {
	row := listing.decision()
	if err := listing.stateErr; err != nil {
		reason := "unreadable: " + err.Error()
		var escaping ledger.EscapingStateError
		switch {
		case errors.As(err, &escaping):
			reason = "refused: " + escaping.File + " is outside the ledger"
		case errors.Is(err, fs.ErrNotExist):
			reason = "missing: " + page.Path(listing.statePath)
		}
		e := row.StateElision
		return []cli.Fact{{Label: "state", Text: widget.Size(e.Bytes) + " · " + reason}, {Label: "head", Text: e.Head}, {Label: "tail", Text: e.Tail}}, "", ""
	}
	if len(listing.state) == 0 && row.Schema < ledger.StateBodySchema {
		return []cli.Fact{{Label: "state", Text: "none, schema " + strconv.Itoa(row.Schema) + " predates it"}}, "", ""
	}
	if len(listing.state) == 0 {
		return []cli.Fact{{Label: "state", Text: "none recorded"}}, "", ""
	}
	body, hint := string(listing.state), ""
	if len(body) > konst.WhyStateBytes || widget.Cells(body)+2*len(cli.Gap) > page.Width {
		body, hint = strings.ToValidUTF8(body[:min(len(body), konst.WhyStateBytes)], ""), "tofu why "+row.ID+" --state"
	}
	return []cli.Fact{{Label: "state", Text: widget.Size(len(listing.state))}}, body, hint
}

func colorVerdict(v ledger.Verdict, color bool) string {
	label := strings.ToUpper(string(v))
	if !color {
		return label
	}
	code := ""
	switch v {
	case ledger.VerdictAllow:
		code = "32"
	case ledger.VerdictAsk:
		code = "33"
	case ledger.VerdictDeny:
		code = "31"
	case ledger.VerdictUnset:
		return label
	}
	return "\x1b[" + code + "m" + label + "\x1b[0m"
}

func agoString(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%d ms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.0f s", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%.0f m", d.Minutes())
	case d < 24*time.Hour:
		return fmt.Sprintf("%.0f h", d.Hours())
	default:
		return fmt.Sprintf("%.0f d", d.Hours()/24)
	}
}
