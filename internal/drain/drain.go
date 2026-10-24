package drain

import (
	"cmp"
	"encoding/json"
	"slices"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/turn"
)

const (
	band50k  = 50_000
	band100k = 100_000
	band200k = 200_000
	band400k = 400_000

	fiveMinuteTTL = 5 * time.Minute
	oneHourTTL    = time.Hour

	assistantBlock = "assistant"
	messageBlock   = "message"
	anthropicWire  = "anthropic"
	forkChange     = "fork"
)

type Wake string

const (
	WakePerson     Wake = "person"
	WakeReport     Wake = "child report"
	WakeMessage    Wake = "child message"
	WakeBrief      Wake = "spawn brief"
	WakeCron       Wake = "cron"
	WakeMemory     Wake = "memory"
	WakeTofu       Wake = "tofu"
	WakeClassifier Wake = "classifier"
)

func wakes() []Wake {
	return []Wake{WakePerson, WakeReport, WakeMessage, WakeBrief, WakeCron, WakeMemory, WakeTofu, WakeClassifier}
}

func wakeOf(origin llm.Origin) Wake {
	switch origin.Source {
	case llm.SourceReport, llm.SourceReportFor:
		return WakeReport
	case llm.SourceMessage, llm.SourceAsk:
		return WakeMessage
	case llm.SourceBrief:
		return WakeBrief
	}
	switch origin.Kind() {
	case llm.OriginPerson:
		return WakePerson
	case llm.OriginCron:
		return WakeCron
	case llm.OriginMemory:
		return WakeMemory
	}
	return WakeTofu
}

func bands() []string { return []string{"<50k", "50-100k", "100-200k", "200-400k", "400k+"} }

func bandOf(tokens int) string {
	passed := 0
	for _, edge := range []int{band50k, band100k, band200k, band400k} {
		if tokens >= edge {
			passed++
		}
	}
	return bands()[passed]
}

type Spend struct {
	Calls    int     `json:"calls"`
	Weighted float64 `json:"weighted_tokens"`
	USD      float64 `json:"usd"`
}

func (s *Spend) add(more Spend) {
	s.Calls, s.Weighted, s.USD = s.Calls+more.Calls, s.Weighted+more.Weighted, s.USD+more.USD
}

type Line struct {
	Name string `json:"name"`
	Spend
}

type Tokens struct {
	Input   int `json:"input"`
	Output  int `json:"output"`
	Read    int `json:"cache_read"`
	Write5m int `json:"cache_write_5m"`
	Write1h int `json:"cache_write_1h"`
}

type Resident struct {
	Kind       string  `json:"kind"`
	Blocks     int     `json:"blocks"`
	Tokens     int     `json:"tokens"`
	TokenCalls int64   `json:"token_calls"`
	Weighted   float64 `json:"weighted_tokens"`
}

type Cause string

const (
	CauseIdle    Cause = "idle past ttl"
	CauseHistory Cause = "history past its 5m lifetime"
	CausePrefix  Cause = "prefix changed"
)

type Break struct {
	Request string
	Differs string
}

type Rebuild struct {
	Agent    string  `json:"agent,omitempty"`
	Turn     string  `json:"turn"`
	Request  string  `json:"request"`
	Cause    Cause   `json:"cause"`
	GapMS    int64   `json:"gap_ms"`
	TTLMS    int64   `json:"ttl_ms"`
	Written  int     `json:"written"`
	Weighted float64 `json:"weighted_tokens"`
	Differs  string  `json:"differs,omitempty"`
}

type Agent struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Spend
	PeakContext int     `json:"peak_context"`
	Rebuilt     float64 `json:"rebuilt_weighted"`
	Unread      float64 `json:"unread_weighted"`
}

type Report struct {
	Session      string            `json:"session"`
	Ceiling      int               `json:"context_ceiling"`
	Weights      Weights           `json:"default_weights"`
	PricedBy     map[string]string `json:"priced_by"`
	CostRecorded bool              `json:"cost_recorded"`
	Total        Spend             `json:"total"`
	Tokens       Tokens            `json:"tokens"`
	Lead         Spend             `json:"lead"`
	Memory       Spend             `json:"memory"`
	Unread       float64           `json:"unread_weighted"`
	Wakes        []Line            `json:"wakes"`
	Children     []Line            `json:"child_wakes"`
	Bands        []Line            `json:"bands"`
	Residency    []Resident        `json:"residency"`
	Rebuilds     []Rebuild         `json:"rebuilds"`
	Findings     []Finding         `json:"findings"`
	Agents       []Agent           `json:"agents"`
	Types        []Line            `json:"agent_types"`
}

