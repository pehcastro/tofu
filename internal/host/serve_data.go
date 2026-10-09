package host

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/learn"
	"tofu/internal/memory"
	"tofu/internal/memtree"
	roster "tofu/internal/subagent"
)

func verbAs[T any](s *server, args ...string) (T, error) {
	var typed T
	result, err := s.Verb(args)
	if err != nil {
		return typed, err
	}
	if !result.OK && (len(result.Data) == 0 || string(result.Data) == "null") {
		what := []string{"tofu " + strings.Join(args, " ") + " failed"}
		for _, problem := range result.Problems {
			what = append(what, strings.TrimSpace(problem.What+" "+problem.Hint))
		}
		return typed, &Refusal{Code: CodeRefused, Message: strings.Join(what, ": "), Data: result.Problems}
	}
	decoder := json.NewDecoder(bytes.NewReader(result.Data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&typed); err != nil {
		return typed, &Refusal{Code: CodeRefused, Message: "tofu " + strings.Join(args, " ") + " printed a report this wire does not name: " + err.Error()}
	}
	return typed, nil
}

func verbLine(words []string, switches map[string]bool, options map[string]string, operands ...string) []string {
	for _, flag := range slices.Sorted(maps.Keys(switches)) {
		if switches[flag] {
			words = append(words, flag)
		}
	}
	for _, flag := range slices.Sorted(maps.Keys(options)) {
		if options[flag] != "" {
			words = append(words, flag, options[flag])
		}
	}
	return append(words, slices.DeleteFunc(operands, func(operand string) bool { return operand == "" })...)
}

func typedVerb[T any](s *server, raw json.RawMessage, args ...string) (any, error) {
	return handle(raw, func(NoParams) (any, error) { return verbAs[T](s, args...) })
}

func (s *server) data(method string, raw json.RawMessage) (any, error) {
	switch method {
	case queryPrefix + "models":
		return handle(raw, func(NoParams) (any, error) {
			report, err := verbAs[ModelsQuery](s, "models")
			if s.Wires != nil {
				report.Wires = s.Wires()
			}
			report.Stale = s.Stale != nil && s.Stale()
			return report, err
		})
	case queryPrefix + "settings":
		return typedVerb[SettingsReport](s, raw, "settings")
	case queryPrefix + "rules":
		return handle(raw, func(NoParams) (any, error) {
			report, err := verbAs[RuleListReport](s, "rules", "list")
			if err != nil {
				return nil, err
			}
			return s.withRuleText(report)
		})
	case queryPrefix + "session":
		return handle(raw, s.sessionDetail)
	case queryPrefix + "usage.history":
		return handle(raw, s.usageHistory)
	case queryPrefix + "limits":
		return handle(raw, s.limits)
	case queryPrefix + "skills":
		return handle(raw, s.skills)
	case queryPrefix + "ledger.summary":
		return handle(raw, s.ledgerSummary)
	case queryPrefix + "agents":
		return typedVerb[roster.Found](s, raw, "agents")
	case queryPrefix + "library":
		return typedVerb[LibraryReport](s, raw, "library")
	case queryPrefix + "memory":
		return typedVerb[MemoryReport](s, raw, "memory", "list")
	case queryPrefix + "hooks":
		return typedVerb[HooksReport](s, raw, "hooks")
	case queryPrefix + "changelog":
		return typedVerb[ChangelogReport](s, raw, "changelog")
	case queryPrefix + "update":
		return typedVerb[UpdateReport](s, raw, "update", "--check")
	case queryPrefix + "docs":
		return handle(raw, s.docs)
	case "reload":
		return typedVerb[ReloadDiff](s, raw, "reload")
	case "models.reload":
		return typedVerb[ModelReload](s, raw, "models", "reload")
	case "hooks.trust":
		return typedVerb[HooksTrusted](s, raw, "hooks", "trust")
	case "learn.scan":
		return typedVerb[learn.Run](s, raw, "learn", "scan")
	case "learn.show":
		return handle(raw, func(p LearnParams) (any, error) { return verbAs[learn.Finding](s, "learn", "show", strconv.Itoa(p.ID)) })
	case "learn.reject":
		return handle(raw, func(p LearnParams) (any, error) {
			return verbAs[learn.Finding](s, "learn", "reject", strconv.Itoa(p.ID), "--reason", p.Reason)
		})
	case "memory.view":
		return handle(raw, func(p MemoryViewParams) (any, error) { return s.memoryRead(p.Scope, nil) })
	case "memory.zoom":
		return handle(raw, func(p MemoryZoomParams) (any, error) {
			return s.memoryRead(p.Store, func(store *memtree.Store) ([]string, error) { return store.Zoom(p.ID, p.N) })
		})
	case "memory.recall":
		return handle(raw, func(p MemoryRecallParams) (any, error) {
			return s.memoryRead(p.Store, func(store *memtree.Store) ([]string, error) { return store.Recall(p.Regex) })
		})
	case "memory.add":
		return handle(raw, s.memoryAdd)
	case "memory.edit":
		return handle(raw, s.memoryEdit)
	case "memory.remove":
		return handle(raw, func(p MemoryRemoveParams) (any, error) {
			shelves, err := s.shelves(p.Scope)
			if err != nil {
				return nil, err
			}
			return shelves.Remove(p.Scope, p.ID)
		})
	case "setup.check":
		return handle(raw, func(NoParams) (any, error) {
			if s.Setup == nil {
				return nil, &Refusal{Code: CodeRefused, Message: "this tofu checks no setup"}
			}
			return Setup{Steps: append([]Requirement{}, s.Setup()...)}, nil
		})
	case "login.key":
		return handle(raw, func(p LoginKeyParams) (any, error) {
			if s.SaveKey == nil {
				return nil, &Refusal{Code: CodeRefused, Message: "this tofu stores no keys"}
			}
			note, err := s.SaveKey(p.Provider, p.Key)
			return LoginNote{Note: note}, err
		})
	case "login.logout":
		return handle(raw, func(p LogoutParams) (any, error) {
			if s.Logout == nil {
				return nil, &Refusal{Code: CodeRefused, Message: "this tofu signs nobody out"}
			}
			note, err := s.Logout(p)
			return LoginNote{Note: note}, err
		})
	case queryPrefix + "usage":
		return handle(raw, func(NoParams) (any, error) { return s.usageNow() })
	case queryPrefix + "doctor":
		return typedVerb[DoctorReport](s, raw, "doctor")
	case queryPrefix + "context":
		return handle(raw, func(p SessionParams) (any, error) {
			report, err := verbAs[ContextReport](s, verbLine([]string{"context"}, nil, nil, p.Session)...)
			if err != nil {
				return nil, err
			}
			return s.withItems(report)
		})
	case "session.info":
		return handle(raw, func(p SessionParams) (any, error) { return verbAs[SessionInfo](s, "session", "info", p.Session) })
	case "mention.resolve":
		return handle(raw, s.resolveMention)
	case "session.trace":
		return handle(raw, func(p SessionParams) (any, error) { return verbAs[SessionTrace](s, "session", "trace", p.Session) })
	case queryPrefix + "accounts":
		return typedVerb[Accounts](s, raw, "login", "--status")
	case "session.find":
		return handle(raw, func(p SessionFindParams) (any, error) {
			options := map[string]string{"--tool": p.Tool, "--command": p.Command, "--file": p.File, "--text": p.Text, "--agent": p.Agent, "--since": p.Since, "--until": p.Until}
			return verbAs[SessionFind](s, verbLine([]string{"session", "find"}, nil, options, p.Session)...)
		})
	case "rules.add", "rules.off", "rules.remove", "rules.restore":
		return handle(raw, func(p RuleWriteParams) (any, error) {
			switches, options := map[string]bool{"--global": p.Global, "--replace": p.Replace}, map[string]string{"--reason": p.Reason, "--concern": p.Concern}
			return verbAs[WriteReceipt](s, verbLine([]string{"rules", strings.TrimPrefix(method, "rules.")}, switches, options, p.ID, p.Text)...)
		})
	case "agents.add", "agents.set", "agents.remove":
		return handle(raw, func(p AgentWriteParams) (any, error) {
			verb, options, operands := strings.TrimPrefix(method, "agents."), map[string]string{"--description": p.Description, "--tools": p.Tools}, []string{p.Name}
			if verb == "set" {
				operands = append(operands, p.Model)
			} else {
				options["--model"] = p.Model
			}
			return verbAs[WriteReceipt](s, verbLine([]string{"agents", verb}, map[string]bool{"--global": p.Global}, options, operands...)...)
		})
	case "learn.apply":
		return handle(raw, func(p LearnApplyParams) (any, error) {
			return verbAs[WriteReceipt](s, verbLine([]string{"learn", "apply"}, map[string]bool{"--project": p.Project}, nil, strconv.Itoa(p.ID))...)
		})
	}
	return nil, &Refusal{Code: CodeNoMethod, Message: "tofu.host/1 has no method " + strconv.Quote(method)}
}

func (s *server) usageNow() (UsageAnswer, error) {
	report, err := verbAs[UsageReport](s, "usage")
	now := time.Now()
	answer := UsageAnswer{UsageReport: report, ReadAt: now}
	for _, provider := range report.Providers {
		if !provider.ReadAt.IsZero() && provider.ReadAt.Before(answer.ReadAt) {
			answer.ReadAt = provider.ReadAt
		}
	}
	answer.AgeMs = now.Sub(answer.ReadAt).Milliseconds()
	return answer, err
}

func (s *server) docs(p DocsParams) (any, error) {
	if p.Topic == "" {
		index, err := verbAs[DocsCorpus](s, "docs")
		return DocsAnswer{DocsCorpus: index}, err
	}
	if strings.ContainsAny(p.Topic, " \t") {
		return nil, &Refusal{Code: CodeBadParams, Message: "a topic is one word, as query.docs lists it"}
	}
	page, err := verbAs[DocsTopic](s, "docs", p.Topic)
	return DocsAnswer{Page: &page}, err
}

func (s *server) shelves(scope memory.Scope) (memory.Memory, error) {
	if scope == "" {
		return memory.Memory{}, &Refusal{Code: CodeBadParams, Message: "which scope? " + strings.Join(scopeNames(), ", ")}
	}
	return memory.Open(s.Dir)
}

func scopeNames() []string {
	return []string{string(memory.UserLocal), string(memory.ProjectLocal), string(memory.Project), string(memory.Global)}
}

func (s *server) memoryAdd(p MemoryAddParams) (any, error) {
	shelves, err := s.shelves(p.Scope)
	if err != nil {
		return nil, err
	}
	kind := p.Kind
	switch {
	case kind != "":
	case p.Scope == memory.Global || p.Scope == memory.UserLocal:
		kind = memory.KindPerson
	default:
		kind = memory.KindProject
	}
	added, err := shelves.Add(memory.Entry{Scope: p.Scope, Kind: kind, Text: p.Text, Said: p.Text, Session: s.Host.ID(), At: time.Now(), By: memory.ByPerson}, "")
	if err != nil {
		return nil, err
	}
	s.Host.Remembered(added.Saved())
	return added, nil
}

func (s *server) memoryEdit(p MemoryEditParams) (any, error) {
	shelves, err := s.shelves(p.Scope)
	if err != nil {
		return nil, err
	}
	old, err := shelves.Find(p.Scope, p.ID)
	if err != nil {
		return nil, err
	}
	old.Text = strings.TrimSpace(p.Text)
	changed, err := shelves.Add(old, old.ID)
	if err != nil {
		return nil, err
	}
	s.Host.Remembered(changed.Saved())
	return changed, nil
}
