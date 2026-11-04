package wiring

import (
	"cmp"
	"encoding/json"
	"io/fs"
	"os"

	"tofu/internal/agent/loop"
	"tofu/internal/librarian/rules"
	platformfs "tofu/internal/platform/fs"
	"tofu/internal/store/session"
	shipped "tofu/library"
)

const (
	LibraryWhatRule      = "rule"
	LibraryWhatAgent     = "agent"
	LibraryWhatSkill     = "skill"
	LibraryWhatReference = "reference"
)

func LibrarySource() (fs.FS, string, string) {
	dir, err := platformfs.LibraryDir()
	if err != nil {
		return shipped.Files(), LibraryRoot, RulesFromTheBinary
	}
	isDir, err := platformfs.IsDir(dir)
	if err != nil || !isDir {
		return shipped.Files(), LibraryRoot, RulesFromTheBinary
	}
	return os.DirFS(dir), dir, RulesFromTheProject
}

func LibraryItem(part session.PromptPart) (string, string) {
	switch {
	case part.Rule != "":
		return LibraryWhatRule, part.Rule
	case part.Kind == string(rules.ConcernIdentity) && (part.Agent != "" || part.File != ""):
		return LibraryWhatAgent, cmp.Or(part.Agent, "unnamed")
	case part.Kind == string(loop.ConcernSkills) && part.Name != "":
		return LibraryWhatSkill, part.Name
	case part.Kind == string(loop.ConcernReferences):
		return LibraryWhatReference, part.Name
	}
	return "", ""
}

func LibraryLoad(call session.CallBody) (string, string) {
	var named struct{ Name string }
	what := map[string]string{loop.SkillToolName: LibraryWhatSkill, loop.ReferenceToolName: LibraryWhatReference}[call.Tool]
	if what == "" || json.Unmarshal(call.Args, &named) != nil {
		return "", ""
	}
	return what, named.Name
}
