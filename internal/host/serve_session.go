package host

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/shell"
	"tofu/internal/sys"
	"tofu/internal/turn/tools"
)

const (
	attachedImageMode = 0o644
	attachedDirMode   = 0o755
	attachedImageName = "%s-image-%02d%s"
)

func (s *server) sessions(p SessionListParams) (any, error) {
	list, _, err := s.listing()
	if err != nil {
		return nil, err
	}
	switch p.Kind {
	case "", KindMain, KindSide:
	default:
		return nil, &Refusal{Code: CodeBadParams, Message: "kind " + p.Kind + " is neither " + KindMain + " nor " + KindSide}
	}
	search := strings.ToLower(p.Search)
	list.Sessions = slices.DeleteFunc(list.Sessions, func(row SessionRow) bool {
		return !strings.Contains(strings.ToLower(strings.Join([]string{row.ID, row.Name, row.Handle, row.Task}, "\n")), search) || p.Kind != "" && row.Kind != p.Kind
	})
	if p.Limit > 0 {
		list.Sessions = list.Sessions[:min(p.Limit, len(list.Sessions))]
	}
	return list, nil
}

func (s *server) branch(p SessionBranchParams) (any, error) {
	switch {
	case s.Branch == nil:
		return nil, &Refusal{Code: CodeRefused, Message: "this tofu branches no sessions"}
	case p.Kind != KindSide:
		return nil, &Refusal{Code: CodeBadParams, Message: "kind " + p.Kind + " is not built: only " + KindSide + " is, and a full branch for the Forks screen is not"}
	}
	branched, err := s.Branch(p)
	if err != nil || branched.Effort == "" {
		return branched, err
	}
	store, err := session.OpenIn(s.Host.dir)
	if err == nil {
		_, err = store.SetEffort(branched.Session, string(branched.Effort))
	}
	return branched, err
}

func (s *server) access(p SessionAccessParams) (any, error) {
	if s.Access == nil {
		return nil, &Refusal{Code: CodeRefused, Message: "this tofu changes no side chat's access"}
	}
	for _, l := range s.tracked() {
		if turn, running := l.host.Turn(); running && p.Session == l.host.ID() {
			return nil, &Refusal{Code: CodeRefused, Message: "turn " + turn + " is running in this side chat: change its access when it ends"}
		}
	}
	return s.Access(p)
}

func (s *server) listing() (SessionList, []*lane, error) {
	if s.Sessions == nil {
		return SessionList{}, nil, &Refusal{Code: CodeRefused, Message: "this tofu lists no sessions"}
	}
	lanes, focus := s.tracked(), s.focus()
	list, err := s.Sessions(focus.host.ID())
	ids, families := make([]string, len(lanes)), make([]string, len(lanes))
	store, storeErr := session.OpenIn(s.Host.dir)
	for at, l := range lanes {
		ids[at] = l.host.ID()
		if storeErr != nil {
			continue
		}
		if family, err := store.Identity(ids[at]); err == nil {
			families[at] = family.Family
		}
	}
	owners := make([]*lane, len(list.Sessions))
	for index, row := range list.Sessions {
		running := row.Open && focus.running()
		for at, l := range lanes {
			if row.ID == ids[at] || families[at] != "" && row.Family == families[at] {
				owners[index], running = l, l.running()
			}
		}
		list.Sessions[index].Open, list.Sessions[index].Running, list.Sessions[index].Wire = row.Open || owners[index] != nil, running, s.sourceOf(row.Wire)
	}
	return list, owners, err
}

func (s *server) listed(l *lane) {
	list, owners, err := s.listing()
	at := slices.Index(owners, l)
	if err != nil || at < 0 {
		return
	}
	row := list.Sessions[at]
	s.mu.Lock()
	id := l.items.identity("", row.ID)
	s.mu.Unlock()
	s.box.push(merged("session.listed", row.ID, &SessionListed{Identity: id, SessionRow: row}))
}

func (s *server) state() SessionState {
	focus := s.focus()
	turn, running := focus.host.Turn()
	asking, pick := focus.host.Settings()
	state := SessionState{Session: focus.host.ID(), Running: running, Asking: asking, Pick: s.shown(pick), Standing: focus.host.Standing(), Shells: s.shellsNow(), Cron: focus.host.cronState()}
	s.mu.Lock()
	defer s.mu.Unlock()
	if running && focus.items.turn == turn {
		state.Turn = &RunningTurn{ID: turn, Task: focus.items.task, StartedAt: focus.items.began}
	}
	state.Context = focus.items.context
	state.Approvals = slices.AppendSeq([]ApprovalRequest{}, maps.Values(focus.pending))
	slices.SortFunc(state.Approvals, func(a, b ApprovalRequest) int { return strings.Compare(a.Approval, b.Approval) })
	state.Questions = slices.AppendSeq([]QuestionRequest{}, maps.Values(focus.asked))
	slices.SortFunc(state.Questions, func(a, b QuestionRequest) int { return strings.Compare(a.Question, b.Question) })
	state.Agents = []AgentNow{}
	for _, row := range focus.items.agents {
		state.Agents = append(state.Agents, AgentNow{Instance: row.Name, Kind: row.Agent, Task: row.Doing, Owns: row.Owns, Model: row.Model,
			State: AgentState(row.State.String()), Steps: row.Steps, Tokens: row.Tokens, StartedAt: row.Started})
	}
	slices.SortFunc(state.Agents, func(a, b AgentNow) int {
		return cmp.Or(a.StartedAt.Compare(b.StartedAt), strings.Compare(a.Instance, b.Instance))
	})
	return state
}

