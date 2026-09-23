package corpus

import (
	"reflect"
	"strings"
)

type UnreadKey struct {
	Level  string
	Key    string
	Reason string
}

var DeliberatelyUnread = []UnreadKey{
	{"turn", "schema", "the wire-format version; the reader already knows which schema a turn came from by how it found the file"},
	{"turn", "model", "which model wrote the turn; no bench point reads it from the corpus yet"},
	{"turn", "wire", "which wire carried the turn; no bench point reads it from the corpus yet"},
	{"turn", "spend", "subscription or key; no bench point reads it from the corpus yet"},
	{"turn", "total_cost_usd", "already summed by the recorder; bench/cost reads spend from its own ledger, not this field"},
	{"turn", "decision_ids", "pointers into the judge ledger, looked up there rather than duplicated here"},
	{"turn", "warnings", "recorder warnings about the turn itself, not about the work it produced"},
	{"turn", "root", "session lineage: which turn this one traces back to"},
	{"turn", "name", "the human label a session was given, not part of the decision it made"},
	{"turn", "parent", "session lineage: the immediate parent turn"},
	{"turn", "child_ids", "session lineage: turns forked from this one"},
	{"turn", "forked_into", "session lineage: the turn this one forked into"},
	{"turn", "forked_from", "session lineage: the turn this one forked from"},
	{"turn", "fork_kind", "session lineage: why the fork happened"},
	{"turn", "fork_tokens_before", "session lineage: token count at the fork point"},
	{"turn", "fork_tokens_after", "session lineage: token count after the fork"},
	{"step", "cost_usd", "already summed into the turn's total_cost_usd, which is itself unread; see that entry"},
	{"step", "occupancy", "context-window occupancy at the step; bench/recall reads this from its own fixtures, not RecordedStep"},
	{"step", "bands", "context-window band split at the step; same as occupancy"},
	{"step", "fork", "whether this step is where a fork happened; session lineage, see the turn-level fork fields"},
	{"call", "duration_ms", "how long the call took; no bench point reads it from the corpus yet"},
	{"call", "gate_verdict", "pointer into the judge ledger, looked up there rather than duplicated here; bench/stopcheck looks up the row by gate_decision_id instead, checked 2026-09-20"},
	{"call", "rendered_bytes", "how much of the result was shown to the model, distinct from result_bytes which is already read"},
	{"call", "result_handle", "a pointer to an off-record artifact; no bench point reads it from the corpus yet"},
	{"call", "child_id", "session lineage: a subagent spawned by this call"},
	{"call", "parallel_batch", "which parallel batch a call belongs to; no bench point reads it from the corpus yet"},
	{"message", "tool_outcome", "whether a tool message's own call succeeded; no bench point reads it from the corpus yet"},
	{"message", "tool_result_bytes", "the size of a tool message's result before rendering; no bench point reads it from the corpus yet"},
	{"message_call", "arguments", "the raw arguments an assistant message's tool call carried; no bench point reads it from the corpus yet"},
}

func taggedKeys(t reflect.Type) map[string]bool {
	keys := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		keys[strings.ToLower(name)] = true
	}
	return keys
}

func knownKeys(level string) map[string]bool {
	switch level {
	case "turn":
		return taggedKeys(reflect.TypeOf(RecordedTurn{}))
	case "step":
		known := taggedKeys(reflect.TypeOf(RecordedStep{}))
		known["assistanttext"] = true
		known["prompttokens"] = true
		known["completiontokens"] = true
		return known
	case "call":
		return taggedKeys(reflect.TypeOf(RecordedCall{}))
	case "message":
		return taggedKeys(reflect.TypeOf(RecordedMessage{}))
	case "message_call":
		return taggedKeys(reflect.TypeOf(RecordedToolCallName{}))
	}
	return nil
}

func ignoredKeys(level string) map[string]bool {
	ignored := map[string]bool{}
	for _, entry := range DeliberatelyUnread {
		if entry.Level == level {
			ignored[strings.ToLower(entry.Key)] = true
		}
	}
	return ignored
}
