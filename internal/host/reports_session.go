package host

import (
	"encoding/json"
	"time"

	"tofu/internal/hook"
	"tofu/internal/session"
	"tofu/internal/turn"
)

type SessionSkip struct {
	Session string `json:"session"`
	Reason  string `json:"reason"`
}

type ContextReport struct {
	Session                string            `json:"session,omitempty"`
	Name                   string            `json:"name,omitempty"`
	Task                   string            `json:"task,omitempty"`
	Steps                  int               `json:"steps,omitempty"`
	Occupancy              *ContextOccupancy `json:"occupancy,omitempty"`
	Unmeasured             string            `json:"unmeasured,omitempty"`
	Fork                   *ContextFork      `json:"fork,omitempty"`
	Skipped                []SessionSkip     `json:"skipped,omitempty"`
	Ceiling                int               `json:"ceiling,omitempty"`
	BytesPerThousandTokens int               `json:"bytes_per_thousand_tokens,omitempty"`
}

type ContextOccupancy struct {
	Step         int         `json:"step"`
	Identity     ContextBand `json:"identity"`
	Facts        ContextBand `json:"facts"`
	WorkingSet   ContextBand `json:"working_set"`
	Recent       ContextBand `json:"recent"`
	Total        int         `json:"total"`
	Mark         int         `json:"mark"`
	CapsRecorded bool        `json:"caps_recorded"`
}

type ContextBand struct {
	Tokens  int `json:"tokens"`
	Cap     int `json:"cap"`
	Percent int `json:"fill_percent"`
}

type ContextFork struct {
	Into   string             `json:"into"`
	Kind   string             `json:"kind,omitempty"`
	Counts *ContextForkCounts `json:"counts,omitempty"`
}

type ContextForkCounts struct {
	TokensBefore int `json:"tokens_before"`
	TokensAfter  int `json:"tokens_after"`
}

type SessionParams struct {
	Session string `json:"session,omitempty"`
}

type SessionInfo struct {
	ID               string    `json:"id"`
	Name             string    `json:"name,omitempty"`
	Handle           string    `json:"handle"`
	Family           string    `json:"family,omitempty"`
	Generation       int       `json:"generation,omitempty"`
	Generations      int       `json:"generations,omitempty"`
	At               time.Time `json:"at"`
	LastAt           time.Time `json:"lastAt,omitzero"`
	Task             string    `json:"task,omitempty"`
	Turns            int       `json:"turns"`
	Agents           int       `json:"sub_agents"`
	Steps            int       `json:"steps"`
	Carried          int       `json:"carried_messages"`
	Outcome          string    `json:"outcome,omitempty"`
	Error            string    `json:"error,omitempty"`
	Wire             string    `json:"wire,omitempty"`
	Model            string    `json:"model,omitempty"`
	Parent           string    `json:"parent,omitempty"`
	Root             string    `json:"root,omitempty"`
	ForkedInto       string    `json:"forked_into,omitempty"`
	ForkIntoKind     string    `json:"fork_into_kind,omitempty"`
	ForkKind         string    `json:"fork_kind,omitempty"`
	ContextCeiling   int       `json:"context_ceiling,omitempty"`
	ContextTarget    int       `json:"context_target,omitempty"`
	AutoCompaction   string    `json:"auto_compaction,omitempty"`
	ForkTokensBefore int       `json:"fork_tokens_before,omitempty"`
	ForkTokensAfter  int       `json:"fork_tokens_after,omitempty"`
	CostUSD          float64   `json:"cost_usd,omitempty"`
	Head             bool      `json:"head,omitempty"`

	Reads      int               `json:"reads"`
	Unrecorded int               `json:"unrecorded_reads,omitempty"`
	EndedAt    *time.Time        `json:"ended_at,omitempty"`
	EndReason  session.EndReason `json:"end_reason,omitempty"`
	Expired    bool              `json:"expired,omitempty"`
}

