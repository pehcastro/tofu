package boardy

import (
	"cmp"
	"fmt"
	"slices"
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

type Author struct {
	Kind    AuthorKind `json:"kind"`
	Name    string     `json:"name"`
	Session string     `json:"session,omitempty"`
	Trace   string     `json:"trace,omitempty"`
}

const (
	OrchestratorName  = "orchestrator"
	serveActor        = "tofu serve"
	claudeCode        = "claude-code"
	claudeCodeSession = "#"
	personName        = "you"
	leadTraceAgent    = "lead"
	shortSessionChars = 8
)

var tofuSessionID = sys.LazyRegexp(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func ClaudeCodeActor(session, agent string) string {
	actor := claudeCode
	if session != "" {
		actor += claudeCodeSession + session
	}
	if agent != "" {
		actor += subAgentOfSession + agent
	}
	return actor
}

func LeadAuthor(session string) *Author {
	return &Author{Kind: AuthorLead, Name: OrchestratorName + "-" + shortSession(session), Session: session, Trace: sessionTrace(session, leadTraceAgent)}
}

func SubAgentAuthor(session, agent string) *Author {
	return &Author{Kind: AuthorSubAgent, Name: agent, Session: session, Trace: sessionTrace(session, agent)}
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
	session, agent, bySubAgent := strings.Cut(actor, subAgentOfSession)
	switch {
	case actor == string(RolePerson):
		return &Author{Kind: AuthorPerson, Name: personName}
	case strings.HasPrefix(actor, hookActorPrefix):
		return &Author{Kind: AuthorHook, Name: actor, Trace: "tofu boardy rule show " + strings.TrimPrefix(actor, hookActorPrefix)}
	case actor == serveActor:
		return &Author{Kind: AuthorHook, Name: actor}
	case strings.HasPrefix(actor, claudeCode):
		return claudeCodeAuthor(strings.TrimPrefix(strings.TrimPrefix(session, claudeCode), claudeCodeSession), agent)
	case bySubAgent:
		return SubAgentAuthor(session, s.spawnedAs(agent))
	case tofuSessionID().MatchString(actor):
		return LeadAuthor(actor)
	case actor == OrchestratorName:
		return &Author{Kind: AuthorLead, Name: actor}
	}
	return &Author{Kind: AuthorSubAgent, Name: actor}
}

func claudeCodeAuthor(session, agent string) *Author {
	author := &Author{Kind: AuthorClaudeCode, Name: claudeCode, Session: session}
	if session != "" {
		author.Name, author.Trace = claudeCode+claudeCodeSession+shortSession(session), "claude --resume "+session
	}
	if agent != "" {
		author.Kind, author.Name = AuthorSubAgent, agent
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
