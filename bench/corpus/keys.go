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
	{"turn", "TotalCostUSD", "the same total as total_cost_usd, under the key an older single-file row wrote"},
	{"turn", "session", "session lineage: the session folder the turn was recorded in"},
	{"turn", "spawned_by", "session lineage: the call that spawned this sub-agent run"},
	{"turn", "spawned_from", "session lineage: the turn that spawned this sub-agent run"},
	{"step", "model", "the build that answered this request; the turn's model is the one a bench point reads"},
	{"step", "CostUSD", "the same cost as cost_usd, under the key an older single-file row wrote"},
	{"turn", "decision_ids", "pointers into the judge ledger, looked up there rather than duplicated here"},
	{"turn", "warnings", "recorder warnings about the turn itself, not about the work it produced"},
	{"turn", "root", "session lineage: which turn this one traces back to"},
	{"turn", "name", "the human label a session was given, not part of the decision it made"},
	{"turn", "parent", "session lineage: the immediate parent turn"},
	{"turn", "child_ids", "session lineage: turns forked from this one, before rc-fix8"},
	{"turn", "sub_agent_ids", "session lineage: turns forked from this one"},
	{"turn", "forked_into", "session lineage: the turn this one forked into"},
	{"turn", "forked_from", "session lineage: the turn this one forked from"},
	{"turn", "fork_kind", "session lineage: why the fork happened"},
	{"turn", "fork_tokens_before", "session lineage: token count at the fork point"},
	{"turn", "fork_tokens_after", "session lineage: token count after the fork"},
	{"step", "cost_usd", "already summed into the turn's total_cost_usd, which is itself unread; see that entry"},
	{"step", "occupancy", "context-window occupancy at the step; bench/recall reads this from its own fixtures, not RecordedStep"},
	{"step", "bands", "context-window band split at the step; same as occupancy"},
	{"step", "fork", "whether this step is where a fork happened; session lineage, see the turn-level fork fields"},
	{"call", "gate_verdict", "pointer into the judge ledger, looked up there rather than duplicated here; bench/stopcheck looks up the row by gate_decision_id instead, checked 2026-09-20"},
	{"call", "rendered_bytes", "how much of the result was shown to the model, distinct from result_bytes which is already read"},
	{"call", "result_handle", "a pointer to an off-record artifact; no bench point reads it from the corpus yet"},
	{"call", "child_id", "session lineage: a sub-agent spawned by this call, before rc-fix8"},
	{"call", "sub_agent_id", "session lineage: a sub-agent spawned by this call"},
	{"call", "sift_saved_bytes", "the bytes the shell sift kept from the model on this call; bench/sift measures savings on its own corpus, not from this field"},
	{"call", "parallel_batch", "which parallel batch a call belongs to; no bench point reads it from the corpus yet"},
	{"message", "tool_outcome", "whether a tool message's own call succeeded; no bench point reads it from the corpus yet"},
	{"message", "thinking_signature", "the vendor's opaque seal over a thinking block, replayed to the wire and meaningless to a bench"},
	{"message", "tool_result_bytes", "the size of a tool message's result before rendering; no bench point reads it from the corpus yet"},
	{"turn", "ended_in_fork", "session lineage: the fork that ended this turn, see the turn-level fork fields"},
	{"turn", "error", "the error text that ended the turn; outcome already says how it ended, and no bench point reads the text from the corpus yet"},
	{"turn", "loop_guard", "the repeated call the loop guard stopped the turn on; no bench point reads it from the corpus yet"},
	{"step", "cache_write_1h_tokens", "the one hour share of cache_write_tokens, which is already read; no bench point splits the write by lifetime yet"},
	{"step", "compaction", "an in place rewrite at the step; bench/recall reads it through turn.StepRow, not RecordedStep"},
	{"step", "duration_ms", "how long the request took; no bench point reads request latency from the corpus yet"},
	{"step", "first_token_ms", "time to the first streamed token; no bench point reads request latency from the corpus yet"},
	{"step", "warnings", "recorder warnings about the step itself, not about the work it produced"},
	{"call", "gate_reason", "the judge's reason for the gate verdict, a copy of the reason on the ledger row gate_decision_id names"},
	{"call", "proxy", "the rewrite a command proxy made to a shell command; no bench point reads it from the corpus yet"},
	{"call", "refused", "the call was refused before it ran; no bench point reads it from the corpus yet"},
	{"message", "origin", "where a message came from, such as a memory view or a fork carry; no bench point reads it from the corpus yet"},
	{"message", "posted_at", "when a person posted a message while the turn ran; no bench point reads queue timing from the corpus yet"},
	{"message", "taken_at", "when the turn took a posted message into the conversation; no bench point reads queue timing from the corpus yet"},
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
		known := taggedKeys(reflect.TypeOf(RecordedTurn{}))
		known["wallclockms"] = true
		known["contextceiling"] = true
		known["contexttarget"] = true
		known["autocompaction"] = true
		return known
	case "step":
		known := taggedKeys(reflect.TypeOf(RecordedStep{}))
		known["assistanttext"] = true
		known["prompttokens"] = true
		known["completiontokens"] = true
		known["toolcalls"] = true
		return known
	case "call":
		known := taggedKeys(reflect.TypeOf(RecordedCall{}))
		known["exitcode"] = true
		known["durationms"] = true
		known["resultbytes"] = true
		known["renderedbytes"] = true
		known["resulthash"] = true
		return known
	case "message":
		known := taggedKeys(reflect.TypeOf(RecordedMessage{}))
		known["toolcallid"] = true
		known["toolcalls"] = true
		return known
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
