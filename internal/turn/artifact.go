package turn

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"tofu/internal/classifier"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/library/questions"
)

const truncationMarker = "\n...(truncated)...\n"

const droppedMarker = "\n...(%s dropped from the middle of this result, which was not stored anywhere. " +
	"run a narrower command to read the part you need.)...\n"

const unstoredMarker = "\n...(%s dropped from the middle of this result. the whole output could not be stored, " +
	"so there is no artifact handle for it and artifact_fetch cannot reach the missing bytes. " +
	"run a narrower command to read the part you need.)...\n"

const lineCutMarker = "…(%d characters cut)…"

const escape = 0x1b

const artifactHandleBytes = 16

const heldDropLead = "bash: the command printed "

type Artifacts struct {
	store    *recall.Store
	preview  recall.Config
	handles  bool
	standing recall.StandingOrders
	keep     *keepJudge
}

const (
	keepResultRef     = "keep_result@1"
	keepResultPoint   = "keep_result"
	keepResultName    = "needed"
	keepResultBuilder = "keep_result_state@1"
	refetchedOutcome  = "refetched"
)

type keepJudge struct {
	client    *jev.Client
	unasked   error
	set       question.Set
	asked     jev.Question
	threshold float64
	ledger    *ledger.Writer
	notify    func(string)
	task      string
	unfound   string
	earlier   func() ([]session.Event, error)
	mu        sync.Mutex
	decided   map[string]keepDecision
	dropped   map[string]string
}

type keepDecision struct {
	row     string
	verdict ledger.Verdict
}

type dropCandidate struct {
	at       int
	step     int
	call     llm.ToolCall
	reason   recall.DropReason
	decision string
}

func newKeepJudge(project, log, task string, notify func(string)) *keepJudge {
	judge := &keepJudge{ledger: ledger.NewWriter(log), notify: notify, task: task, decided: map[string]keepDecision{}, dropped: map[string]string{}}
	judge.unasked = judge.open(project)
	return judge
}

func (j *keepJudge) open(project string) error {
	layers, err := question.Layers(questions.Files(), project)
	if err != nil {
		return err
	}
	set, fields, err := question.Resolve(keepResultRef, layers)
	if err != nil {
		return err
	}
	asked, found := set.Question(keepResultName)
	at := slices.IndexFunc(fields, func(f question.Field) bool { return f.Path == "thresholds.keep_at" })
	if !found || at < 0 {
		return errors.New(keepResultRef + " names no " + keepResultName + " question or no thresholds.keep_at")
	}
	if j.threshold, err = strconv.ParseFloat(fields[at].Value, 64); err != nil {
		return err
	}
	j.set, j.asked = set, asked.ToJev()
	j.client, err = classifier.Open(project, jev.ReadKeyring(), konst.CarryStandingJudged)
	return err
}

func (j *keepJudge) ordered(preview recall.Config, messages []llm.Message, candidates []dropCandidate, keptYield bool) []dropCandidate {
	latest := ""
	for _, message := range messages {
		if message.Role == llm.RoleAssistant {
			latest = message.Content
			for _, call := range message.ToolCalls {
				latest += "\n" + call.Name + " " + string(call.Arguments)
			}
		}
	}
	decided := make([]keepDecision, len(candidates))
	var asking sync.WaitGroup
	for i, candidate := range candidates {
		asking.Go(func() {
			decided[i] = j.decide(candidate.call, shownToJudge(preview, WithoutToolRef(messages[candidate.at].Content)), shownToJudge(preview, strings.TrimSpace(latest)))
		})
	}
	asking.Wait()
	var judged, aged, kept []dropCandidate
	for i, candidate := range candidates {
		candidate.decision = decided[i].row
		switch decided[i].verdict {
		case ledger.VerdictDeny:
			candidate.reason = recall.DroppedJudged
			judged = append(judged, candidate)
		case ledger.VerdictAllow:
			kept = append(kept, candidate)
		default:
			aged = append(aged, candidate)
		}
	}
	if !keptYield {
		kept = nil
	}
	return slices.Concat(judged, aged, kept)
}

func shownToJudge(preview recall.Config, text string) string {
	if len(text) <= preview.HeadBytes+preview.TailBytes {
		return text
	}
	return runeSafeHead(text, preview.HeadBytes) + truncationMarker + runeSafeTail(text, preview.TailBytes)
}

