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

type CredentialReport struct {
	Provider string         `json:"provider"`
	Plan     string         `json:"plan,omitempty"`
	State    string         `json:"state"`
	Windows  []WindowReport `json:"windows,omitempty"`
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
	Role     AccountRole `json:"role"`
	Provider string      `json:"provider"`
	Variable string      `json:"variable"`
	Key      string      `json:"key,omitempty"`
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
