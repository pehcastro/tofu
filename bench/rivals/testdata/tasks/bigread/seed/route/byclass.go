package route

import "example.com/atlas/intake"

type byClass struct {
	table Table
}

func ByClass(t Table) byClass {
	return byClass{table: t}
}

func (b byClass) Pick(o intake.Order) string {
	switch o.Class {
	case intake.ClassUrgent:
		return b.table.Urgent
	case intake.ClassBulk:
		return b.table.Bulk
	}
	return b.table.Standard
}
