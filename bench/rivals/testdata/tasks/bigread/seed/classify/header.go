package classify

import (
	"strings"

	"example.com/atlas/intake"
)

func FromHeader(value string) intake.Class {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "high", "urgent":
		return intake.ClassUrgent
	case "low", "bulk":
		return intake.ClassBulk
	}
	return intake.ClassStandard
}