func (j *keepJudge) decide(call llm.ToolCall, result, latest string) keepDecision {
	j.mu.Lock()
	decided, done := j.decided[call.ID]
	j.mu.Unlock()
	if done {
		return decided
	}
	body, _ := json.Marshal(struct {
		Task    string `json:"task"`
		Unfound string `json:"no_task,omitempty"`
		Call    string `json:"call"`
		Result  string `json:"result"`
		Latest  string `json:"latest"`
	}{j.task, j.unfound, call.Name + " " + string(call.Arguments), result, latest})
	row := ledger.Row{Point: keepResultPoint, Questions: j.set.Name, Version: j.set.QuestionsVersion, StateHash: ledger.HashOf(body), StateBuilder: keepResultBuilder, State: body, Verdict: ledger.VerdictAsk,
		Reason: &ledger.Reason{Question: keepResultName, Comparison: ">= " + strconv.FormatFloat(j.threshold, 'f', -1, 64), Threshold: j.threshold, Mode: ledger.ModeEnforced}}
	if j.unasked != nil {
		why := "jev cannot be asked, so the age rule decides: " + j.unasked.Error()
		row.Reason.ModeReason = &why
	} else if decision, err := j.client.Ask(context.Background(), jev.Request{State: body, Questions: []jev.Question{j.asked}}); err != nil {
		why := "jev could not answer, so the age rule decides: " + err.Error()
		row.Reason.ModeReason = &why
	} else {
		decision.Record(&row, []jev.Question{j.asked}, j.set.QuestionsVersion)
		row.Reason.Value, row.Verdict = row.Answers[0].Noul, ledger.VerdictDeny
		if row.Reason.Value >= j.threshold {
			row.Verdict = ledger.VerdictAllow
		}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	written, err := j.ledger.Append(row)
	decided = keepDecision{row: written.ID, verdict: row.Verdict}
	if err != nil {
		decided = keepDecision{verdict: ledger.VerdictAsk}
		if j.notify != nil {
			j.notify("a keep_result decision was not written to the ledger, so the age rule decides that result: " + err.Error())
		}
	}
	j.decided[call.ID] = decided
	return decided
}

func (j *keepJudge) refetched(called ToolCallRow) string {
	var args fetchArgs
	if called.Tool != (FetchTool{}).Name() || called.Error != "" || json.Unmarshal(called.Args, &args) != nil {
		return ""
	}
	row, known := j.dropped[args.Handle]
	if !known && j.earlier != nil {
		row = droppedEarlier(j.earlier, args.Handle)
	}
	if row == "" {
		return ""
	}
	j.dropped[args.Handle] = ""
	said := "artifact " + args.Handle + " was read again after compaction dropped it, so " + keepResultPoint + " row " + row
	if err := j.ledger.Backfill(row, ledger.Outcome{Kind: refetchedOutcome, Detail: string(ledger.VerdictAllow)}); err != nil {
		return said + " could not be labelled " + refetchedOutcome + ": " + err.Error()
	}
	return said + " is labelled " + refetchedOutcome
}

func droppedEarlier(earlier func() ([]session.Event, error), handle string) string {
	events, err := earlier()
	if err != nil {
		return ""
	}
	decision := ""
	for _, event := range events {
		var row compactionRow
		if event.Kind != session.EventCompaction || json.Unmarshal(event.Body, &row) != nil || row.Compaction == nil {
			continue
		}
		for _, drop := range row.Compaction.Drops {
			if drop.Handle == handle {
				decision = drop.Decision
			}
		}
	}
	return decision
}

func NewArtifacts(dir string, handles bool) (Artifacts, error) {
	preview, err := recall.LoadConfig()
	if err != nil {
		return Artifacts{}, err
	}
	return Artifacts{store: recall.NewStore(dir), preview: preview, handles: handles, standing: recall.WordsAlone}, nil
}

func (a Artifacts) Render(tool string, args json.RawMessage, content string, bytesCap int) (string, string, error) {
	shown, wideLines := content, 0
	if capsLineWidth(tool, args) {
		shown, wideLines = capLineWidths(content)
	}
	if wideLines == 0 && len(content) <= bytesCap {
		return content, "", nil
	}
	if !a.handles {
		return cutOnRuneBoundary(shown, bytesCap, droppedMarker), "", nil
	}
	stored, err := recall.Elide(a.store, recall.Config{}, []byte(content), true)
	if err != nil {
		return cutOnRuneBoundary(shown, bytesCap, unstoredMarker), "", err
	}
	holds := "this result whole"
	if strings.HasPrefix(content, heldDropLead) {
		holds = "what tofu kept of this result"
	}
	header := fmt.Sprintf("artifact %s holds %s: %d bytes, ", stored.Reference.ID, holds, len(content))
	if wideLines > 0 {
		header += fmt.Sprintf("and %d lines wider than %d bytes are cut to their start and end below. ", wideLines, konst.TurnResultLineWidth)
	}
	if len(shown) <= bytesCap {
		return header + "call artifact_fetch with this handle, an offset and a length to read what was cut.\n" + shown, stored.Reference.ID, nil
	}
	head, tail := runeSafeHead(shown, a.preview.HeadBytes), runeSafeTail(shown, a.preview.TailBytes)
	return fmt.Sprintf(
		"%sthe first %d and the last %d bytes follow. "+
			"call artifact_fetch with this handle, an offset and a length to read any other range.\n%s%s%s",
		header, len(head), len(tail), head, truncationMarker, tail,
	), stored.Reference.ID, nil
}

func capsLineWidth(tool string, args json.RawMessage) bool {
	switch tool {
	case "artifact_fetch", "search":
		return false
	case "read":
		var ranged readArgs
		return json.Unmarshal(args, &ranged) != nil || (ranged.StartLine <= 0 && ranged.EndLine <= 0)
	}
	return true
}

func capLineWidths(content string) (string, int) {
	var capped strings.Builder
	wide := 0
	for line := range strings.SplitAfterSeq(content, "\n") {
		body := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if len(body) <= konst.TurnResultLineWidth {
			capped.WriteString(line)
			continue
		}
		wide++
		capped.WriteString(cutWideLine(body))
		capped.WriteString(line[len(body):])
	}
	if wide == 0 {
		return content, 0
	}
	return capped.String(), wide
}

func cutWideLine(line string) string {
	head := runeSafeHead(line, konst.TurnResultLineWidth/2)
	if at := strings.LastIndexByte(head, escape); at >= 0 && escapeEnd(line, at) > len(head) {
		head = head[:at]
	}
	tailStart := len(line) - len(runeSafeTail(line, konst.TurnResultLineWidth/2))
	if at := strings.LastIndexByte(line[:tailStart], escape); at >= 0 {
		tailStart = max(tailStart, escapeEnd(line, at))
	}
	return head + fmt.Sprintf(lineCutMarker, utf8.RuneCountInString(line[len(head):tailStart])) + line[tailStart:]
}

func escapeEnd(line string, at int) int {
	end := at + 1
	if end < len(line) && line[end] == '[' {
		end++
		for end < len(line) && line[end] >= ' ' && line[end] <= '?' {
			end++
		}
	}
	return min(end+1, len(line))
}

func cutOnRuneBoundary(content string, bytesCap int, marker string) string {
	if len(content) <= bytesCap {
		return content
	}
	head := runeSafeHead(content, bytesCap/2)
	tail := runeSafeTail(content, bytesCap-len(head))
	dropped := len(content) - len(head) - len(tail)
	amount := fmt.Sprintf("%d bytes", dropped)
	if dropped == 1 {
		amount = "1 byte"
	}
	return head + fmt.Sprintf(marker, amount) + tail
}

func (a Artifacts) FetchTool() FetchTool {
	return FetchTool{store: a.store}
}

type FetchTool struct {
	store *recall.Store
}

func (t FetchTool) Name() string { return "artifact_fetch" }

func (t FetchTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "artifact_fetch",
		Description: "reads a byte range out of a tool result that was stored whole instead of pasted. " +
			"the handle comes from the artifact line in that result",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"handle": map[string]any{"type": "string"},
				"offset": map[string]any{"type": "integer"},
				"length": map[string]any{"type": "integer"},
			},
			"required": []string{"handle", "offset", "length"},
		},
	}
}