func (s *server) shellsNow() []ShellNow {
	now := []ShellNow{}
	if s.Shells == nil {
		return now
	}
	found, _ := s.Shells.List()
	mask := sys.LoadKeyRedactor().Redact
	for _, one := range slices.DeleteFunc(found, shell.Shell.OneShot) {
		now = append(now, ShellNow{Shell: one.Name, Command: mask(one.Command), PID: one.PID, State: ShellState(one.State), StartedAt: one.Started, ExitCode: one.ExitCode, EndedAt: one.Ended,
			Kept: ShellKept(one.Kept), Dir: one.Dir, Port: one.Port, Ready: ShellReady(one.Ready), LeftOver: one.LeftOver(), Ref: tools.QuoteRef(one.Call)})
	}
	return now
}

func (s *server) set(p SessionSetParams) (any, error) {
	focus := s.focus()
	asking, picked := focus.host.Settings()
	pick, err := s.chosen(picked, p.ModelPick)
	if err != nil {
		return nil, err
	}
	if err := checkAsking(p.Asking); err != nil {
		return nil, err
	}
	asking = cmp.Or(p.Asking, asking)
	focus.host.SetAsking(asking)
	focus.host.Choose(pick)
	s.mu.Lock()
	id := focus.items.identity("", "settings")
	s.mu.Unlock()
	s.box.push(merged("session.settings", "", &SessionSettings{Identity: id, Asking: asking, Pick: s.shown(pick)}))
	return Ack{OK: true}, nil
}

func (s *server) shown(pick Pick) ModelPick {
	return ModelPick{Wire: s.sourceOf(pick.Wire), Model: pick.Model, Effort: pick.Effort}
}

func (s *server) sourceOf(wire string) string { return cmp.Or(s.Sources[wire], wire) }

func (s *server) wireOf(source string) (string, bool) {
	for wire, named := range s.Sources {
		if named == source {
			return wire, true
		}
	}
	return "", false
}

func checkAsking(asking AskingMode) error {
	switch asking {
	case AskingAsk, AskingAuto, "":
		return nil
	}
	return &Refusal{Code: CodeBadParams, Message: "asking is ask or auto, not " + strconv.Quote(string(asking))}
}

func (s *server) chosen(pick Pick, asked ModelPick) (Pick, error) {
	if asked.Wire != "" {
		var signedIn []string
		if s.Wires != nil {
			signedIn = s.Wires()
		}
		wire, known := s.wireOf(asked.Wire)
		if !known || !slices.Contains(signedIn, wire) {
			sources := make([]string, len(signedIn))
			for index, held := range signedIn {
				sources[index] = s.sourceOf(held)
			}
			return pick, &Refusal{Code: CodeBadParams, Message: "no account here is signed in on " + strconv.Quote(asked.Wire) + "; signed in: " + cmp.Or(strings.Join(sources, ", "), "none")}
		}
		if wire != pick.Wire {
			pick.Model = ""
		}
		asked.Wire = wire
	}
	if asked.Effort != "" {
		if _, err := llm.ParseEffort(string(asked.Effort)); err != nil {
			return pick, &Refusal{Code: CodeBadParams, Message: err.Error()}
		}
	}
	return Pick{Wire: cmp.Or(asked.Wire, pick.Wire), Model: cmp.Or(asked.Model, pick.Model), Effort: cmp.Or(asked.Effort, pick.Effort)}, nil
}

func attach(h *Host, images []AttachedImage) (string, error) {
	if len(images) == 0 {
		return "", nil
	}
	bodies := make([][]byte, len(images))
	for index, image := range images {
		if _, known := imageMediaTypes()[strings.ToLower(filepath.Ext(image.Path))]; !known {
			return "", &Refusal{Code: CodeBadParams, Message: image.Path + " is not an image tofu can attach: png, jpg, gif or webp"}
		}
		body, err := os.ReadFile(image.Path)
		if err != nil {
			return "", &Refusal{Code: CodeBadParams, Message: err.Error()}
		}
		if len(body) > sys.ClipboardMaxBytes {
			return "", &Refusal{Code: CodeBadParams, Message: image.Path + " is past the attachment ceiling of " + strconv.Itoa(sys.ClipboardMaxBytes) + " bytes"}
		}
		bodies[index] = body
	}
	dir, err := h.AttachmentDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, attachedDirMode); err != nil {
		return "", err
	}
	held, _ := os.ReadDir(dir)
	var tokens string
	for index, image := range images {
		suffix := strings.ToLower(filepath.Ext(image.Path))
		name := fmt.Sprintf(attachedImageName, filepath.Base(dir), len(held)+index+1, suffix)
		if err := sys.WriteFile(filepath.Join(dir, name), bodies[index], attachedImageMode); err != nil {
			return "", err
		}
		h.Attached(index+1, name, len(bodies[index]), strings.ToUpper(strings.TrimPrefix(suffix, ".")))
		tokens += " " + ImageToken(index+1)
	}
	return tokens, nil
}
