package host

import (
	"encoding/json"

	"tofu/internal/sys"
)

const scratchPrefix = "scratch."

type ScratchCleanParams struct {
	Session string `json:"session,omitempty"`
	Cache   bool   `json:"cache,omitempty"`
	DryRun  bool   `json:"dryRun,omitempty"`
}

type ScratchCleaned struct {
	Root    string              `json:"root"`
	DryRun  bool                `json:"dry_run"`
	Removed []sys.ScratchFolder `json:"removed"`
}

func scratchRequests() []method {
	return []method{
		{name: scratchPrefix + "list", params: NoParams{}, result: sys.ScratchReport{}},
		{name: scratchPrefix + "clean", params: ScratchCleanParams{}, result: ScratchCleaned{}},
	}
}

func (s *server) scratch(method string, raw json.RawMessage) (any, error) {
	switch method {
	case scratchPrefix + "list":
		return handle(raw, func(NoParams) (any, error) { return sys.ReadScratch(s.Dir) })
	case scratchPrefix + "clean":
		return handle(raw, s.scratchClean)
	}
	return nil, &Refusal{Code: CodeNoMethod, Message: "no method " + method}
}

func (s *server) scratchClean(p ScratchCleanParams) (any, error) {
	args := verbLine([]string{"scratch", "clean"}, map[string]bool{"--cache": p.Cache, "--dry-run": p.DryRun}, map[string]string{"--session": p.Session})
	cleaned, err := verbAs[ScratchCleaned](s, args...)
	if err == nil && !cleaned.DryRun {
		if report, readErr := sys.ReadScratch(s.Dir); readErr == nil {
			s.box.push(notify(scratchPrefix+"changed", report))
		}
	}
	return cleaned, err
}
