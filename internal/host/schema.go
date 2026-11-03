package host

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"tofu/internal/agent/commands"
	"tofu/internal/agent/hooks"
	"tofu/internal/boardy"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/learn"
	"tofu/internal/llm/quota"
	"tofu/internal/memory"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/settings"
	"tofu/internal/skill"
	"tofu/internal/snapshot"
	"tofu/internal/store/session/drain"
	"tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/tools/web/browser/driver"
	"tofu/internal/turn"
)

type schemaDefs struct {
	built map[string]any
	names map[reflect.Type]string
}

func pinnedNames() map[reflect.Type]string {
	names := map[reflect.Type]string{}
	for prefix, values := range map[string][]any{
		"boardy": {
			boardy.AcceptanceLine{}, boardy.Actual{}, boardy.AgeBucket{}, boardy.Aging{}, boardy.AnswerRequest{},
			boardy.AskedQuestion{}, boardy.AssignRequest{}, boardy.AuditAnswer{}, boardy.AuditRequest{},
			boardy.Author{}, boardy.BoardSummary{}, boardy.BoardsAnswer{}, boardy.BypassAnswerAnswer{},
			boardy.BypassAnswerRequest{}, boardy.BypassListAnswer{}, boardy.BypassListRequest{},
			boardy.BypassRequest{}, boardy.Changed{}, boardy.CreateRequest{}, boardy.Duplicate{}, boardy.Event{},
			boardy.EventsAnswer{}, boardy.EventsRequest{}, boardy.Filter{}, boardy.FlaggedHook{},
			boardy.GetAnswer{}, boardy.GetRequest{}, boardy.Group{}, boardy.HandRequest{}, boardy.HookRow{},
			boardy.InitRequest{}, boardy.Lag{}, boardy.Lagged{}, boardy.ListAnswer{}, boardy.ListRequest{},
			boardy.LogRequest{}, boardy.MoveRequest{}, boardy.PlanAnswer{}, boardy.PlanRequest{},
			boardy.PlannedEpic{}, boardy.PlannedMilestone{}, boardy.PlannedSprint{}, boardy.Question{},
			boardy.QuestionsAnswer{}, boardy.QuestionsRequest{}, boardy.ReportAnswer{}, boardy.ReportRequest{},
			boardy.Request{}, boardy.Row{}, boardy.RowsAnswer{}, boardy.RuleAnswer{}, boardy.RulePutRequest{},
			boardy.RuleRequest{}, boardy.RuleTestAnswer{}, boardy.RuleTestRequest{}, boardy.RulesAnswer{},
			boardy.RulesRequest{}, boardy.Tally{}, boardy.Ticket{}, boardy.TicketAnswer{}, boardy.TriageAnswer{},
			boardy.TriageRequest{}, boardy.Unacted{}, boardy.View{}, boardy.Words{},
		},
		"browser": {browser.NativeHost{}},
		"command": {command.Command{}},
		"drain": {
			drain.Agent{}, drain.Finding{}, drain.Line{}, drain.Rebuild{}, drain.Resident{}, drain.Spend{},
			drain.Tokens{}, drain.Weights{},
		},
		"hook": {hook.Hook{}, hook.Result{}},
		"learn": {
			learn.Check{}, learn.Finding{}, learn.Job{}, learn.JobRun{}, learn.Label{}, learn.Quote{},
			learn.Read{}, learn.Run{}, learn.Said{}, learn.Sent{}, learn.Summary{},
		},
		"ledger": {
			ledger.Answer{}, ledger.Charge{}, ledger.Outcome{}, ledger.PointSummary{}, ledger.Reason{},
			ledger.Row{}, ledger.Slice{}, ledger.StateElision{}, ledger.Threshold{}, ledger.Usage{},
		},
		"memory": {memory.Entry{}, memory.Weight{}},
		"quota":  {quota.Burn{}},
		"recall": {recall.Item{}},
		"session": {
			session.AgentRun{}, session.Hit{}, session.Origin{}, session.PromptPart{}, session.Query{},
			session.Ran{}, session.RequestParts{}, session.RuleFinding{}, session.SentBody{}, session.SentItem{},
			session.Sizes{}, session.TracedChange{}, session.TracedInsert{}, session.TracedMemory{},
			session.TracedNotice{}, session.Usage{}, session.UsageBucket{}, session.UsageRow{},
			session.UsageTotals{},
		},
		"settings": {settings.RaisedVersion{}},
		"skill":    {skill.Skill{}},
		"snapshot": {snapshot.Refusal{}, snapshot.Report{}},
		"state":    {state.Subject{}},
		"subagent": {
			subagent.Broken{}, subagent.Definition{}, subagent.Found{}, subagent.Reference{},
			subagent.Transition{},
		},
		"sys": {sys.ScratchFolder{}, sys.ScratchReport{}},
		"turn": {
			turn.HookRun{}, turn.LoopGuardStop{}, turn.PersonOption{}, turn.PersonQuestion{}, turn.PersonReply{},
			turn.ProxyRow{}, turn.ToolCallRow{},
		},
	} {
		for _, value := range values {
			shape := reflect.TypeOf(value)
			names[shape] = prefix + "." + shape.Name()
		}
	}
	return names
}

