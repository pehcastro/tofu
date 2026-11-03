package wiring

import (
	"cmp"
	"encoding/json"
	"io/fs"
	"os"

	"tofu/internal/agent/loop"
	"tofu/internal/librarian/rules"
	"tofu/internal/store/session"
	"tofu/internal/sys"
	shipped "tofu/library"
)

const (
	LibraryWhatRule      = "rule"
	LibraryWhatAgent     = "agent"
	LibraryWhatSkill     = "skill"
	LibraryWhatReference = "reference"
)

func LibrarySource() (fs.FS, string, string) {
	dir, err := sys.LibraryDir()
	if err != nil {
		return shipped.Files(), LibraryRoot, RulesFromTheBinary
	}
	isDir, err := sys.IsDir(dir)
	if err != nil || !isDir {
		return shipped.Files(), LibraryRoot, RulesFromTheBinary
	}
	return os.DirFS(dir), dir, RulesFromTheProject
}

func LibraryItem(part session.PromptPart) (string, string) {
	switch {
	case part.Rule != "":
		return LibraryWhatRule, part.Rule
	case part.Kind == string(rule.ConcernIdentity) && (part.Agent != "" || part.File != ""):
		return LibraryWhatAgent, cmp.Or(part.Agent, "unnamed")
	case part.Kind == string(turn.ConcernSkills) && part.Name != "":
		return LibraryWhatSkill, part.Name
	case part.Kind == string(turn.ConcernReferences):
		return LibraryWhatReference, part.Name
	}
	return "", ""
}

func LibraryLoad(call session.CallBody) (string, string) {
	var named struct{ Name string }
	what := map[string]string{turn.SkillToolName: LibraryWhatSkill, turn.ReferenceToolName: LibraryWhatReference}[call.Tool]
	if what == "" || json.Unmarshal(call.Args, &named) != nil {
		return "", ""
	}
	return what, named.Name
}
