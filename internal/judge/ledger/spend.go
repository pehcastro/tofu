package ledger

import (
	"fmt"
	"strings"
)

type Money float64

type ListPrice float64

type Unit int

const (
	UnitMoney Unit = iota
	UnitListPrice
	UnitUnpriced
	UnitUndetermined
)

func Units() []Unit {
	return []Unit{UnitMoney, UnitListPrice, UnitUnpriced, UnitUndetermined}
}

func (u Unit) String() string {
	switch u {
	case UnitMoney:
		return "money"
	case UnitListPrice:
		return "list price"
	case UnitUnpriced:
		return "unpriced"
	case UnitUndetermined:
		return "undetermined"
	}
	panic("ledger: unknown spend unit")
}

const UnitsDoNotAdd = "Money and list price do not add. Money left an account. A list price is what a rate card says the call would have cost, and nobody was charged it. An unpriced call spent subscription quota, which carries no price at all."

type Spend struct {
	Money         Money
	List          ListPrice
	UnpricedCalls int
}

func (s Spend) Plus(other Spend) Spend {
	return Spend{
		Money:         s.Money + other.Money,
		List:          s.List + other.List,
		UnpricedCalls: s.UnpricedCalls + other.UnpricedCalls,
	}
}

func PoolUnitsLine(units []Unit) string {
	if len(units) == 0 {
		return "spend units: none, no credential"
	}
	counts := make(map[Unit]int, len(units))
	for _, unit := range units {
		counts[unit]++
	}
	parts := make([]string, 0, len(counts))
	for _, unit := range Units() {
		if counts[unit] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[unit], unit))
		}
	}
	line := "spend units: " + strings.Join(parts, ", ")
	if counts[UnitMoney] > 0 && counts[UnitMoney] < len(units) {
		return line + ". Warning: this pool mixes a metered credential with one that spends quota, so one comparison across it would add dollars to something that is not dollars. " + UnitsDoNotAdd
	}
	return line
}
