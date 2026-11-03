package calibration

import "tofu/internal/konst"

const ReachMultiple = 3

const ReachReason = "One dead band (0.06) is already the width decide.go treats as a tie, so a label sitting exactly there says nothing about which side of the line is right. Three dead bands (0.18) is the smallest window that also sees the slope on both sides of a threshold, matching the design doc's own phrase: within a few dead bands of the candidate threshold."

func ReachBand() float64 {
	return ReachMultiple * konst.ThresholdDeadBand
}

func Near(value, threshold float64) bool {
	distance := value - threshold
	if distance < 0 {
		distance = -distance
	}
	return distance <= ReachBand()
}
