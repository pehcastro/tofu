package boardy

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/sys"
)

type Role string

const (
	RolePerson  Role = "person"
	RoleManager Role = "manager"
	RoleAgent   Role = "agent"
)

type Managed struct {
	Local
	Person string
	Lead   string
}

type RefusedError struct {
	Ticket string
	Why    string
}

func (e RefusedError) Error() string {
	if e.Ticket == "" {
		return "refused: " + e.Why
	}
	return fmt.Sprintf("%s refused: %s", e.Ticket, e.Why)
}

type WaitingError struct {
	Ticket string
	Rule   string
	Detail string
}

func (e WaitingError) Error() string {
	return fmt.Sprintf("%s waits for %s: %s (rule %s)", e.Ticket, RolePerson, e.Detail, e.Rule)
}

const subAgentOfSession = "/"

type AuthorKind string

const (
	AuthorPerson     AuthorKind = "person"
	AuthorClaudeCode AuthorKind = "claude-code"
	AuthorLead       AuthorKind = "lead"
	AuthorSubAgent   AuthorKind = "sub-agent"
	AuthorHook       AuthorKind = "hook"
)

func (AuthorKind) Enum() []string {
	return enum(AuthorPerson, AuthorClaudeCode, AuthorLead, AuthorSubAgent, AuthorHook)
}

type AuthorRole string

const (
	AsPM       AuthorRole = "pm"
	AsEngineer AuthorRole = "engineer"
	AsPerson   AuthorRole = "person"
	AsHook     AuthorRole = "hook"
)

func (AuthorRole) Enum() []string { return enum(AsPM, AsEngineer, AsPerson, AsHook) }

type Harness string

const (
	HarnessTofu       Harness = "tofu"
	HarnessClaudeCode Harness = "claude-code"
	HarnessCodex      Harness = "codex"
	HarnessOther      Harness = "other"
)

func (Harness) Enum() []string {
	return enum(HarnessTofu, HarnessClaudeCode, HarnessCodex, HarnessOther)
}

type Author struct {
	Kind    AuthorKind `json:"kind"`
	Role    AuthorRole `json:"role,omitempty"`
	Name    string     `json:"name"`
	Harness Harness    `json:"harness,omitempty"`
	Model   string     `json:"model,omitempty"`
	Agent   string     `json:"agent,omitempty"`
	Number  int        `json:"number,omitempty"`
	Session string     `json:"session,omitempty"`
	Handle  string     `json:"handle,omitempty"`
	Trace   string     `json:"trace,omitempty"`
}

const (
	OrchestratorName  = "orchestrator"
	serveActor        = "tofu serve"
	harnessSession    = "#"
	personName        = "you"
	leadTraceAgent    = "lead"
	shortSessionChars = 8
)

