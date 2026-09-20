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
	{"step", "prompt_tokens", "token accounting per step; no bench point reads it from the corpus yet"},
	{"step", "completion_tokens", "token accounting per step; no bench point reads it from the corpus yet"},
	{"step", "cache_read_tokens", "token accounting per step; no bench point reads it from the corpus yet"},
	{"step", "cache_write_tokens", "token accounting per step; no bench point reads it from the corpus yet"},
	{"step", "cost_usd", "already summed into the turn's total_cost_usd, which is itself unread; see that entry"},
	{"step", "stop_reason", "per-step stop reason, distinct from the turn-level outcome this ticket adds; bench/stopcheck already has its own tolerant reader for it"},
	{"step", "occupancy", "context-window occupancy at the step; bench/recall reads this from its own fixtures, not RecordedStep"},
	{"step", "bands", "context-window band split at the step; same as occupancy"},
	{"step", "fork", "whether this step is where a fork happened; session lineage, see the turn-level fork fields"},
	{"call", "exit_code", "a shell call's exit code; no bench point reads it from the corpus yet"},
	{"call", "duration_ms", "how long the call took; no bench point reads it from the corpus yet"},
	{"call", "gate_decision_id", "pointer into the judge ledger, looked up there rather than duplicated here"},
	{"call", "gate_verdict", "pointer into the judge ledger, looked up there rather than duplicated here"},
	{"call", "result_hash", "used to detect a rerun producing the same result; nothing reads RecordedCall for that today"},
	{"call", "rendered_bytes", "how much of the result was shown to the model, distinct from result_bytes which is already read"},
	{"call", "result_handle", "a pointer to an off-record artifact; no bench point reads it from the corpus yet"},
	{"call", "child_id", "session lineage: a subagent spawned by this call"},
	{"call", "command", "the human-readable render of args; args itself is already read"},
	{"call", "parallel_batch", "which parallel batch a call belongs to; no bench point reads it from the corpus yet"},
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
		return known
	case "call":
		return taggedKeys(reflect.TypeOf(RecordedCall{}))
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
