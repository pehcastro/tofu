package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"tofu/internal/classifier"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/method"
	"tofu/internal/judge/question"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/prompt"
	"tofu/internal/rule"
	"tofu/internal/session"
	"tofu/internal/skill"
	"tofu/internal/subagent"
	"tofu/internal/sys"
	shipped "tofu/library"
	"tofu/library/questions"
)

const TheFormatContract = prompt.TheFormatContract

type PromptPart struct {
	Concern rule.Concern
	RuleID  string
	Agent   string
	Name    string
	File    string
	Text    string
	Why     string
	Reason  rule.ReasonKind
	Mode    rule.Mode
	State   session.PartState
	ByTask  bool
	Off     bool
}

func (p PromptPart) Rule() string {
	switch {
	case p.Agent != "":
		return "the sub-agent " + p.Agent
	case p.RuleID != "":
		return "the rule " + p.RuleID
	}
	return "tofu itself"
}

func (p PromptPart) From() string {
	if p.File == "" {
		return p.Rule()
	}
	return p.Rule() + " in " + p.File
}

type ComposeSpec struct {
	Task         string
	Paths        []string
	Environment  string
	ToolGuidance string
	Rules        []rule.Rule
	SwitchedOff  []rule.Overriding
	Agent        subagent.Definition
	Skills       []skill.Skill
	WindowTokens int
	Role         rule.Role
	Frameworks   []string
	Alone        bool
	Languages    []string
	Dropped      []PromptPart
	Kinds        *KindJudge
	Applies      *RuleJudge
}

const (
	ConcernSkills     rule.Concern = "skills"
	ConcernReferences rule.Concern = "references"
)

const SkillToolName = "skill"

const referencesOnDemand = "your references, each read whole with the " + ReferenceToolName + " tool when the work reaches it:"

func agentPart(definition subagent.Definition) PromptPart {
	var text strings.Builder
	text.WriteString("you are the " + definition.Name + " sub-agent, and these are your instructions:\n" + definition.Instructions)
	if len(definition.References) > 0 {
		text.WriteString("\n\n" + referenceIndex(definition.References))
	}
	if len(definition.Cut) > 0 {
		fmt.Fprintf(&text, "\n\nthese references were cut to keep them within %d bytes and are not here: %s", konst.SubAgentReferenceBytes, strings.Join(definition.Cut, ", "))
	}
	return PromptPart{Concern: rule.ConcernIdentity, Agent: definition.Name, File: filepath.ToSlash(definition.Path), Text: text.String()}
}

func referenceIndex(references []subagent.Reference) string {
	text := referencesOnDemand
	for _, reference := range references {
		text += reference.IndexEntry()
	}
	return text
}