type fetchArgs struct {
	Handle string `json:"handle"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

func (t FetchTool) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	var args fetchArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("artifact_fetch: arguments are not the expected shape: %w", err)
	}
	decoded, err := hex.DecodeString(args.Handle)
	if err != nil || len(decoded) != artifactHandleBytes {
		return Result{}, fmt.Errorf("artifact_fetch: %q is not an artifact handle", args.Handle)
	}
	if args.Length <= 0 {
		return Result{}, fmt.Errorf("artifact_fetch: length %d is not a range, ask for at least one byte", args.Length)
	}
	if args.Offset < 0 {
		return Result{}, fmt.Errorf("artifact_fetch: offset %d is before the start of artifact %s", args.Offset, args.Handle)
	}
	if t.store == nil {
		return Result{}, fmt.Errorf("artifact_fetch: this turn stored no artifacts, so handle %s cannot be read", args.Handle)
	}
	body, err := t.store.Fetch(args.Handle)
	if err != nil {
		return Result{}, fmt.Errorf("artifact_fetch: no stored result for handle %s: %w", args.Handle, err)
	}
	if args.Offset >= len(body) {
		return Result{}, fmt.Errorf("artifact_fetch: offset %d is past the end of artifact %s, which is %d bytes",
			args.Offset, args.Handle, len(body))
	}
	end := min(args.Offset+args.Length, len(body))
	return Result{
		Content: string(body[args.Offset:end]),
		Command: fmt.Sprintf("%s bytes %d to %d of %d", args.Handle, args.Offset, end, len(body)),
	}, nil
}
