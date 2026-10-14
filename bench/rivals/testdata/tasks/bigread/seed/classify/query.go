package classify

import (
	"net/url"

	"example.com/atlas/intake"
)

func FromQuery(q url.Values) intake.Class {
	switch q.Get("priority") {
	case "high":
		return intake.ClassBulk
	case "rush":
		return intake.ClassUrgent
	}
	return intake.ClassStandard
}
