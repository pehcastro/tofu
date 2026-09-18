package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"boji/internal/judge/ledger"
	"boji/internal/konst"
)

func printWhy(out io.Writer, wr whyRow, now time.Time, color bool, precedent *ledger.Row) {
	if wr.isReplay {
		_, _ = fmt.Fprintf(out, "%s is a replay of %s, recorded %s ago\n\n", wr.queried.ID, wr.chain.ID, agoString(now.Sub(wr.queried.At)))
	}
	row := wr.chain
	state := row.StateBuilder
	if state == "" {
		state = "absent, writer has not adopted the state builder"
		if row.Schema < ledger.SchemaVersion {
			state = fmt.Sprintf("absent, schema %d predates the state builder", row.Schema)
		}
	}
	_, _ = fmt.Fprintf(out, "%s  %s  %s@v%d  state %s  %s ago\n", row.Point, row.Build, row.Questions, row.Version, state, agoString(now.Sub(row.At)))
	for _, answer := range row.Answers {
		printAnswer(out, answer)
	}
	_, _ = fmt.Fprintln(out)
	if row.Verdict != ledger.VerdictUnset {
		_, _ = fmt.Fprintf(out, "  verdict  %s\n", colorVerdict(row.Verdict, color))
	}
	printThreshold(out, row)
	if precedent != nil {
		_, _ = fmt.Fprintf(out, "  nearest precedent (exact state match)  %s, %s ago\n", precedent.ID, agoString(now.Sub(precedent.At)))
	}
}

func printThreshold(out io.Writer, row ledger.Row) {
	r := row.Reason
	if r == nil {
		_, _ = fmt.Fprintln(out, "  threshold  absent: no policy or calibration lock yet, waiting on E3")
		return
	}
	_, _ = fmt.Fprintf(out, "  threshold  %s %.2f vs %s %.2f  (%s@v%d)\n", r.Question, r.Value, r.Comparison, r.Threshold, row.Policy, row.PolicyVersion)
	if r.Ambiguous != "" {
		_, _ = fmt.Fprintf(out, "  ambiguous  %s sat inside the dead band of its threshold\n", r.Ambiguous)
	}
}

func printAnswer(out io.Writer, answer ledger.Answer) {
	if answer.Kind == ledger.AnswerNoul {
		p := answer.Noul
		_, _ = fmt.Fprintf(out, "  %-16s %.2f\n", answer.Question, p)
		printBar(out, "yes", p)
		printBar(out, "no", 1-p)
		return
	}
	chosen := answer.Choice
	if answer.Kind == ledger.AnswerScore {
		chosen = strconv.FormatFloat(answer.Score, 'g', -1, 64)
	}
	_, _ = fmt.Fprintf(out, "  %-16s chose %s\n", answer.Question, chosen)
	for _, slice := range answer.Dist {
		printBar(out, slice.Option, slice.P)
	}
}

func printBar(out io.Writer, label string, p float64) {
	_, _ = fmt.Fprintf(out, "    %-8s %.2f  %s\n", label, p, bar(p))
}

func bar(p float64) string {
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	filled := int(p*konst.WhyBarWidthChars + 0.5)
	return strings.Repeat("█", filled) + strings.Repeat("░", konst.WhyBarWidthChars-filled)
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

func thresholdJSON(row ledger.Row) map[string]any {
	r := row.Reason
	if r == nil {
		return map[string]any{"present": false, "note": "no policy or calibration lock yet, waiting on E3"}
	}
	out := map[string]any{
		"present":        true,
		"question":       r.Question,
		"comparison":     r.Comparison,
		"threshold":      r.Threshold,
		"value":          r.Value,
		"policy":         row.Policy,
		"policy_version": row.PolicyVersion,
	}
	if r.Ambiguous != "" {
		out["ambiguous"] = r.Ambiguous
	}
	return out
}

func rowFields(row ledger.Row) (map[string]any, error) {
	raw, err := json.Marshal(row)
	if err != nil {
		return nil, err
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func printWhyJSON(out io.Writer, wr whyRow, now time.Time, precedent *ledger.Row) error {
	fields, err := rowFields(wr.queried)
	if err != nil {
		return err
	}
	fields["ago"] = agoString(now.Sub(wr.chain.At))
	fields["threshold"] = thresholdJSON(wr.chain)
	if precedent != nil {
		fields["precedent"] = map[string]any{
			"row_id":      precedent.ID,
			"exact_match": true,
			"ago":         agoString(now.Sub(precedent.At)),
		}
	}
	if wr.isReplay {
		chain, err := rowFields(wr.chain)
		if err != nil {
			return err
		}
		fields["chain"] = chain
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(body))
	return err
}
