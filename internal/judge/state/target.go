package state

import (
	"path"
	"strings"
)

type TargetClass string

const (
	ClassSourceCode  TargetClass = "source_code"
	ClassTicket      TargetClass = "ticket"
	ClassDocument    TargetClass = "document"
	ClassLibraryData TargetClass = "library_data"
	ClassConfig      TargetClass = "config"
	ClassUnknown     TargetClass = "unknown_class"
)

type TargetLocation string

const (
	LocationSessionScratch TargetLocation = "session_scratch"
	LocationInsideProject  TargetLocation = "inside_project"
	LocationOutsideProject TargetLocation = "outside_project"
	LocationUnknown        TargetLocation = "unknown_location"
)

type Determination string

const (
	TargetsResolved Determination = "targets_resolved"
	TargetUnknown   Determination = "writes_but_target_unknown"
	NoWriteFound    Determination = "no_write_recognised"
)

type WriteTarget struct {
	Path     string         `json:"path"`
	Class    TargetClass    `json:"class"`
	Location TargetLocation `json:"location"`
}

type WriteTargets struct {
	Determination Determination `json:"determination"`
	Targets       []WriteTarget `json:"targets"`
}

type roots struct {
	Cwd     string
	Project string
	Scratch string
}

func nothingFound(determination Determination) WriteTargets {
	return WriteTargets{Determination: determination, Targets: []WriteTarget{}}
}

func TargetsOf(in ToolGateInput) WriteTargets {
	where := roots{Cwd: in.Cwd, Project: in.ProjectDir, Scratch: in.ScratchDir}
	switch strings.ToLower(in.Tool) {
	case "read", "grep", "glob", "ls", "webfetch", "websearch", "todowrite", "task":
		return nothingFound(NoWriteFound)
	case "write", "edit", "notebookedit":
		named, ok := in.Input["path"].(string)
		if !ok {
			named, ok = in.Input["file_path"].(string)
		}
		if !ok || strings.TrimSpace(named) == "" {
			return nothingFound(TargetUnknown)
		}
		return WriteTargets{Determination: TargetsResolved, Targets: []WriteTarget{describe(resolve(where.Cwd, named), where)}}
	case "bash", "powershell", "shell":
		command, ok := in.Input["command"].(string)
		if !ok {
			return nothingFound(TargetUnknown)
		}
		found := scanCommand(command, where)
		if in.InputTruncated {
			return WriteTargets{Determination: TargetUnknown, Targets: found.Targets}
		}
		return found
	}
	return nothingFound(TargetUnknown)
}

func resolve(base, target string) string {
	slashed := strings.ReplaceAll(target, "\\", "/")
	if isAbsolute(slashed) || base == "" {
		return path.Clean(slashed)
	}
	return path.Clean(strings.TrimSuffix(strings.ReplaceAll(base, "\\", "/"), "/") + "/" + slashed)
}

func isAbsolute(slashed string) bool {
	if strings.HasPrefix(slashed, "/") {
		return true
	}
	return len(slashed) > 2 && slashed[1] == ':' && slashed[2] == '/'
}

func describe(full string, where roots) WriteTarget {
	return WriteTarget{Path: full, Class: classOf(full), Location: locationOf(full, where)}
}

func classOf(full string) TargetClass {
	parts := strings.Split(strings.ToLower(full), "/")
	for _, part := range parts[:len(parts)-1] {
		switch part {
		case "library":
			return ClassLibraryData
		case "tickets":
			return ClassTicket
		}
	}
	base := parts[len(parts)-1]
	dot := strings.LastIndex(base, ".")
	if dot <= 0 {
		if strings.HasPrefix(base, ".") {
			return ClassConfig
		}
		return ClassUnknown
	}
	switch base[dot+1:] {
	case "go", "py", "js", "mjs", "ts", "tsx", "jsx", "rs", "c", "h", "cc", "cpp", "java", "rb", "sh", "ps1", "bat", "sql":
		return ClassSourceCode
	case "md", "txt", "rst", "adoc":
		return ClassDocument
	case "json", "yaml", "yml", "toml", "ini", "cfg", "conf", "env", "lock", "properties":
		return ClassConfig
	}
	if strings.HasPrefix(base, ".") {
		return ClassConfig
	}
	return ClassUnknown
}

func locationOf(full string, where roots) TargetLocation {
	switch {
	case under(full, where.Scratch):
		return LocationSessionScratch
	case under(full, where.Project):
		return LocationInsideProject
	case where.Project == "":
		return LocationUnknown
	}
	return LocationOutsideProject
}

func under(full, root string) bool {
	if root == "" {
		return false
	}
	lowered := strings.ToLower(full)
	folder := slashedLower(root)
	return lowered == folder || strings.HasPrefix(lowered, strings.TrimSuffix(folder, "/")+"/")
}
