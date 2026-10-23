package host

import (
	"time"

	"tofu/internal/browser"
)

type UsageState string

const (
	UsageServing   UsageState = "serving"
	UsageAttention UsageState = "needs attention"
	UsageNone      UsageState = "none"
)

func (UsageState) enum() []string {
	return []string{string(UsageServing), string(UsageAttention), string(UsageNone)}
}

type UsageReport struct {
	State      UsageState         `json:"state"`
	Fullest    string             `json:"fullest_window,omitempty"`
	Providers  []CredentialReport `json:"providers"`
	SpendLimit string             `json:"spend_limit"`
	Missing    []DoctorBlocker    `json:"missing,omitempty"`
}

type UsageAnswer struct {
	UsageReport
	ReadAt time.Time `json:"read_at"`
	AgeMs  int64     `json:"age_ms"`
}

type CredentialReport struct {
	Provider string         `json:"provider"`
	Account  string         `json:"account,omitempty"`
	Name     string         `json:"name,omitempty"`
	Plan     string         `json:"plan,omitempty"`
	State    string         `json:"state"`
	Windows  []WindowReport `json:"windows,omitempty"`
	ReadAt   time.Time      `json:"read_at,omitzero"`
	Source   string         `json:"source,omitempty"`
	Stale    bool           `json:"stale,omitempty"`
	RetryAt  time.Time      `json:"retry_at,omitzero"`
}

type WindowReport struct {
	ID       string    `json:"id"`
	Used     float64   `json:"used_fraction"`
	Reported bool      `json:"used_reported"`
	ResetsAt time.Time `json:"resets_at,omitzero"`
}

type AccountRole string

const (
	RoleLLM        AccountRole = "llm"
	RoleClassifier AccountRole = "classifier"
	RoleSearch     AccountRole = "search"
)

func (AccountRole) enum() []string {
	return []string{string(RoleLLM), string(RoleClassifier), string(RoleSearch)}
}

type AccountState string

const (
	AccountInUse         AccountState = "in_use"
	AccountStandby       AccountState = "standby"
	AccountSetAside      AccountState = "set_aside"
	AccountRefreshFailed AccountState = "refresh_failed"
	AccountExpired       AccountState = "expired"
	AccountRefused       AccountState = "refused"
	AccountSpent         AccountState = "spent"
	AccountRateLimited   AccountState = "rate_limited"
	AccountUnread        AccountState = "unread"
	AccountUnchecked     AccountState = "unchecked"
)

func (AccountState) enum() []string {
	return []string{string(AccountInUse), string(AccountStandby), string(AccountSetAside), string(AccountRefreshFailed), string(AccountExpired),
		string(AccountRefused), string(AccountSpent), string(AccountRateLimited), string(AccountUnread), string(AccountUnchecked)}
}

type Accounts struct {
	Subscriptions []SubscriptionStatus `json:"subscriptions"`
	Keys          []KeyStatus          `json:"keys"`
}

type SubscriptionStatus struct {
	Role     AccountRole     `json:"role"`
	Source   string          `json:"source"`
	Accounts []AccountStatus `json:"accounts"`
}

type AccountStatus struct {
	ID        int64          `json:"id"`
	Account   string         `json:"account"`
	State     AccountState   `json:"state"`
	Login     string         `json:"login"`
	ReloginBy time.Time      `json:"relogin_by,omitzero"`
	Plan      string         `json:"plan,omitempty"`
	Windows   []WindowStatus `json:"windows,omitempty"`
}

type WindowStatus struct {
	ID       string    `json:"id"`
	Used     float64   `json:"used"`
	ResetsAt time.Time `json:"resets_at,omitzero"`
	Only     []string  `json:"only,omitempty"`
}

type KeyStatus struct {
	Role            AccountRole    `json:"role"`
	Provider        string         `json:"provider"`
	Variable        string         `json:"variable"`
	Key             string         `json:"key,omitempty"`
	Label           string         `json:"label,omitempty"`
	Model           string         `json:"model,omitempty"`
	ProviderReports string         `json:"provider_reports"`
	Calls           int            `json:"calls"`
	LastUsedAt      time.Time      `json:"last_used_at,omitzero"`
	Periods         []KeyPeriod    `json:"periods,omitempty"`
	ByModel         []KeyModelUse  `json:"by_model,omitempty"`
	Queries         int            `json:"queries,omitempty"`
	Limit           *KeyLimit      `json:"limit,omitempty"`
	LimitUnread     string         `json:"limit_unread,omitempty"`
	Balance         *KeyBalance    `json:"balance,omitempty"`
	BalanceUnread   string         `json:"balance_unread,omitempty"`
	Allowances      []KeyAllowance `json:"allowances,omitempty"`
	LastError       *KeyFailure    `json:"last_error,omitempty"`
}

type KeyBalance struct {
	From       string    `json:"from"`
	CreditsUSD float64   `json:"credits_usd"`
	UsedUSD    float64   `json:"used_usd"`
	BalanceUSD float64   `json:"balance_usd"`
	ReadAt     time.Time `json:"read_at"`
}

type AllowanceScope string

const (
	ScopeKey  AllowanceScope = "key"
	ScopeTeam AllowanceScope = "team"
)

func (AllowanceScope) enum() []string { return []string{string(ScopeKey), string(ScopeTeam)} }

type AllowanceUnit string

const (
	UnitCalls  AllowanceUnit = "calls"
	UnitTokens AllowanceUnit = "tokens"
)

func (AllowanceUnit) enum() []string { return []string{string(UnitCalls), string(UnitTokens)} }

