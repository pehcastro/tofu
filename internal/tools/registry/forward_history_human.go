package registry

import (
	"tofu/internal/agent/loop"
	"tofu/internal/librarian/skills"
	"tofu/internal/store/session"
	"tofu/internal/tools/agents/cron"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/history/quote"
	"tofu/internal/tools/history/recall"
	"tofu/internal/tools/history/zoom"
	"tofu/internal/tools/human/ask"
	"tofu/internal/tools/human/plan"
	"tofu/internal/tools/human/settings"
	"tofu/internal/tools/knowledge/skill"
	"tofu/internal/tools/scratch"
)

type (
	AskPerson    = ask.AskPerson
	Cron         = cron.Cron
	Plan         = plan.Plan
	PlanItem     = plan.PlanItem
	PlanState    = plan.PlanState
	Recall       = recall.Recall
	RuleOverride = settings.RuleOverride
	Zoom         = zoom.Zoom
)

const (
	CronToolName   = cron.CronToolName
	PlanToolName   = plan.PlanToolName
	PlanPending    = plan.PlanPending
	PlanRunning    = plan.PlanRunning
	PlanDone       = plan.PlanDone
	PlanDropped    = plan.PlanDropped
	QuoteItem      = quote.QuoteItem
	QuoteNotFound  = quote.QuoteNotFound
	QuoteAmbiguous = quote.QuoteAmbiguous
)

var ErrQuoteNoID = quote.ErrQuoteNoID

func NewPlan() *Plan { return plan.NewPlan() }

func NewQuote(store *session.Store, recorded string) quote.Quote {
	return quote.NewQuote(store, recorded)
}

func QuoteRef(id string) string { return quote.QuoteRef(id) }

func ResolveQuote(store *session.Store, recorded, ref string) (quote.Quoted, error) {
	return quote.ResolveQuote(store, recorded, ref)
}

func SaidEventID(talk session.Conversation, index int) string { return quote.SaidEventID(talk, index) }

func ShortID(id string) string { return quote.ShortID(id) }

func NewSettings(global, project string) settings.Settings {
	return settings.NewSettings(global, project)
}

func NewSkill(found []skills.Skill) skill.Skill { return skill.NewSkill(found) }

func ScratchTools(person loop.Person) []contract.Tool { return scratch.ScratchTools(person) }
