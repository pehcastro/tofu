package command

type Scope string

const (
	Tofu      Scope = "tofu"
	Interface Scope = "interface"
)

type Needs string

const (
	Always  Needs = "always"
	Reload  Needs = "reload"
	Resume  Needs = "resume"
	Fresh   Needs = "new"
	Compact Needs = "compact"
	Undo    Needs = "undo"
	Cron    Needs = "cron"
)

func (Scope) Enum() []string { return []string{string(Tofu), string(Interface)} }

func (Needs) Enum() []string {
	return []string{string(Always), string(Reload), string(Resume), string(Fresh), string(Compact), string(Undo), string(Cron)}
}

type Command struct {
	Name    string `json:"name"`
	Args    string `json:"args,omitempty"`
	Summary string `json:"summary"`
	Scope   Scope  `json:"scope"`
	Needs   Needs  `json:"needs"`
}

func Table() []Command {
	return []Command{
		{Name: "chat", Summary: "the conversation and the composer", Scope: Interface, Needs: Always},
		{Name: "sub-agents", Summary: "every tool call and every sub-agent, in one feed", Scope: Interface, Needs: Always},
		{Name: "file-edits", Summary: "every changed line, who made it and where", Scope: Interface, Needs: Always},
		{Name: "shells", Summary: "the persistent processes an agent left running", Scope: Interface, Needs: Always},
		{Name: "models", Summary: "every model the signed subscriptions serve, and which one the next turn runs", Scope: Tofu, Needs: Always},
		{Name: "status", Summary: "each subscription's quota windows and when they reset", Scope: Interface, Needs: Always},
		{Name: "attach", Summary: "reference a file in this workspace", Scope: Interface, Needs: Always},
		{Name: "settings", Summary: "appearance, keys, roles, and the file each value came from", Scope: Interface, Needs: Always},
		{Name: "keys", Summary: "every key the app answers to, and what it does", Scope: Interface, Needs: Always},
		{Name: "links", Summary: "every link this conversation carried, newest first", Scope: Interface, Needs: Always},
		{Name: "quote", Summary: "cite a past turn by id, newest first", Scope: Interface, Needs: Always},
		{Name: "copy", Summary: "put the last answer on the clipboard", Scope: Interface, Needs: Always},
		{Name: "copy-call", Summary: "put the last tool call and its result on the clipboard", Scope: Interface, Needs: Always},
		{Name: "memory", Summary: "what tofu remembers for you, global and for this project", Scope: Tofu, Needs: Always},
		{Name: "remember", Args: "<what>", Summary: "keep a line for every later session: /remember <what>", Scope: Tofu, Needs: Always},
		{Name: "reload", Summary: "re-read settings, rules, skills, sub-agents, models, instructions and keys from disk", Scope: Tofu, Needs: Reload},
		{Name: "resume", Summary: "pick a session of this project to carry into the next task", Scope: Tofu, Needs: Resume},
		{Name: "new", Summary: "start fresh, carrying nothing from the last session", Scope: Tofu, Needs: Fresh},
		{Name: "compact", Summary: "shrink the old tool results the next turn carries to an artifact handle each", Scope: Tofu, Needs: Compact},
		{Name: "undo", Args: "[N]", Summary: "put back the files the last turn changed, or the last N with /undo N", Scope: Tofu, Needs: Undo},
		{Name: "cron", Args: "[list | history | edit | pause | resume | delete]", Summary: "the scheduled jobs: list, history, edit, pause, resume, delete", Scope: Tofu, Needs: Cron},
		{Name: "loop", Args: "<interval> <prompt>", Summary: "repeat a prompt on an interval: /loop 10m <prompt>", Scope: Tofu, Needs: Cron},
		{Name: "goal", Args: `<prompt> --until "<cmd>"`, Summary: `fire after each turn until a check passes: /goal <prompt> --until "<cmd>"`, Scope: Tofu, Needs: Cron},
		{Name: "quit", Summary: "leave tofu", Scope: Interface, Needs: Always},
	}
}