type KeyAllowance struct {
	From      string         `json:"from"`
	Scope     AllowanceScope `json:"scope"`
	Unit      AllowanceUnit  `json:"unit"`
	Limit     int            `json:"limit"`
	Remaining int            `json:"remaining"`
	ResetsAt  time.Time      `json:"resets_at,omitzero"`
	ReadAt    time.Time      `json:"read_at"`
}

type KeyFailure struct {
	Status int       `json:"status,omitempty"`
	What   string    `json:"what"`
	At     time.Time `json:"at"`
}

type CostSource string

const (
	CostProvider CostSource = "provider"
	CostList     CostSource = "list"
	CostNone     CostSource = "none"
)

func (CostSource) enum() []string {
	return []string{string(CostProvider), string(CostList), string(CostNone)}
}

type KeyPeriodID string

const (
	PeriodToday KeyPeriodID = "today"
	PeriodWeek  KeyPeriodID = "7d"
	PeriodMonth KeyPeriodID = "30d"
	PeriodAll   KeyPeriodID = "all"
)

func (KeyPeriodID) enum() []string {
	return []string{string(PeriodToday), string(PeriodWeek), string(PeriodMonth), string(PeriodAll)}
}

type KeyTokens struct {
	InputTokens           int `json:"input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	CacheReadInputTokens  int `json:"cache_read_input_tokens"`
	CacheWriteInputTokens int `json:"cache_write_input_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
}

type KeySpend struct {
	Calls       int        `json:"calls"`
	Tokens      KeyTokens  `json:"tokens"`
	CostUSD     float64    `json:"cost_usd,omitempty"`
	CostSource  CostSource `json:"cost_source"`
	UpstreamUSD float64    `json:"upstream_usd,omitempty"`
}

type KeyPeriod struct {
	Period KeyPeriodID `json:"period"`
	KeySpend
}

type KeyModelUse struct {
	Model string `json:"model"`
	KeySpend
}

type KeyLimit struct {
	From          string         `json:"from"`
	Capped        bool           `json:"capped"`
	LimitUSD      *float64       `json:"limit_usd,omitempty"`
	RemainingUSD  *float64       `json:"remaining_usd,omitempty"`
	Resets        string         `json:"resets,omitempty"`
	ProviderSpend *ProviderSpend `json:"provider_spend,omitempty"`
	FreeTier      bool           `json:"free_tier,omitempty"`
	ReadAt        time.Time      `json:"read_at"`
}

type ProviderSpend struct {
	TodayUSD float64 `json:"today_usd"`
	WeekUSD  float64 `json:"week_usd"`
	MonthUSD float64 `json:"month_usd"`
	AllUSD   float64 `json:"all_usd"`
	BYOKUSD  float64 `json:"byok_usd"`
}

type DoctorVerdict string

const (
	DoctorReady    DoctorVerdict = "ready"
	DoctorNotReady DoctorVerdict = "not ready"
)

func (DoctorVerdict) enum() []string {
	return []string{string(DoctorReady), string(DoctorNotReady)}
}

type DoctorReport struct {
	Version     string               `json:"version"`
	Verdict     DoctorVerdict        `json:"verdict"`
	Blockers    []DoctorBlocker      `json:"blockers,omitempty"`
	Credentials []CredentialReport   `json:"credentials"`
	Store       string               `json:"credential_store"`
	Gate        DoctorGate           `json:"gate"`
	Library     DoctorLibrary        `json:"library"`
	Rules       []DoctorRule         `json:"rules"`
	Overrides   []OverrideListing    `json:"overrides,omitempty"`
	Unreadable  string               `json:"overrides_unreadable,omitempty"`
	Calibration string               `json:"calibration"`
	Ledger      string               `json:"ledger"`
	SpendLimit  string               `json:"spend_limit"`
	Wires       []DoctorWire         `json:"wires"`
	Proxy       DoctorProxy          `json:"proxy"`
	Browser     []browser.NativeHost `json:"browser"`
	Root        string               `json:"root"`
	Go          string               `json:"go"`
	OS          string               `json:"os"`
}

type DoctorBlocker struct {
	Label   string `json:"label"`
	What    string `json:"what"`
	Command string `json:"command"`
}

type DoctorGate struct {
	Variable string `json:"variable"`
	Source   string `json:"source"`
	Path     string `json:"path,omitempty"`
}

type DoctorLibrary struct {
	Dir         string `json:"dir"`
	Points      int    `json:"points"`
	FromProject int    `json:"from_project"`
	Unreadable  string `json:"unreadable,omitempty"`
}

type DoctorThresholds struct {
	RiskAskAt            float64 `json:"risk_ask_at"`
	RiskDenyAt           float64 `json:"risk_deny_at"`
	UserRequestedRelaxAt float64 `json:"user_requested_relax_at"`
	ApprovalRelaxAt      float64 `json:"approval_relax_at"`
	FromUntrustedBlockAt float64 `json:"from_untrusted_block_at"`
}

type DoctorRule struct {
	Point          string           `json:"point"`
	Schema         string           `json:"schema,omitempty"`
	Mode           string           `json:"mode"`
	Declared       string           `json:"declared,omitempty"`
	Fallback       string           `json:"fallback,omitempty"`
	Origin         string           `json:"origin,omitempty"`
	File           string           `json:"file,omitempty"`
	ThresholdsFrom string           `json:"thresholds_from,omitempty"`
	Thresholds     DoctorThresholds `json:"thresholds"`
	Unusable       string           `json:"unusable,omitempty"`
}

type DoctorWire struct {
	Name  string `json:"name"`
	Spend string `json:"spend"`
}

type DoctorProxy struct {
	Use        string `json:"use"`
	From       string `json:"from"`
	Version    string `json:"version,omitempty"`
	Install    string `json:"install,omitempty"`
	Unreadable string `json:"unreadable,omitempty"`
}
