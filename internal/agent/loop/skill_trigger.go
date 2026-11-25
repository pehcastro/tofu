package loop

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/agent/context/recall"
	skill "tofu/internal/librarian/skills"
	"tofu/internal/models/llm/core"
	shipped "tofu/library"
)

const (
	skillLoadedBySaying = "auto"
	skillTurnedOff      = "off"
	skillOffTyped       = "/skill off "
	skillToolLoaded     = "the model loaded it with the skill tool"
)

type ActiveSkill struct {
	Name string    `json:"name"`
	Said string    `json:"said"`
	At   time.Time `json:"at,omitzero"`
}

func ActiveSkills(conversation []core.Message) []ActiveSkill {
	var active []ActiveSkill
	for _, message := range conversation {
		origin, said := message.Origin, ""
		switch {
		case origin.Source != core.SourceSkillLoad:
			continue
		case message.Role == core.RoleUser && origin.ID == skillTurnedOff:
		case message.Role == core.RoleUser && origin.ID == skillLoadedBySaying:
			said = origin.Shown
		case message.Role == core.RoleTool && origin.ID == "" && !recall.AlreadyDropped(message.Content):
			said = skillToolLoaded
		default:
			continue
		}
		active = slices.DeleteFunc(active, func(one ActiveSkill) bool { return one.Name == origin.Name })
		if said != "" {
			active = append(active, ActiveSkill{Name: origin.Name, Said: said, At: origin.TakenAt})
		}
	}
	return active
}

func (c Config) speakerOf(origin core.Origin) (shown, told string, hears bool) {
	if c.SpawnedFrom == "" {
		return "you", "the person", saidByThePerson(origin)
	}
	return "the lead's message", "the lead's message", origin.Source == core.SourceMessage
}

func (c Config) withSkillsSaid(messages, unheard []core.Message, recorded *record) []core.Message {
	for _, said := range unheard {
		shownSpeaker, toldSpeaker, hears := c.speakerOf(said.Origin)
		words, typed := TaskIn(said.Content)
		if !hears || !typed {
			continue
		}
		if name, off := strings.CutPrefix(strings.TrimSpace(words), skillOffTyped); off && c.SpawnedFrom == "" {
			messages = c.skillOff(messages, strings.TrimSpace(name), said.Origin.TakenAt, recorded)
			continue
		}
		for _, one := range c.Skills {
			phrase := one.Said(words)
			if phrase == "" {
				continue
			}
			heard := shownSpeaker + " said " + strconv.Quote(phrase)
			active := ActiveSkills(messages)
			if at := slices.IndexFunc(active, func(held ActiveSkill) bool { return held.Name == one.Name }); at >= 0 {
				recorded.notice("skill " + one.Name + " not loaded again: " + heard + ", and it is already active: " + active[at].Said)
				continue
			}
			body, err := skill.Load(c.Skills, shipped.Files(), one.Name, "")
			if err != nil {
				c.skillNotice(recorded, "skill "+one.Name+" did not load: "+heard+", and it did not read: "+err.Error())
				continue
			}
			shown := "skill " + one.Name + " loaded: " + heard
			recorded.notice(shown + ", one of its keywords, and it was not active")
			messages = append(messages, core.Message{Role: core.RoleUser, Content: toldSpeaker + " said " + strconv.Quote(phrase) + ", one of the keywords of the skill " + one.Name +
				", so tofu loaded it. follow it while it applies:\n\n" + body,
				Origin: core.Origin{Source: core.SourceSkillLoad, Name: one.Name, ID: skillLoadedBySaying, Shown: shown, TakenAt: said.Origin.TakenAt}})
		}
	}
	return messages
}

func (c Config) skillOff(messages []core.Message, name string, at time.Time, recorded *record) []core.Message {
	if !slices.ContainsFunc(ActiveSkills(messages), func(held ActiveSkill) bool { return held.Name == name }) {
		c.skillNotice(recorded, "skill "+name+" is not active, so "+skillOffTyped+name+" changed nothing")
		return messages
	}
	shown := "skill " + name + " off: you typed " + skillOffTyped + name
	recorded.notice(shown + ", so the person turned it off by hand")
	return append(messages, core.Message{Role: core.RoleUser, Content: "the person turned the skill " + name + " off by hand: stop following it from here on. " +
		"it loads again only when they say one of its keywords.", Origin: core.Origin{Source: core.SourceSkillLoad, Name: name, ID: skillTurnedOff, Shown: shown, TakenAt: at}})
}

func (c Config) skillNotice(recorded *record, text string) {
	recorded.notice(text)
	if c.Notify != nil {
		c.Notify(text)
	}
}