type Input struct {
	Breaks                 []Break
	Classifier             []session.ClassifierCall
	Runs                   []session.AgentRun
	Weights                func(model string) Weights
	LeadWindow             func(model string) int
	BytesPerThousandTokens int
}

type requestBody struct {
	Model      string          `json:"model"`
	Prompt     int             `json:"prompt_tokens"`
	Completion int             `json:"completion_tokens"`
	Read       int             `json:"cache_read_tokens"`
	Write      int             `json:"cache_write_tokens"`
	Write5m    int             `json:"cache_write_5m_tokens"`
	Write1h    int             `json:"cache_write_1h_tokens"`
	CostUSD    *float64        `json:"cost_usd"`
	DurationMS int64           `json:"duration_ms"`
	ToolCalls  json.RawMessage `json:"tool_calls"`
}

type resultBody struct {
	Outcome  string `json:"tool_outcome"`
	Bytes    int    `json:"result_bytes"`
	Rendered int    `json:"rendered_bytes"`
	Exit     *int   `json:"exit_code"`
	Error    string `json:"error"`
}

type callArgs struct {
	Command string `json:"command"`
	Path    string `json:"path"`
}

type thread struct {
	wake     Wake
	waking   Wake
	busy     bool
	at       time.Time
	context  int
	cached   int
	ttl      time.Duration
	history  time.Duration
	wrote    float64
	wroteAt  where
	refill   int
	resident map[string]int
	reads    map[string]int
	failing  int
}

type pass struct {
	Input
	report    Report
	inclusive bool
	threads   map[string]*thread
	agents    map[string]*Agent
	wakes     map[Wake]*Spend
	children  map[Wake]*Spend
	bands     map[string]*Spend
	resident  map[string]*Resident
	turns     map[string]float64
	breaks    map[string]string
	calls     map[string]string
	tests     map[string]bool
	signals   signals
}

func Read(id string, events []session.Event, in Input) Report {
	p := pass{Input: in, report: Report{Session: id, Ceiling: konst.ContextCeilingTokens, Weights: defaultWeights(), PricedBy: map[string]string{}, CostRecorded: len(in.Classifier) > 0},
		threads: map[string]*thread{}, agents: map[string]*Agent{}, wakes: map[Wake]*Spend{}, children: map[Wake]*Spend{}, bands: map[string]*Spend{},
		resident: map[string]*Resident{}, turns: map[string]float64{}, breaks: map[string]string{}, calls: map[string]string{}, tests: map[string]bool{}}
	for _, cut := range in.Breaks {
		p.breaks[cut.Request] = cut.Differs
	}
	for _, event := range events {
		p.read(event)
	}
	for _, th := range p.threads {
		p.unread(th.wroteAt, th.wrote)
	}
	for _, call := range in.Classifier {
		tally(p.wakes, WakeClassifier).add(Spend{Calls: 1, USD: call.CostUSD})
	}
	return p.finish()
}

func (p *pass) thread(agent string) *thread {
	th := p.threads[agent]
	if th == nil {
		th = &thread{resident: map[string]int{}, reads: map[string]int{}}
		p.threads[agent] = th
	}
	return th
}

func lines[K ~string](spent map[K]*Spend) []Line {
	listed := []Line{}
	for name, spend := range spent {
		listed = append(listed, Line{Name: string(name), Spend: *spend})
	}
	slices.SortFunc(listed, func(a, b Line) int {
		return cmp.Or(cmp.Compare(b.Weighted, a.Weighted), cmp.Compare(b.USD, a.USD), cmp.Compare(a.Name, b.Name))
	})
	return listed
}

func tally[K comparable, V any](counted map[K]*V, key K) *V {
	if counted[key] == nil {
		counted[key] = new(V)
	}
	return counted[key]
}

func (p *pass) tokens(bytes int) int { return bytes * 1000 / max(p.BytesPerThousandTokens, 1) }