var tofuSessionID = sys.LazyRegexp(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func HarnessActor(harness Harness, session, agent string) string {
	actor := string(harness)
	if session != "" {
		actor += harnessSession + session
	}
	if agent != "" {
		actor += subAgentOfSession + agent
	}
	return actor
}

func LeadAuthor(session, handle, name, model string) *Author {
	return &Author{Kind: AuthorLead, Role: AsPM, Name: cmp.Or(name, handle, OrchestratorName+"-"+shortSession(session)), Harness: HarnessTofu, Model: model,
		Session: session, Handle: handle, Trace: sessionTrace(session, leadTraceAgent)}
}

func EngineerAuthor(session, handle, id, model string) *Author {
	agent := AgentTypeOf(id)
	number, _ := strconv.Atoi(strings.TrimPrefix(id, agent+"-"))
	return &Author{Kind: AuthorSubAgent, Role: AsEngineer, Name: id, Harness: HarnessTofu, Model: model, Agent: agent, Number: number,
		Session: session, Handle: handle, Trace: sessionTrace(session, id)}
}

func SubAgentAuthor(session, agent string) *Author {
	return &Author{Kind: AuthorSubAgent, Role: AsEngineer, Name: agent, Harness: HarnessTofu, Session: session, Trace: sessionTrace(session, agent)}
}

func AgentTypeOf(name string) string {
	at := strings.LastIndex(name, "-")
	if _, err := strconv.Atoi(name[at+1:]); at <= 0 || err != nil {
		return name
	}
	return name[:at]
}

func sessionTrace(session, agent string) string {
	if session == "" {
		return ""
	}
	return "tofu session trace " + session + " --agent " + agent
}

func shortSession(session string) string {
	return session[:min(len(session), shortSessionChars)]
}

func (s Store) authorOf(actor string) *Author {
	if author, spoken := s.Speakers[actor]; spoken {
		return &author
	}
	session, agent, bySubAgent := strings.Cut(actor, subAgentOfSession)
	ranIn := func(harness Harness) bool {
		return session == string(harness) || strings.HasPrefix(session, string(harness)+harnessSession)
	}
	switch {
	case actor == string(RolePerson):
		return &Author{Kind: AuthorPerson, Role: AsPerson, Name: personName}
	case strings.HasPrefix(actor, hookActorPrefix):
		return &Author{Kind: AuthorHook, Role: AsHook, Name: actor, Trace: "tofu boardy rule show " + strings.TrimPrefix(actor, hookActorPrefix)}
	case actor == serveActor:
		return &Author{Kind: AuthorHook, Role: AsHook, Name: actor}
	case ranIn(HarnessClaudeCode):
		return harnessAuthor(HarnessClaudeCode, session, agent, "claude --resume ")
	case ranIn(HarnessCodex):
		return harnessAuthor(HarnessCodex, session, agent, "codex resume ")
	case bySubAgent:
		return EngineerAuthor(session, "", s.spawnedAs(agent), "")
	case tofuSessionID().MatchString(actor):
		return LeadAuthor(actor, "", "", "")
	case actor == OrchestratorName:
		return &Author{Kind: AuthorLead, Role: AsPM, Name: actor, Harness: HarnessOther}
	}
	return &Author{Kind: AuthorSubAgent, Role: AsEngineer, Name: actor, Harness: HarnessOther, Agent: AgentTypeOf(actor)}
}

func harnessAuthor(harness Harness, actorSession, agent, resume string) *Author {
	session := strings.TrimPrefix(strings.TrimPrefix(actorSession, string(harness)), harnessSession)
	author := &Author{Kind: AuthorLead, Role: AsPM, Name: string(harness), Harness: harness, Session: session, Handle: session}
	if harness == HarnessClaudeCode {
		author.Kind = AuthorClaudeCode
	}
	if session != "" {
		author.Name, author.Trace = string(harness)+harnessSession+shortSession(session), resume+session
	}
	if agent != "" {
		author.Kind, author.Role, author.Name, author.Agent = AuthorSubAgent, AsEngineer, agent, AgentTypeOf(agent)
	}
	return author
}

func (s Store) spawnedAs(ticketOrAgent string) string {
	thread, _ := s.Thread(ticketOrAgent)
	for _, event := range slices.Backward(thread) {
		if event.Kind == Spawned && event.Agent != "" {
			return event.Agent
		}
	}
	return ticketOrAgent
}

func heldBy(ticket Ticket, actor string) bool {
	_, agent, bySubAgent := strings.Cut(actor, subAgentOfSession)
	return ticket.Assignee == actor || bySubAgent && (ticket.Assignee == agent || agent == ticket.ID)
}

func (m Managed) actsForPerson(actor string) bool {
	if m.Person == "" {
		return actor != "" && !strings.Contains(actor, subAgentOfSession)
	}
	return actor == m.Person
}

func (m Managed) role(board Board, actor string) Role {
	forPerson := m.actsForPerson(actor)
	switch {
	case slices.Contains(board.Managers, actor):
		return RoleManager
	case (forPerson || actor != "" && actor == m.Lead) && (len(board.Managers) == 0 || slices.Contains(board.Managers, cmp.Or(m.Person, string(RolePerson)))):
		return RoleManager
	case forPerson:
		return RolePerson
	}
	return RoleAgent
}

func (m Managed) Manages(key, actor, doing string) error {
	board, err := m.Store.Board(key)
	if err != nil {
		return err
	}
	if m.role(board, actor) != RoleManager {
		return RefusedError{Why: fmt.Sprintf("%s is not a manager of %s, and only a manager may %s", actor, key, doing)}
	}
	return nil
}