func libraryReferences(library fs.FS, files []string) ([]subagent.Reference, error) {
	var references []subagent.Reference
	for _, file := range files {
		data, err := fs.ReadFile(library, file)
		if err != nil {
			return nil, err
		}
		_, body, _ := strings.Cut(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n---\n")
		references = append(references, subagent.Reference{Name: strings.TrimSuffix(path.Base(file), ".md"), Path: "library/" + file, Text: strings.TrimLeft(body, "\n")})
	}
	return references, nil
}

func referenceParts(definition subagent.Definition) []PromptPart {
	var parts []PromptPart
	for _, reference := range definition.References {
		parts = append(parts, PromptPart{Concern: ConcernReferences, Agent: definition.Name, Name: reference.Name, File: reference.Path, State: session.PartOffered})
	}
	for _, reference := range definition.Unreached {
		parts = append(parts, PromptPart{Concern: ConcernReferences, Agent: definition.Name, Name: reference.Name, File: reference.Path, State: session.PartDropped,
			Why: "the reference is for " + strings.Join(reference.Frameworks, " or ") + ", which no manifest the task reaches lists"})
	}
	for _, name := range definition.Cut {
		parts = append(parts, PromptPart{Concern: ConcernReferences, Agent: definition.Name, Name: name, State: session.PartDropped,
			Why: fmt.Sprintf("cut to keep the references within %d bytes", konst.SubAgentReferenceBytes)})
	}
	return parts
}

func autoloadedSkills(definition subagent.Definition, skills []skill.Skill) (string, []PromptPart, error) {
	var loaded []PromptPart
	if definition.Origin == "library" {
		text, err := librarySkills(shipped.Files(), definition)
		if err != nil {
			return "", nil, err
		}
		for _, name := range definition.Skills {
			loaded = append(loaded, PromptPart{Concern: ConcernSkills, Agent: definition.Name, Name: name, File: "library/" + librarySkillFiles(shipped.Files(), name)[0], State: session.PartLoaded,
				Why: "the library sub-agent's definition names it"})
		}
		return text, loaded, nil
	}
	var text strings.Builder
	for _, name := range definition.Skills {
		body, err := skill.Load(skills, shipped.Files(), name, "")
		switch {
		case err == nil:
			text.WriteString("\n\nthe skill " + name + ", loaded before your task:\n" + body)
			at := slices.IndexFunc(skills, func(one skill.Skill) bool { return one.Name == name })
			loaded = append(loaded, PromptPart{Concern: ConcernSkills, Agent: definition.Name, Name: name, File: filepath.ToSlash(skills[at].File), State: session.PartLoaded,
				Why: "the sub-agent's definition names it"})
		case definition.Origin == ".tofu" || definition.Origin == "~/.tofu":
			return "", nil, fmt.Errorf("the sub-agent %s names the skill %s: %w", definition.Name, name, err)
		}
	}
	return text.String(), loaded, nil
}

func librarySkillFiles(library fs.FS, name string) []string {
	found, _ := fs.Glob(library, "*/skills/"+name+".md")
	deeper, _ := fs.Glob(library, "*/*/skills/"+name+".md")
	return append(found, deeper...)
}

func librarySkills(library fs.FS, definition subagent.Definition) (string, error) {
	var text strings.Builder
	for _, name := range definition.Skills {
		found := librarySkillFiles(library, name)
		if len(found) != 1 {
			return "", fmt.Errorf("the library sub-agent %s names the skill %s, which the library ships %d times: %s", definition.Name, name, len(found), strings.Join(found, ", "))
		}
		data, err := fs.ReadFile(library, found[0])
		if err != nil {
			return "", err
		}
		_, body, _ := strings.Cut(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n---\n")
		text.WriteString("\n\nthe skill " + name + ", loaded before your task:\n" + strings.TrimLeft(body, "\n"))
	}
	if text.Len() > konst.SubAgentReferenceBytes {
		return "", fmt.Errorf("the library sub-agent %s loads %d bytes of skills, past the %d a sub-agent is given", definition.Name, text.Len(), konst.SubAgentReferenceBytes)
	}
	return text.String(), nil
}

func skillParts(skills []skill.Skill, skillTool bool, loaded []PromptPart) []PromptPart {
	var parts []PromptPart
	for _, one := range skills {
		part := PromptPart{Concern: ConcernSkills, Name: one.Name, File: filepath.ToSlash(one.File), State: session.PartOffered}
		switch {
		case slices.ContainsFunc(loaded, func(loaded PromptPart) bool { return loaded.Name == one.Name }):
			continue
		case !skillTool:
			part.State, part.Why = session.PartDropped, "the sub-agent has no "+SkillToolName+" tool"
		case one.Hidden:
			part.State, part.Why = session.PartHeld, "the skill is hidden from the listing"
		}
		parts = append(parts, part)
	}
	return parts
}

func unofferedLibrarySkills(library fs.FS, listed []skill.Skill, loaded []PromptPart) []PromptPart {
	var dropped []PromptPart
	for _, file := range librarySkillFiles(library, "*") {
		name := strings.TrimSuffix(path.Base(file), ".md")
		if slices.ContainsFunc(listed, func(one skill.Skill) bool { return one.Name == name }) || slices.ContainsFunc(loaded, func(part PromptPart) bool { return part.Name == name }) {
			continue
		}
		dropped = append(dropped, PromptPart{Concern: ConcernSkills, Name: name, File: "library/" + file, State: session.PartDropped,
			Why: "this run lists no skill: the skills setting is off or the run gives three tools"})
	}
	return dropped
}

type Composed struct {
	Task       rule.Task
	Parts      []PromptPart
	HeldBack   []rule.Match
	Unsent     []PromptPart
	References []subagent.Reference
}

func (c Composed) Recorded() []PromptPart { return slices.Concat(c.Parts, c.Unsent) }

func composedOrder() []rule.Concern {
	return []rule.Concern{
		rule.ConcernEnvironment,
		rule.ConcernToolGuidance,
		rule.ConcernSafety,
		rule.ConcernOutputShape,
		rule.ConcernFormatContract,
		rule.ConcernCodeRules,
		rule.ConcernProcessDiscipline,
		rule.ConcernDomainKnowledge,
		rule.ConcernTaskShaping,
		rule.ConcernIdentity,
	}
}

func Compose(spec ComposeSpec) (Composed, error) {
	for _, reference := range spec.Agent.References {
		if unknown := slices.IndexFunc(reference.Frameworks, func(framework string) bool { return !rule.KnownFramework(framework) }); unknown >= 0 {
			return Composed{}, fmt.Errorf("the reference %s in %s declares the framework %q, which no manifest detector knows", reference.Name, reference.Path, reference.Frameworks[unknown])
		}
	}
	spec.Agent = spec.Agent.Reaching(spec.Frameworks)
	composed := Composed{Task: TaskNamed(spec.Task)}
	composed.Task.Role, composed.Task.Frameworks, composed.Task.Alone, composed.Task.Languages = spec.Role, spec.Frameworks, spec.Alone, spec.Languages
	var wordsDecided error
	if spec.Kinds != nil {
		composed.Task.Verb, wordsDecided = spec.Kinds.Of(spec.Task, composed.Task.Verb)
		composed.Task.KindBy = "Jev"
		if wordsDecided != nil {
			composed.Task.KindBy = "the word list"
		}
	}
	for _, owned := range spec.Paths {
		composed.Task.Paths = append(composed.Task.Paths, filepath.ToSlash(owned))
	}
	var loaded []PromptPart
	if spec.Agent.Name != "" {
		text, autoloaded, err := autoloadedSkills(spec.Agent, spec.Skills)
		if err != nil {
			return Composed{}, err
		}
		part := agentPart(spec.Agent)
		part.Text += text
		composed.Parts = append(composed.Parts, part)
		loaded, composed.Unsent = autoloaded, slices.Concat(autoloaded, referenceParts(spec.Agent))
	} else {
		files, _ := fs.Glob(shipped.Files(), "general/references/*.md")
		indexed := len(files)
		for _, one := range spec.Skills {
			named, _ := skill.ReferenceFiles(shipped.Files(), one.Name, one.References)
			files = append(files, slices.DeleteFunc(named, func(file string) bool { return slices.Contains(files, file) })...)
		}
		references, err := libraryReferences(shipped.Files(), files)
		if err != nil {
			return Composed{}, err
		}
		composed.References = references
		composed.Parts = append(composed.Parts, PromptPart{Concern: rule.ConcernIdentity, Text: referenceIndex(references[:indexed])})
		composed.Unsent = referenceParts(subagent.Definition{References: references[:indexed]})
	}
	if composed.Task.Language = spec.Agent.Language; composed.Task.Language != "" && !rule.KnownLanguage(composed.Task.Language) {
		return Composed{}, fmt.Errorf("the sub-agent %s declares the language %q, which no rule knows", spec.Agent.Name, composed.Task.Language)
	}
	for _, builtin := range []PromptPart{
		{Concern: rule.ConcernEnvironment, Text: spec.Environment},
		{Concern: rule.ConcernToolGuidance, Text: spec.ToolGuidance},
		{Concern: rule.ConcernFormatContract, Text: TheFormatContract},
	} {
		if strings.TrimSpace(builtin.Text) == "" {
			return Composed{}, fmt.Errorf("the composed prompt carries nothing for %s, which tofu writes itself and is never conditional", builtin.Concern)
		}
		composed.Parts = append(composed.Parts, builtin)
	}
	agentDomain := cmp.Or(spec.Agent.Domain, rule.DomainDev)
	if asked := rule.AskedOfTheTask(spec.Rules, agentDomain, composed.Task); spec.Applies != nil && len(asked) > 0 {
		var keywordDecided error
		text := "Jev read which of the framework's rules reach this task"
		if composed.Task.Judged, keywordDecided = spec.Applies.Of(composed.Task, asked); keywordDecided != nil {
			text = "the touches keyword read which of the framework's rules reach this task, since " + keywordDecided.Error()
		}
		composed.Parts = append(composed.Parts, PromptPart{Concern: rule.ConcernTaskShaping, Text: text, ByTask: true})
	}
	withoutTask := rule.IndexReaching(spec.Rules, agentDomain, rule.Task{Role: composed.Task.Role, Language: composed.Task.Language, Frameworks: composed.Task.Frameworks})
	var firedByKind []string
	for i, match := range rule.IndexReaching(spec.Rules, agentDomain, composed.Task) {
		if !match.Fires {
			held := spec.Rules[i]
			composed.HeldBack = append(composed.HeldBack, match)
			composed.Unsent = append(composed.Unsent, PromptPart{Concern: held.Concern, RuleID: held.ID, File: filepath.ToSlash(held.File), Why: match.Why, Reason: match.Reason, Mode: held.Mode, State: session.PartHeld})
			continue
		}
		fired := spec.Rules[i]
		if kind := fired.Trigger.Kind(); kind != rule.VerbNone && kind == composed.Task.Verb {
			firedByKind = append(firedByKind, fired.ID)
		}
		text := cmp.Or(strings.TrimSpace(fired.Text), strings.TrimSpace(fired.Notes))
		if text == "" {
			return Composed{}, fmt.Errorf("rule %s fires, %s, and carries no text the prompt can send: %s",
				fired.ID, match.Why, filepath.ToSlash(fired.File))
		}
		composed.Parts = append(composed.Parts, PromptPart{
			Concern: fired.Concern,
			RuleID:  fired.ID,
			File:    filepath.ToSlash(fired.File),
			Text:    text,
			Why:     match.Why,
			Reason:  match.Reason,
			Mode:    fired.Mode,
			ByTask:  !withoutTask[i].Fires,
		})
	}
	if spec.Kinds != nil {
		text := "tofu reads this task as " + cmp.Or(string(composed.Task.Verb), "no kind") + ", as " + composed.Task.KindBy + " reads it"
		if wordsDecided != nil {
			text += ", since " + wordsDecided.Error()
		}
		if len(firedByKind) > 0 {
			text += ", so the rules for that kind fire: " + strings.Join(firedByKind, ", ")
		}
		composed.Parts = append(composed.Parts, PromptPart{Concern: rule.ConcernTaskShaping, Text: text, ByTask: true})
	}
	composed.Unsent = append(composed.Unsent, spec.Dropped...)
	for _, off := range spec.SwitchedOff {
		text := "the person switched this rule off on purpose, so it does not apply here"
		if off.Rule.Override.Reason != "" {
			text += ": " + off.Rule.Override.Reason
		}
		composed.Parts = append(composed.Parts, PromptPart{Concern: off.Base.Concern, RuleID: off.Base.ID, File: filepath.ToSlash(off.Rule.File), Text: text, Reason: rule.ReasonSetting, Mode: off.Rule.Mode, Off: true})
	}
	order := composedOrder()
	slices.SortStableFunc(composed.Parts, func(a, b PromptPart) int {
		return slices.Index(order, a.Concern) - slices.Index(order, b.Concern)
	})
	skillTool := len(spec.Agent.Tools) == 0 || slices.Contains(spec.Agent.Tools, SkillToolName)
	composed.Unsent = slices.Concat(composed.Unsent, skillParts(spec.Skills, skillTool, loaded), unofferedLibrarySkills(shipped.Files(), spec.Skills, loaded))
	if skillTool {
		if listing := skill.Listing(spec.Skills, spec.WindowTokens); listing != "" {
			composed.Parts = append(composed.Parts, PromptPart{Concern: ConcernSkills, Text: listing})
		}
		return composed, nil
	}
	if len(spec.Skills) == 0 {
		return composed, nil
	}
	text := "you have no " + SkillToolName + " tool and load no skill yourself. the skills loaded for you above: " + cmp.Or(strings.Join(spec.Agent.Skills, ", "), "none")
	if named := skillsNamedIn(spec.Task, spec.Skills, spec.Agent.Skills); len(named) > 0 {
		text += ". your brief names " + strings.Join(named, ", ") + ", which you cannot load: work without it and say so in your report"
	}
	composed.Parts = append(composed.Parts, PromptPart{Concern: ConcernSkills, Text: text})
	return composed, nil
}

func skillsNamedIn(task string, skills []skill.Skill, loaded []string) []string {
	var named []string
	for _, one := range skills {
		word := regexp.MustCompile(`(^|[^\w-])` + regexp.QuoteMeta(one.Name) + `($|[^\w-])`)
		if !slices.Contains(loaded, one.Name) && word.MatchString(task) {
			named = append(named, one.Name)
		}
	}
	return named
}

func (c Composed) System() string {
	return c.render(func(PromptPart) bool { return true })
}

func (c Composed) Head() string {
	return c.render(func(part PromptPart) bool { return !part.ByTask })
}

func (c Composed) WithTaskRules(environment string) string {
	return strings.TrimSpace(environment + "\n\n" + c.render(func(part PromptPart) bool { return part.ByTask }))
}

func (c Composed) render(keep func(PromptPart) bool) string {
	var block strings.Builder
	for _, part := range c.Parts {
		if part.Concern == rule.ConcernEnvironment || part.State != session.PartFired || !keep(part) {
			continue
		}
		if block.Len() > 0 {
			block.WriteString("\n\n")
		}
		block.WriteString(part.rendered())
	}
	return block.String()
}

func (p PromptPart) rendered() string {
	return fmt.Sprintf("[%s, from %s]\n%s", p.Concern, p.Rule(), p.Text)
}

func TaskNamed(text string) rule.Task {
	task := rule.Task{Text: text}
	for _, word := range strings.Fields(text) {
		word = strings.Trim(word, ",;:()[]{}\"'`")
		if strings.Contains(word, "://") {
			continue
		}
		if strings.ContainsAny(word, `/\`) || namedExtension(word) {
			task.Paths = append(task.Paths, filepath.ToSlash(word))
			continue
		}
		if verb := verbNamed(word); verb == rule.VerbPlan || task.Verb == rule.VerbNone {
			task.Verb = verb
		}
	}
	return task
}

func namedExtension(word string) bool {
	extension := strings.TrimPrefix(path.Ext(word), ".")
	if len(extension) < 2 {
		return false
	}
	for _, letter := range extension {
		if !unicode.IsLower(letter) {
			return false
		}
	}
	return true
}

func verbNamed(word string) rule.Verb {
	named := rule.Verb(strings.ToLower(word))
	if slices.Contains([]rule.Verb{rule.VerbPlan, "ticket", "tickets", "epic", "epics"}, named) {
		return rule.VerbPlan
	}
	if slices.Contains([]rule.Verb{rule.VerbDebug, rule.VerbExplore, rule.VerbReview, rule.VerbWrite}, named) {
		return named
	}
	return rule.VerbNone
}

const (
	taskKindRef     = "task_kind@1"
	taskKindPoint   = "task_kind"
	taskKindName    = "kind"
	taskKindBuilder = "task_kind_state@1"
)

const (
	ruleAppliesRef     = "rule_applies@1"
	ruleAppliesPoint   = "rule_applies"
	ruleAppliesName    = "applies"
	ruleAppliesBuilder = "rule_applies_state@1"
)

type pointJudge struct {
	methods   method.Table
	asked     question.Question
	version   int
	threshold float64
	client    *jev.Client
	ledger    *ledger.Writer
	unasked   error
}

type KindJudge struct{ pointJudge }

type RuleJudge struct{ pointJudge }

func NewKindJudge(project string) *KindJudge {
	judge := &KindJudge{}
	judge.unasked = judge.open(project, taskKindRef, taskKindName, question.KindChoice)
	return judge
}

func NewRuleJudge(project string) *RuleJudge {
	judge := &RuleJudge{}
	judge.unasked = judge.open(project, ruleAppliesRef, ruleAppliesName, question.KindNoul)
	return judge
}

func (j *pointJudge) open(project, ref, name string, kind question.Kind) error {
	var err error
	if j.methods, err = method.Load(shipped.Files()); err != nil {
		return err
	}
	layers, err := question.Layers(questions.Files(), project)
	if err != nil {
		return err
	}
	set, fields, err := question.Resolve(ref, layers)
	if err != nil {
		return err
	}
	asked, found := set.Question(name)
	at := slices.IndexFunc(fields, func(f question.Field) bool { return f.Path == "thresholds."+name+"_at" })
	if !found || asked.Kind != kind || at < 0 {
		return fmt.Errorf("%s names no %s question %s or no thresholds.%s_at", ref, kind, name, name)
	}
	if j.threshold, err = strconv.ParseFloat(fields[at].Value, 64); err != nil {
		return err
	}
	places, err := sys.ResolvePlaces(project)
	if err != nil {
		return err
	}
	j.asked, j.version, j.ledger = asked, set.QuestionsVersion, ledger.NewWriter(places.Log)
	j.client, err = classifier.Open(project, jev.ReadKeyring(), 1)
	return err
}

func (j *KindJudge) Of(task string, named rule.Verb) (rule.Verb, error) {
	if strings.TrimSpace(task) == "" {
		return named, errors.New("the task is blank, so jev is not asked")
	}
	kind, chosen, err := method.Run(context.Background(), j.methods, taskKindPoint, method.Arms[rule.Verb]{
		Cheap:  func(context.Context) (rule.Verb, error) { return named, nil },
		Judged: func(ctx context.Context) (rule.Verb, error) { return j.ask(ctx, task) },
	})
	switch {
	case err != nil:
		return named, err
	case chosen == method.Cheap:
		return named, fmt.Errorf("%s names %s for %s", j.methods.File, chosen, taskKindPoint)
	}
	return kind, nil
}

func (j *KindJudge) ask(ctx context.Context, task string) (rule.Verb, error) {
	if j.unasked != nil {
		return rule.VerbNone, fmt.Errorf("jev cannot be asked: %w", j.unasked)
	}
	body, err := json.Marshal(struct {
		Task string `json:"task"`
	}{task})
	if err != nil {
		return rule.VerbNone, err
	}
	asked := []jev.Question{j.asked.ToJev()}
	row := ledger.Row{Point: taskKindPoint, Questions: taskKindPoint, Version: j.version, StateHash: ledger.HashOf(body), StateBuilder: taskKindBuilder, State: body,
		Verdict: ledger.VerdictAsk, Reason: &ledger.Reason{Question: taskKindName, Comparison: ">= " + strconv.FormatFloat(j.threshold, 'f', -1, 64), Threshold: j.threshold, Mode: ledger.ModeEnforced}}
	decision, err := j.client.Ask(ctx, jev.Request{State: json.RawMessage(body), Questions: asked})
	if err != nil {
		failed := "jev could not answer, so the word list names the task's kind: " + err.Error()
		row.Reason.ModeReason = &failed
		_, unwritten := j.ledger.Append(row)
		return rule.VerbNone, errors.Join(fmt.Errorf("jev could not answer: %w", err), unwritten)
	}
	decision.Record(&row, asked, j.version)
	answer := decision.Answers[taskKindName]
	if row.Reason.Value = answer.Probabilities[answer.Choice]; row.Reason.Value >= j.threshold {
		row.Verdict = ledger.VerdictAllow
	}
	if _, err := j.ledger.Append(row); err != nil {
		return rule.VerbNone, fmt.Errorf("the decision did not reach the ledger: %w", err)
	}
	if row.Verdict != ledger.VerdictAllow {
		return rule.VerbNone, fmt.Errorf("jev chose %s at %.2f, under kind_at %.2f", answer.Choice, row.Reason.Value, j.threshold)
	}
	if answer.Choice == j.asked.Escape {
		return rule.VerbNone, nil
	}
	return rule.Verb(answer.Choice), nil
}

func (j *RuleJudge) Of(task rule.Task, asked []rule.Rule) (map[string]rule.Judged, error) {
	if strings.TrimSpace(task.Text) == "" {
		return nil, errors.New("the task is blank, so jev is not asked")
	}
	judged, chosen, err := method.Run(context.Background(), j.methods, ruleAppliesPoint, method.Arms[map[string]rule.Judged]{
		Cheap:  func(context.Context) (map[string]rule.Judged, error) { return nil, nil },
		Judged: func(ctx context.Context) (map[string]rule.Judged, error) { return j.ask(ctx, task, asked) },
	})
	if err == nil && chosen == method.Cheap {
		err = fmt.Errorf("%s names %s for %s", j.methods.File, chosen, ruleAppliesPoint)
	}
	return judged, err
}

func (j *RuleJudge) ask(ctx context.Context, task rule.Task, asked []rule.Rule) (map[string]rule.Judged, error) {
	if j.unasked != nil {
		return nil, fmt.Errorf("jev cannot be asked: %w", j.unasked)
	}
	type ruleState struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	state := struct {
		Task  string      `json:"task"`
		Paths []string    `json:"paths"`
		Rules []ruleState `json:"rules"`
	}{Task: task.Text, Paths: append([]string{}, task.Paths...)}
	rubric := j.asked.ToJev()
	var each []jev.Question
	for i, r := range asked {
		state.Rules = append(state.Rules, ruleState{ID: r.ID, Text: cmp.Or(strings.TrimSpace(r.Text), strings.TrimSpace(r.Notes))})
		each = append(each, jev.Question{ID: ruleAppliesName + "_" + strconv.Itoa(i), Kind: jev.QuestionNoul, Instructions: rubric.Instructions + " The rule is rules[" + strconv.Itoa(i) + "].", True: rubric.True, False: rubric.False})
	}
	body, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	row := ledger.Row{Point: ruleAppliesPoint, Questions: ruleAppliesPoint, Version: j.version, StateHash: ledger.HashOf(body), StateBuilder: ruleAppliesBuilder, State: body,
		Verdict: ledger.VerdictAllow, Reason: &ledger.Reason{Question: ruleAppliesName, Comparison: ">= " + strconv.FormatFloat(j.threshold, 'f', -1, 64), Threshold: j.threshold, Mode: ledger.ModeEnforced}}
	decision, err := j.client.Ask(ctx, jev.Request{State: json.RawMessage(body), Questions: each})
	if err != nil {
		failed := "jev could not answer, so the touches keyword decides which rules reach the task: " + err.Error()
		row.Verdict, row.Reason.ModeReason = ledger.VerdictAsk, &failed
		_, unwritten := j.ledger.Append(row)
		return nil, errors.Join(fmt.Errorf("jev could not answer: %w", err), unwritten)
	}
	decision.Record(&row, each, j.version)
	if row, err = j.ledger.Append(row); err != nil {
		return nil, fmt.Errorf("the decision did not reach the ledger: %w", err)
	}
	judged := map[string]rule.Judged{}
	for i, r := range asked {
		applies := row.Answers[i].Noul
		judged[r.ID] = rule.Judged{Applies: applies >= j.threshold,
			Why: fmt.Sprintf("Jev decides rather than the touches, and it answered applies %.2f against applies_at %.2f in ledger row %s", applies, j.threshold, row.ID)}
	}
	return judged, nil
}

const ReferenceToolName = "reference"

type referenceTool struct {
	held map[string]string
}

func (referenceTool) Name() string { return ReferenceToolName }

func (referenceTool) Definition() llm.Tool {
	return llm.Tool{
		Name:        ReferenceToolName,
		Description: "returns one of your references whole, named as your instructions list it, such as go-verify",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
			"required":   []string{"name"},
		},
	}
}

func (r referenceTool) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("reference: arguments are not the expected shape: %w", err)
	}
	text, held := r.held[args.Name]
	if !held {
		return Result{}, fmt.Errorf("reference: no reference is named %q; the references are %s", args.Name, strings.Join(slices.Sorted(maps.Keys(r.held)), ", "))
	}
	return Result{Content: text, Command: ReferenceToolName + " " + args.Name, Origin: llm.Origin{Source: llm.SourceReference, Name: args.Name}}, nil
}