func (p *pass) read(event session.Event) {
	switch event.Kind {
	case session.EventTurnStart:
		var start session.TurnStart
		if json.Unmarshal(event.Body, &start) != nil || event.Agent != "" {
			return
		}
		p.report.Ceiling = cmp.Or(start.ContextCeiling, p.report.Ceiling)
		p.inclusive = start.Wire != "" && start.Wire != anthropicWire
		th := p.thread("")
		th.busy, th.waking = false, ""
	case session.EventSpawn:
		var spawned session.SpawnBody
		if json.Unmarshal(event.Body, &spawned) == nil {
			tally(p.agents, spawned.Agent).Type = spawned.Definition
		}
	case session.EventRequest, session.EventMemoryRequest:
		var body requestBody
		if json.Unmarshal(event.Body, &body) == nil {
			p.request(event, body)
		}
	case session.EventMessage:
		var said session.MessageBody
		if json.Unmarshal(event.Body, &said) != nil || said.Role != session.RoleUser {
			return
		}
		th, tokens := p.thread(event.Agent), p.tokens(len(said.Content))
		th.resident[messageBlock] += tokens
		p.block(messageBlock, tokens)
		if woke := wakeOf(turn.OriginOf(said)); !th.busy && (th.waking == "" || slices.Index(wakes(), woke) < slices.Index(wakes(), th.waking)) {
			th.waking = woke
		}
	case session.EventToolCall:
		var call session.CallBody
		var args callArgs
		if json.Unmarshal(event.Body, &call) != nil {
			return
		}
		_ = json.Unmarshal(call.Args, &args)
		p.calls[event.Call] = call.Tool
		at := where{agent: event.Agent, turn: event.Turn, call: event.Call}
		switch {
		case call.Tool == turn.ShellToolName || call.Tool == "bash":
			p.tests[event.Call] = runsTests(args.Command)
		case call.Tool == "read" && args.Path != "":
			p.signals.reread(p.thread(event.Agent), args.Path, at)
		}
	case session.EventToolResult:
		var result resultBody
		tool, known := p.calls[event.Call]
		if !known || json.Unmarshal(event.Body, &result) != nil {
			return
		}
		th, tokens := p.thread(event.Agent), p.tokens(cmp.Or(result.Rendered, result.Bytes))
		th.resident[tool] += tokens
		p.block(tool, tokens)
		if p.tests[event.Call] {
			failed := result.Exit != nil && *result.Exit != 0 || result.Outcome == session.ToolOutcomeFailed || result.Error != ""
			p.signals.tested(th, failed, where{agent: event.Agent, turn: event.Turn, call: event.Call})
		}
	case session.EventCompaction:
		var compacted struct {
			Compaction, Fork *struct {
				Before int `json:"tokens_before"`
			}
		}
		if json.Unmarshal(event.Body, &compacted) != nil {
			return
		}
		th := p.thread(event.Agent)
		clear(th.resident)
		if dropped := cmp.Or(compacted.Compaction, compacted.Fork); dropped != nil {
			th.refill = dropped.Before
		}
	case session.EventListChange:
		var change session.ListChangeBody
		if json.Unmarshal(event.Body, &change) == nil && change.Kind == forkChange {
			clear(p.thread(event.Agent).resident)
		}
	}
}

func (p *pass) block(kind string, tokens int) {
	resident := tally(p.resident, kind)
	resident.Blocks, resident.Tokens = resident.Blocks+1, resident.Tokens+tokens
}

func FiveMinuteWrites(written, split5m, split1h int) int {
	return max(split5m, written-split1h)
}

