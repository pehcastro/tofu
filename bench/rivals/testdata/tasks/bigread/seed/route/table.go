package route

type Table struct {
	Standard string
	Urgent   string
	Bulk     string
}

func Defaults() Table {
	return Table{Standard: queueOrders, Urgent: queueExpress, Bulk: queueBatch}
}

func (t Table) WithPrefix(prefix string) Table {
	return Table{Standard: prefix + t.Standard, Urgent: prefix + t.Urgent, Bulk: prefix + t.Bulk}
}
