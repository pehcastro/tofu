package dispatch

import "example.com/atlas/intake"

const deadLetter = "orders-dead"

type Fixed string

func (f Fixed) Pick(intake.Order) string {
	return string(f)
}

type ByRegion map[string]string

func (r ByRegion) Pick(o intake.Order) string {
	if queue, found := r[o.Region]; found {
		return queue
	}
	return deadLetter
}