func (p *pass) request(event session.Event, body requestBody) {
	weights := p.Weights(body.Model)
	p.report.PricedBy[body.Model] = weights.From
	if p.LeadWindow != nil && event.Agent == "" {
		p.report.Ceiling, p.LeadWindow = cmp.Or(p.LeadWindow(body.Model), p.report.Ceiling), nil
	}
	var usd float64
	if body.CostUSD != nil {
		usd, p.report.CostRecorded = *body.CostUSD, true
	}
	input, write1h := body.Prompt, body.Write1h
	if p.inclusive {
		input = max(0, body.Prompt-body.Read)
	}
	write5m := FiveMinuteWrites(body.Write, body.Write5m, body.Write1h)
	written := weights.Write5m*float64(write5m) + weights.Write1h*float64(write1h)
	spend := Spend{Calls: 1, USD: usd,
		Weighted: weights.Input*float64(input) + written + weights.Read*float64(body.Read) + weights.Output*float64(body.Completion)}
	if event.Kind == session.EventMemoryRequest {
		p.report.Memory.add(spend)
		tally(p.wakes, WakeMemory).add(spend)
		return
	}
	context, at := input+body.Read+body.Write, where{agent: event.Agent, turn: event.Turn, call: event.ID}
	th := p.thread(event.Agent)
	if !th.busy {
		th.wake, th.waking = cmp.Or(th.waking, th.wake, WakeTofu), ""
	}
	th.busy = len(body.ToolCalls) > len("[]")
	tokens := &p.report.Tokens
	tokens.Input, tokens.Output, tokens.Read, tokens.Write5m, tokens.Write1h = tokens.Input+input, tokens.Output+body.Completion, tokens.Read+body.Read, tokens.Write5m+write5m, tokens.Write1h+write1h
	p.report.Total.add(spend)
	tally(p.bands, bandOf(context)).add(spend)
	p.turns[event.Turn] += spend.Weighted
	for kind, resident := range th.resident {
		p.resident[kind].TokenCalls += int64(resident)
		p.resident[kind].Weighted += float64(resident) * weights.Read
	}
	th.resident[assistantBlock] += body.Completion
	p.block(assistantBlock, body.Completion)
	started := event.At.Add(-time.Duration(body.DurationMS) * time.Millisecond)
	gap := started.Sub(th.at)
	if th.at.IsZero() {
		gap = 0
	}
	if th.wrote > 0 && body.Read+konst.CacheBreakSlackTokens < th.cached {
		p.unread(th.wroteAt, th.wrote)
	}
	if differs, broke := p.breaks[event.ID]; broke {
		cause, ttl := CausePrefix, cmp.Or(th.ttl, fiveMinuteTTL)
		switch {
		case gap >= ttl:
			cause = CauseIdle
		case th.history > 0 && gap >= th.history:
			cause, ttl = CauseHistory, th.history
		}
		p.report.Rebuilds = append(p.report.Rebuilds, Rebuild{Agent: event.Agent, Turn: event.Turn, Request: event.ID, Cause: cause, GapMS: gap.Milliseconds(),
			TTLMS: ttl.Milliseconds(), Written: write5m + write1h, Weighted: written, Differs: differs})
		if event.Agent != "" {
			tally(p.agents, event.Agent).Rebuilt += written
		}
	}
	if event.Agent == "" {
		p.report.Lead.add(spend)
		tally(p.wakes, th.wake).add(spend)
		p.signals.idleRewrite(gap, write5m+write1h, at)
	} else {
		run := tally(p.agents, event.Agent)
		run.add(spend)
		run.PeakContext = max(run.PeakContext, context)
		tally(p.children, th.wake).add(spend)
	}
	p.signals.sized(th, context, at)
	th.at, th.context, th.cached, th.wrote, th.wroteAt = event.At, context, body.Read+body.Write, written, at
	switch {
	case write1h > 0 && write5m > 0:
		th.ttl, th.history = oneHourTTL, fiveMinuteTTL
	case write1h > 0:
		th.ttl, th.history = oneHourTTL, 0
	case write5m > 0:
		th.ttl, th.history = fiveMinuteTTL, 0
	}
}

func (p *pass) unread(at where, weighted float64) {
	if weighted <= 0 {
		return
	}
	p.report.Unread += weighted
	if at.agent != "" {
		tally(p.agents, at.agent).Unread += weighted
	}
	p.signals.unread(weighted, at)
}

func (p *pass) finish() Report {
	report := p.report
	report.Wakes, report.Children = lines(p.wakes), lines(p.children)
	for _, band := range bands() {
		report.Bands = append(report.Bands, Line{Name: band, Spend: *tally(p.bands, band)})
	}
	for kind, resident := range p.resident {
		resident.Kind = kind
		report.Residency = append(report.Residency, *resident)
	}
	slices.SortFunc(report.Residency, func(a, b Resident) int {
		return cmp.Or(cmp.Compare(b.TokenCalls, a.TokenCalls), cmp.Compare(a.Kind, b.Kind))
	})
	types, definitions := map[string]*Spend{}, map[string]string{}
	for _, run := range p.Runs {
		definitions[run.Agent] = run.Definition
	}
	for name, run := range p.agents {
		run.Name, run.Type = name, cmp.Or(run.Type, definitions[name], "unnamed")
		report.Agents = append(report.Agents, *run)
		tally(types, run.Type).add(run.Spend)
	}
	slices.SortFunc(report.Agents, func(a, b Agent) int { return cmp.Or(cmp.Compare(b.Weighted, a.Weighted), cmp.Compare(a.Name, b.Name)) })
	report.Types = lines(types)
	report.Findings = p.signals.findings(report, p.turns)
	return report
}