type SessionTrace struct {
	Session  string             `json:"session"`
	Name     string             `json:"name,omitempty"`
	Handle   string             `json:"handle"`
	Error    string             `json:"error,omitempty"`
	Events   int                `json:"events"`
	Agents   []session.AgentRun `json:"agents"`
	Requests []TraceRequest     `json:"requests"`
	Calls    []TraceCall        `json:"calls"`
	Hooks    []TraceHook        `json:"hooks,omitempty"`
	Failures []TraceFailure     `json:"failures,omitempty"`
	Messages []TraceMessage     `json:"messages,omitempty"`

	Sizes    *session.Sizes         `json:"sizes,omitempty"`
	Inserted []session.TracedInsert `json:"inserted,omitempty"`
	Changes  []session.TracedChange `json:"list_changes,omitempty"`
	Notices  []session.TracedNotice `json:"notices,omitempty"`
	Outlived []TraceOutlived        `json:"outlived,omitempty"`
	Cache    TraceCache             `json:"cache"`
}

type TraceRequest struct {
	Request string        `json:"request"`
	Agent   string        `json:"agent,omitempty"`
	Turn    string        `json:"turn"`
	Model   string        `json:"model,omitempty"`
	Usage   session.Usage `json:"usage"`
	CostUSD float64       `json:"cost_usd"`

	Why               string `json:"why,omitempty"`
	Messages          int    `json:"messages,omitempty"`
	New               int    `json:"new_messages,omitempty"`
	Attempts          int    `json:"attempts,omitempty"`
	Status            int    `json:"status,omitempty"`
	ProviderRequestID string `json:"provider_request_id,omitempty"`
	DurationMS        int64  `json:"duration_ms,omitempty"`
	Error             string `json:"error,omitempty"`
	RecordedIn        string `json:"recorded_in,omitempty"`
}

type TraceCall struct {
	Call    string `json:"call"`
	Agent   string `json:"agent,omitempty"`
	Turn    string `json:"turn"`
	Request string `json:"request,omitempty"`
	Tool    string `json:"tool"`
	Result  string `json:"result,omitempty"`
	Outcome string `json:"outcome,omitempty"`
	Bytes   int    `json:"result_bytes"`
	Reason  string `json:"reason,omitempty"`

	Args       json.RawMessage `json:"args,omitempty"`
	DurationMS int64           `json:"duration_ms,omitempty"`
	Refused    bool            `json:"refused,omitempty"`
	Gate       string          `json:"gate,omitempty"`
	Hooks      []turn.HookRun  `json:"hooks,omitempty"`
	RecordedIn string          `json:"recorded_in,omitempty"`
}

type TraceHook struct {
	Agent string `json:"agent,omitempty"`
	Turn  string `json:"turn"`
	Call  string `json:"call,omitempty"`
	Tool  string `json:"tool,omitempty"`
	hook.Run
}

type TraceMessage struct {
	ID   string    `json:"id"`
	Turn string    `json:"turn"`
	Role string    `json:"role"`
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

type TraceFailure struct {
	Agent string `json:"agent,omitempty"`
	Turn  string `json:"turn"`
	Error string `json:"error"`
}

type TraceOutlived struct {
	Agent      string `json:"agent"`
	Calls      int    `json:"calls"`
	RecordedIn string `json:"recorded_in"`
	LeadIn     string `json:"lead_in"`
}

type TraceCache struct {
	Lifetimes map[string]string `json:"lifetimes,omitempty"`
	Breaks    []TraceCacheBreak `json:"breaks,omitempty"`
}

type TraceCacheBreak struct {
	Agent   string `json:"agent,omitempty"`
	Request string `json:"request"`
	After   string `json:"after"`
	Gap     string `json:"gap"`
	Read    int    `json:"cache_read_tokens"`
	Cached  int    `json:"after_cached_tokens"`
	Differs string `json:"first_difference"`
}

type SessionFindParams struct {
	Session string `json:"session"`
	Tool    string `json:"tool,omitempty"`
	Command string `json:"command,omitempty"`
	File    string `json:"file,omitempty"`
	Text    string `json:"text,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Since   string `json:"since,omitempty"`
	Until   string `json:"until,omitempty"`
}

type SessionFind struct {
	Handle string        `json:"handle"`
	Query  session.Query `json:"query"`
	Hits   []session.Hit `json:"hits"`
}