func Schema() ([]byte, error) {
	defs := schemaDefs{built: map[string]any{}, names: pinnedNames()}
	numbered := map[string]any{"id": map[string]any{"type": []string{"string", "integer"}}}
	var server, client, results []any
	notified, requested, answered := map[string]any{}, map[string]any{}, map[reflect.Type]bool{}
	for _, one := range notifications() {
		params := defs.of(reflect.TypeOf(one.params))
		notified[one.name] = params
		server = append(server, envelope(one.name, params, nil))
	}
	for _, one := range requests() {
		params, result := defs.of(reflect.TypeOf(one.params)), defs.of(reflect.TypeOf(one.result))
		requested[one.name] = map[string]any{"params": params, "result": result}
		client = append(client, envelope(one.name, params, numbered))
		if shape := reflect.TypeOf(one.result); !answered[shape] {
			answered[shape], results = true, append(results, result)
		}
	}
	asked, answer := defs.of(reflect.TypeOf(ApprovalRequest{})), defs.of(reflect.TypeOf(ApprovalAnswer{}))
	question, chosen := defs.of(reflect.TypeOf(QuestionRequest{})), defs.of(reflect.TypeOf(QuestionAnswer{}))
	server = append(server, envelope(ApprovalMethod, asked, numbered), envelope(QuestionMethod, question, numbered), reply("result", map[string]any{"anyOf": results}), reply("error", defs.of(reflect.TypeOf(Refusal{}))))
	defs.built["clientMessage"] = map[string]any{"oneOf": append(client, reply("result", answer), reply("result", chosen))}
	return json.MarshalIndent(map[string]any{
		"$schema":         "https://json-schema.org/draft/2020-12/schema",
		"$id":             Protocol,
		"title":           Protocol + ", every line tofu serve --stdio writes; clientMessage is every line it reads",
		"oneOf":           server,
		"$defs":           defs.built,
		"x-notifications": notified,
		"x-requests":      requested,
		"x-capabilities":  capabilities(),
		"x-serverRequests": map[string]any{
			ApprovalMethod: map[string]any{"params": asked, "result": answer},
			QuestionMethod: map[string]any{"params": question, "result": chosen},
		},
	}, "", "  ")
}

func envelope(method string, params any, more map[string]any) map[string]any {
	properties := map[string]any{"jsonrpc": map[string]any{"const": rpcVersion}, "method": map[string]any{"const": method}, "params": params}
	maps.Copy(properties, more)
	return closed(properties, slices.Sorted(maps.Keys(properties)))
}

func reply(field string, body any) map[string]any {
	properties := map[string]any{"jsonrpc": map[string]any{"const": rpcVersion}, "id": map[string]any{"type": []string{"string", "integer", "null"}}, field: body}
	return closed(properties, slices.Sorted(maps.Keys(properties)))
}

func closed(properties map[string]any, required []string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func (defs schemaDefs) of(t reflect.Type) any {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch named := reflect.Zero(t).Interface().(type) {
	case enumerated:
		return map[string]any{"type": "string", "enum": named.enum()}
	case interface{ Enum() []string }:
		return map[string]any{"type": "string", "enum": named.Enum()}
	}
	switch t {
	case reflect.TypeFor[time.Time]():
		return map[string]any{"type": "string", "format": "date-time"}
	case reflect.TypeFor[json.RawMessage]():
		return map[string]any{}
	case reflect.TypeFor[[]byte]():
		return map[string]any{"type": "string", "contentEncoding": "base64"}
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int64:
		return map[string]any{"type": "integer"}
	case reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Slice:
		return map[string]any{"type": []string{"array", "null"}, "items": defs.of(t.Elem())}
	case reflect.Array:
		return map[string]any{"type": "array", "items": defs.of(t.Elem()), "minItems": t.Len(), "maxItems": t.Len()}
	case reflect.Map:
		return map[string]any{"type": []string{"object", "null"}, "additionalProperties": defs.of(t.Elem())}
	case reflect.Struct:
		name := t.Name()
		if t.PkgPath() != reflect.TypeFor[Identity]().PkgPath() {
			pinned, known := defs.names[t]
			if !known {
				panic("host: no pinned schema name for " + t.String())
			}
			name = pinned
		}
		if _, done := defs.built[name]; !done {
			defs.built[name] = map[string]any{}
			properties, required := map[string]any{}, []string{}
			defs.fields(t, properties, &required)
			defs.built[name] = closed(properties, required)
		}
		return map[string]any{"$ref": "#/$defs/" + name}
	}
	panic("host: no schema for " + t.String())
}

func (defs schemaDefs) fields(t reflect.Type, properties map[string]any, required *[]string) {
	for index := range t.NumField() {
		field := t.Field(index)
		if field.Anonymous {
			defs.fields(field.Type, properties, required)
			continue
		}
		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if !field.IsExported() || name == "-" {
			continue
		}
		properties[name] = defs.of(field.Type)
		if options != "omitempty" && options != "omitzero" {
			*required = append(*required, name)
		}
	}
}
