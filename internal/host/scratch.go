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

func scratchRequests() []method {
	return []method{
		{name: scratchPrefix + "list", params: NoParams{}, result: sys.ScratchReport{}},
		{name: scratchPrefix + "clean", params: ScratchCleanParams{}, result: VerbResult{}},
	}
}

func (s *server) scratch(method string, raw json.RawMessage) (any, error) {
	switch method {
	case scratchPrefix + "list":
		return handle(raw, func(NoParams) (any, error) { return s.scratchReport() })
	case scratchPrefix + "clean":
		return handle(raw, s.scratchClean)
	}
	return nil, &Refusal{Code: CodeNoMethod, Message: "no method " + method}
}

func (s *server) scratchReport() (sys.ScratchReport, error) {
	report, err := sys.ReadScratch(s.Dir)
	if report.Folders == nil {
		report.Folders = []sys.ScratchFolder{}
	}
	return report, err
}

func (s *server) scratchClean(p ScratchCleanParams) (any, error) {
	args := []string{"scratch", "clean"}
	if p.Session != "" {
		args = append(args, "--session", p.Session)
	}
	if p.Cache {
		args = append(args, "--cache")
	}
	if p.DryRun {
		args = append(args, "--dry-run")
	}
	result, err := s.Verb(args)
	if err == nil && !p.DryRun {
		if report, readErr := s.scratchReport(); readErr == nil {
			s.box.push(notify(scratchPrefix+"changed", report))
		}
	}
	return result, err
}
