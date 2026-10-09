package host

import (
	"tofu/internal/hook"
	"tofu/internal/memory"
	"tofu/internal/session"
	settingspkg "tofu/internal/settings"
)

type ModelsReport struct {
	Subscriptions []string      `json:"subscriptions"`
	Defaults      []string      `json:"defaults"`
	Usable        int           `json:"usable"`
	Table         string        `json:"table"`
	Windowed      int           `json:"windowed"`
	Published     int           `json:"published_windows"`
	Unbound       []string      `json:"unbound_roles,omitempty"`
	Models        []ModelReport `json:"models"`
}

type ModelReport struct {
	Slug          string   `json:"slug"`
	Provider      string   `json:"provider"`
	ID            string   `json:"id"`
	Subscription  string   `json:"subscription"`
	Use           string   `json:"use"`
	Kind          string   `json:"kind"`
	Pays          string   `json:"pays"`
	Windows       []string `json:"windows"`
	ContextTokens int      `json:"context_tokens,omitempty"`
	WindowFrom    string   `json:"window_from,omitempty"`
	Roles         []string `json:"roles,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	Notice        string   `json:"notice,omitempty"`
	Layer         string   `json:"layer"`
	From          string   `json:"from,omitempty"`
	File          string   `json:"file"`
}

type ModelsQuery struct {
	ModelsReport
	Wires []string `json:"wires"`
	Stale bool     `json:"stale"`
}

type ModelReload struct {
	ContextWindows int            `json:"context_windows"`
	Table          string         `json:"table"`
	TableError     string         `json:"table_error,omitempty"`
	Unshadowed     []string       `json:"removed_from_catalog,omitempty"`
	Sources        []ReloadSource `json:"sources"`
	Unresolved     []string       `json:"unresolved_tiers,omitempty"`
	Versions       VersionCheck   `json:"versions"`
}

type VersionCheck struct {
	Raised  []settingspkg.RaisedVersion `json:"raised,omitempty"`
	Skipped string                      `json:"skipped,omitempty"`
	Error   string                      `json:"error,omitempty"`
}

type ReloadSource struct {
	Source  string        `json:"source"`
	State   string        `json:"state"`
	Served  int           `json:"served"`
	Changes []ModelChange `json:"changes"`
	Failure string        `json:"failure,omitempty"`
	Error   string        `json:"error,omitempty"`
	Hint    string        `json:"hint,omitempty"`
}

type ModelChange struct {
	Model  string `json:"model"`
	Slug   string `json:"slug"`
	Change string `json:"change"`
	Use    string `json:"use"`
	Reason string `json:"reason,omitempty"`
	File   string `json:"file"`
}

type SettingsReport struct {
	Global   string         `json:"global_file"`
	Project  string         `json:"project_file"`
	Settings []SettingValue `json:"settings"`
}

type SettingValue struct {
	Key         string      `json:"key"`
	Category    string      `json:"category"`
	Value       any         `json:"value"`
	Source      string      `json:"source"`
	Label       string      `json:"label"`
	Description string      `json:"description"`
	Kind        SettingKind `json:"kind"`
	Choices     []string    `json:"choices,omitempty"`
	Min         *int        `json:"min,omitempty"`
	Max         *int        `json:"max,omitempty"`
	Unit        string      `json:"unit,omitempty"`
	Restart     bool        `json:"restart,omitempty"`
}

type SettingKind string

const (
	SettingBool   SettingKind = "bool"
	SettingInt    SettingKind = "int"
	SettingText   SettingKind = "text"
	SettingChoice SettingKind = "choice"
	SettingList   SettingKind = "list"
)

func (SettingKind) enum() []string {
	return []string{string(SettingBool), string(SettingInt), string(SettingText), string(SettingChoice), string(SettingList)}
}

type RuleListReport struct {
	Origin string        `json:"origin"`
	Rules  []RuleListing `json:"rules"`
}

type RuleListing struct {
	ID        string           `json:"id"`
	Kind      string           `json:"kind"`
	Origin    string           `json:"origin"`
	Mode      string           `json:"mode,omitempty"`
	File      string           `json:"file,omitempty"`
	Switch    string           `json:"switch,omitempty"`
	Override  *OverrideListing `json:"override,omitempty"`
	Text      string           `json:"text,omitempty"`
	Trigger   string           `json:"trigger,omitempty"`
	Fires     int              `json:"fires,omitempty"`
	FiresWeek session.Week     `json:"fires_week,omitzero"`
}

type OverrideListing struct {
	RuleID  string `json:"rule_id"`
	Version int    `json:"version,omitempty"`
	Current int    `json:"current,omitempty"`
	Layer   string `json:"layer"`
	Change  string `json:"change"`
	Text    string `json:"text,omitempty"`
	Reason  string `json:"reason,omitempty"`
	By      string `json:"by,omitempty"`
	At      string `json:"at,omitempty"`
	Stale   bool   `json:"stale"`
	File    string `json:"file"`
}

type LibraryReport struct {
	Layers        []LibraryLayer  `json:"layers"`
	Models        int             `json:"models"`
	Subscriptions int             `json:"subscriptions"`
	Roles         int             `json:"roles"`
	Questions     int             `json:"questions"`
	DocPages      int             `json:"doc_pages"`
	DocEntries    int             `json:"doc_entries"`
	Proxy         string          `json:"proxy"`
	ProxyFrom     string          `json:"proxy_from"`
	DomainsFrom   string          `json:"domains_from"`
	DomainsDir    string          `json:"domains_dir"`
	Domains       []LibraryDomain `json:"domains"`
}

type LibraryLayer struct {
	Name   string `json:"name"`
	Origin string `json:"origin"`
}

type LibraryDomain struct {
	Name       string `json:"name"`
	Rules      int    `json:"rules"`
	Thresholds int    `json:"thresholds"`
	Skills     int    `json:"skills"`
	Agents     int    `json:"agents"`
	References int    `json:"references"`
	Refused    int    `json:"refused"`
}

type MemoryReport struct {
	Scopes []MemoryShelf `json:"scopes"`
}

type MemoryShelf struct {
	memory.Shelf
	Bytes int `json:"bytes"`
	Limit int `json:"limit"`
}

type MemoryAddParams struct {
	Text  string       `json:"text"`
	Scope memory.Scope `json:"scope"`
	Kind  memory.Kind  `json:"kind,omitempty"`
}

type MemoryEditParams struct {
	Scope memory.Scope `json:"scope"`
	ID    string       `json:"id"`
	Text  string       `json:"text"`
}

type MemoryRemoveParams struct {
	Scope memory.Scope `json:"scope"`
	ID    string       `json:"id"`
}

type ReloadDiff struct {
	Project    string     `json:"project"`
	First      bool       `json:"first"`
	Unreadable string     `json:"last_unreadable,omitempty"`
	Parts      []PartDiff `json:"parts"`
}

type PartDiff struct {
	Name    string   `json:"name"`
	Detail  string   `json:"detail,omitempty"`
	Count   int      `json:"count"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Changed []string `json:"changed,omitempty"`
}

