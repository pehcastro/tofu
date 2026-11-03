package route

import "example.com/atlas/intake"

type legacy struct{}

func Legacy() legacy {
	return legacy{}
}

func (legacy) Pick(o intake.Order) string {
	switch o.Class {
	case intake.ClassUrgent:
		return queuePriority
	case intake.ClassBulk:
		return queueBatch
	}
	return queueQuotes
}
