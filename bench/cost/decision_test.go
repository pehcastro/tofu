package cost

import (
	"os"
	"strings"
	"testing"

	"boji/internal/judge/policy"
)

func publishedPoint(t *testing.T) OperatingPoint {
	t.Helper()
	calibration, _, err := GateCalibration()
	if err != nil {
		t.Fatalf("reading the operating point: %v", err)
	}
	return calibration.Point
}

func TestTheSupersededArmProceedsWhenUserRequestedIsHighEvenIfApprovalIsHigh(t *testing.T) {
	if got := DecideByApproval(0.90, 0.94, publishedPoint(t)); got != Proceed {
		t.Fatalf("got %s, want proceed, the requested override should win", got)
	}
}

func TestTheSupersededArmBlocksWhenApprovalIsHighAndTheUserDidNotAsk(t *testing.T) {
	if got := DecideByApproval(0.02, 0.95, publishedPoint(t)); got != Block {
		t.Fatalf("got %s, want block", got)
	}
}

func TestTheSupersededArmProceedsWhenNeitherSignalFires(t *testing.T) {
	if got := DecideByApproval(0.17, 0.09, publishedPoint(t)); got != Proceed {
		t.Fatalf("got %s, want proceed", got)
	}
}

func TestNeitherTheSupersededArmNorTheGateCarriesAThresholdInItsOwnSource(t *testing.T) {
	for _, file := range []string{"decision.go", "gate.go"} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		for _, literal := range []string{"0.5", "0.75", "0.8", "1.5", "2.5", "const decisionThreshold"} {
			if strings.Contains(string(source), literal) {
				t.Fatalf("%s carries %q, so an operating point is in code again", file, literal)
			}
		}
	}
}

func TestTheSupersededPointStillReadsFromTheCalibrationFileTheSweepRunsOver(t *testing.T) {
	calibration, _, err := GateCalibration()
	if err != nil {
		t.Fatalf("reading the operating point: %v", err)
	}
	if calibration.Point.ApprovalBlockAt != 0.5 {
		t.Fatalf("the file pins approval_block_at %.2f, and the published run decided at 0.50", calibration.Point.ApprovalBlockAt)
	}
	if got := DecideByApproval(0.04, 0.60, calibration.Point); got != Block {
		t.Fatalf("at the file's point a 0.60 approval gives %s, want block", got)
	}
	if got := DecideByApproval(0.04, 0.60, OperatingPoint{UserRequestedOverrideAt: 0.5, ApprovalBlockAt: 0.80}); got != Proceed {
		t.Fatalf("at a 0.80 point the same answers give %s, want proceed, so the verdict follows the data", got)
	}
}

func TestThePointTheArmsRanAtIsUncalibratedAndSoResolvesToShadow(t *testing.T) {
	calibration, resolution, err := GateCalibration()
	if err != nil {
		t.Fatalf("reading the operating point: %v", err)
	}
	if calibration.Mode != policy.ModeEnforced {
		t.Fatalf("the file declares %s, and the arms did decide at this point", calibration.Mode)
	}
	if resolution.Mode != policy.ModeShadow {
		t.Fatalf("mode = %s, want shadow: nothing fitted this point", resolution.Mode)
	}
	if !strings.Contains(resolution.Reason, "below the floor") {
		t.Fatalf("reason = %q, want the sample floor named", resolution.Reason)
	}
}