type ChangelogReport struct {
	Versions []ChangelogVersion `json:"versions"`
	Seen     string             `json:"seen,omitempty"`
}

type ChangelogVersion struct {
	Version string `json:"version"`
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

type UpdateReport struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	Release   bool   `json:"release"`
	Source    string `json:"source"`
	Installed string `json:"installed,omitempty"`
	Aside     string `json:"aside,omitempty"`
}

type DocsParams struct {
	Topic string `json:"topic,omitempty"`
}

type DocsAnswer struct {
	DocsCorpus
	Page *DocsTopic `json:"page,omitempty"`
}

type DocsCorpus struct {
	Pages   []DocsPage  `json:"topics"`
	Entries []DocsEntry `json:"entries"`
}

type DocsPage struct {
	Topic   string `json:"topic"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Body    string `json:"markdown,omitempty"`
}

type DocsEntry struct {
	Ask   string `json:"ask"`
	Do    string `json:"do"`
	Check string `json:"check"`
	Topic string `json:"topic"`
}

type DocsTopic struct {
	DocsPage
	Settings []SettingDoc `json:"settings,omitempty"`
}

type SettingDoc struct {
	Key         string `json:"key"`
	Category    string `json:"category"`
	Default     string `json:"default"`
	Takes       string `json:"takes"`
	Restart     bool   `json:"restart"`
	Description string `json:"description"`
}

type HooksReport struct {
	Hooks    []hook.Hook `json:"hooks"`
	Problems []string    `json:"problems"`
}

type HooksTrusted struct {
	Trusted []hook.Hook `json:"trusted"`
}

type LearnParams struct {
	ID     int    `json:"id"`
	Reason string `json:"reason,omitempty"`
}

type Setup struct {
	Steps []Requirement `json:"steps"`
}

type Requirement struct {
	Step    string              `json:"step"`
	What    string              `json:"what"`
	Fix     string              `json:"fix,omitempty"`
	Done    string              `json:"done,omitempty"`
	Choices []RequirementChoice `json:"choices,omitempty"`
}

type RequirementChoice struct {
	Label string `json:"label"`
	Key   string `json:"key,omitempty"`
}

type LoginKeyParams struct {
	Provider string `json:"provider"`
	Key      string `json:"key"`
}

type LogoutParams struct {
	Role     string `json:"role"`
	Provider string `json:"provider"`
	Number   int    `json:"number,omitempty"`
}

type LoginNote struct {
	Note string `json:"note"`
}

type ChangeKind string

const (
	ChangeAdded   ChangeKind = "added"
	ChangeChanged ChangeKind = "changed"
	ChangeRemoved ChangeKind = "removed"
)

func (ChangeKind) enum() []string {
	return []string{string(ChangeAdded), string(ChangeChanged), string(ChangeRemoved)}
}

type WriteReceipt struct {
	Changes []FileChange `json:"changes"`
	Undo    string       `json:"undo"`
}

type FileChange struct {
	Change ChangeKind `json:"change"`
	What   string     `json:"what"`
	File   string     `json:"file"`
}

type RuleWriteParams struct {
	ID      string `json:"id"`
	Text    string `json:"text,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Concern string `json:"concern,omitempty"`
	Global  bool   `json:"global,omitempty"`
	Replace bool   `json:"replace,omitempty"`
}

type AgentWriteParams struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Model       string `json:"model,omitempty"`
	Tools       string `json:"tools,omitempty"`
	Global      bool   `json:"global,omitempty"`
}

type LearnApplyParams struct {
	ID      int  `json:"id"`
	Project bool `json:"project,omitempty"`
}
