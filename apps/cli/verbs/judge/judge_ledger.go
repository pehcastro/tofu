package judge

import (
	"cmp"
	"context"
	"encoding/json"

	"tofu/apps/cli/wiring"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	memoryscope "tofu/internal/judge/points/memory_scope"
	question "tofu/internal/judge/questions"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/platform/project"
	"tofu/internal/tools/human/remember"
	"tofu/library/questions"
)

type Outcome struct {
	answers  []ledger.Answer
	Decision core.Decision
	fresh    bool
	verdict  ledger.Verdict
	mode     gate.Mode
}

func (o Outcome) output() map[string]any {
	out := make(map[string]any, len(o.answers))
	if o.fresh {
		for id, answer := range o.Decision.Answers {
			out[id] = jevAnswerJSON(answer)
		}
		return out
	}
	for _, answer := range o.answers {
		out[answer.Question] = ledgerAnswerJSON(answer)
	}
	return out
}

func AskFresh(ctx context.Context, client *core.Client, req core.Request, set wiring.Battery) (Outcome, ledger.Entry, error) {
	decision, err := client.Ask(ctx, req)
	if err != nil {
		state, notEncoded := json.Marshal(req.State)
		if notEncoded != nil {
			return Outcome{}, ledger.Entry{}, err
		}
		if _, unwritten := wiring.AppendFallbackRow(state, set, wiring.RowInput{}, err); unwritten != nil {
			return Outcome{}, ledger.Entry{}, unwritten
		}
		return Outcome{}, ledger.Entry{}, err
	}
	answers := wiring.ToLedgerAnswers(set.QuestionsVersion, decision.Answers)
	row, err := wiring.AppendRow(req.State, set, wiring.RowInput{Decision: &decision, Answers: answers})
	if err != nil {
		return Outcome{}, ledger.Entry{}, err
	}
	entry := ledger.Entry{RowID: row.ID, Build: decision.Build, RequestID: cmp.Or(decision.RequestID, decision.TransportID), Answers: answers}
	return Outcome{answers: answers, fresh: true, Decision: decision, verdict: row.Verdict, mode: set.Mode}, entry, nil
}

func RunJudge(ctx context.Context, client *core.Client, req core.Request, set wiring.Battery, noCache bool) (Outcome, error) {
	if noCache {
		outcome, _, err := AskFresh(ctx, client, req, set)
		return outcome, err
	}

	cacheDir, err := ledger.CacheDir()
	if err != nil {
		return Outcome{}, err
	}
	cache := ledger.NewCache(cacheDir)
	ledgerReq := ledger.Request{State: req.State, Questions: set.SetName, Model: client.Model(), Version: set.QuestionsVersion}
	key, err := cache.Key(ledgerReq)
	if err != nil {
		return Outcome{}, err
	}
	entry, hit, err := cache.Load(key)
	if err != nil {
		return Outcome{}, err
	}
	if !hit {
		outcome, entry, err := AskFresh(ctx, client, req, set)
		if err != nil {
			return Outcome{}, err
		}
		ledgerReq.Model = outcome.Decision.Alias
		if key, err = cache.Key(ledgerReq); err != nil {
			return Outcome{}, err
		}
		if err := cache.Store(key, ledgerReq, entry); err != nil {
			return Outcome{}, err
		}
		return outcome, nil
	}
	dir, err := project.LogDir()
	if err != nil {
		return Outcome{}, err
	}
	replayed, _, err := ledger.NewReader(dir).ByID(entry.RowID)
	if err != nil {
		return Outcome{}, err
	}
	replayInput := wiring.RowInput{
		ReplayOf:    entry.RowID,
		Build:       entry.Build,
		RequestID:   entry.RequestID,
		Answers:     entry.Answers,
		Fingerprint: replayed.Fingerprint,
	}
	row, err := wiring.AppendRow(req.State, set, replayInput)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{answers: entry.Answers, verdict: row.Verdict, mode: set.Mode}, nil
}

func jevAnswerJSON(a core.Answer) map[string]any {
	switch a.Kind {
	case core.QuestionNoul:
		return map[string]any{"type": "noul", "noul": a.Noul}
	case core.QuestionChoice:
		return map[string]any{"type": "choice", "choice": a.Choice, "probabilities": a.Probabilities, "confidence": a.Confidence}
	case core.QuestionScore:
		return map[string]any{"type": "score", "score": a.Score, "probabilities": a.Probabilities, "confidence": a.Confidence}
	}
	panic("tofu: unknown question kind")
}

func ledgerAnswerJSON(a ledger.Answer) map[string]any {
	switch a.Kind {
	case ledger.AnswerNoul:
		return map[string]any{"type": "noul", "noul": a.Noul}
	case ledger.AnswerChoice:
		return map[string]any{"type": "choice", "choice": a.Choice, "probabilities": distMap(a.Dist)}
	case ledger.AnswerScore:
		return map[string]any{"type": "score", "score": a.Score, "probabilities": distMap(a.Dist)}
	}
	panic("tofu: unknown answer kind")
}

func distMap(dist []ledger.Slice) map[string]float64 {
	out := make(map[string]float64, len(dist))
	for _, slice := range dist {
		out[slice.Option] = slice.P
	}
	return out
}

func MemoryScopeJudge(dir, log string, keys core.Keyring, say func(string)) *remember.ScopeJudge {
	layers, err := question.Layers(questions.Files(), dir)
	var set question.Set
	if err == nil {
		set, _, err = question.Resolve(memoryscope.MemoryScopeRef, layers)
	}
	var client *core.Client
	if err == nil {
		client, err = wiring.JevClientFrom(keys, wiring.OneCallAtATime)
	}
	if err != nil {
		if say != nil {
			say("remember asks Jev nothing and refuses project local: " + err.Error())
		}
		return nil
	}
	return &remember.ScopeJudge{Client: client, Set: set, Ledger: ledger.NewWriter(log)}
}
