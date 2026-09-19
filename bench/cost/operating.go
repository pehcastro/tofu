package cost

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"boji/internal/judge/policy"
)

const calibrationFilePath = "bench/cost/calibration/tool_gate@1.json"

//go:embed calibration/tool_gate@1.json
var calibrationFile []byte

type Calibration struct {
	Policy           string         `json:"policy"`
	PolicyVersion    int            `json:"policy_version"`
	Questions        string         `json:"questions"`
	QuestionsVersion int            `json:"questions_version"`
	Build            string         `json:"build"`
	Mode             policy.Mode    `json:"mode"`
	SampleFloor      int            `json:"sample_floor"`
	NFit             int            `json:"n_fit"`
	NVerify          int            `json:"n_verify"`
	Point            OperatingPoint `json:"thresholds"`
	Rule             string         `json:"rule"`
	Provenance       string         `json:"provenance"`
}

func GateCalibration() (Calibration, policy.Resolution, error) {
	var calibration Calibration
	if err := json.Unmarshal(calibrationFile, &calibration); err != nil {
		return Calibration{}, policy.Resolution{}, fmt.Errorf("%s: %w", calibrationFilePath, err)
	}
	if calibration.Mode != policy.ModeShadow && calibration.Mode != policy.ModeEnforced {
		return Calibration{}, policy.Resolution{}, fmt.Errorf("%s: mode is %q or %q, found %q", calibrationFilePath, policy.ModeShadow, policy.ModeEnforced, calibration.Mode)
	}
	pol := policy.Policy{
		Name:             calibration.Policy,
		PolicyVersion:    calibration.PolicyVersion,
		Questions:        calibration.Questions,
		QuestionsVersion: calibration.QuestionsVersion,
		Mode:             calibration.Mode,
		ModeDeclared:     true,
		SampleFloor:      calibration.SampleFloor,
		File:             calibrationFilePath,
	}
	lock := policy.Lock{
		Policy:           calibration.Policy,
		PolicyVersion:    calibration.PolicyVersion,
		Questions:        calibration.Questions,
		QuestionsVersion: calibration.QuestionsVersion,
		Build:            calibration.Build,
		NFit:             calibration.NFit,
		NVerify:          calibration.NVerify,
		File:             calibrationFilePath,
	}
	current := policy.Current{Build: calibration.Build, QuestionsVersion: calibration.QuestionsVersion, Known: true}
	return calibration, policy.Resolve(pol, policy.LockLookup{Present: true, Lock: lock}, current), nil
}
